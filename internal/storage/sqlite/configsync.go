package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"relay-gateway/internal/configsync"
	"relay-gateway/internal/core"
)

func (s *Store) ExportConfigSnapshot(ctx context.Context) (configsync.Snapshot, error) {
	stations, err := s.ListStations(ctx)
	if err != nil {
		return configsync.Snapshot{}, err
	}
	mappings, err := s.ListMappings(ctx)
	if err != nil {
		return configsync.Snapshot{}, err
	}

	stationNameByID := make(map[int64]string, len(stations))
	snapshot := configsync.Snapshot{
		SchemaVersion: configsync.SnapshotSchemaVersion,
		Stations:      make([]configsync.SnapshotStation, 0, len(stations)),
		Mappings:      make([]configsync.SnapshotMapping, 0, len(mappings)),
	}
	for _, station := range stations {
		stationNameByID[station.ID] = station.Name
		snapshot.Stations = append(snapshot.Stations, configsync.SnapshotStation{
			Name:                         station.Name,
			Enabled:                      station.Enabled,
			Priority:                     station.Priority,
			CooldownSeconds:              station.CooldownSeconds,
			HealthCheckIntervalSeconds:   station.HealthCheckIntervalSeconds,
			HealthCheckTimeoutSeconds:    station.HealthCheckTimeoutSeconds,
			ConsecutiveFailureThreshold:  station.ConsecutiveFailureThreshold,
			ConsecutiveRecoveryThreshold: station.ConsecutiveRecoveryThreshold,
			OpenAIBaseURL:                station.OpenAIBaseURL,
			OpenAIAPIKey:                 station.OpenAIAPIKey,
			AnthropicBaseURL:             station.AnthropicBaseURL,
			AnthropicAPIKey:              station.AnthropicAPIKey,
		})
	}
	for _, mapping := range mappings {
		stationName := stationNameByID[mapping.StationID]
		if stationName == "" {
			continue
		}
		snapshot.Mappings = append(snapshot.Mappings, configsync.SnapshotMapping{
			StationName:   stationName,
			Protocol:      mapping.Protocol,
			Alias:         mapping.Alias,
			UpstreamModel: mapping.UpstreamModel,
			Enabled:       mapping.Enabled,
		})
	}
	return snapshot, nil
}

func (s *Store) ApplyConfigSnapshot(ctx context.Context, snapshot configsync.Snapshot) (result configsync.ApplyResult, err error) {
	if err := configsync.ValidateSnapshot(snapshot); err != nil {
		return configsync.ApplyResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return configsync.ApplyResult{}, err
	}
	defer rollbackUnlessCommitted(tx)

	existingStations, err := listStationsTx(ctx, tx)
	if err != nil {
		return configsync.ApplyResult{}, err
	}
	existingByName := make(map[string]core.Station, len(existingStations))
	for _, station := range existingStations {
		existingByName[station.Name] = station
	}

	remoteNames := make(map[string]struct{}, len(snapshot.Stations))
	stationIDByName := make(map[string]int64, len(snapshot.Stations))
	for _, station := range snapshot.Stations {
		remoteNames[station.Name] = struct{}{}
		coreStation := core.Station{
			Name:                         station.Name,
			Enabled:                      station.Enabled,
			Priority:                     station.Priority,
			CooldownSeconds:              station.CooldownSeconds,
			HealthCheckIntervalSeconds:   station.HealthCheckIntervalSeconds,
			HealthCheckTimeoutSeconds:    station.HealthCheckTimeoutSeconds,
			ConsecutiveFailureThreshold:  station.ConsecutiveFailureThreshold,
			ConsecutiveRecoveryThreshold: station.ConsecutiveRecoveryThreshold,
			OpenAIBaseURL:                station.OpenAIBaseURL,
			OpenAIAPIKey:                 station.OpenAIAPIKey,
			AnthropicBaseURL:             station.AnthropicBaseURL,
			AnthropicAPIKey:              station.AnthropicAPIKey,
		}
		if existing, ok := existingByName[station.Name]; ok {
			coreStation.ID = existing.ID
			if err = updateStationTx(ctx, tx, coreStation); err != nil {
				return configsync.ApplyResult{}, err
			}
			result.UpdatedStations++
			stationIDByName[station.Name] = existing.ID
			continue
		}
		stationID, createErr := createStationTx(ctx, tx, coreStation)
		if createErr != nil {
			err = createErr
			return configsync.ApplyResult{}, err
		}
		result.CreatedStations++
		stationIDByName[station.Name] = stationID
	}

	for _, station := range existingStations {
		if _, ok := remoteNames[station.Name]; ok {
			continue
		}
		deletedMappings, deleteErr := deleteMappingsForStationTx(ctx, tx, station.ID)
		if deleteErr != nil {
			err = deleteErr
			return configsync.ApplyResult{}, err
		}
		result.DeletedMappings += deletedMappings
		if _, err = tx.ExecContext(ctx, `DELETE FROM station_statuses WHERE station_id = ?`, station.ID); err != nil {
			return configsync.ApplyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM stations WHERE id = ?`, station.ID); err != nil {
			return configsync.ApplyResult{}, err
		}
		result.DeletedStations++
	}

	existingMappings, err := listMappingsTx(ctx, tx)
	if err != nil {
		return configsync.ApplyResult{}, err
	}
	activeStationNameByID := make(map[int64]string, len(stationIDByName))
	for name, stationID := range stationIDByName {
		activeStationNameByID[stationID] = name
	}
	existingMappingByKey := make(map[string]core.ModelMapping, len(existingMappings))
	for _, mapping := range existingMappings {
		stationName := activeStationNameByID[mapping.StationID]
		if stationName == "" {
			continue
		}
		existingMappingByKey[configMappingKey(stationName, mapping.Protocol, mapping.Alias)] = mapping
	}

	remoteMappingKeys := make(map[string]struct{}, len(snapshot.Mappings))
	for _, mapping := range snapshot.Mappings {
		stationID := stationIDByName[mapping.StationName]
		key := configMappingKey(mapping.StationName, mapping.Protocol, mapping.Alias)
		remoteMappingKeys[key] = struct{}{}
		coreMapping := core.ModelMapping{
			StationID:     stationID,
			Protocol:      mapping.Protocol,
			Alias:         mapping.Alias,
			UpstreamModel: mapping.UpstreamModel,
			Enabled:       mapping.Enabled,
		}
		if existing, ok := existingMappingByKey[key]; ok {
			coreMapping.ID = existing.ID
			if err = updateMappingTx(ctx, tx, coreMapping); err != nil {
				return configsync.ApplyResult{}, err
			}
			result.UpdatedMappings++
			continue
		}
		if err = upsertMappingTx(ctx, tx, coreMapping); err != nil {
			return configsync.ApplyResult{}, err
		}
		result.CreatedMappings++
	}

	for key, mapping := range existingMappingByKey {
		if _, ok := remoteMappingKeys[key]; ok {
			continue
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM model_mappings WHERE id = ?`, mapping.ID); err != nil {
			return configsync.ApplyResult{}, err
		}
		result.DeletedMappings++
	}

	err = tx.Commit()
	return result, err
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	_ = tx.Rollback()
}

func createStationTx(ctx context.Context, tx *sql.Tx, station core.Station) (int64, error) {
	result, err := tx.ExecContext(ctx, `
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

func updateStationTx(ctx context.Context, tx *sql.Tx, station core.Station) error {
	result, err := tx.ExecContext(ctx, `
        UPDATE stations
        SET name = ?, enabled = ?, priority = ?, cooldown_seconds = ?,
            health_check_interval_seconds = ?, health_check_timeout_seconds = ?,
            consecutive_failure_threshold = ?, consecutive_recovery_threshold = ?,
            openai_base_url = ?, openai_api_key = ?, anthropic_base_url = ?, anthropic_api_key = ?
        WHERE id = ?
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
		station.ID,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func listStationsTx(ctx context.Context, tx *sql.Tx) ([]core.Station, error) {
	rows, err := tx.QueryContext(ctx, `
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

func upsertMappingTx(ctx context.Context, tx *sql.Tx, mapping core.ModelMapping) error {
	if !isSupportedProtocol(mapping.Protocol) {
		return fmt.Errorf("unsupported protocol: %q", mapping.Protocol)
	}
	_, err := tx.ExecContext(ctx, `
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

func updateMappingTx(ctx context.Context, tx *sql.Tx, mapping core.ModelMapping) error {
	if !isSupportedProtocol(mapping.Protocol) {
		return fmt.Errorf("unsupported protocol: %q", mapping.Protocol)
	}
	result, err := tx.ExecContext(ctx, `
        UPDATE model_mappings
        SET station_id = ?, protocol = ?, alias = ?, upstream_model = ?, enabled = ?
        WHERE id = ?
    `,
		mapping.StationID,
		string(mapping.Protocol),
		mapping.Alias,
		mapping.UpstreamModel,
		boolToInt(mapping.Enabled),
		mapping.ID,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func listMappingsTx(ctx context.Context, tx *sql.Tx) ([]core.ModelMapping, error) {
	rows, err := tx.QueryContext(ctx, `
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

func deleteMappingsForStationTx(ctx context.Context, tx *sql.Tx, stationID int64) (int, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM model_mappings WHERE station_id = ?`, stationID)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rowsAffected), nil
}

func configMappingKey(stationName string, protocol core.Protocol, alias string) string {
	return stationName + "\x00" + string(protocol) + "\x00" + alias
}
