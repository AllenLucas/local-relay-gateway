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

	handler := newGatewayTestServer(t, []gatewayStationFixture{
		{
			station: core.Station{
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
			},
			mapping: core.ModelMapping{
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5",
				Enabled:       true,
			},
		},
		{
			station: core.Station{
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
			},
			mapping: core.ModelMapping{
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5.1",
				Enabled:       true,
			},
		},
	})

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

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

func TestResponsesHandlerRejectsMissingModel(t *testing.T) {
	handler := newGatewayTestServer(t, nil)

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"input":"hello","stream":false}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestResponsesHandlerRejectsNonStringModel(t *testing.T) {
	handler := newGatewayTestServer(t, nil)

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":123,"input":"hello","stream":false}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestResponsesHandlerRejectsUnauthorizedRequest(t *testing.T) {
	handler := newGatewayTestServer(t, nil)

	recorder := performResponsesRequest(handler, "wrong-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestResponsesHandlerReturnsBadGatewayWhenAllUpstreamsFail(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusTooManyRequests)
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer second.Close()

	handler := newGatewayTestServer(t, []gatewayStationFixture{
		{
			station: core.Station{
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
			},
			mapping: core.ModelMapping{
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5",
				Enabled:       true,
			},
		},
		{
			station: core.Station{
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
			},
			mapping: core.ModelMapping{
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5.1",
				Enabled:       true,
			},
		},
	})

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}

func TestResponsesHandlerProxiesSuccessfulResponseBodyAndHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Station", "station-a")
		_, _ = w.Write([]byte(`{"id":"resp_success","status":"completed"}`))
	}))
	defer upstream.Close()

	handler := newGatewayTestServer(t, []gatewayStationFixture{
		{
			station: core.Station{
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
			},
			mapping: core.ModelMapping{
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5",
				Enabled:       true,
			},
		},
	})

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Upstream-Station"); got != "station-a" {
		t.Fatalf("X-Upstream-Station = %q, want %q", got, "station-a")
	}
	if got := recorder.Body.String(); got != `{"id":"resp_success","status":"completed"}` {
		t.Fatalf("body = %q", got)
	}
}

type gatewayStationFixture struct {
	station core.Station
	mapping core.ModelMapping
}

func newGatewayTestServer(t *testing.T, fixtures []gatewayStationFixture) http.Handler {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close error = %v", err)
		}
	})

	ctx := context.Background()
	for _, fixture := range fixtures {
		stationID, err := store.CreateStation(ctx, fixture.station)
		if err != nil {
			t.Fatalf("CreateStation error = %v", err)
		}

		mapping := fixture.mapping
		mapping.StationID = stationID
		if err := store.UpsertModelMapping(ctx, mapping); err != nil {
			t.Fatalf("UpsertModelMapping error = %v", err)
		}
	}

	return gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))
}

func performResponsesRequest(handler http.Handler, apiKey string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
