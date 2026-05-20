package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"relay-gateway/internal/admin"
	"relay-gateway/internal/config"
	"relay-gateway/internal/core"
	"relay-gateway/internal/protocol/anthropic"
	"relay-gateway/internal/protocol/openai"
	"relay-gateway/internal/routing"
)

type store interface {
	CreateStation(ctx context.Context, station core.Station) (int64, error)
	ListStations(ctx context.Context) ([]core.Station, error)
	ListMappings(ctx context.Context) ([]core.ModelMapping, error)
	UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error
	FindMappings(ctx context.Context, protocol core.Protocol, alias string) ([]core.ModelMapping, error)
	ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error)
	SaveStationStatus(ctx context.Context, status core.StationStatus) error
	ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error)
	InsertRequestLog(ctx context.Context, entry core.RequestLog) error
	ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error)
	InsertFailoverEvent(ctx context.Context, event core.FailoverEvent) error
	UsageByStation(ctx context.Context) ([]core.UsageRow, error)
	UsageByAlias(ctx context.Context) ([]core.UsageRow, error)
}

type Server struct {
	cfg      config.Runtime
	store    store
	selector *routing.Selector
	client   *http.Client
}

type requestBuilder func(target core.ResolvedTarget, normalized core.NormalizedRequest) (*http.Request, error)

func NewServer(cfg config.Runtime, store store, selector *routing.Selector) http.Handler {
	server := &Server{
		cfg:      cfg,
		store:    store,
		selector: selector,
		client: &http.Client{
			Transport: cloneDefaultTransport(),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/openai/v1/responses", server.handleResponses)
	mux.HandleFunc("/openai/v1/chat/completions", server.handleChatCompletions)
	mux.HandleFunc("/anthropic/v1/messages", server.handleAnthropicMessages)
	adminHandler, err := admin.NewHandler(store, deriveAdminWriteToken(cfg.LocalAPIKey))
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
		return r.Header.Get("x-api-key") == s.cfg.LocalAPIKey
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
		if err != nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			errorKind = failureKind(err, resp)
			status := s.selector.RecordFailure(target.Station, statuses[target.Station.ID], failureMessage(err, resp))
			statuses[target.Station.ID] = status
			_ = s.store.SaveStationStatus(ctx, status)
			if resp != nil {
				_ = resp.Body.Close()
			}
			previous = &target
			continue
		}

		if err := s.writeUpstreamResponse(w, resp); err != nil {
			status := s.selector.RecordFailure(target.Station, statuses[target.Station.ID], err.Error())
			statuses[target.Station.ID] = status
			_ = s.store.SaveStationStatus(ctx, status)
			_ = s.store.InsertRequestLog(ctx, requestLog(normalized, target.Station.Name, resp.StatusCode, startedAt, didFailover, "stream_copy_error"))
			return
		}

		status := s.selector.RecordSuccess(target.Station, statuses[target.Station.ID])
		statuses[target.Station.ID] = status
		_ = s.store.SaveStationStatus(ctx, status)
		_ = s.store.InsertRequestLog(ctx, requestLog(normalized, target.Station.Name, resp.StatusCode, startedAt, didFailover, ""))
		return
	}

	if attempted != nil {
		_ = s.store.InsertRequestLog(ctx, requestLog(normalized, attempted.Station.Name, http.StatusBadGateway, startedAt, didFailover, errorKind))
	}
	http.Error(w, "all upstream stations failed", http.StatusBadGateway)
}

func (s *Server) writeUpstreamResponse(w http.ResponseWriter, resp *http.Response) error {
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if flusher, ok := w.(http.Flusher); ok {
		if _, err := io.Copy(w, resp.Body); err != nil {
			flusher.Flush()
			_ = resp.Body.Close()
			return err
		}
		flusher.Flush()
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			_ = resp.Body.Close()
			return err
		}
	}
	return resp.Body.Close()
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

func requestLog(req core.NormalizedRequest, stationName string, statusCode int, startedAt time.Time, didFailover bool, errorKind string) core.RequestLog {
	return core.RequestLog{
		Protocol:    req.Protocol,
		Alias:       req.Alias,
		StationName: stationName,
		StatusCode:  statusCode,
		DurationMS:  time.Since(startedAt).Milliseconds(),
		WasStream:   req.Stream,
		DidFailover: didFailover,
		ErrorKind:   errorKind,
		CreatedAt:   time.Now().UTC(),
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

func failureKind(err error, resp *http.Response) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		return "transport_error"
	}
	if resp == nil {
		return "upstream_error"
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return "rate_limited"
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return "upstream_error"
	}
	return "gateway_error"
}
