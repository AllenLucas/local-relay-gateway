package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"relay-gateway/internal/config"
	"relay-gateway/internal/core"
	"relay-gateway/internal/gateway"
	"relay-gateway/internal/routing"
	sqlitestore "relay-gateway/internal/storage/sqlite"
)

func TestStreamDoesNotSwitchAfterFirstChunk(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: first\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer first.Close()

	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: second\n\n"))
	}))
	defer second.Close()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, _ := sqlitestore.NewStore(dbPath)
	defer store.Close()

	ctx := context.Background()
	firstID, _ := store.CreateStation(ctx, core.Station{Name: "station-a", Enabled: true, Priority: 20, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: first.URL, OpenAIAPIKey: "OPENAI_A", AnthropicBaseURL: first.URL, AnthropicAPIKey: "ANTHROPIC_A"})
	secondID, _ := store.CreateStation(ctx, core.Station{Name: "station-b", Enabled: true, Priority: 10, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: second.URL, OpenAIAPIKey: "OPENAI_B", AnthropicBaseURL: second.URL, AnthropicAPIKey: "ANTHROPIC_B"})
	_ = store.UpsertModelMapping(ctx, core.ModelMapping{StationID: firstID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true})
	_ = store.UpsertModelMapping(ctx, core.ModelMapping{StationID: secondID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1", Enabled: true})

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5","input":"hello","stream":true}`)))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	scanner := bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	if !scanner.Scan() {
		t.Fatal("expected at least one SSE line")
	}
	if recorder.Body.String() != "data: first\n\n" {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), "data: first\\n\\n")
	}
	if secondCalls != 0 {
		t.Fatalf("secondCalls = %d, want 0", secondCalls)
	}
	if firstCalls != 1 {
		t.Fatalf("firstCalls = %d, want 1", firstCalls)
	}
}
