package store

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

type SQLite struct{ db *sql.DB }

func Open(path string) (*SQLite, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &SQLite{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return sqliteDSNFromAbsolutePath(absolute, runtime.GOOS), nil
}

func sqliteDSNFromAbsolutePath(absolutePath, goos string) string {
	uriPath := filepath.ToSlash(absolutePath)
	if goos == "windows" {
		uriPath = strings.ReplaceAll(absolutePath, `\`, "/")
		if hasWindowsDrivePrefix(uriPath) && !strings.HasPrefix(uriPath, "/") {
			uriPath = "/" + uriPath
		}
	}
	databaseURL := url.URL{
		Scheme: "file",
		Path:   uriPath,
	}
	query := databaseURL.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_journal_mode", "WAL")
	databaseURL.RawQuery = query.Encode()
	return databaseURL.String()
}

func hasWindowsDrivePrefix(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	drive := path[0]
	return drive >= 'A' && drive <= 'Z' || drive >= 'a' && drive <= 'z'
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
	var transformation string
	if e.Transformation != nil {
		raw, _ := json.Marshal(e.Transformation)
		transformation = string(raw)
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO request_logs (
request_id,client_name,virtual_model,upstream_model,channel_id,protocol_in,protocol_out,
started_at,first_token_at,completed_at,ttft_ms,tpot_ms,tps,status_code,error_code,
prompt_tokens,completion_tokens,total_tokens,cache_read_tokens,cache_write_tokens,cache_hit_ratio,
input_labels_json,output_labels_json,transformation_json
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.RequestID, e.ClientName, e.VirtualModel, e.UpstreamModel, e.ChannelID, e.ProtocolIn, e.ProtocolOut,
		e.StartedAt, first, completed, e.TTFTMillis, e.TPOTMillis, e.TPS, e.StatusCode, e.ErrorCode,
		e.Usage.PromptTokens, e.Usage.CompletionTokens, e.Usage.TotalTokens, e.Usage.CacheReadTokens,
		e.Usage.CacheWriteTokens, e.Usage.CacheHitRatio, e.InputLabelsJSON, e.OutputLabelsJSON, transformation)
}

func (s *SQLite) Retain(days int) error {
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	_, err := s.db.Exec(`DELETE FROM request_logs WHERE started_at < ?`, cutoff)
	return err
}

func (s *SQLite) MetricsSummary(todayStart time.Time) (telemetry.MetricsSummary, error) {
	var summary telemetry.MetricsSummary
	err := s.db.QueryRow(`
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN started_at >= ? THEN prompt_tokens ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN started_at >= ? THEN completion_tokens ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN started_at >= ? THEN total_tokens ELSE 0 END), 0)
FROM request_logs
`, todayStart, todayStart, todayStart).Scan(
		&summary.TotalRequests,
		&summary.TodayTokens.Prompt,
		&summary.TodayTokens.Completion,
		&summary.TodayTokens.Total,
	)
	return summary, err
}

func (s *SQLite) MetricsHistory(since time.Time, bucket time.Duration) ([]telemetry.MetricPoint, error) {
	if bucket <= 0 {
		bucket = 5 * time.Minute
	}
	rows, err := s.db.Query(`
SELECT started_at, status_code, ttft_ms, tpot_ms, prompt_tokens, completion_tokens
FROM request_logs
WHERE started_at >= ?
ORDER BY started_at ASC
`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type aggregate struct {
		point telemetry.MetricPoint
		ttft  []int64
		tpot  []float64
	}
	aggregates := map[int64]*aggregate{}
	bucketNanos := bucket.Nanoseconds()
	for rows.Next() {
		var (
			startedAt        time.Time
			statusCode       int
			ttftMillis       int64
			tpotMillis       float64
			promptTokens     int64
			completionTokens int64
		)
		if err := rows.Scan(
			&startedAt,
			&statusCode,
			&ttftMillis,
			&tpotMillis,
			&promptTokens,
			&completionTokens,
		); err != nil {
			return nil, err
		}
		bucketKey := startedAt.UnixNano() / bucketNanos * bucketNanos
		current := aggregates[bucketKey]
		if current == nil {
			current = &aggregate{
				point: telemetry.MetricPoint{Timestamp: time.Unix(0, bucketKey).UTC()},
			}
			aggregates[bucketKey] = current
		}
		current.point.Requests++
		if statusCode >= 400 {
			current.point.Errors++
		}
		current.point.TokensPrompt += promptTokens
		current.point.TokensCompletion += completionTokens
		if ttftMillis > 0 {
			current.ttft = append(current.ttft, ttftMillis)
		}
		if tpotMillis > 0 {
			current.tpot = append(current.tpot, tpotMillis)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	keys := make([]int64, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	points := make([]telemetry.MetricPoint, 0, len(keys))
	for _, key := range keys {
		current := aggregates[key]
		current.point.TTFTP50 = percentileInt64(current.ttft, 0.50)
		current.point.TTFTP95 = percentileInt64(current.ttft, 0.95)
		current.point.TTFTP99 = percentileInt64(current.ttft, 0.99)
		current.point.TPOTP50 = percentileFloat64(current.tpot, 0.50)
		current.point.TPOTP95 = percentileFloat64(current.tpot, 0.95)
		points = append(points, current.point)
	}
	return points, nil
}

func percentileInt64(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func percentileFloat64(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func (s *SQLite) RecentFinished(limit int) ([]telemetry.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
SELECT
	request_id, client_name, virtual_model, upstream_model, channel_id, protocol_in, protocol_out,
	started_at, first_token_at, completed_at, ttft_ms, tpot_ms, tps, status_code, error_code,
	prompt_tokens, completion_tokens, total_tokens, cache_read_tokens, cache_write_tokens,
	cache_hit_ratio, input_labels_json, output_labels_json, transformation_json
FROM request_logs
ORDER BY started_at DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]telemetry.Event, 0, limit)
	for rows.Next() {
		var (
			event              telemetry.Event
			firstTokenAt       sql.NullTime
			completedAt        sql.NullTime
			transformationJSON sql.NullString
		)
		if err := rows.Scan(
			&event.RequestID,
			&event.ClientName,
			&event.VirtualModel,
			&event.UpstreamModel,
			&event.ChannelID,
			&event.ProtocolIn,
			&event.ProtocolOut,
			&event.StartedAt,
			&firstTokenAt,
			&completedAt,
			&event.TTFTMillis,
			&event.TPOTMillis,
			&event.TPS,
			&event.StatusCode,
			&event.ErrorCode,
			&event.Usage.PromptTokens,
			&event.Usage.CompletionTokens,
			&event.Usage.TotalTokens,
			&event.Usage.CacheReadTokens,
			&event.Usage.CacheWriteTokens,
			&event.Usage.CacheHitRatio,
			&event.InputLabelsJSON,
			&event.OutputLabelsJSON,
			&transformationJSON,
		); err != nil {
			return nil, err
		}
		if firstTokenAt.Valid {
			first := firstTokenAt.Time
			event.FirstTokenAt = &first
		}
		if completedAt.Valid {
			completed := completedAt.Time
			event.CompletedAt = &completed
		}
		if transformationJSON.Valid && transformationJSON.String != "" {
			var transformation telemetry.TransformationSummary
			if err := json.Unmarshal([]byte(transformationJSON.String), &transformation); err == nil {
				event.Transformation = &transformation
			}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLite) Close() error { return s.db.Close() }
