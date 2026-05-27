package sqlite_test

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"relay-gateway/internal/configsync"
	"relay-gateway/internal/core"
	sqlitestore "relay-gateway/internal/storage/sqlite"

	_ "modernc.org/sqlite"
)

func TestStorePersistsStationsAndMappings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "primary-a",
		Enabled:                      true,
		Priority:                     10,
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
	if err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}

	err = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.1",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("UpsertModelMapping error = %v", err)
	}

	stations, err := store.ListStations(ctx)
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("len(stations) = %d, want 1", len(stations))
	}
	if !stations[0].Enabled {
		t.Fatalf("stations[0].Enabled = %v, want true", stations[0].Enabled)
	}

	mappings, err := store.FindMappings(ctx, core.ProtocolOpenAI, "gpt-5")
	if err != nil {
		t.Fatalf("FindMappings error = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) = %d, want 1", len(mappings))
	}
	if mappings[0].UpstreamModel != "gpt-5.1" {
		t.Fatalf("UpstreamModel = %q, want %q", mappings[0].UpstreamModel, "gpt-5.1")
	}
	if !mappings[0].Enabled {
		t.Fatalf("Enabled = %v, want true", mappings[0].Enabled)
	}

	err = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.2",
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("UpsertModelMapping update error = %v", err)
	}

	mappings, err = store.FindMappings(ctx, core.ProtocolOpenAI, "gpt-5")
	if err != nil {
		t.Fatalf("FindMappings after update error = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) after update = %d, want 1", len(mappings))
	}
	if mappings[0].UpstreamModel != "gpt-5.2" {
		t.Fatalf("UpstreamModel after update = %q, want %q", mappings[0].UpstreamModel, "gpt-5.2")
	}
	if mappings[0].Enabled {
		t.Fatalf("Enabled after update = %v, want false", mappings[0].Enabled)
	}

	err = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.Protocol("invalid"),
		Alias:         "bad-model",
		UpstreamModel: "bad-model",
		Enabled:       true,
	})
	if err == nil {
		t.Fatal("UpsertModelMapping with invalid protocol error = nil, want non-nil")
	}

	_, err = store.FindMappings(ctx, core.Protocol("invalid"), "bad-model")
	if err == nil {
		t.Fatal("FindMappings with invalid protocol error = nil, want non-nil")
	}
}

func TestStoreCanGetAndUpdateModelMappingByID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     10,
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

	mappings, err := store.ListMappings(ctx)
	if err != nil {
		t.Fatalf("ListMappings error = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) = %d, want 1", len(mappings))
	}

	got, err := store.GetMapping(ctx, mappings[0].ID)
	if err != nil {
		t.Fatalf("GetMapping error = %v", err)
	}
	if got.Alias != "claude-sonnet" {
		t.Fatalf("Alias = %q, want %q", got.Alias, "claude-sonnet")
	}

	got.Alias = "claude-opus"
	got.UpstreamModel = "claude-opus-4.6"
	got.Enabled = false
	if err := store.UpdateModelMapping(ctx, got); err != nil {
		t.Fatalf("UpdateModelMapping error = %v", err)
	}

	updated, err := store.GetMapping(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetMapping after update error = %v", err)
	}
	if updated.Alias != "claude-opus" {
		t.Fatalf("Alias after update = %q, want %q", updated.Alias, "claude-opus")
	}
	if updated.UpstreamModel != "claude-opus-4.6" {
		t.Fatalf("UpstreamModel = %q, want %q", updated.UpstreamModel, "claude-opus-4.6")
	}
	if updated.Enabled {
		t.Fatalf("Enabled = %v, want false", updated.Enabled)
	}
}

func TestUpsertModelMappingRejectsUnknownStationID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	err = store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     999,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "gpt-5.1",
		Enabled:       true,
	})
	if err == nil {
		t.Fatal("UpsertModelMapping error = nil, want non-nil")
	}

	mappings, err := store.ListMappings(ctx)
	if err != nil {
		t.Fatalf("ListMappings error = %v", err)
	}
	if len(mappings) != 0 {
		t.Fatalf("len(mappings) = %d, want 0", len(mappings))
	}
}

func TestStoreUpdatesStationAndAllowsSingleProtocolConfig(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "primary-a",
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
		AnthropicBaseURL:             "",
		AnthropicAPIKey:              "",
	})
	if err != nil {
		t.Fatalf("CreateStation error = %v", err)
	}

	err = store.UpdateStation(ctx, core.Station{
		ID:                           stationID,
		Name:                         "primary-a-renamed",
		Enabled:                      false,
		Priority:                     50,
		CooldownSeconds:              45,
		HealthCheckIntervalSeconds:   20,
		HealthCheckTimeoutSeconds:    8,
		ConsecutiveFailureThreshold:  2,
		ConsecutiveRecoveryThreshold: 3,
		OpenAIBaseURL:                "",
		OpenAIAPIKey:                 "",
		AnthropicBaseURL:             "https://a.example.com/anthropic",
		AnthropicAPIKey:              "ANTHROPIC_A",
	})
	if err != nil {
		t.Fatalf("UpdateStation error = %v", err)
	}

	station, err := store.GetStation(ctx, stationID)
	if err != nil {
		t.Fatalf("GetStation error = %v", err)
	}
	if station.Name != "primary-a-renamed" {
		t.Fatalf("Name = %q, want %q", station.Name, "primary-a-renamed")
	}
	if station.Enabled {
		t.Fatalf("Enabled = %v, want false", station.Enabled)
	}
	if station.OpenAIBaseURL != "" || station.OpenAIAPIKey != "" {
		t.Fatalf("OpenAI fields = %q / %q, want empty", station.OpenAIBaseURL, station.OpenAIAPIKey)
	}
	if station.AnthropicBaseURL != "https://a.example.com/anthropic" {
		t.Fatalf("AnthropicBaseURL = %q, want %q", station.AnthropicBaseURL, "https://a.example.com/anthropic")
	}
}

func TestStoreDeletesStationConfigWithoutDeletingHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
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
		t.Fatalf("UpsertModelMapping error = %v", err)
	}
	if err := store.SaveStationStatus(ctx, core.StationStatus{
		StationID:           stationID,
		State:               "cooldown",
		ConsecutiveFailures: 1,
		LastCheckedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveStationStatus error = %v", err)
	}
	if err := store.InsertRequestLog(ctx, core.RequestLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-a",
		StatusCode:  200,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertRequestLog error = %v", err)
	}
	if err := store.InsertUpstreamErrorLog(ctx, core.UpstreamErrorLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-a",
		StatusCode:  http.StatusPaymentRequired,
		ErrorKind:   "quota_limited",
		Body:        `{"error":"quota exceeded"}`,
		Headers:     `{"cf-ray":["abc"]}`,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("InsertUpstreamErrorLog error = %v", err)
	}

	if err := store.DeleteStation(ctx, stationID); err != nil {
		t.Fatalf("DeleteStation error = %v", err)
	}

	stations, err := store.ListStations(ctx)
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 0 {
		t.Fatalf("len(stations) = %d, want 0", len(stations))
	}
	mappings, err := store.ListMappings(ctx)
	if err != nil {
		t.Fatalf("ListMappings error = %v", err)
	}
	if len(mappings) != 0 {
		t.Fatalf("len(mappings) = %d, want 0", len(mappings))
	}
	statuses, err := store.ListStationStatuses(ctx)
	if err != nil {
		t.Fatalf("ListStationStatuses error = %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("len(statuses) = %d, want 0", len(statuses))
	}
	logs, err := store.ListRequestLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(logs) != 1 || logs[0].StationName != "station-a" {
		t.Fatalf("logs after delete = %+v, want station-a history retained", logs)
	}
	upstreamErrors, err := store.ListUpstreamErrorLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListUpstreamErrorLogs error = %v", err)
	}
	if len(upstreamErrors) != 1 || upstreamErrors[0].StationName != "station-a" {
		t.Fatalf("upstream errors after delete = %+v, want station-a history retained", upstreamErrors)
	}
}

func TestStorePersistsUpstreamErrorLogs(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	first := core.UpstreamErrorLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-a",
		StatusCode:  http.StatusForbidden,
		ErrorKind:   "subscription_not_found",
		Body:        `{"code":"SUBSCRIPTION_NOT_FOUND"}`,
		Headers:     `{"request-id":["abc"]}`,
		Truncated:   false,
		CreatedAt:   time.Date(2026, 5, 26, 1, 0, 0, 0, time.UTC),
	}
	second := core.UpstreamErrorLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-b",
		StatusCode:  http.StatusPaymentRequired,
		ErrorKind:   "insufficient_balance",
		Body:        `{"error":"insufficient balance"}`,
		Headers:     `{"cf-ray":["ray"]}`,
		Truncated:   true,
		CreatedAt:   time.Date(2026, 5, 26, 2, 0, 0, 0, time.UTC),
	}
	if err := store.InsertUpstreamErrorLog(ctx, first); err != nil {
		t.Fatalf("InsertUpstreamErrorLog first error = %v", err)
	}
	if err := store.InsertUpstreamErrorLog(ctx, second); err != nil {
		t.Fatalf("InsertUpstreamErrorLog second error = %v", err)
	}

	logs, err := store.ListUpstreamErrorLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListUpstreamErrorLogs error = %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}
	if logs[0].StationName != "station-b" || logs[0].ErrorKind != "insufficient_balance" || !logs[0].Truncated {
		t.Fatalf("newest log = %+v, want station-b insufficient_balance truncated", logs[0])
	}
	if logs[1].StationName != "station-a" || logs[1].ErrorKind != "subscription_not_found" || logs[1].Truncated {
		t.Fatalf("oldest log = %+v, want station-a subscription_not_found not truncated", logs[1])
	}

	byStation, err := store.ListRecentUpstreamErrorLogsByStation(ctx, 5)
	if err != nil {
		t.Fatalf("ListRecentUpstreamErrorLogsByStation error = %v", err)
	}
	if len(byStation["station-a"]) != 1 || len(byStation["station-b"]) != 1 {
		t.Fatalf("byStation = %+v, want one log per station", byStation)
	}
}

func TestStoreDeletesSingleMapping(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
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
		t.Fatalf("UpsertModelMapping error = %v", err)
	}
	mappings, err := store.ListMappings(ctx)
	if err != nil {
		t.Fatalf("ListMappings error = %v", err)
	}

	if err := store.DeleteModelMapping(ctx, mappings[0].ID); err != nil {
		t.Fatalf("DeleteModelMapping error = %v", err)
	}

	mappings, err = store.ListMappings(ctx)
	if err != nil {
		t.Fatalf("ListMappings after delete error = %v", err)
	}
	if len(mappings) != 0 {
		t.Fatalf("len(mappings) = %d, want 0", len(mappings))
	}
	stations, err := store.ListStations(ctx)
	if err != nil {
		t.Fatalf("ListStations error = %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("len(stations) = %d, want 1", len(stations))
	}
}

func TestStoreExportsAndAppliesAuthoritativeConfigSnapshot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	localOnlyID, err := store.CreateStation(ctx, core.Station{
		Name:                         "local-only",
		Enabled:                      true,
		Priority:                     1,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://local.example.com/openai",
		OpenAIAPIKey:                 "LOCAL_OPENAI",
	})
	if err != nil {
		t.Fatalf("CreateStation local-only error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     localOnlyID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "local-model",
		UpstreamModel: "local-upstream",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping local-only error = %v", err)
	}
	stationID, err := store.CreateStation(ctx, core.Station{
		Name:                         "station-a",
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://old.example.com/openai",
		OpenAIAPIKey:                 "OLD_OPENAI",
	})
	if err != nil {
		t.Fatalf("CreateStation station-a error = %v", err)
	}
	if err := store.UpsertModelMapping(ctx, core.ModelMapping{
		StationID:     stationID,
		Protocol:      core.ProtocolOpenAI,
		Alias:         "gpt-5",
		UpstreamModel: "old-upstream",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertModelMapping station-a error = %v", err)
	}

	result, err := store.ApplyConfigSnapshot(ctx, configsync.Snapshot{
		SchemaVersion: configsync.SnapshotSchemaVersion,
		Stations: []configsync.SnapshotStation{
			{
				Name:                         "station-a",
				Enabled:                      false,
				Priority:                     100,
				CooldownSeconds:              45,
				HealthCheckIntervalSeconds:   20,
				HealthCheckTimeoutSeconds:    8,
				ConsecutiveFailureThreshold:  2,
				ConsecutiveRecoveryThreshold: 3,
				OpenAIBaseURL:                "https://new.example.com/openai",
				OpenAIAPIKey:                 "NEW_OPENAI",
				AnthropicBaseURL:             "https://new.example.com/anthropic",
				AnthropicAPIKey:              "NEW_ANTHROPIC",
			},
			{
				Name:                         "station-b",
				Enabled:                      true,
				Priority:                     50,
				CooldownSeconds:              60,
				HealthCheckIntervalSeconds:   15,
				HealthCheckTimeoutSeconds:    5,
				ConsecutiveFailureThreshold:  1,
				ConsecutiveRecoveryThreshold: 2,
				AnthropicBaseURL:             "https://b.example.com/anthropic",
				AnthropicAPIKey:              "B_ANTHROPIC",
			},
		},
		Mappings: []configsync.SnapshotMapping{
			{
				StationName:   "station-a",
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5.1",
				Enabled:       false,
			},
			{
				StationName:   "station-b",
				Protocol:      core.ProtocolAnthropic,
				Alias:         "claude-sonnet",
				UpstreamModel: "claude-sonnet-4-5",
				Enabled:       true,
			},
		},
	})
	if err != nil {
		t.Fatalf("ApplyConfigSnapshot error = %v", err)
	}
	if result.CreatedStations != 1 || result.UpdatedStations != 1 || result.DeletedStations != 1 {
		t.Fatalf("station result = %+v, want 1 created, 1 updated, 1 deleted", result)
	}
	if result.CreatedMappings != 1 || result.UpdatedMappings != 1 || result.DeletedMappings != 1 {
		t.Fatalf("mapping result = %+v, want 1 created, 1 updated, 1 deleted", result)
	}

	exported, err := store.ExportConfigSnapshot(ctx)
	if err != nil {
		t.Fatalf("ExportConfigSnapshot error = %v", err)
	}
	if len(exported.Stations) != 2 {
		t.Fatalf("len(exported.Stations) = %d, want 2", len(exported.Stations))
	}
	if len(exported.Mappings) != 2 {
		t.Fatalf("len(exported.Mappings) = %d, want 2", len(exported.Mappings))
	}
	if exported.Stations[0].Name == "local-only" || exported.Stations[1].Name == "local-only" {
		t.Fatalf("local-only station was not deleted in authoritative import: %+v", exported.Stations)
	}
	if exported.Stations[0].OpenAIAPIKey == "" && exported.Stations[1].OpenAIAPIKey == "" {
		t.Fatalf("upstream API keys were not exported: %+v", exported.Stations)
	}
	for _, mapping := range exported.Mappings {
		if mapping.StationName == "" {
			t.Fatalf("exported mapping did not include station name: %+v", mapping)
		}
		if mapping.StationName == "local-only" {
			t.Fatalf("local-only mapping was not deleted: %+v", exported.Mappings)
		}
	}
}

func TestStoreSummarizesUsageAndPrunesOldLogs(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)

	err = store.InsertRequestLog(ctx, core.RequestLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-a",
		StatusCode:  500,
		DurationMS:  123,
		ErrorKind:   "upstream_error",
		CreatedAt:   cutoff.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertRequestLog old error = %v", err)
	}

	err = store.InsertRequestLog(ctx, core.RequestLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-b",
		StatusCode:  200,
		DurationMS:  45,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("InsertRequestLog current error = %v", err)
	}
	if err := store.InsertUpstreamErrorLog(ctx, core.UpstreamErrorLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-a",
		StatusCode:  http.StatusPaymentRequired,
		ErrorKind:   "quota_limited",
		Body:        `{"error":"old quota"}`,
		CreatedAt:   cutoff.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("InsertUpstreamErrorLog old error = %v", err)
	}
	if err := store.InsertUpstreamErrorLog(ctx, core.UpstreamErrorLog{
		Protocol:    core.ProtocolOpenAI,
		Alias:       "gpt-5",
		StationName: "station-b",
		StatusCode:  http.StatusForbidden,
		ErrorKind:   "subscription_not_found",
		Body:        `{"error":"current subscription"}`,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("InsertUpstreamErrorLog current error = %v", err)
	}

	err = store.DeleteRequestLogsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteRequestLogsBefore error = %v", err)
	}
	err = store.DeleteUpstreamErrorLogsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteUpstreamErrorLogsBefore error = %v", err)
	}

	rows, err := store.UsageByStation(ctx)
	if err != nil {
		t.Fatalf("UsageByStation error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].StationName != "station-b" {
		t.Fatalf("StationName = %q, want %q", rows[0].StationName, "station-b")
	}
	if rows[0].Alias != "gpt-5" {
		t.Fatalf("Alias = %q, want %q", rows[0].Alias, "gpt-5")
	}
	if rows[0].RequestCount != 1 {
		t.Fatalf("RequestCount = %d, want 1", rows[0].RequestCount)
	}
	if rows[0].ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0", rows[0].ErrorCount)
	}
	upstreamErrors, err := store.ListUpstreamErrorLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListUpstreamErrorLogs error = %v", err)
	}
	if len(upstreamErrors) != 1 {
		t.Fatalf("len(upstreamErrors) = %d, want 1", len(upstreamErrors))
	}
	if upstreamErrors[0].StationName != "station-b" {
		t.Fatalf("upstream error station = %q, want station-b", upstreamErrors[0].StationName)
	}
}

func TestStorePersistsTokenUsageAndDailySummary(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	day := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	entries := []core.RequestLog{
		{
			Protocol:     core.ProtocolOpenAI,
			Alias:        "gpt-5",
			StationName:  "station-a",
			StatusCode:   http.StatusOK,
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
			CreatedAt:    day,
		},
		{
			Protocol:     core.ProtocolOpenAI,
			Alias:        "gpt-5",
			StationName:  "station-a",
			StatusCode:   http.StatusOK,
			InputTokens:  20,
			OutputTokens: 10,
			TotalTokens:  30,
			CreatedAt:    day.Add(2 * time.Hour),
		},
		{
			Protocol:     core.ProtocolAnthropic,
			Alias:        "claude-sonnet",
			StationName:  "station-b",
			StatusCode:   http.StatusOK,
			InputTokens:  7,
			OutputTokens: 3,
			TotalTokens:  10,
			CreatedAt:    day.Add(24 * time.Hour),
		},
	}
	for _, entry := range entries {
		if err := store.InsertRequestLog(ctx, entry); err != nil {
			t.Fatalf("InsertRequestLog error = %v", err)
		}
	}

	logs, err := store.ListRequestLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if logs[0].InputTokens != 7 || logs[0].OutputTokens != 3 || logs[0].TotalTokens != 10 {
		t.Fatalf("newest log tokens = %d/%d/%d, want 7/3/10", logs[0].InputTokens, logs[0].OutputTokens, logs[0].TotalTokens)
	}

	rows, err := store.DailyTokenUsage(ctx, 10)
	if err != nil {
		t.Fatalf("DailyTokenUsage error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Day != "2026-05-27" || rows[0].Protocol != core.ProtocolAnthropic || rows[0].Alias != "claude-sonnet" {
		t.Fatalf("first row identity = %+v, want 2026-05-27 anthropic claude-sonnet", rows[0])
	}
	if rows[0].RequestCount != 1 || rows[0].InputTokens != 7 || rows[0].OutputTokens != 3 || rows[0].TotalTokens != 10 {
		t.Fatalf("first row totals = %+v, want one request and 7/3/10 tokens", rows[0])
	}
	if rows[1].Day != "2026-05-26" || rows[1].StationName != "station-a" || rows[1].RequestCount != 2 {
		t.Fatalf("second row identity = %+v, want 2026-05-26 station-a two requests", rows[1])
	}
	if rows[1].InputTokens != 30 || rows[1].OutputTokens != 15 || rows[1].TotalTokens != 45 {
		t.Fatalf("second row totals = %+v, want 30/15/45 tokens", rows[1])
	}
}

func TestStoreMigratesRequestLogTokenColumns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
        CREATE TABLE request_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            protocol TEXT NOT NULL,
            alias TEXT NOT NULL,
            station_name TEXT NOT NULL,
            status_code INTEGER NOT NULL,
            duration_ms INTEGER NOT NULL,
            was_stream INTEGER NOT NULL,
            did_failover INTEGER NOT NULL,
            error_kind TEXT NOT NULL,
            created_at TEXT NOT NULL
        )
    `); err != nil {
		t.Fatalf("create old request_logs schema error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close error = %v", err)
	}

	store, err := sqlitestore.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	if err := store.InsertRequestLog(ctx, core.RequestLog{
		Protocol:     core.ProtocolOpenAI,
		Alias:        "gpt-5",
		StationName:  "station-a",
		StatusCode:   http.StatusOK,
		InputTokens:  1,
		OutputTokens: 2,
		TotalTokens:  3,
		CreatedAt:    time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertRequestLog after migration error = %v", err)
	}
	logs, err := store.ListRequestLogs(ctx, 1)
	if err != nil {
		t.Fatalf("ListRequestLogs error = %v", err)
	}
	if len(logs) != 1 || logs[0].TotalTokens != 3 {
		t.Fatalf("logs after migration = %+v, want migrated token columns", logs)
	}
}
