package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

type migration struct {
	version int
	apply   func(*sql.Tx) error
}

var sqliteMigrations = []migration{
	{version: 1, apply: createRequestLogSchema},
	{version: 2, apply: addTraceObservabilitySchema},
	{version: 3, apply: addHTTPContextSchema},
	{version: 4, apply: addPayloadCaptureMetadataSchema},
}

func (s *SQLite) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
version INTEGER PRIMARY KEY,
applied_at DATETIME NOT NULL
);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedMigrationVersions(s.db)
	if err != nil {
		return err
	}
	migrations := append([]migration(nil), sqliteMigrations...)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for _, item := range migrations {
		if applied[item.version] {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", item.version, err)
		}
		if err := item.apply(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", item.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, item.version, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", item.version, err)
		}
	}
	return nil
}

func appliedMigrationVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read schema migrations: %w", err)
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema migrations: %w", err)
	}
	return applied, nil
}

func createRequestLogSchema(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS request_logs (
request_id TEXT PRIMARY KEY,
client_name TEXT,
virtual_model TEXT,
upstream_model TEXT,
channel_id TEXT,
protocol_in TEXT,
protocol_out TEXT,
started_at DATETIME,
first_token_at DATETIME,
completed_at DATETIME,
ttft_ms INTEGER,
tpot_ms REAL,
tps REAL,
status_code INTEGER,
error_code TEXT,
prompt_tokens INTEGER,
completion_tokens INTEGER,
total_tokens INTEGER,
cache_read_tokens INTEGER,
cache_write_tokens INTEGER,
cache_hit_ratio REAL,
input_labels_json TEXT,
output_labels_json TEXT
);`)
	return err
}

func addTraceObservabilitySchema(tx *sql.Tx) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "transformation_json", definition: "TEXT"},
		{name: "trace_id", definition: "TEXT"},
		{name: "span_id", definition: "TEXT"},
		{name: "parent_span_id", definition: "TEXT"},
		{name: "session_id", definition: "TEXT"},
		{name: "session_name", definition: "TEXT"},
		{name: "session_kind", definition: "TEXT"},
		{name: "session_path", definition: "TEXT"},
		{name: "parent_request_id", definition: "TEXT"},
		{name: "principal_name", definition: "TEXT"},
		{name: "agent_id", definition: "TEXT"},
		{name: "agent_name", definition: "TEXT"},
		{name: "agent_version", definition: "TEXT"},
		{name: "agent_source", definition: "TEXT"},
		{name: "agent_confidence", definition: "TEXT"},
		{name: "project_id", definition: "TEXT"},
		{name: "duration_ms", definition: "INTEGER"},
		{name: "initial_provider", definition: "TEXT"},
		{name: "initial_model", definition: "TEXT"},
		{name: "finish_reason", definition: "TEXT"},
		{name: "upstream_request_id", definition: "TEXT"},
		{name: "retry_count", definition: "INTEGER"},
		{name: "input_message_count", definition: "INTEGER"},
		{name: "input_block_count", definition: "INTEGER"},
		{name: "input_tool_count", definition: "INTEGER"},
		{name: "input_image_count", definition: "INTEGER"},
		{name: "input_text_chars", definition: "INTEGER"},
		{name: "output_message_count", definition: "INTEGER"},
		{name: "output_block_count", definition: "INTEGER"},
		{name: "output_tool_call_count", definition: "INTEGER"},
		{name: "output_reasoning_chars", definition: "INTEGER"},
		{name: "output_text_chars", definition: "INTEGER"},
		{name: "request_shape_json", definition: "TEXT"},
		{name: "capture_mode", definition: "TEXT"},
		{name: "capture_status", definition: "TEXT"},
		{name: "capture_truncated", definition: "INTEGER"},
		{name: "redaction_count", definition: "INTEGER"},
	}
	existing, err := tableColumns(tx, "request_logs")
	if err != nil {
		return err
	}
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE request_logs ADD COLUMN ` + column.name + ` ` + column.definition); err != nil {
			return fmt.Errorf("add request_logs.%s: %w", column.name, err)
		}
	}

	statements := []string{
		`CREATE INDEX IF NOT EXISTS request_logs_started_idx ON request_logs(started_at DESC, request_id)`,
		`CREATE INDEX IF NOT EXISTS request_logs_session_idx ON request_logs(session_id, started_at, request_id)`,
		`CREATE INDEX IF NOT EXISTS request_logs_agent_idx ON request_logs(agent_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS request_logs_project_idx ON request_logs(project_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS request_logs_trace_idx ON request_logs(trace_id, started_at, request_id)`,
		`CREATE INDEX IF NOT EXISTS request_logs_status_idx ON request_logs(status_code, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trace_observations (
observation_id TEXT PRIMARY KEY,
request_id TEXT NOT NULL,
trace_id TEXT,
span_id TEXT,
parent_span_id TEXT,
type TEXT,
name TEXT,
started_at DATETIME,
completed_at DATETIME,
status TEXT,
error_code TEXT,
attributes_json TEXT
)`,
		`CREATE INDEX IF NOT EXISTS trace_observations_request_idx ON trace_observations(request_id, started_at, observation_id)`,
		`CREATE INDEX IF NOT EXISTS trace_observations_trace_idx ON trace_observations(trace_id, started_at, observation_id)`,
		`CREATE TABLE IF NOT EXISTS payload_snapshots (
request_id TEXT NOT NULL,
stage TEXT NOT NULL,
schema_version INTEGER NOT NULL,
capture_mode TEXT NOT NULL,
media_type TEXT,
content_encoding TEXT,
body_blob BLOB,
original_bytes INTEGER,
stored_bytes INTEGER,
truncated INTEGER,
truncation_reason TEXT,
redaction_count INTEGER,
sha256 TEXT,
created_at DATETIME,
expires_at DATETIME,
PRIMARY KEY (request_id, stage)
)`,
		`CREATE INDEX IF NOT EXISTS payload_snapshots_expiry_idx ON payload_snapshots(expires_at, created_at)`,
		`CREATE TABLE IF NOT EXISTS session_annotations (
session_id TEXT PRIMARY KEY,
name TEXT,
kind TEXT,
tags_json TEXT,
bookmarked INTEGER,
notes TEXT,
updated_at DATETIME
)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func addHTTPContextSchema(tx *sql.Tx) error {
	existing, err := tableColumns(tx, "request_logs")
	if err != nil {
		return err
	}
	for _, column := range []string{"http_method", "http_path"} {
		if existing[column] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE request_logs ADD COLUMN ` + column + ` TEXT`); err != nil {
			return fmt.Errorf("add request_logs.%s: %w", column, err)
		}
	}
	return nil
}

func addPayloadCaptureMetadataSchema(tx *sql.Tx) error {
	existing, err := tableColumns(tx, "payload_snapshots")
	if err != nil {
		return err
	}
	columns := []struct {
		name       string
		definition string
	}{
		{name: "capture_status", definition: "TEXT"},
		{name: "headers_json", definition: "TEXT"},
		{name: "capture_error", definition: "TEXT"},
	}
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE payload_snapshots ADD COLUMN ` + column.name + ` ` + column.definition); err != nil {
			return fmt.Errorf("add payload_snapshots.%s: %w", column.name, err)
		}
	}
	return nil
}

func tableColumns(tx *sql.Tx, table string) (map[string]bool, error) {
	if table != "request_logs" && table != "payload_snapshots" {
		return nil, fmt.Errorf("unsupported migration table %q", table)
	}
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
