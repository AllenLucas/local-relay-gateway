package configsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"relay-gateway/internal/core"
)

const SnapshotSchemaVersion = 1

type Snapshot struct {
	SchemaVersion int               `json:"schema_version"`
	ExportedAt    time.Time         `json:"exported_at,omitempty"`
	DeviceName    string            `json:"device_name,omitempty"`
	Stations      []SnapshotStation `json:"stations"`
	Mappings      []SnapshotMapping `json:"mappings"`
}

type SnapshotStation struct {
	Name                         string `json:"name"`
	Enabled                      bool   `json:"enabled"`
	Priority                     int    `json:"priority"`
	CooldownSeconds              int    `json:"cooldown_seconds"`
	HealthCheckIntervalSeconds   int    `json:"health_check_interval_seconds"`
	HealthCheckTimeoutSeconds    int    `json:"health_check_timeout_seconds"`
	ConsecutiveFailureThreshold  int    `json:"consecutive_failure_threshold"`
	ConsecutiveRecoveryThreshold int    `json:"consecutive_recovery_threshold"`
	OpenAIBaseURL                string `json:"openai_base_url,omitempty"`
	OpenAIAPIKey                 string `json:"openai_api_key,omitempty"`
	AnthropicBaseURL             string `json:"anthropic_base_url,omitempty"`
	AnthropicAPIKey              string `json:"anthropic_api_key,omitempty"`
}

type SnapshotMapping struct {
	StationName   string        `json:"station_name"`
	Protocol      core.Protocol `json:"protocol"`
	Alias         string        `json:"alias"`
	UpstreamModel string        `json:"upstream_model"`
	Enabled       bool          `json:"enabled"`
}

type ApplyResult struct {
	CreatedStations int
	UpdatedStations int
	DeletedStations int
	CreatedMappings int
	UpdatedMappings int
	DeletedMappings int
}

func EncodeSnapshot(snapshot Snapshot) ([]byte, error) {
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = SnapshotSchemaVersion
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func DecodeSnapshot(body []byte) (Snapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func ValidateSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("unsupported snapshot schema_version: %d", snapshot.SchemaVersion)
	}

	stationNames := make(map[string]struct{}, len(snapshot.Stations))
	for _, station := range snapshot.Stations {
		name := strings.TrimSpace(station.Name)
		if name == "" {
			return errors.New("station name is empty")
		}
		if _, ok := stationNames[name]; ok {
			return fmt.Errorf("duplicate station name: %s", name)
		}
		if err := validateStationProtocolPairs(station); err != nil {
			return fmt.Errorf("station %s: %w", name, err)
		}
		stationNames[name] = struct{}{}
	}

	mappingKeys := make(map[string]struct{}, len(snapshot.Mappings))
	for _, mapping := range snapshot.Mappings {
		stationName := strings.TrimSpace(mapping.StationName)
		if stationName == "" {
			return errors.New("mapping station_name is empty")
		}
		if _, ok := stationNames[stationName]; !ok {
			return fmt.Errorf("mapping references unknown station: %s", stationName)
		}
		if !supportedProtocol(mapping.Protocol) {
			return fmt.Errorf("mapping %s/%s uses unsupported protocol: %s", stationName, mapping.Alias, mapping.Protocol)
		}
		alias := strings.TrimSpace(mapping.Alias)
		if alias == "" {
			return errors.New("mapping alias is empty")
		}
		if strings.TrimSpace(mapping.UpstreamModel) == "" {
			return errors.New("mapping upstream_model is empty")
		}
		key := mappingKey(stationName, mapping.Protocol, alias)
		if _, ok := mappingKeys[key]; ok {
			return fmt.Errorf("duplicate mapping identity: %s", key)
		}
		mappingKeys[key] = struct{}{}
	}
	return nil
}

func mappingKey(stationName string, protocol core.Protocol, alias string) string {
	return stationName + "\x00" + string(protocol) + "\x00" + alias
}

func supportedProtocol(protocol core.Protocol) bool {
	return protocol == core.ProtocolOpenAI || protocol == core.ProtocolAnthropic
}

func validateStationProtocolPairs(station SnapshotStation) error {
	hasOpenAIBase := strings.TrimSpace(station.OpenAIBaseURL) != ""
	hasOpenAIKey := strings.TrimSpace(station.OpenAIAPIKey) != ""
	if hasOpenAIBase != hasOpenAIKey {
		return errors.New("openai base url and api key must be provided together")
	}

	hasAnthropicBase := strings.TrimSpace(station.AnthropicBaseURL) != ""
	hasAnthropicKey := strings.TrimSpace(station.AnthropicAPIKey) != ""
	if hasAnthropicBase != hasAnthropicKey {
		return errors.New("anthropic base url and api key must be provided together")
	}

	if !hasOpenAIBase && !hasAnthropicBase {
		return errors.New("at least one protocol must be configured")
	}
	return nil
}
