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

	handler := gateway.NewServerWithOptions(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(nil))

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

	handler := gateway.NewServerWithOptions(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(nil))

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

func TestResponsesHandlerPersistsTokenUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_success","status":"completed","usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20}}`))
	}))
	defer upstream.Close()

	handler, store := newGatewayTestServerAndStore(t, []gatewayStationFixture{
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
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	logs, err := store.ListRequestLogs(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].InputTokens != 12 || logs[0].OutputTokens != 8 || logs[0].TotalTokens != 20 {
		t.Fatalf("request log tokens = %d/%d/%d, want 12/8/20", logs[0].InputTokens, logs[0].OutputTokens, logs[0].TotalTokens)
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

	handler := gateway.NewServerWithOptions(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(func() time.Time {
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

func TestResponsesHandlerFailsOverOn404(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.NotFound(w, r)
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

	handler := gateway.NewServerWithOptions(gateway.Options{Runtime: config.Runtime{LocalAPIKey: "local-test-key"}}, store, routing.NewSelector(func() time.Time {
		return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	}))

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

	statuses, err := store.ListStationStatuses(ctx)
	if err != nil {
		t.Fatalf("ListStationStatuses error = %v", err)
	}
	if status := statuses[firstID]; status.State != "cooldown" {
		t.Fatalf("station-a state = %q, want %q", status.State, "cooldown")
	}

	requestLogs, err := store.ListRequestLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(requestLogs) != 1 {
		t.Fatalf("len(requestLogs) = %d, want 1", len(requestLogs))
	}
	if requestLogs[0].StationName != "station-b" {
		t.Fatalf("request log station = %q, want station-b", requestLogs[0].StationName)
	}
	if !requestLogs[0].DidFailover {
		t.Fatalf("request log DidFailover = %v, want true", requestLogs[0].DidFailover)
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
	if failoverEvents[0].Reason != "endpoint_not_supported" {
		t.Fatalf("failover reason = %q, want endpoint_not_supported", failoverEvents[0].Reason)
	}
}

func TestResponsesHandlerFailsOverOnQuotaLimited402AndLogsUpstreamError(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("cf-ray", "a01981ef0b7b9bda-SIN")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":"已达到用量上限，将在5月28日下午3点42分（北京时间）恢复"}`))
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

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(func() time.Time {
		return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	}))

	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 1/1", firstCalls, secondCalls)
	}

	upstreamErrors, err := store.ListUpstreamErrorLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListUpstreamErrorLogs error = %v", err)
	}
	if len(upstreamErrors) != 1 {
		t.Fatalf("len(upstreamErrors) = %d, want 1", len(upstreamErrors))
	}
	got := upstreamErrors[0]
	if got.StationName != "station-a" || got.StatusCode != http.StatusPaymentRequired || got.ErrorKind != "quota_limited" {
		t.Fatalf("upstream error = %+v, want station-a 402 quota_limited", got)
	}
	if !strings.Contains(got.Body, "已达到用量上限") {
		t.Fatalf("upstream body = %q, want original body", got.Body)
	}
	if !strings.Contains(got.Headers, "a01981ef0b7b9bda-SIN") {
		t.Fatalf("upstream headers = %q, want cf-ray", got.Headers)
	}
	if got.Truncated {
		t.Fatal("upstream error was unexpectedly truncated")
	}

	failoverEvents, err := store.ListFailoverEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListFailoverEvents error = %v", err)
	}
	if len(failoverEvents) != 1 {
		t.Fatalf("len(failoverEvents) = %d, want 1", len(failoverEvents))
	}
	if failoverEvents[0].Reason != "quota_limited" {
		t.Fatalf("failover reason = %q, want quota_limited", failoverEvents[0].Reason)
	}
}

func TestResponsesHandlerFailsOverOnSubscription403AndReturnsSummaryWhenAllStationsFail(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "eccec991-3718-43b5-b6d0-77b7a94ca33a")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"SUBSCRIPTION_NOT_FOUND","message":"No active subscription found for this group"}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":"insufficient balance"}`))
	}))
	defer second.Close()

	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	firstID, err := store.CreateStation(ctx, core.Station{Name: "station-a", Enabled: true, Priority: 20, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: first.URL, OpenAIAPIKey: "OPENAI_A"})
	if err != nil {
		t.Fatalf("CreateStation first error = %v", err)
	}
	secondID, err := store.CreateStation(ctx, core.Station{Name: "station-b", Enabled: true, Priority: 10, CooldownSeconds: 30, HealthCheckIntervalSeconds: 15, HealthCheckTimeoutSeconds: 5, ConsecutiveFailureThreshold: 1, ConsecutiveRecoveryThreshold: 2, OpenAIBaseURL: second.URL, OpenAIAPIKey: "OPENAI_B"})
	if err != nil {
		t.Fatalf("CreateStation second error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: firstID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping first error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{StationID: secondID, Protocol: core.ProtocolOpenAI, Alias: "gpt-5", UpstreamModel: "gpt-5", Enabled: true}); err != nil {
		t.Fatalf("UpsertModelMapping second error = %v", err)
	}

	handler := gateway.NewServer(config.Runtime{LocalAPIKey: "local-test-key"}, store, routing.NewSelector(nil))
	recorder := performResponsesRequest(handler, "local-test-key", []byte(`{"model":"gpt-5","input":"hello","stream":false}`))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "station-a: 403 subscription_not_found") {
		t.Fatalf("body did not summarize station-a subscription failure: %s", body)
	}
	if !strings.Contains(body, "station-b: 402 insufficient_balance") {
		t.Fatalf("body did not summarize station-b balance failure: %s", body)
	}

	upstreamErrors, err := store.ListUpstreamErrorLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListUpstreamErrorLogs error = %v", err)
	}
	if len(upstreamErrors) != 2 {
		t.Fatalf("len(upstreamErrors) = %d, want 2", len(upstreamErrors))
	}
	if upstreamErrors[1].ErrorKind != "subscription_not_found" || !strings.Contains(upstreamErrors[1].Headers, "eccec991") {
		t.Fatalf("first upstream error = %+v, want subscription_not_found with request id", upstreamErrors[1])
	}
	if upstreamErrors[0].ErrorKind != "insufficient_balance" {
		t.Fatalf("second upstream error kind = %q, want insufficient_balance", upstreamErrors[0].ErrorKind)
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

	handler, _ := newGatewayTestServerAndStoreWithOptions(t, options, fixtures)
	return handler
}

func newGatewayTestServerAndStore(t *testing.T, fixtures []gatewayStationFixture) (http.Handler, *sqlitestore.Store) {
	t.Helper()

	return newGatewayTestServerAndStoreWithOptions(t, gateway.Options{
		Runtime: config.Runtime{LocalAPIKey: "local-test-key"},
	}, fixtures)
}

func newGatewayTestServerAndStoreWithOptions(t *testing.T, options gateway.Options, fixtures []gatewayStationFixture) (http.Handler, *sqlitestore.Store) {
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

	return gateway.NewServerWithOptions(options, store, routing.NewSelector(nil)), store
}

func performResponsesRequest(handler http.Handler, apiKey string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
