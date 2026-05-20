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
	exists, err := s.stationExists(ctx, mapping.StationID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("unknown station_id: %d", mapping.StationID)
	}

	_, err = s.db.ExecContext(ctx, `
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

func (s *Store) ListMappings(ctx context.Context) ([]core.ModelMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, station_id, protocol, alias, upstream_model, enabled
        FROM model_mappings
        ORDER BY alias ASC, protocol ASC, station_id ASC
    `)
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

func (s *Store) ListStationStatuses(ctx context.Context) (map[int64]core.StationStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT station_id, state, cooldown_until, consecutive_failures,
               consecutive_recoveries, last_error, last_checked_at
        FROM station_statuses
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := make(map[int64]core.StationStatus)
	for rows.Next() {
		var status core.StationStatus
		var cooldownUntil string
		var lastCheckedAt string
		if err := rows.Scan(
			&status.StationID,
			&status.State,
			&cooldownUntil,
			&status.ConsecutiveFailures,
			&status.ConsecutiveRecoveries,
			&status.LastError,
			&lastCheckedAt,
		); err != nil {
			return nil, err
		}
		if cooldownUntil != "" {
			status.CooldownUntil, _ = time.Parse(time.RFC3339, cooldownUntil)
		}
		if lastCheckedAt != "" {
			status.LastCheckedAt, _ = time.Parse(time.RFC3339, lastCheckedAt)
		}
		statuses[status.StationID] = status
	}
	return statuses, rows.Err()
}

func (s *Store) InsertRequestLog(ctx context.Context, entry core.RequestLog) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO request_logs (
            protocol, alias, station_name, status_code, duration_ms,
            was_stream, did_failover, error_kind, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		string(entry.Protocol),
		entry.Alias,
		entry.StationName,
		entry.StatusCode,
		entry.DurationMS,
		boolToInt(entry.WasStream),
		boolToInt(entry.DidFailover),
		entry.ErrorKind,
		entry.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *Store) DeleteRequestLogsBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM request_logs WHERE created_at < ?`, cutoff.Format(time.RFC3339))
	return err
}

func (s *Store) ListRequestLogs(ctx context.Context, limit int) ([]core.RequestLog, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, protocol, alias, station_name, status_code, duration_ms,
               was_stream, did_failover, error_kind, created_at
        FROM request_logs
        ORDER BY created_at DESC, id DESC
        LIMIT ?
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []core.RequestLog
	for rows.Next() {
		var item core.RequestLog
		var protocolValue string
		var wasStream int
		var didFailover int
		var createdAt string
		if err := rows.Scan(
			&item.ID,
			&protocolValue,
			&item.Alias,
			&item.StationName,
			&item.StatusCode,
			&item.DurationMS,
			&wasStream,
			&didFailover,
			&item.ErrorKind,
			&createdAt,
		); err != nil {
			return nil, err
		}
		item.Protocol = core.Protocol(protocolValue)
		item.WasStream = wasStream == 1
		item.DidFailover = didFailover == 1
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func (s *Store) InsertFailoverEvent(ctx context.Context, event core.FailoverEvent) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO failover_events (
            protocol, alias, from_station_name, to_station_name, reason, created_at
        ) VALUES (?, ?, ?, ?, ?, ?)
    `,
		string(event.Protocol),
		event.Alias,
		event.FromStationName,
		event.ToStationName,
		event.Reason,
		event.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *Store) ListFailoverEvents(ctx context.Context, limit int) ([]core.FailoverEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, protocol, alias, from_station_name, to_station_name, reason, created_at
        FROM failover_events
        ORDER BY created_at DESC, id DESC
        LIMIT ?
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []core.FailoverEvent
	for rows.Next() {
		var item core.FailoverEvent
		var protocolValue string
		var createdAt string
		if err := rows.Scan(
			&item.ID,
			&protocolValue,
			&item.Alias,
			&item.FromStationName,
			&item.ToStationName,
			&item.Reason,
			&createdAt,
		); err != nil {
			return nil, err
		}
		item.Protocol = core.Protocol(protocolValue)
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		events = append(events, item)
	}
	return events, rows.Err()
}

func (s *Store) UsageByStation(ctx context.Context) ([]core.UsageRow, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT station_name, alias, COUNT(*) AS request_count,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS error_count
        FROM request_logs
        GROUP BY station_name, alias
        ORDER BY request_count DESC, station_name ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.UsageRow
	for rows.Next() {
		var row core.UsageRow
		if err := rows.Scan(&row.StationName, &row.Alias, &row.RequestCount, &row.ErrorCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) UsageByAlias(ctx context.Context) ([]core.UsageRow, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT station_name, alias, COUNT(*) AS request_count,
               SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS error_count
        FROM request_logs
        GROUP BY alias, station_name
        ORDER BY alias ASC, request_count DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.UsageRow
	for rows.Next() {
		var row core.UsageRow
		if err := rows.Scan(&row.StationName, &row.Alias, &row.RequestCount, &row.ErrorCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
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

func (s *Store) stationExists(ctx context.Context, stationID int64) (bool, error) {
	if stationID <= 0 {
		return false, nil
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM stations WHERE id = ?`, stationID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
