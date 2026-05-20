package gateway_test

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

func TestResponsesHandlerFailsOverBeforeSendingClientOutput(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, "overloaded", http.StatusTooManyRequests)
	}))
	defer first.Close()

	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if payload["model"] != "gpt-5.1" {
			t.Fatalf("rewritten model = %v, want gpt-5.1", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer second.Close()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	firstID, _ := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     20,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                first.URL,
		OpenAIAPIKey:                 "OPENAI_A",
		AnthropicBaseURL:             first.URL,
		AnthropicAPIKey:              "ANTHROPIC_A",
	})
	secondID, _ := store.CreateStation(ctx, core.Station{
		Name:                         "station-b",
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                second.URL,
		OpenAIAPIKey:                 "OPENAI_B",
		AnthropicBaseURL:             second.URL,
		AnthropicAPIKey:              "ANTHROPIC_B",
	})

	_ = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     firstID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5",
		Enabled:       true,
	})
	_ = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     secondID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.1",
		Enabled:       true,
	})

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))

	body := []byte(`{"model":"gpt-5","input":"hello","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if firstCalls != 1 {
		t.Fatalf("firstCalls = %d, want 1", firstCalls)
	}
	if secondCalls != 1 {
		t.Fatalf("secondCalls = %d, want 1", secondCalls)
	}
}
