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
	"relay-gateway/internal/config"
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

func TestSetupPostSavesRuntimeFileAndRedirectsToStations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	runtimePath := filepath.Join(t.TempDir(), "runtime.json")
	handler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      "127.0.0.1:8787",
		RuntimeFilePath: runtimePath,
		RuntimeWarning:  "runtime warning",
		SetupMode:       true,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions error = %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("setup get status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(getRecorder.Body.String(), "runtime warning") {
		t.Fatalf("setup page did not contain warning: %s", getRecorder.Body.String())
	}

	form := url.Values{
		"write_token":   []string{adminWriteToken},
		"local_api_key": []string{"local-runtime-key"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/stations" {
		t.Fatalf("redirect location = %q, want %q", got, "/admin/stations")
	}

	saved, err := config.LoadRuntimeFile(runtimePath)
	if err != nil {
		t.Fatalf("LoadRuntimeFile error = %v", err)
	}
	if saved.LocalAPIKey != "local-runtime-key" {
		t.Fatalf("saved LocalAPIKey = %q, want %q", saved.LocalAPIKey, "local-runtime-key")
	}

	redirectReq := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	redirectRecorder := httptest.NewRecorder()
	handler.ServeHTTP(redirectRecorder, redirectReq)
	if redirectRecorder.Code != http.StatusSeeOther {
		t.Fatalf("valid runtime setup get status = %d, want %d", redirectRecorder.Code, http.StatusSeeOther)
	}
	if got := redirectRecorder.Header().Get("Location"); got != "/admin/runtime" {
		t.Fatalf("valid runtime redirect location = %q, want %q", got, "/admin/runtime")
	}
}

func TestRuntimePageShowsEndpointsAndSavedRestartNotice(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	runtimePath := filepath.Join(t.TempDir(), "runtime.json")
	if err := config.SaveRuntimeFile(runtimePath, config.RuntimeFile{LocalAPIKey: "old-key"}); err != nil {
		t.Fatalf("SaveRuntimeFile initial error = %v", err)
	}

	handler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      "127.0.0.1:8787",
		RuntimeFilePath: runtimePath,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions error = %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/runtime", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("runtime get status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	body := getRecorder.Body.String()
	for _, want := range []string{
		"http://127.0.0.1:8787/openai/v1",
		"http://127.0.0.1:8787/anthropic",
		`value="old-key"`,
		runtimePath,
		"Restart required",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("runtime page did not contain %q: %s", want, body)
		}
	}

	form := url.Values{
		"write_token":   []string{adminWriteToken},
		"local_api_key": []string{"new-key"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/admin/runtime", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusSeeOther {
		t.Fatalf("runtime post status = %d, want %d", postRecorder.Code, http.StatusSeeOther)
	}
	if got := postRecorder.Header().Get("Location"); got != "/admin/runtime?saved=1" {
		t.Fatalf("runtime post redirect = %q, want %q", got, "/admin/runtime?saved=1")
	}

	saved, err := config.LoadRuntimeFile(runtimePath)
	if err != nil {
		t.Fatalf("LoadRuntimeFile error = %v", err)
	}
	if saved.LocalAPIKey != "new-key" {
		t.Fatalf("saved LocalAPIKey = %q, want %q", saved.LocalAPIKey, "new-key")
	}

	savedReq := httptest.NewRequest(http.MethodGet, "/admin/runtime?saved=1", nil)
	savedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(savedRecorder, savedReq)
	if savedRecorder.Code != http.StatusOK {
		t.Fatalf("runtime saved get status = %d, want %d", savedRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(savedRecorder.Body.String(), "Saved. Restart required") {
		t.Fatalf("runtime saved page did not contain restart notice: %s", savedRecorder.Body.String())
	}
}

func TestRuntimePageShowsRuntimeFileLoadWarning(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	runtimePath := filepath.Join(t.TempDir(), "missing-runtime.json")
	handler, err := admin.NewHandlerWithOptions(store, admin.Options{
		WriteToken:      adminWriteToken,
		ListenAddr:      "127.0.0.1:8787",
		RuntimeFilePath: runtimePath,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/runtime", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Runtime file could not be loaded") {
		t.Fatalf("body did not contain runtime load warning: %s", body)
	}
	if !strings.Contains(body, runtimePath) {
		t.Fatalf("body did not contain runtime path: %s", body)
	}
}

func TestRuntimePageNormalizesWildcardListenAddresses(t *testing.T) {
	testCases := []struct {
		name       string
		listenAddr string
		wantURL    string
	}{
		{name: "port only", listenAddr: ":8787", wantURL: "http://127.0.0.1:8787/openai/v1"},
		{name: "ipv4 wildcard", listenAddr: "0.0.0.0:8787", wantURL: "http://127.0.0.1:8787/openai/v1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "gateway.db")
			store, err := sqlitestore.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore error = %v", err)
			}
			defer store.Close()

			runtimePath := filepath.Join(t.TempDir(), "runtime.json")
			if err := config.SaveRuntimeFile(runtimePath, config.RuntimeFile{LocalAPIKey: "local-key"}); err != nil {
				t.Fatalf("SaveRuntimeFile error = %v", err)
			}

			handler, err := admin.NewHandlerWithOptions(store, admin.Options{
				WriteToken:      adminWriteToken,
				ListenAddr:      tc.listenAddr,
				RuntimeFilePath: runtimePath,
			})
			if err != nil {
				t.Fatalf("NewHandlerWithOptions error = %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/admin/runtime", nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if !strings.Contains(recorder.Body.String(), tc.wantURL) {
				t.Fatalf("body did not contain normalized url %q: %s", tc.wantURL, recorder.Body.String())
			}
		})
	}
}

func TestSetupPostRejectsMissingOrInvalidWriteToken(t *testing.T) {
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

			runtimePath := filepath.Join(t.TempDir(), "runtime.json")
			handler, err := admin.NewHandlerWithOptions(store, admin.Options{
				WriteToken:      adminWriteToken,
				RuntimeFilePath: runtimePath,
			})
			if err != nil {
				t.Fatalf("NewHandlerWithOptions error = %v", err)
			}

			form := url.Values{"local_api_key": []string{"local-key"}}
			if tc.value != "" {
				form.Set("write_token", tc.value)
			}
			req := httptest.NewRequest(http.MethodPost, "/admin/setup", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
			if _, err := config.LoadRuntimeFile(runtimePath); err == nil {
				t.Fatal("runtime file was unexpectedly saved")
			}
		})
	}
}

func TestRuntimePostRejectsMissingOrInvalidWriteToken(t *testing.T) {
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

			runtimePath := filepath.Join(t.TempDir(), "runtime.json")
			if err := config.SaveRuntimeFile(runtimePath, config.RuntimeFile{LocalAPIKey: "old-key"}); err != nil {
				t.Fatalf("SaveRuntimeFile error = %v", err)
			}
			handler, err := admin.NewHandlerWithOptions(store, admin.Options{
				WriteToken:      adminWriteToken,
				RuntimeFilePath: runtimePath,
			})
			if err != nil {
				t.Fatalf("NewHandlerWithOptions error = %v", err)
			}

			form := url.Values{"local_api_key": []string{"new-key"}}
			if tc.value != "" {
				form.Set("write_token", tc.value)
			}
			req := httptest.NewRequest(http.MethodPost, "/admin/runtime", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
			saved, err := config.LoadRuntimeFile(runtimePath)
			if err != nil {
				t.Fatalf("LoadRuntimeFile error = %v", err)
			}
			if saved.LocalAPIKey != "old-key" {
				t.Fatalf("saved LocalAPIKey = %q, want %q", saved.LocalAPIKey, "old-key")
			}
		})
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

func TestStationCreatePostAllowsOpenAIOnlyStation(t *testing.T) {
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
	form.Set("anthropic_base_url", "")
	form.Set("anthropic_api_key", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}

	stations, err := store.ListStations(context.Background())
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("len(stations) = %d, want 1", len(stations))
	}
	if stations[0].OpenAIBaseURL == "" || stations[0].OpenAIAPIKey == "" {
		t.Fatalf("openai fields were not saved: %+v", stations[0])
	}
	if stations[0].AnthropicBaseURL != "" || stations[0].AnthropicAPIKey != "" {
		t.Fatalf("anthropic fields = %q / %q, want empty", stations[0].AnthropicBaseURL, stations[0].AnthropicAPIKey)
	}
}

func TestStationCreatePostAllowsAnthropicOnlyStation(t *testing.T) {
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
	form.Set("openai_base_url", "")
	form.Set("openai_api_key", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}

	stations, err := store.ListStations(context.Background())
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("len(stations) = %d, want 1", len(stations))
	}
	if stations[0].AnthropicBaseURL == "" || stations[0].AnthropicAPIKey == "" {
		t.Fatalf("anthropic fields were not saved: %+v", stations[0])
	}
	if stations[0].OpenAIBaseURL != "" || stations[0].OpenAIAPIKey != "" {
		t.Fatalf("openai fields = %q / %q, want empty", stations[0].OpenAIBaseURL, stations[0].OpenAIAPIKey)
	}
}

func TestStationCreatePostRejectsIncompleteProtocolPairs(t *testing.T) {
	testCases := []struct {
		name  string
		field string
		value string
	}{
		{name: "openai base without key", field: "openai_api_key", value: ""},
		{name: "openai key without base", field: "openai_base_url", value: ""},
		{name: "anthropic base without key", field: "anthropic_api_key", value: ""},
		{name: "anthropic key without base", field: "anthropic_base_url", value: ""},
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
		})
	}
}

func TestStationCreatePostRejectsWhenNoProtocolConfigured(t *testing.T) {
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
	form.Set("openai_base_url", "")
	form.Set("openai_api_key", "")
	form.Set("anthropic_base_url", "")
	form.Set("anthropic_api_key", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/stations", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestStationEditPageAndUpdatePostModifySavedStation(t *testing.T) {
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

	getReq := httptest.NewRequest(http.MethodGet, "/admin/stations/edit?id=1", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("edit page status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(getRecorder.Body.String(), `value="station-a"`) {
		t.Fatalf("edit page did not contain station name: %s", getRecorder.Body.String())
	}
	if !strings.Contains(getRecorder.Body.String(), `action="/admin/stations/update"`) {
		t.Fatalf("edit page did not contain update action: %s", getRecorder.Body.String())
	}

	form := validStationForm()
	form.Set("id", "1")
	form.Set("name", "station-a-renamed")
	form.Set("priority", "50")
	form.Set("openai_base_url", "")
	form.Set("openai_api_key", "")
	form.Set("anthropic_base_url", "https://b.example.com/anthropic")
	form.Set("anthropic_api_key", "ANTHROPIC_B")

	postReq := httptest.NewRequest(http.MethodPost, "/admin/stations/update", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)

	if postRecorder.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, want %d", postRecorder.Code, http.StatusSeeOther)
	}
	if got := postRecorder.Header().Get("Location"); got != "/admin/stations" {
		t.Fatalf("redirect location = %q, want %q", got, "/admin/stations")
	}

	stations, err := store.ListStations(context.Background())
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("len(stations) = %d, want 1", len(stations))
	}
	if stations[0].ID != stationID {
		t.Fatalf("id = %d, want %d", stations[0].ID, stationID)
	}
	if stations[0].Name != "station-a-renamed" {
		t.Fatalf("name = %q, want %q", stations[0].Name, "station-a-renamed")
	}
	if stations[0].Priority != 50 {
		t.Fatalf("priority = %d, want %d", stations[0].Priority, 50)
	}
	if stations[0].OpenAIBaseURL != "" || stations[0].OpenAIAPIKey != "" {
		t.Fatalf("openai fields = %q / %q, want empty", stations[0].OpenAIBaseURL, stations[0].OpenAIAPIKey)
	}
	if stations[0].AnthropicBaseURL != "https://b.example.com/anthropic" {
		t.Fatalf("anthropic base = %q, want %q", stations[0].AnthropicBaseURL, "https://b.example.com/anthropic")
	}
	if stations[0].AnthropicAPIKey != "ANTHROPIC_B" {
		t.Fatalf("anthropic key = %q, want %q", stations[0].AnthropicAPIKey, "ANTHROPIC_B")
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
