package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"relay-gateway/internal/core"
	sqlitestore "relay-gateway/internal/storage/sqlite"
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

	err = store.DeleteRequestLogsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteRequestLogsBefore error = %v", err)
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
}
