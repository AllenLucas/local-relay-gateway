package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer does not support hijacking")
		}

		conn, buf, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("Hijack error = %v", err)
		}
		defer conn.Close()

		if _, err := buf.WriteString(
			"HTTP/1.1 200 OK\r\n" +
				"Content-Type: text/event-stream\r\n" +
				"Content-Length: 999\r\n" +
				"Connection: close\r\n\r\n" +
				"data: first\n\n",
		); err != nil {
			t.Fatalf("WriteString error = %v", err)
		}
		if err := buf.Flush(); err != nil {
			t.Fatalf("Flush error = %v", err)
		}
	}))
	defer first.Close()

	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: second\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer second.Close()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close error = %v", closeErr)
		}
	}()

	ctx := context.Background()
	firstID, err := store.CreateStation(ctx, core.Station{Name: "station-a", Enabled: true, Priority: 20, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: first.URL, OpenAIAPIKey: "OPENAI_A", AnthropicBaseURL: first.URL, AnthropicAPIKey: "ANTHROPIC_A"})
	if err != nil {
		t.Fatalf("CreateStation first error = %v", err)
	}
	secondID, err := store.CreateStation(ctx, core.Station{Name: "station-b", Enabled: true, Priority: 10, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: second.URL, OpenAIAPIKey: "OPENAI_B", AnthropicBaseURL: second.URL, AnthropicAPIKey: "ANTHROPIC_B"})
	if err != nil {
		t.Fatalf("CreateStation second error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: firstID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping first error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: secondID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping second error = %v", err)
	}

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5","input":"hello","stream":true}`)))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")

	writer := newFlushRecorder()
	handler.ServeHTTP(writer, req)

	body := writer.BodyString()
	scanner := bufio.NewScanner(strings.NewReader(body))
	if !scanner.Scan() {
		t.Fatal("expected at least one SSE line")
	}
	if body != "data: first\n\n" {
		t.Fatalf("body = %q, want %q", body, "data: first\\n\\n")
	}
	if writer.FlushCount() == 0 {
		t.Fatal("expected stream bytes to become visible only after flush")
	}
	if secondCalls != 0 {
		t.Fatalf("secondCalls = %d, want 0", secondCalls)
	}
	if firstCalls != 1 {
		t.Fatalf("firstCalls = %d, want 1", firstCalls)
	}
}

type flushRecorder struct {
	header     http.Header
	statusCode int

	mu        sync.Mutex
	pending   bytes.Buffer
	visible   bytes.Buffer
	flushes   int
	wroteHead bool
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header)}
}

func (r *flushRecorder) Header() http.Header {
	return r.header
}

func (r *flushRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wroteHead {
		return
	}
	r.statusCode = statusCode
	r.wroteHead = true
}

func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.wroteHead {
		r.statusCode = http.StatusOK
		r.wroteHead = true
	}
	return r.pending.Write(p)
}

func (r *flushRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
	if r.pending.Len() == 0 {
		return
	}
	_, _ = r.visible.Write(r.pending.Bytes())
	r.pending.Reset()
}

func (r *flushRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.visible.String()
}

func (r *flushRecorder) FlushCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushes
}
