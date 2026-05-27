package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"relay-gateway/internal/admin"
	"relay-gateway/internal/config"
	"relay-gateway/internal/configsync"
	"relay-gateway/internal/core"
	"relay-gateway/internal/protocol/anthropic"
	"relay-gateway/internal/protocol/openai"
	"relay-gateway/internal/routing"
)

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
	GetStation(ctx context.Context, stationID int64) (core.Station, error)
	UpdateStation(ctx context.Context, station core.Station) error
	DeleteStation(ctx context.Context, stationID int64) error
	ListStations(ctx context.Context) ([]core.Station, error)
	ListMappings(ctx context.Context) ([]core.ModelMapping, error)
	UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error
	GetMapping(ctx context.Context, mappingID int64) (core.ModelMapping, error)
	UpdateModelMapping(ctx context.Context, mapping core.ModelMapping) error
	DeleteModelMapping(ctx context.Context, mappingID int64) error
	ExportConfigSnapshot(ctx context.Context) (configsync.Snapshot, error)
	ApplyConfigSnapshot(ctx context.Context, snapshot configsync.Snapshot) (configsync.ApplyResult, error)
	FindMappings(ctx context.Context, protocol core.Protocol, alias string) ([]core.ModelMapping, error)
	ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error)
	SaveStationStatus(ctx context.Context, status core.StationStatus) error
	ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error)
	InsertRequestLog(ctx context.Context, entry core.RequestLog) error
	ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error)
	InsertFailoverEvent(ctx context.Context, event core.FailoverEvent) error
	InsertUpstreamErrorLog(ctx context.Context, entry core.UpstreamErrorLog) error
	ListUpstreamErrorLogs(ctx context.Context, limit int) ([]core.UpstreamErrorLog, error)
	ListRecentUpstreamErrorLogsByStation(ctx context.Context, limitPerStation int) (map[string][]core.UpstreamErrorLog, error)
	UsageByStation(ctx context.Context) ([]core.UsageRow, error)
	UsageByAlias(ctx context.Context) ([]core.UsageRow, error)
	DailyTokenUsage(ctx context.Context, limit int) ([]core.DailyTokenUsageRow, error)
}

type Server struct {
	cfg       config.Runtime
	setupMode bool
	store     store
	selector  *routing.Selector
	client    *http.Client
}

type requestBuilder func(target core.ResolvedTarget, normalized core.NormalizedRequest) (*http.Request, error)

const upstreamErrorBodyLogLimit = 10 * 1024

type Options struct {
	Runtime         config.Runtime
	AdminWriteToken string
	SetupMode       bool
	RuntimeFilePath string
	RuntimeWarning  string
}

func NewServer(cfg config.Runtime, store store, selector *routing.Selector) http.Handler {
	return NewServerWithOptions(Options{Runtime: cfg}, store, selector)
}

func NewServerWithOptions(options Options, store store, selector *routing.Selector) http.Handler {
	server := &Server{
		cfg:       options.Runtime,
		setupMode: options.SetupMode,
		store:     store,
		selector:  selector,
		client: &http.Client{
			Transport: cloneDefaultTransport(),
		},
	}
	adminWriteToken := options.AdminWriteToken
	if adminWriteToken == "" {
		adminWriteToken = deriveAdminWriteToken(options.Runtime.LocalAPIKey)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/openai/v1/responses", server.handleResponses)
	mux.HandleFunc("/openai/v1/chat/completions", server.handleChatCompletions)
	mux.HandleFunc("/anthropic/v1/messages", server.handleAnthropicMessages)
	adminHandler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      options.Runtime.ListenAddr,
		RuntimeFilePath: options.RuntimeFilePath,
		RuntimeWarning:  options.RuntimeWarning,
		SetupMode:       options.SetupMode,
	})
	if err != nil {
		panic(err)
	}
	mux.Handle("/admin", adminHandler)
	mux.Handle("/admin/", adminHandler)
	return mux
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	s.handleNormalizedRequest(w, r, core.ProtocolOpenAI, openai.NormalizeResponses, openai.BuildResponsesRequest)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleNormalizedRequest(w, r, core.ProtocolOpenAI, openai.NormalizeChatCompletions, openai.BuildChatCompletionsRequest)
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	s.handleNormalizedRequest(w, r, core.ProtocolAnthropic, anthropic.NormalizeMessages, anthropic.BuildMessagesRequest)
}

func (s *Server) handleNormalizedRequest(
	w http.ResponseWriter,
	r *http.Request,
	protocol core.Protocol,
	normalize func(*http.Request) (core.NormalizedRequest, error),
	build requestBuilder,
) {
	if s.setupMode {
		http.Error(w, "setup required: open /admin/setup to configure runtime before using relay endpoints", http.StatusServiceUnavailable)
		return
	}

	if !s.authorize(r, protocol) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	normalized, err := normalize(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.proxyNormalizedRequest(w, r, normalized, build)
}

func (s *Server) authorize(r *http.Request, protocol core.Protocol) bool {
	switch protocol {
	case core.ProtocolAnthropic:
		return r.Header.Get("x-api-key") == s.cfg.LocalAPIKey ||
			r.Header.Get("Authorization") == "Bearer "+s.cfg.LocalAPIKey
	default:
		return r.Header.Get("Authorization") == "Bearer "+s.cfg.LocalAPIKey
	}
}

func (s *Server) proxyNormalizedRequest(
	w http.ResponseWriter,
	r *http.Request,
	normalized core.NormalizedRequest,
	build requestBuilder,
) {
	ctx := r.Context()
	startedAt := time.Now().UTC()
	stations, err := s.store.ListStations(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mappings, err := s.store.FindMappings(ctx, normalized.Protocol, normalized.Alias)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	statuses, err := s.store.ListStationStatuses(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	targets, err := s.selector.Candidates(normalized, stations, mappings, statuses)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Failover is allowed only until the gateway commits headers to the client.
	// Once response bytes have started, the stream belongs to that upstream.
	var previous *core.ResolvedTarget
	var attempted *core.ResolvedTarget
	var didFailover bool
	var errorKind string
	var attemptFailures []string
	for _, target := range targets {
		attempted = &target
		if previous != nil {
			didFailover = true
			_ = s.store.InsertFailoverEvent(ctx, core.FailoverEvent{
				Protocol:        normalized.Protocol,
				Alias:           normalized.Alias,
				FromStationName: previous.Station.Name,
				ToStationName:   target.Station.Name,
				Reason:          errorKind,
				CreatedAt:       time.Now().UTC(),
			})
		}

		upstreamReq, err := build(target, normalized)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := s.client.Do(upstreamReq.WithContext(ctx))
		if err != nil && errors.Is(err, context.Canceled) {
			return
		}
		var captured *capturedUpstreamError
		if err == nil && resp != nil && resp.StatusCode >= http.StatusBadRequest {
			var captureErr error
			captured, captureErr = captureUpstreamError(resp)
			if captureErr != nil {
				err = captureErr
			} else if captured != nil {
				logKind := upstreamHTTPErrorKind(resp, captured.Body)
				_ = s.store.InsertUpstreamErrorLog(ctx, upstreamErrorLog(normalized, target.Station.Name, resp.StatusCode, logKind, captured))
			}
		}
		if shouldFailover(err, resp, captured) {
			errorKind = failureKind(err, resp, captured)
			attemptFailures = append(attemptFailures, attemptFailureMessage(target.Station.Name, err, resp, errorKind))
			status := s.selector.RecordFailure(target.Station, statuses[target.Station.ID], failureMessage(err, resp))
			statuses[target.Station.ID] = status
			_ = s.store.SaveStationStatus(ctx, status)
			if resp != nil {
				_ = resp.Body.Close()
			}
			previous = &target
			continue
		}

		usage, err := s.writeUpstreamResponse(w, resp, normalized.Stream)
		if err != nil {
			status := s.selector.RecordFailure(target.Station, statuses[target.Station.ID], err.Error())
			statuses[target.Station.ID] = status
			_ = s.store.SaveStationStatus(ctx, status)
			_ = s.store.InsertRequestLog(ctx, requestLog(normalized, target.Station.Name, resp.StatusCode, startedAt, didFailover, "stream_copy_error", usage))
			return
		}

		status := s.selector.RecordSuccess(target.Station, statuses[target.Station.ID])
		statuses[target.Station.ID] = status
		_ = s.store.SaveStationStatus(ctx, status)
		_ = s.store.InsertRequestLog(ctx, requestLog(normalized, target.Station.Name, resp.StatusCode, startedAt, didFailover, "", usage))
		return
	}

	if attempted != nil {
		_ = s.store.InsertRequestLog(ctx, requestLog(normalized, attempted.Station.Name, http.StatusBadGateway, startedAt, didFailover, errorKind, core.TokenUsage{}))
	}
	if len(attemptFailures) > 0 {
		http.Error(w, "all upstream stations failed: "+strings.Join(attemptFailures, "; "), http.StatusBadGateway)
		return
	}
	http.Error(w, "all upstream stations failed", http.StatusBadGateway)
}

func (s *Server) writeUpstreamResponse(w http.ResponseWriter, resp *http.Response, stream bool) (core.TokenUsage, error) {
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if !stream {
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return core.TokenUsage{}, err
		}
		if _, err := w.Write(body); err != nil {
			return core.TokenUsage{}, err
		}
		return extractTokenUsage(body), nil
	}
	if flusher, ok := w.(http.Flusher); ok {
		if _, err := io.Copy(w, resp.Body); err != nil {
			flusher.Flush()
			_ = resp.Body.Close()
			return core.TokenUsage{}, err
		}
		flusher.Flush()
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			_ = resp.Body.Close()
			return core.TokenUsage{}, err
		}
	}
	return core.TokenUsage{}, resp.Body.Close()
}

func copyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func cloneDefaultTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	return transport
}

func deriveAdminWriteToken(localAPIKey string) string {
	sum := sha256.Sum256([]byte("admin-write-token:" + localAPIKey))
	return hex.EncodeToString(sum[:16])
}

func requestLog(req core.NormalizedRequest, stationName string, statusCode int, startedAt time.Time, didFailover bool, errorKind string, usage core.TokenUsage) core.RequestLog {
	return core.RequestLog{
		Protocol:     req.Protocol,
		Alias:        req.Alias,
		StationName:  stationName,
		StatusCode:   statusCode,
		DurationMS:   time.Since(startedAt).Milliseconds(),
		WasStream:    req.Stream,
		DidFailover:  didFailover,
		ErrorKind:    errorKind,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		CreatedAt:    time.Now().UTC(),
	}
}

func failureMessage(err error, resp *http.Response) string {
	if err != nil {
		return err.Error()
	}
	if resp != nil {
		return resp.Status
	}
	return "upstream failure"
}

func failureKind(err error, resp *http.Response, captured *capturedUpstreamError) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		return "transport_error"
	}
	if resp == nil {
		return "upstream_error"
	}
	if captured != nil {
		if kind := semanticFailoverKind(resp.StatusCode, captured.Body); kind != "" {
			return kind
		}
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout:
		return "timeout"
	case http.StatusNotFound:
		return "endpoint_not_supported"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return "upstream_error"
	}
	return "gateway_error"
}

// shouldFailover decides whether the gateway should treat an upstream attempt
// as a failure and try the next candidate station.
//
// A 404 here means the upstream station does not implement the endpoint we
// just called (typical for OpenAI-compatible relays that only ship
// /v1/chat/completions and reject /v1/responses). Forwarding that 404 to the
// client would mask a perfectly working second station — so we treat 404 as a
// station-level failure. 401/403 stay forwarded because they almost always
// signal a local key/permission problem the user must fix per-station; auto
// failover would just hide it.
func shouldFailover(err error, resp *http.Response, captured *capturedUpstreamError) bool {
	if err != nil {
		return true
	}
	if resp == nil {
		return true
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return true
	}
	if captured != nil && semanticFailoverKind(resp.StatusCode, captured.Body) != "" {
		return true
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusNotFound:
		return true
	}
	return false
}

type capturedUpstreamError struct {
	Body      string
	Headers   string
	Truncated bool
}

func captureUpstreamError(resp *http.Response) (*capturedUpstreamError, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	logBody := body
	truncated := false
	if len(logBody) > upstreamErrorBodyLogLimit {
		logBody = logBody[:upstreamErrorBodyLogLimit]
		truncated = true
	}

	return &capturedUpstreamError{
		Body:      string(logBody),
		Headers:   upstreamErrorHeaders(resp.Header),
		Truncated: truncated,
	}, nil
}

func upstreamErrorHeaders(header http.Header) string {
	allowed := []string{
		"cf-ray",
		"request-id",
		"x-request-id",
		"x-correlation-id",
		"openai-processing-ms",
		"content-type",
	}
	out := make(map[string][]string)
	for _, key := range allowed {
		if values := header.Values(key); len(values) > 0 {
			out[key] = values
		}
	}
	if len(out) == 0 {
		return "{}"
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func upstreamHTTPErrorKind(resp *http.Response, body string) string {
	if resp == nil {
		return "upstream_error"
	}
	if kind := semanticFailoverKind(resp.StatusCode, body); kind != "" {
		return kind
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusRequestTimeout:
		return "timeout"
	case http.StatusNotFound:
		return "endpoint_not_supported"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth_or_permission"
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return "upstream_error"
	}
	return "upstream_http_error"
}

func semanticFailoverKind(statusCode int, body string) string {
	if statusCode != http.StatusPaymentRequired && statusCode != http.StatusForbidden {
		return ""
	}
	lower := strings.ToLower(body)
	switch {
	case containsAny(lower,
		"subscription_not_found",
		"no active subscription",
		"subscription not found",
		"订阅不存在",
		"无有效订阅",
		"没有有效订阅",
	):
		return "subscription_not_found"
	case containsAny(lower,
		"insufficient balance",
		"insufficient credit",
		"insufficient credits",
		"out of credit",
		"out of credits",
		"余额不足",
		"账户余额",
		"账号余额",
		"额度不足",
		"无可用额度",
		"欠费",
		"充值",
	):
		return "insufficient_balance"
	case containsAny(lower,
		"usage limit",
		"usage quota",
		"quota exceeded",
		"quota limit",
		"limit exceeded",
		"rate limit exceeded",
		"已达到用量上限",
		"用量上限",
		"达到上限",
		"超出限制",
		"限制使用",
		"受限制",
	):
		return "quota_limited"
	case containsAny(lower,
		"model unavailable",
		"model not available",
		"model is not available",
		"model unsupported",
		"unsupported model",
		"model not found",
		"模型不可用",
		"模型不存在",
		"不支持该模型",
	):
		return "model_unavailable"
	case containsAny(lower,
		"payment required",
		"billing required",
		"billing issue",
		"billing",
		"需要付费",
		"支付",
	):
		return "billing_required"
	default:
		return ""
	}
}

func containsAny(value string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}

func upstreamErrorLog(req core.NormalizedRequest, stationName string, statusCode int, errorKind string, captured *capturedUpstreamError) core.UpstreamErrorLog {
	if errorKind == "" {
		errorKind = "upstream_http_error"
	}
	entry := core.UpstreamErrorLog{
		Protocol:    req.Protocol,
		Alias:       req.Alias,
		StationName: stationName,
		StatusCode:  statusCode,
		ErrorKind:   errorKind,
		CreatedAt:   time.Now().UTC(),
	}
	if captured != nil {
		entry.Body = captured.Body
		entry.Headers = captured.Headers
		entry.Truncated = captured.Truncated
	}
	return entry
}

func attemptFailureMessage(stationName string, err error, resp *http.Response, errorKind string) string {
	if err != nil {
		return fmt.Sprintf("%s: %s", stationName, errorKind)
	}
	if resp != nil {
		return fmt.Sprintf("%s: %d %s", stationName, resp.StatusCode, errorKind)
	}
	return fmt.Sprintf("%s: %s", stationName, errorKind)
}
