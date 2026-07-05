package store

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

type SQLite struct{ db *sql.DB }

func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	s := &SQLite{db: db}
	return s, s.migrate()
}

func (s *SQLite) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS request_logs (
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

func (s *SQLite) RequestStarted(e telemetry.Event) {}
func (s *SQLite) Token(e telemetry.Event)          {}

func (s *SQLite) RequestFinished(e telemetry.Event) {
	var first, completed any
	if e.FirstTokenAt != nil {
		first = *e.FirstTokenAt
	}
	if e.CompletedAt != nil {
		completed = *e.CompletedAt
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO request_logs VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.RequestID, e.ClientName, e.VirtualModel, e.UpstreamModel, e.ChannelID, e.ProtocolIn, e.ProtocolOut,
		e.StartedAt, first, completed, e.TTFTMillis, e.TPOTMillis, e.TPS, e.StatusCode, e.ErrorCode,
		e.Usage.PromptTokens, e.Usage.CompletionTokens, e.Usage.TotalTokens, e.Usage.CacheReadTokens,
		e.Usage.CacheWriteTokens, e.Usage.CacheHitRatio, e.InputLabelsJSON, e.OutputLabelsJSON)
}

func (s *SQLite) Retain(days int) error {
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	_, err := s.db.Exec(`DELETE FROM request_logs WHERE started_at < ?`, cutoff)
	return err
}

func (s *SQLite) Close() error { return s.db.Close() }
