package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestResponsesHandlerReturnsSetupGuidanceBeforeAuthInSetupMode(t *testing.T) {
	handler := newGatewayTestServerWithOptions(t, gateway.Options{
		Runtime:         config.Runtime{LocalAPIKey: "local-test-key", ListenAddr: "127.0.0.1:8787"},
		SetupMode:       true,
		RuntimeFilePath: "runtime.json",
	}, nil)

	recorder := performResponsesRequest(handler, "wrong-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "/admin/setup") {
		t.Fatalf("body did not contain setup guidance: %s", recorder.Body.String())
	}
}

func TestAnthropicHandlerAcceptsBearerAuthorization(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
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
		AnthropicBaseURL:             upstream.URL,
		AnthropicAPIKey:              "ANTHROPIC_A",
	})
	if err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolAnthropic,
		Alias:         "claude-sonnet",
		UpstreamModel: "claude-sonnet-4-5",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping error = %v", err)
	}

	handler := gateway.NewServer(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(nil))

	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"claude-sonnet","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstreamCalls = %d, want 1", upstreamCalls)
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

func TestResponsesHandlerDoesNotPersistFailureOnCanceledClient(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "should not be reached", http.StatusInternalServerError)
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
		UpstreamModel: "gpt-5",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping error = %v", err)
	}

	handler := gateway.NewServer(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(nil))

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5","input":"hello","stream":false}`))).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	statuses, err := store.ListStationStatuses(ctx)
	if err != nil {
		t.Fatalf("ListStationStatuses error = %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("len(statuses) = %d, want 0", len(statuses))
	}

	requestLogs, err := store.ListRequestLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(requestLogs) != 0 {
		t.Fatalf("len(requestLogs) = %d, want 0", len(requestLogs))
	}

	failoverEvents, err := store.ListFailoverEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListFailoverEvents error = %v", err)
	}
	if len(failoverEvents) != 0 {
		t.Fatalf("len(failoverEvents) = %d, want 0", len(failoverEvents))
	}

	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
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

func TestResponsesHandlerTrimsModelAliasBeforeRouting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if payload["model"] != "gpt-5.1" {
			t.Fatalf("rewritten model = %v, want gpt-5.1", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Station", "station-a")
		_, _ = w.Write([]byte(`{"id":"resp_trimmed","status":"completed"}`))
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
				UpstreamModel: "gpt-5.1",
				Enabled:       true,
			},
		},
	})

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":" gpt-5 ","input":"hello","stream":false}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Upstream-Station"); got != "station-a" {
		t.Fatalf("X-Upstream-Station = %q, want %q", got, "station-a")
	}
	if got := recorder.Body.String(); got != `{"id":"resp_trimmed","status":"completed"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestResponsesHandlerPersistsCooldownAndLogsFailoverUsage(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, "overloaded", http.StatusTooManyRequests)
	}))
	defer first.Close()

	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","status":"completed"}`))
	}))
	defer second.Close()

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
	firstID, err := store.CreateStation(ctx, core.Station{
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
	if err != nil {
		t.Fatalf("CreateStation first error = %v", err)
	}
	secondID, err := store.CreateStation(ctx, core.Station{
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
	if err != nil {
		t.Fatalf("CreateStation second error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: firstID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping first error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: secondID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5.1", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping second error = %v", err)
	}

	handler := gateway.NewServer(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(func() time.Time {
		return time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	}))

	firstRecorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	statuses, err := store.ListStationStatuses(ctx)
	if err != nil {
		t.Fatalf("ListStationStatuses error = %v", err)
	}
	status, ok := statuses[firstID]
	if !ok {
		t.Fatal("missing persisted status for station-a")
	}
	if status.State != "cooldown" {
		t.Fatalf("status.State = %q, want %q", status.State, "cooldown")
	}

	secondRecorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"again","stream":false}`))
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", secondRecorder.Code, http.StatusOK)
	}
	if firstCalls != 1 {
		t.Fatalf("firstCalls = %d, want 1", firstCalls)
	}
	if secondCalls != 2 {
		t.Fatalf("secondCalls = %d, want 2", secondCalls)
	}

	requestLogs, err := store.ListRequestLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(requestLogs) != 2 {
		t.Fatalf("len(requestLogs) = %d, want 2", len(requestLogs))
	}
	if requestLogs[0].StationName != "station-b" || requestLogs[1].StationName != "station-b" {
		t.Fatalf("request log stations = [%q, %q], want both station-b", requestLogs[0].StationName, requestLogs[1].StationName)
	}
	if !requestLogs[1].DidFailover {
		t.Fatalf("first request DidFailover = %v, want true", requestLogs[1].DidFailover)
	}
	if requestLogs[0].DidFailover {
		t.Fatalf("second request DidFailover = %v, want false", requestLogs[0].DidFailover)
	}

	failoverEvents, err := store.ListFailoverEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListFailoverEvents error = %v", err)
	}
	if len(failoverEvents) != 1 {
		t.Fatalf("len(failoverEvents) = %d, want 1", len(failoverEvents))
	}
	if failoverEvents[0].FromStationName != "station-a" || failoverEvents[0].ToStationName != "station-b" {
		t.Fatalf("failover event = %q -> %q, want station-a -> station-b", failoverEvents[0].FromStationName, failoverEvents[0].ToStationName)
	}
}

type gatewayStationFixture struct {
	station core.Station
	mapping core.ModelMapping
}

func newGatewayTestServer(t *testing.T, fixtures []gatewayStationFixture) http.Handler {
	t.Helper()

	return newGatewayTestServerWithOptions(t, gateway.Options{
		Runtime: config.Runtime{LocalAPIKey: "local-test-key"},
	}, fixtures)
}

func newGatewayTestServerWithOptions(t *testing.T, options gateway.Options, fixtures []gatewayStationFixture) http.Handler {
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

	return gateway.NewServer(options, store, routing.NewSelector(nil))
}

func performResponsesRequest(handler http.Handler, apiKey string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
