package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"relay-gateway/internal/admin"
	"relay-gateway/internal/config"
	"relay-gateway/internal/core"
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
	ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error)
	ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error)
	UsageByStation(ctx context.Context) ([]core.UsageRow, error)
	UsageByAlias(ctx context.Context) ([]core.UsageRow, error)
}

type Server struct {
	cfg      config.Runtime
	store    store
	selector *routing.Selector
	client   *http.Client
}

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
	adminHandler, err := admin.NewHandler(store, deriveAdminWriteToken(cfg.LocalAPIKey))
	if err != nil {
		panic(err)
	}
	mux.Handle("/admin", adminHandler)
	mux.Handle("/admin/", adminHandler)
	return mux
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.cfg.LocalAPIKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	normalized, err := openai.NormalizeResponses(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	stations, err := s.store.ListStations(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mappings, err := s.store.FindMappings(ctx, core.ProtocolOpenAI, normalized.Alias)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	targets, err := s.selector.Candidates(normalized, stations, mappings, map[int64]core.StationStatus{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Failover is allowed only until the gateway commits headers to the client.
	// Once response bytes have started, the stream belongs to that upstream.
	for _, target := range targets {
		upstreamReq, err := openai.BuildResponsesRequest(target, normalized)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := s.client.Do(upstreamReq.WithContext(ctx))
		if err != nil {
			continue
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			continue
		}

		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if flusher, ok := w.(http.Flusher); ok {
			if _, err := io.Copy(w, resp.Body); err != nil {
				flusher.Flush()
				_ = resp.Body.Close()
				return
			}
			flusher.Flush()
		} else {
			_, _ = io.Copy(w, resp.Body)
		}
		_ = resp.Body.Close()
		return
	}

	http.Error(w, "all upstream stations failed", http.StatusBadGateway)
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
