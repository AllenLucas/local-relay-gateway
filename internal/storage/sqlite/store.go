package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"relay-gateway/internal/core"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateStation(ctx context.Context, station core.Station) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO stations (
            name, enabled, priority, cooldown_seconds,
            health_check_interval_seconds, health_check_timeout_seconds,
            consecutive_failure_threshold, consecutive_recovery_threshold,
            openai_base_url, openai_api_key, anthropic_base_url, anthropic_api_key
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		station.Name,
		boolToInt(station.Enabled),
		station.Priority,
		station.CooldownSeconds,
		station.HealthCheckIntervalSeconds,
		station.HealthCheckTimeoutSeconds,
		station.ConsecutiveFailureThreshold,
		station.ConsecutiveRecoveryThreshold,
		station.OpenAIBaseURL,
		station.OpenAIAPIKey,
		station.AnthropicBaseURL,
		station.AnthropicAPIKey,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) ListStations(ctx context.Context) ([]core.Station, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, enabled, priority, cooldown_seconds,
               health_check_interval_seconds, health_check_timeout_seconds,
               consecutive_failure_threshold, consecutive_recovery_threshold,
               openai_base_url, openai_api_key, anthropic_base_url, anthropic_api_key
        FROM stations
        ORDER BY priority DESC, id ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stations []core.Station
	for rows.Next() {
		var station core.Station
		var enabled int
		if err := rows.Scan(
			&station.ID,
			&station.Name,
			&enabled,
			&station.Priority,
			&station.CooldownSeconds,
			&station.HealthCheckIntervalSeconds,
			&station.HealthCheckTimeoutSeconds,
			&station.ConsecutiveFailureThreshold,
			&station.ConsecutiveRecoveryThreshold,
			&station.OpenAIBaseURL,
			&station.OpenAIAPIKey,
			&station.AnthropicBaseURL,
			&station.AnthropicAPIKey,
		); err != nil {
			return nil, err
		}
		station.Enabled = enabled == 1
		stations = append(stations, station)
	}
	return stations, rows.Err()
}

func (s *Store) UpsertModelMapping(ctx context.Context, mapping core.ModelMapping) error {
	if !isSupportedProtocol(mapping.Protocol) {
		return fmt.Errorf("unsupported protocol: %q", mapping.Protocol)
	}

	_, err := s.db.ExecContext(ctx, `
        INSERT INTO model_mappings (station_id, protocol, alias, upstream_model, enabled)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(station_id, protocol, alias)
        DO UPDATE SET upstream_model = excluded.upstream_model, enabled = excluded.enabled
    `,
		mapping.StationID,
		string(mapping.Protocol),
		mapping.Alias,
		mapping.UpstreamModel,
		boolToInt(mapping.Enabled),
	)
	return err
}

func (s *Store) FindMappings(ctx context.Context, protocol core.Protocol, alias string) ([]core.ModelMapping, error) {
	if !isSupportedProtocol(protocol) {
		return nil, fmt.Errorf("unsupported protocol: %q", protocol)
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, station_id, protocol, alias, upstream_model, enabled
        FROM model_mappings
        WHERE protocol = ? AND alias = ?
    `, string(protocol), alias)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []core.ModelMapping
	for rows.Next() {
		var mapping core.ModelMapping
		var enabled int
		var protocolValue string
		if err := rows.Scan(
			&mapping.ID,
			&mapping.StationID,
			&protocolValue,
			&mapping.Alias,
			&mapping.UpstreamModel,
			&enabled,
		); err != nil {
			return nil, err
		}
		mapping.Protocol = core.Protocol(protocolValue)
		mapping.Enabled = enabled == 1
		mappings = append(mappings, mapping)
	}
	return mappings, rows.Err()
}

func (s *Store) SaveStationStatus(ctx context.Context, status core.StationStatus) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO station_statuses (
            station_id, state, cooldown_until, consecutive_failures,
            consecutive_recoveries, last_error, last_checked_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(station_id) DO UPDATE SET
            state = excluded.state,
            cooldown_until = excluded.cooldown_until,
            consecutive_failures = excluded.consecutive_failures,
            consecutive_recoveries = excluded.consecutive_recoveries,
            last_error = excluded.last_error,
            last_checked_at = excluded.last_checked_at
    `,
		status.StationID,
		status.State,
		status.CooldownUntil.Format(time.RFC3339),
		status.ConsecutiveFailures,
		status.ConsecutiveRecoveries,
		status.LastError,
		status.LastCheckedAt.Format(time.RFC3339),
	)
	return err
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isSupportedProtocol(protocol core.Protocol) bool {
	switch protocol {
	case core.ProtocolOpenAI, core.ProtocolAnthropic:
		return true
	default:
		return false
	}
}
