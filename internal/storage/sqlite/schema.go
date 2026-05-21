package sqlite

const schemaSQL = `
CREATE TABLE IF NOT EXISTS stations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL,
    priority INTEGER NOT NULL,
    cooldown_seconds INTEGER NOT NULL,
    health_check_interval_seconds INTEGER NOT NULL,
    health_check_timeout_seconds INTEGER NOT NULL,
    consecutive_failure_threshold INTEGER NOT NULL,
    consecutive_recovery_threshold INTEGER NOT NULL,
    openai_base_url TEXT NOT NULL DEFAULT '',
    openai_api_key TEXT NOT NULL DEFAULT '',
    anthropic_base_url TEXT NOT NULL DEFAULT '',
    anthropic_api_key TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS model_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id INTEGER NOT NULL,
    protocol TEXT NOT NULL,
    alias TEXT NOT NULL,
    upstream_model TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    UNIQUE(station_id, protocol, alias)
);

CREATE TABLE IF NOT EXISTS station_statuses (
    station_id INTEGER PRIMARY KEY,
    state TEXT NOT NULL,
    cooldown_until TEXT NOT NULL,
    consecutive_failures INTEGER NOT NULL,
    consecutive_recoveries INTEGER NOT NULL,
    last_error TEXT NOT NULL,
    last_checked_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS request_logs (
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
);

CREATE TABLE IF NOT EXISTS failover_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    protocol TEXT NOT NULL,
    alias TEXT NOT NULL,
    from_station_name TEXT NOT NULL,
    to_station_name TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`
