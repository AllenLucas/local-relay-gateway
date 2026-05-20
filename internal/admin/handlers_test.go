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

const adminWriteToken = "admin-write-token"

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

	handler, err := admin.NewHandler(store, adminWriteToken)
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
	if !strings.Contains(recorder.Body.String(), `value="`+adminWriteToken+`"`) {
		t.Fatalf("body did not contain write token: %s", recorder.Body.String())
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

func TestStationsPageShowsOnboardingWhenEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	handler, err := admin.NewHandler(store, adminWriteToken)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/stations", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "Quick Setup / 快速配置") {
		t.Fatalf("body did not contain onboarding heading: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "claude-sonnet") {
		t.Fatalf("body did not contain onboarding alias example: %s", recorder.Body.String())
	}
}

func TestStationsPageHidesOnboardingWhenStationsExist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	if _, err := createAdminTestStation(store, "station-a"); err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}

	handler, err := admin.NewHandler(store, adminWriteToken)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/stations", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if strings.Contains(recorder.Body.String(), "Quick Setup / 快速配置") {
		t.Fatalf("body unexpectedly contained onboarding heading: %s", recorder.Body.String())
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

	handler, err := admin.NewHandler(store, adminWriteToken)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	form := url.Values{
		"write_token":    []string{adminWriteToken},
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

	handler, err := admin.NewHandler(store, adminWriteToken)
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

func TestStationCreatePostRejectsMalformedNumericFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	handler, err := admin.NewHandler(store, adminWriteToken)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	form := url.Values{
		"write_token":                    []string{adminWriteToken},
		"name":                           []string{"station-a"},
		"enabled":                        []string{"on"},
		"priority":                       []string{"oops"},
		"cooldown_seconds":               []string{"30"},
		"health_check_interval_seconds":  []string{"15"},
		"health_check_timeout_seconds":   []string{"5"},
		"consecutive_failure_threshold":  []string{"1"},
		"consecutive_recovery_threshold": []string{"2"},
		"openai_base_url":                []string{"https://a.example.com/openai"},
		"openai_api_key":                 []string{"OPENAI_A"},
		"anthropic_base_url":             []string{"https://a.example.com/anthropic"},
		"anthropic_api_key":              []string{"ANTHROPIC_A"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	stations, err := store.ListStations(context.Background())
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 0 {
		t.Fatalf("len(stations) = %d, want 0", len(stations))
	}
}

func TestMappingCreatePostRejectsInvalidStationIDs(t *testing.T) {
	testCases := []struct {
		name      string
		stationID string
	}{
		{name: "malformed", stationID: "abc"},
		{name: "non_positive", stationID: "0"},
		{name: "orphan", stationID: "999"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			if _, err := store.CreateStation(context.Background(), core.Station{
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
			}); err != nil {
				t.Fatalf("CreateStation error = %v", err)
			}

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := url.Values{
				"write_token":    []string{adminWriteToken},
				"station_id":     []string{tc.stationID},
				"protocol":       []string{"openai"},
				"alias":          []string{"gpt-5"},
				"upstream_model": []string{"gpt-5.1"},
			}
			req := httptest.NewRequest(http.MethodPost, "/admin/mappings", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}

			mappings, err := store.ListMappings(context.Background())
			if err != nil {
				t.Fatalf("ListMappings error = %v", err)
			}
			if len(mappings) != 0 {
				t.Fatalf("len(mappings) = %d, want 0", len(mappings))
			}
		})
	}
}

func TestMappingCreatePostPersistsUncheckedMappingAsDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := createAdminTestStation(store, "station-a")
	if err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}

	handler, err := admin.NewHandler(store, adminWriteToken)
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}

	form := url.Values{
		"write_token":    []string{adminWriteToken},
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
	if mappings[0].Enabled {
		t.Fatalf("enabled = %t, want false", mappings[0].Enabled)
	}
}

func TestStationCreatePostRejectsNonPositiveNumericFields(t *testing.T) {
	testCases := []struct {
		name  string
		field string
		value string
	}{
		{name: "zero priority", field: "priority", value: "0"},
		{name: "negative cooldown", field: "cooldown_seconds", value: "-1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := validStationForm()
			form.Set(tc.field, tc.value)

			req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}

			stations, err := store.ListStations(context.Background())
			if err != nil {
				t.Fatalf("ListStations error = %v", err)
			}
			if len(stations) != 0 {
				t.Fatalf("len(stations) = %d, want 0", len(stations))
			}
		})
	}
}

func TestStationCreatePostRejectsBlankRequiredFields(t *testing.T) {
	testCases := []struct {
		name  string
		field string
		value string
	}{
		{name: "blank name", field: "name", value: "   "},
		{name: "blank openai base url", field: "openai_base_url", value: "   "},
		{name: "blank openai api key", field: "openai_api_key", value: "   "},
		{name: "blank anthropic base url", field: "anthropic_base_url", value: "   "},
		{name: "blank anthropic api key", field: "anthropic_api_key", value: "   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := validStationForm()
			form.Set(tc.field, tc.value)

			req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}

			stations, err := store.ListStations(context.Background())
			if err != nil {
				t.Fatalf("ListStations error = %v", err)
			}
			if len(stations) != 0 {
				t.Fatalf("len(stations) = %d, want 0", len(stations))
			}
		})
	}
}

func TestMappingCreatePostRejectsBlankRequiredFields(t *testing.T) {
	testCases := []struct {
		name  string
		field string
		value string
	}{
		{name: "blank protocol", field: "protocol", value: "   "},
		{name: "blank alias", field: "alias", value: "   "},
		{name: "blank upstream model", field: "upstream_model", value: "   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			if _, err := createAdminTestStation(store, "station-a"); err != nil {
				t.Fatalf("CreateStation error = %v", err)
			}

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := validMappingForm("1")
			form.Set(tc.field, tc.value)

			req := httptest.NewRequest(http.MethodPost, "/admin/mappings", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}

			mappings, err := store.ListMappings(context.Background())
			if err != nil {
				t.Fatalf("ListMappings error = %v", err)
			}
			if len(mappings) != 0 {
				t.Fatalf("len(mappings) = %d, want 0", len(mappings))
			}
		})
	}
}

func createAdminTestStation(store *sqlitestore.Store, name string) (int64, error) {
	return store.CreateStation(context.Background(), core.Station{
		Name:                         name,
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
}

func validStationForm() url.Values {
	return url.Values{
		"write_token":                    []string{adminWriteToken},
		"name":                           []string{"station-a"},
		"enabled":                        []string{"on"},
		"priority":                       []string{"20"},
		"cooldown_seconds":               []string{"30"},
		"health_check_interval_seconds":  []string{"15"},
		"health_check_timeout_seconds":   []string{"5"},
		"consecutive_failure_threshold":  []string{"1"},
		"consecutive_recovery_threshold": []string{"2"},
		"openai_base_url":                []string{"https://a.example.com/openai"},
		"openai_api_key":                 []string{"OPENAI_A"},
		"anthropic_base_url":             []string{"https://a.example.com/anthropic"},
		"anthropic_api_key":              []string{"ANTHROPIC_A"},
	}
}

func validMappingForm(stationID string) url.Values {
	return url.Values{
		"write_token":    []string{adminWriteToken},
		"station_id":     []string{stationID},
		"protocol":       []string{"openai"},
		"alias":          []string{"gpt-5"},
		"upstream_model": []string{"gpt-5.1"},
		"enabled":        []string{"on"},
	}
}

func TestStationCreatePostRejectsMissingOrInvalidWriteToken(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "invalid", value: "wrong-token"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := validStationForm()
			if tc.value == "" {
				form.Del("write_token")
			} else {
				form.Set("write_token", tc.value)
			}

			req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}

			stations, err := store.ListStations(context.Background())
			if err != nil {
				t.Fatalf("ListStations error = %v", err)
			}
			if len(stations) != 0 {
				t.Fatalf("len(stations) = %d, want 0", len(stations))
			}
		})
	}
}

func TestMappingCreatePostRejectsMissingOrInvalidWriteToken(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "invalid", value: "wrong-token"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			if _, err := createAdminTestStation(store, "station-a"); err != nil {
				t.Fatalf("CreateStation error = %v", err)
			}

			handler, err := admin.NewHandler(store, adminWriteToken)
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			form := validMappingForm("1")
			if tc.value == "" {
				form.Del("write_token")
			} else {
				form.Set("write_token", tc.value)
			}

			req := httptest.NewRequest(http.MethodPost, "/admin/mappings", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}

			mappings, err := store.ListMappings(context.Background())
			if err != nil {
				t.Fatalf("ListMappings error = %v", err)
			}
			if len(mappings) != 0 {
				t.Fatalf("len(mappings) = %d, want 0", len(mappings))
			}
		})
	}
}
