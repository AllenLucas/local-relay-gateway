package configsync_test

import (
	"strings"
	"testing"

	"relay-gateway/internal/configsync"
	"relay-gateway/internal/core"
)

func TestSnapshotRoundTripPreservesUpstreamKeysAndExcludesLocalRuntime(t *testing.T) {
	snapshot := configsync.Snapshot{
		SchemaVersion: configsync.SnapshotSchemaVersion,
		DeviceName:    "dev-machine",
		Stations: []configsync.SnapshotStation{
			{
				Name:                         "station-a",
				Enabled:                      true,
				Priority:                     100,
				CooldownSeconds:              30,
				HealthCheckIntervalSeconds:   15,
				HealthCheckTimeoutSeconds:    5,
				ConsecutiveFailureThreshold:  1,
				ConsecutiveRecoveryThreshold: 2,
				OpenAIBaseURL:                "https://a.example.com/openai",
				OpenAIAPIKey:                 "OPENAI_A",
				AnthropicBaseURL:             "https://a.example.com/anthropic",
				AnthropicAPIKey:              "ANTHROPIC_A",
			},
		},
		Mappings: []configsync.SnapshotMapping{
			{
				StationName:   "station-a",
				Protocol:      core.ProtocolOpenAI,
				Alias:         "gpt-5",
				UpstreamModel: "gpt-5.1",
				Enabled:       true,
			},
		},
	}

	body, err := configsync.EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("EncodeSnapshot error = %v", err)
	}
	text := string(body)
	for _, want := range []string{"OPENAI_A", "ANTHROPIC_A", "dev-machine"} {
		if !strings.Contains(text, want) {
			t.Fatalf("snapshot JSON did not contain %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"local_api_key", "runtime_file", "listen_addr", "db_path"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snapshot JSON contained runtime field %q: %s", forbidden, text)
		}
	}

	decoded, err := configsync.DecodeSnapshot(body)
	if err != nil {
		t.Fatalf("DecodeSnapshot error = %v", err)
	}
	if decoded.Stations[0].OpenAIAPIKey != "OPENAI_A" {
		t.Fatalf("OpenAIAPIKey = %q, want OPENAI_A", decoded.Stations[0].OpenAIAPIKey)
	}
	if decoded.Mappings[0].StationName != "station-a" {
		t.Fatalf("StationName = %q, want station-a", decoded.Mappings[0].StationName)
	}
}

func TestValidateSnapshotRejectsAmbiguousIdentities(t *testing.T) {
	testCases := []struct {
		name     string
		snapshot configsync.Snapshot
		want     string
	}{
		{
			name: "duplicate station name",
			snapshot: configsync.Snapshot{
				SchemaVersion: configsync.SnapshotSchemaVersion,
				Stations: []configsync.SnapshotStation{
					validSnapshotStation("station-a"),
					validSnapshotStation("station-a"),
				},
			},
			want: "duplicate station",
		},
		{
			name: "mapping references missing station",
			snapshot: configsync.Snapshot{
				SchemaVersion: configsync.SnapshotSchemaVersion,
				Stations:      []configsync.SnapshotStation{validSnapshotStation("station-a")},
				Mappings: []configsync.SnapshotMapping{
					{
						StationName:   "missing",
						Protocol:      core.ProtocolOpenAI,
						Alias:         "gpt-5",
						UpstreamModel: "gpt-5.1",
						Enabled:       true,
					},
				},
			},
			want: "unknown station",
		},
		{
			name: "duplicate mapping identity",
			snapshot: configsync.Snapshot{
				SchemaVersion: configsync.SnapshotSchemaVersion,
				Stations:      []configsync.SnapshotStation{validSnapshotStation("station-a")},
				Mappings: []configsync.SnapshotMapping{
					validSnapshotMapping("station-a", core.ProtocolOpenAI, "gpt-5"),
					validSnapshotMapping("station-a", core.ProtocolOpenAI, "gpt-5"),
				},
			},
			want: "duplicate mapping",
		},
		{
			name: "same alias across protocols is allowed",
			snapshot: configsync.Snapshot{
				SchemaVersion: configsync.SnapshotSchemaVersion,
				Stations:      []configsync.SnapshotStation{validSnapshotStation("station-a")},
				Mappings: []configsync.SnapshotMapping{
					validSnapshotMapping("station-a", core.ProtocolOpenAI, "shared"),
					validSnapshotMapping("station-a", core.ProtocolAnthropic, "shared"),
				},
			},
			want: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := configsync.ValidateSnapshot(tc.snapshot)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateSnapshot error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateSnapshot error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateSnapshot error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func validSnapshotStation(name string) configsync.SnapshotStation {
	return configsync.SnapshotStation{
		Name:                         name,
		Enabled:                      true,
		Priority:                     10,
		CooldownSeconds:              30,
		HealthCheckIntervalSeconds:   15,
		HealthCheckTimeoutSeconds:    5,
		ConsecutiveFailureThreshold:  1,
		ConsecutiveRecoveryThreshold: 2,
		OpenAIBaseURL:                "https://a.example.com/openai",
		OpenAIAPIKey:                 "OPENAI_A",
	}
}

func validSnapshotMapping(stationName string, protocol core.Protocol, alias string) configsync.SnapshotMapping {
	return configsync.SnapshotMapping{
		StationName:   stationName,
		Protocol:      protocol,
		Alias:         alias,
		UpstreamModel: alias + "-upstream",
		Enabled:       true,
	}
}
