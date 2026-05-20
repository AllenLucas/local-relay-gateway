package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"relay-gateway/internal/admin"
	"relay-gateway/internal/core"
	sqlitestore "relay-gateway/internal/storage/sqlite"
)

func TestStationsPageRendersSavedStations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, _ := store.CreateStation(context.Background(), core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     20,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
		AnthropicBaseURL:             "https://a.example.com/anthropic",
		AnthropicAPIKey:              "ANTHROPIC_A",
	})
	_ = store.UpsertModelMapping(context.Background(), core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.1",
		Enabled:       true,
	})

	handler, err := admin.NewHandler(store)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/stations", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "station-a") {
		t.Fatalf("body did not contain station name: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `/admin/status`) {
		t.Fatalf("body unexpectedly contained status link: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `/admin/logs`) {
		t.Fatalf("body unexpectedly contained logs link: %s", recorder.Body.String())
	}

	mappingReq := httptest.NewRequest(http.MethodGet, "/admin/mappings", nil)
	mappingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(mappingRecorder, mappingReq)

	if mappingRecorder.Code != http.StatusOK {
		t.Fatalf("mapping status = %d, want %d", mappingRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(mappingRecorder.Body.String(), "gpt-5") {
		t.Fatalf("mapping page did not contain alias: %s", mappingRecorder.Body.String())
	}
}

func TestMappingCreatePostRedirectsBackToPage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, _ := store.CreateStation(context.Background(), core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     20,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
		AnthropicBaseURL:             "https://a.example.com/anthropic",
		AnthropicAPIKey:              "ANTHROPIC_A",
	})

	handler, err := admin.NewHandler(store)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	form := url.Values{
		"station_id":     []string{`1`},
		"protocol":       []string{`openai`},
		"alias":          []string{`gpt-5`},
		"upstream_model": []string{`gpt-5.1`},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/mappings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/mappings" {
		t.Fatalf("redirect location = %q, want %q", got, "/admin/mappings")
	}

	mappings, err := store.ListMappings(context.Background())
	if err != nil {
		t.Fatalf("ListMappings error = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) = %d, want 1", len(mappings))
	}
	if mappings[0].StationID != stationID {
		t.Fatalf("station id = %d, want %d", mappings[0].StationID, stationID)
	}
}

func TestAdminRootWithTrailingSlashRedirectsToStations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	handler, err := admin.NewHandler(store)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/stations" {
		t.Fatalf("redirect location = %q, want %q", got, "/admin/stations")
	}
}
