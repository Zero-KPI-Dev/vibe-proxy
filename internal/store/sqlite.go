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
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

type SQLite struct {
	db *sql.DB

	payloadMu        sync.RWMutex
	payloadRetention time.Duration
	payloadQuota     int64
}

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
		first = sqliteTime(*e.FirstTokenAt)
	}
	if e.CompletedAt != nil {
		completed = sqliteTime(*e.CompletedAt)
	}
	var transformation string
	if e.Transformation != nil {
		raw, _ := json.Marshal(e.Transformation)
		transformation = string(raw)
	}
	requestShapeJSON, _ := json.Marshal(e.RequestShape)
	columns := []string{
		"request_id", "client_name", "virtual_model", "upstream_model", "channel_id", "protocol_in", "protocol_out",
		"started_at", "first_token_at", "completed_at", "ttft_ms", "tpot_ms", "tps", "status_code", "error_code",
		"prompt_tokens", "completion_tokens", "total_tokens", "cache_read_tokens", "cache_write_tokens", "cache_hit_ratio",
		"input_labels_json", "output_labels_json", "transformation_json",
		"trace_id", "span_id", "parent_span_id", "session_id", "session_name", "session_kind", "session_path",
		"parent_request_id", "principal_name", "agent_id", "agent_name", "agent_version", "agent_source", "agent_confidence",
		"project_id", "duration_ms", "initial_provider", "initial_model", "finish_reason", "upstream_request_id", "retry_count",
		"input_message_count", "input_block_count", "input_tool_count", "input_image_count", "input_text_chars",
		"output_message_count", "output_block_count", "output_tool_call_count", "output_reasoning_chars", "output_text_chars",
		"request_shape_json", "http_method", "http_path",
		"capture_mode", "capture_status", "capture_truncated", "redaction_count",
	}
	values := []any{
		e.RequestID, e.ClientName, e.VirtualModel, e.UpstreamModel, e.ChannelID, e.ProtocolIn, e.ProtocolOut,
		sqliteTime(e.StartedAt), first, completed, e.TTFTMillis, e.TPOTMillis, e.TPS, e.StatusCode, e.ErrorCode,
		e.Usage.PromptTokens, e.Usage.CompletionTokens, e.Usage.TotalTokens, e.Usage.CacheReadTokens,
		e.Usage.CacheWriteTokens, e.Usage.CacheHitRatio, e.InputLabelsJSON, e.OutputLabelsJSON, transformation,
		e.TraceID, e.SpanID, e.ParentSpanID, e.SessionID, e.SessionName, e.SessionKind, e.SessionPath,
		e.ParentRequestID, e.PrincipalName, e.AgentID, e.AgentName, e.AgentVersion, e.AgentSource, e.AgentConfidence,
		e.ProjectID, e.DurationMillis, e.InitialProvider, e.InitialModel, e.FinishReason, e.UpstreamRequestID, e.RetryCount,
		e.RequestShape.InputMessageCount, e.RequestShape.InputBlockCount, e.RequestShape.InputToolCount,
		e.RequestShape.InputImageCount, e.RequestShape.InputTextChars, e.RequestShape.OutputMessageCount,
		e.RequestShape.OutputBlockCount, e.RequestShape.OutputToolCallCount, e.RequestShape.OutputReasoningChars,
		e.RequestShape.OutputTextChars, string(requestShapeJSON), e.HTTPMethod, e.HTTPPath,
		e.CaptureMode, e.CaptureStatus, e.CaptureTruncated, e.RedactionCount,
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO request_logs (`+strings.Join(columns, ",")+`) VALUES (`+placeholders+`)`, values...)
}

func (s *SQLite) ConfigurePayloadStorage(retention time.Duration, maxBytes int64) error {
	if retention <= 0 {
		retention = 3 * 24 * time.Hour
	}
	s.payloadMu.Lock()
	s.payloadRetention = retention
	s.payloadQuota = maxBytes
	s.payloadMu.Unlock()
	return s.PrunePayloads(time.Now().UTC(), maxBytes)
}

func (s *SQLite) RecordPayload(snapshot telemetry.PayloadSnapshot) error {
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	s.payloadMu.RLock()
	retention := s.payloadRetention
	quota := s.payloadQuota
	s.payloadMu.RUnlock()
	if retention <= 0 {
		retention = 3 * 24 * time.Hour
	}
	if snapshot.ExpiresAt.IsZero() {
		snapshot.ExpiresAt = snapshot.CreatedAt.Add(retention)
	}
	if snapshot.SchemaVersion <= 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.StoredBytes == 0 && len(snapshot.Body) > 0 {
		snapshot.StoredBytes = len(snapshot.Body)
	}
	headersJSON, err := json.Marshal(snapshot.Headers)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO payload_snapshots (
request_id, stage, schema_version, capture_mode, capture_status, media_type, content_encoding,
body_blob, original_bytes, stored_bytes, truncated, truncation_reason, redaction_count, sha256,
headers_json, capture_error, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.RequestID, snapshot.Stage, snapshot.SchemaVersion, snapshot.CaptureMode, snapshot.CaptureStatus,
		snapshot.MediaType, snapshot.ContentEncoding, snapshot.Body, snapshot.OriginalBytes, snapshot.StoredBytes,
		snapshot.Truncated, snapshot.TruncationReason, snapshot.RedactionCount, snapshot.SHA256,
		string(headersJSON), snapshot.Error, sqliteTime(snapshot.CreatedAt), sqliteTime(snapshot.ExpiresAt),
	)
	if err != nil {
		return err
	}
	if quota > 0 {
		return s.PrunePayloads(now, quota)
	}
	return nil
}

func (s *SQLite) RecordObservation(observation telemetry.TraceObservation) error {
	attributesJSON, err := json.Marshal(observation.Attributes)
	if err != nil {
		return err
	}
	var completedAt any
	if observation.CompletedAt != nil {
		completedAt = sqliteTime(*observation.CompletedAt)
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO trace_observations (
observation_id, request_id, trace_id, span_id, parent_span_id, type, name,
started_at, completed_at, status, error_code, attributes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		observation.ObservationID, observation.RequestID, observation.TraceID, observation.SpanID,
		observation.ParentSpanID, observation.Type, observation.Name, sqliteTime(observation.StartedAt), completedAt,
		observation.Status, observation.ErrorCode, string(attributesJSON),
	)
	return err
}

// sqliteTime removes process-local monotonic clock readings before values cross
// the database boundary. SQLite aggregate functions return DATETIME values as
// strings, and monotonic suffixes are neither portable nor parseable timestamps.
func sqliteTime(value time.Time) time.Time {
	return value.UTC().Round(0)
}

func (s *SQLite) PayloadSnapshots(requestID string) ([]telemetry.PayloadSnapshot, error) {
	rows, err := s.db.Query(`SELECT
request_id, stage, schema_version, capture_mode, COALESCE(capture_status, ''), COALESCE(media_type, ''),
COALESCE(content_encoding, ''), body_blob, COALESCE(original_bytes, 0), COALESCE(stored_bytes, 0),
COALESCE(truncated, 0), COALESCE(truncation_reason, ''), COALESCE(redaction_count, 0), COALESCE(sha256, ''),
COALESCE(headers_json, ''), COALESCE(capture_error, ''), created_at, expires_at
FROM payload_snapshots WHERE request_id = ? ORDER BY created_at, stage`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []telemetry.PayloadSnapshot{}
	for rows.Next() {
		var snapshot telemetry.PayloadSnapshot
		var headersJSON string
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&snapshot.RequestID, &snapshot.Stage, &snapshot.SchemaVersion, &snapshot.CaptureMode,
			&snapshot.CaptureStatus, &snapshot.MediaType, &snapshot.ContentEncoding, &snapshot.Body,
			&snapshot.OriginalBytes, &snapshot.StoredBytes, &snapshot.Truncated, &snapshot.TruncationReason,
			&snapshot.RedactionCount, &snapshot.SHA256, &headersJSON, &snapshot.Error,
			&snapshot.CreatedAt, &expiresAt,
		); err != nil {
			return nil, err
		}
		if headersJSON != "" {
			_ = json.Unmarshal([]byte(headersJSON), &snapshot.Headers)
		}
		if expiresAt.Valid {
			snapshot.ExpiresAt = expiresAt.Time
		}
		result = append(result, snapshot)
	}
	return result, rows.Err()
}

func (s *SQLite) TraceObservations(requestID string) ([]telemetry.TraceObservation, error) {
	rows, err := s.db.Query(`SELECT
observation_id, request_id, COALESCE(trace_id, ''), COALESCE(span_id, ''), COALESCE(parent_span_id, ''),
COALESCE(type, ''), COALESCE(name, ''), started_at, completed_at, COALESCE(status, ''),
COALESCE(error_code, ''), COALESCE(attributes_json, '')
FROM trace_observations WHERE request_id = ? ORDER BY started_at, observation_id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []telemetry.TraceObservation{}
	for rows.Next() {
		var observation telemetry.TraceObservation
		var completedAt sql.NullTime
		var attributesJSON string
		if err := rows.Scan(
			&observation.ObservationID, &observation.RequestID, &observation.TraceID, &observation.SpanID,
			&observation.ParentSpanID, &observation.Type, &observation.Name, &observation.StartedAt,
			&completedAt, &observation.Status, &observation.ErrorCode, &attributesJSON,
		); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			completed := completedAt.Time
			observation.CompletedAt = &completed
			observation.DurationMillis = completed.Sub(observation.StartedAt).Milliseconds()
		}
		if attributesJSON != "" {
			_ = json.Unmarshal([]byte(attributesJSON), &observation.Attributes)
		}
		result = append(result, observation)
	}
	return result, rows.Err()
}

func (s *SQLite) PrunePayloads(now time.Time, maxBytes int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE request_logs SET capture_status = ?
WHERE request_id IN (
	SELECT request_id FROM payload_snapshots WHERE expires_at IS NOT NULL AND expires_at <= ?
) AND NOT EXISTS (
	SELECT 1 FROM payload_snapshots
	WHERE payload_snapshots.request_id = request_logs.request_id
	AND (expires_at IS NULL OR expires_at > ?)
)`, telemetry.CaptureStatusExpired, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM payload_snapshots WHERE expires_at IS NOT NULL AND expires_at <= ?`, now); err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN stored_bytes > 0 THEN stored_bytes ELSE length(body_blob) END), 0) FROM payload_snapshots`).Scan(&total); err != nil {
		return err
	}
	if maxBytes >= 0 && total > maxBytes {
		rows, err := tx.Query(`SELECT request_id, stage, COALESCE(CASE WHEN stored_bytes > 0 THEN stored_bytes ELSE length(body_blob) END, 0)
FROM payload_snapshots ORDER BY created_at, request_id, stage`)
		if err != nil {
			return err
		}
		type candidate struct {
			requestID string
			stage     string
			bytes     int64
		}
		candidates := []candidate{}
		for rows.Next() && total > maxBytes {
			var item candidate
			if err := rows.Scan(&item.requestID, &item.stage, &item.bytes); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, item)
			total -= item.bytes
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range candidates {
			if _, err := tx.Exec(`DELETE FROM payload_snapshots WHERE request_id = ? AND stage = ?`, item.requestID, item.stage); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE request_logs SET capture_status = ? WHERE request_id = ?`, telemetry.CaptureStatusDropped, item.requestID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *SQLite) Retain(days int) error {
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`DELETE FROM payload_snapshots WHERE request_id IN (SELECT request_id FROM request_logs WHERE started_at < ?)`,
		`DELETE FROM trace_observations WHERE request_id IN (SELECT request_id FROM request_logs WHERE started_at < ?)`,
		`DELETE FROM request_logs WHERE started_at < ?`,
	} {
		if _, err := tx.Exec(statement, cutoff); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	COALESCE(request_id, ''), COALESCE(client_name, ''), COALESCE(virtual_model, ''), COALESCE(upstream_model, ''),
	COALESCE(channel_id, ''), COALESCE(protocol_in, ''), COALESCE(protocol_out, ''),
	started_at, first_token_at, completed_at, ttft_ms, tpot_ms, tps, status_code, error_code,
	prompt_tokens, completion_tokens, total_tokens, cache_read_tokens, cache_write_tokens,
	cache_hit_ratio, COALESCE(input_labels_json, ''), COALESCE(output_labels_json, ''), transformation_json,
	COALESCE(trace_id, ''), COALESCE(span_id, ''), COALESCE(parent_span_id, ''),
	COALESCE(session_id, ''), COALESCE(session_name, ''), COALESCE(session_kind, ''), COALESCE(session_path, ''),
	COALESCE(parent_request_id, ''), COALESCE(principal_name, ''), COALESCE(agent_id, ''),
	COALESCE(agent_name, ''), COALESCE(agent_version, ''), COALESCE(agent_source, ''), COALESCE(agent_confidence, ''),
	COALESCE(project_id, ''), COALESCE(duration_ms, 0), COALESCE(initial_provider, ''), COALESCE(initial_model, ''),
	COALESCE(finish_reason, ''), COALESCE(upstream_request_id, ''), COALESCE(retry_count, 0),
	COALESCE(input_message_count, 0), COALESCE(input_block_count, 0), COALESCE(input_tool_count, 0),
	COALESCE(input_image_count, 0), COALESCE(input_text_chars, 0), COALESCE(output_message_count, 0),
	COALESCE(output_block_count, 0), COALESCE(output_tool_call_count, 0), COALESCE(output_reasoning_chars, 0),
	COALESCE(output_text_chars, 0), COALESCE(http_method, ''), COALESCE(http_path, ''),
	COALESCE(capture_mode, ''), COALESCE(capture_status, ''), COALESCE(capture_truncated, 0), COALESCE(redaction_count, 0)
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
			&event.TraceID,
			&event.SpanID,
			&event.ParentSpanID,
			&event.SessionID,
			&event.SessionName,
			&event.SessionKind,
			&event.SessionPath,
			&event.ParentRequestID,
			&event.PrincipalName,
			&event.AgentID,
			&event.AgentName,
			&event.AgentVersion,
			&event.AgentSource,
			&event.AgentConfidence,
			&event.ProjectID,
			&event.DurationMillis,
			&event.InitialProvider,
			&event.InitialModel,
			&event.FinishReason,
			&event.UpstreamRequestID,
			&event.RetryCount,
			&event.RequestShape.InputMessageCount,
			&event.RequestShape.InputBlockCount,
			&event.RequestShape.InputToolCount,
			&event.RequestShape.InputImageCount,
			&event.RequestShape.InputTextChars,
			&event.RequestShape.OutputMessageCount,
			&event.RequestShape.OutputBlockCount,
			&event.RequestShape.OutputToolCallCount,
			&event.RequestShape.OutputReasoningChars,
			&event.RequestShape.OutputTextChars,
			&event.HTTPMethod,
			&event.HTTPPath,
			&event.CaptureMode,
			&event.CaptureStatus,
			&event.CaptureTruncated,
			&event.RedactionCount,
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
