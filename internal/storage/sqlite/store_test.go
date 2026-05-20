package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

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
}
