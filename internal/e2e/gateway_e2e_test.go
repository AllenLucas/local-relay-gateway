package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"relay-gateway/internal/config"
	"relay-gateway/internal/core"
	"relay-gateway/internal/gateway"
	"relay-gateway/internal/routing"
	sqlitestore "relay-gateway/internal/storage/sqlite"
)

func TestGatewayServesOpenAIAndAnthropicProtocols(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}

		switch r.URL.Path {
		case "/v1/chat/completions":
			if got := r.Header.Get("Authorization"); got != "Bearer OPENAI_A" {
				t.Fatalf("Authorization = %q, want %q", got, "Bearer OPENAI_A")
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}
			if got := payload["model"]; got != "gpt-5.1" {
				t.Fatalf("chat model = %v, want gpt-5.1", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"chat-ok"}}]}`))
		case "/v1/messages":
			if got := r.Header.Get("x-api-key"); got != "ANTHROPIC_A" {
				t.Fatalf("x-api-key = %q, want %q", got, "ANTHROPIC_A")
			}
			if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
				t.Fatalf("anthropic-version = %q, want %q", got, "2023-06-01")
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want %q", got, "application/json")
			}
			if got := payload["model"]; got != "claude-sonnet-4-5" {
				t.Fatalf("messages model = %v, want claude-sonnet-4-5", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"anthropic-ok"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close error = %v", err)
		}
	}()

	ctx := context.Background()
	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     20,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                upstream.URL,
		OpenAIAPIKey:                 "OPENAI_A",
		AnthropicBaseURL:             upstream.URL,
		AnthropicAPIKey:              "ANTHROPIC_A",
	})
	if err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}

	if err := store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.1",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping openai error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolAnthropic,
		Alias:         "claude-sonnet",
		UpstreamModel: "claude-sonnet-4-5",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping anthropic error = %v", err)
	}

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))

	openAIReq := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":" gpt-5 ","messages":[{"role":"user","content":"hello"}]}`)))
	openAIReq.Header.Set("Authorization", "Bearer local-test-key")
	openAIReq.Header.Set("Content-Type", "application/json")
	openAIResp := httptest.NewRecorder()
	handler.ServeHTTP(openAIResp, openAIReq)
	if openAIResp.Code != http.StatusOK {
		t.Fatalf("openai status = %d, want %d", openAIResp.Code, http.StatusOK)
	}
	if got := openAIResp.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("openai Content-Type = %q, want %q", got, "application/json")
	}
	if got := openAIResp.Body.String(); got != `{"choices":[{"message":{"content":"chat-ok"}}]}` {
		t.Fatalf("openai body = %q", got)
	}

	anthropicReq := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":" claude-sonnet ","messages":[{"role":"user","content":"hello"}]}`)))
	anthropicReq.Header.Set("x-api-key", "local-test-key")
	anthropicReq.Header.Set("Content-Type", "application/json")
	anthropicResp := httptest.NewRecorder()
	handler.ServeHTTP(anthropicResp, anthropicReq)
	if anthropicResp.Code != http.StatusOK {
		t.Fatalf("anthropic status = %d, want %d", anthropicResp.Code, http.StatusOK)
	}
	if got := anthropicResp.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("anthropic Content-Type = %q, want %q", got, "application/json")
	}
	if got := anthropicResp.Body.String(); got != `{"content":[{"type":"text","text":"anthropic-ok"}]}` {
		t.Fatalf("anthropic body = %q", got)
	}
}
