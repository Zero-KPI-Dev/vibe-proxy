package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

const requestEventColumns = `
	COALESCE(request_id, ''), COALESCE(client_name, ''), COALESCE(virtual_model, ''), COALESCE(upstream_model, ''),
	COALESCE(channel_id, ''), COALESCE(protocol_in, ''), COALESCE(protocol_out, ''),
	started_at, first_token_at, completed_at, COALESCE(ttft_ms, 0), COALESCE(tpot_ms, 0), COALESCE(tps, 0),
	COALESCE(status_code, 0), COALESCE(error_code, ''), COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
	COALESCE(total_tokens, 0), COALESCE(cache_read_tokens, 0), COALESCE(cache_write_tokens, 0),
	COALESCE(cache_metrics_reported, 0), COALESCE(cache_hit_ratio, 0), COALESCE(input_labels_json, ''), COALESCE(output_labels_json, ''), transformation_json,
	COALESCE(trace_id, ''), COALESCE(span_id, ''), COALESCE(parent_span_id, ''),
	COALESCE(session_id, ''), COALESCE(session_name, ''), COALESCE(session_kind, ''), COALESCE(session_path, ''),
	COALESCE(parent_request_id, ''), COALESCE(principal_type, ''), COALESCE(principal_name, ''),
	COALESCE(client_key_prefix, ''), COALESCE(agent_id, ''),
	COALESCE(agent_name, ''), COALESCE(agent_version, ''), COALESCE(agent_source, ''), COALESCE(agent_confidence, ''),
	COALESCE(project_id, ''), COALESCE(duration_ms, 0), COALESCE(initial_provider, ''), COALESCE(initial_model, ''),
	COALESCE(finish_reason, ''), COALESCE(upstream_request_id, ''), COALESCE(retry_count, 0),
	COALESCE(input_message_count, 0), COALESCE(input_block_count, 0), COALESCE(input_tool_count, 0),
	COALESCE(input_image_count, 0), COALESCE(input_text_chars, 0), COALESCE(output_message_count, 0),
	COALESCE(output_block_count, 0), COALESCE(output_tool_call_count, 0), COALESCE(output_reasoning_chars, 0),
	COALESCE(output_text_chars, 0), COALESCE(http_method, ''), COALESCE(http_path, ''),
	COALESCE(capture_mode, ''), COALESCE(capture_status, ''), COALESCE(capture_truncated, 0), COALESCE(redaction_count, 0)`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRequestEvent(scanner rowScanner) (telemetry.Event, error) {
	var event telemetry.Event
	var firstTokenAt, completedAt sql.NullTime
	var transformationJSON sql.NullString
	err := scanner.Scan(
		&event.RequestID, &event.ClientName, &event.VirtualModel, &event.UpstreamModel,
		&event.ChannelID, &event.ProtocolIn, &event.ProtocolOut, &event.StartedAt,
		&firstTokenAt, &completedAt, &event.TTFTMillis, &event.TPOTMillis, &event.TPS,
		&event.StatusCode, &event.ErrorCode, &event.Usage.PromptTokens, &event.Usage.CompletionTokens,
		&event.Usage.TotalTokens, &event.Usage.CacheReadTokens, &event.Usage.CacheWriteTokens,
		&event.Usage.CacheMetricsReported, &event.Usage.CacheHitRatio, &event.InputLabelsJSON, &event.OutputLabelsJSON, &transformationJSON,
		&event.TraceID, &event.SpanID, &event.ParentSpanID, &event.SessionID, &event.SessionName,
		&event.SessionKind, &event.SessionPath, &event.ParentRequestID, &event.PrincipalType, &event.PrincipalName,
		&event.ClientKeyPrefix,
		&event.AgentID, &event.AgentName, &event.AgentVersion, &event.AgentSource, &event.AgentConfidence,
		&event.ProjectID, &event.DurationMillis, &event.InitialProvider, &event.InitialModel,
		&event.FinishReason, &event.UpstreamRequestID, &event.RetryCount,
		&event.RequestShape.InputMessageCount, &event.RequestShape.InputBlockCount,
		&event.RequestShape.InputToolCount, &event.RequestShape.InputImageCount,
		&event.RequestShape.InputTextChars, &event.RequestShape.OutputMessageCount,
		&event.RequestShape.OutputBlockCount, &event.RequestShape.OutputToolCallCount,
		&event.RequestShape.OutputReasoningChars, &event.RequestShape.OutputTextChars,
		&event.HTTPMethod, &event.HTTPPath, &event.CaptureMode, &event.CaptureStatus,
		&event.CaptureTruncated, &event.RedactionCount,
	)
	if err != nil {
		return telemetry.Event{}, err
	}
	if firstTokenAt.Valid {
		value := firstTokenAt.Time
		event.FirstTokenAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		event.CompletedAt = &value
	}
	if transformationJSON.Valid && transformationJSON.String != "" {
		var transformation telemetry.TransformationSummary
		if json.Unmarshal([]byte(transformationJSON.String), &transformation) == nil {
			event.Transformation = &transformation
		}
	}
	return event, nil
}

func (s *SQLite) requestEvent(requestID string) (telemetry.Event, error) {
	event, err := scanRequestEvent(s.db.QueryRow(`SELECT `+requestEventColumns+` FROM request_logs WHERE request_id = ?`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return telemetry.Event{}, telemetry.ErrRequestNotFound
	}
	return event, err
}

func (s *SQLite) QueryRequests(query telemetry.RequestQuery) (telemetry.RequestPage, error) {
	limit := boundedPageLimit(query.Limit)
	where := []string{"1 = 1"}
	args := []any{}
	addEqualFilter := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		where = append(where, column+" = ?")
		args = append(args, strings.TrimSpace(value))
	}
	addEqualFilter("agent_id", query.AgentID)
	addEqualFilter("principal_name", query.PrincipalName)
	addEqualFilter("session_id", query.SessionID)
	addEqualFilter("project_id", query.ProjectID)
	addEqualFilter("channel_id", query.Provider)
	addEqualFilter("protocol_in", query.Protocol)
	addEqualFilter("capture_status", query.CaptureStatus)
	if query.Model != "" {
		where = append(where, "(virtual_model = ? OR upstream_model = ?)")
		args = append(args, query.Model, query.Model)
	}
	if query.StatusClass >= 1 && query.StatusClass <= 5 {
		where = append(where, "status_code >= ? AND status_code < ?")
		args = append(args, query.StatusClass*100, (query.StatusClass+1)*100)
	}
	if trimmed := strings.TrimSpace(query.Query); trimmed != "" {
		like := "%" + trimmed + "%"
		where = append(where, `(request_id LIKE ? OR session_id LIKE ? OR agent_id LIKE ? OR principal_name LIKE ? OR virtual_model LIKE ? OR upstream_model LIKE ?)`)
		args = append(args, like, like, like, like, like, like)
	}
	if query.From != nil {
		where = append(where, "started_at >= ?")
		args = append(args, query.From.UTC())
	}
	if query.To != nil {
		where = append(where, "started_at <= ?")
		args = append(args, query.To.UTC())
	}
	if query.Cursor != "" {
		startedAt, requestID, err := telemetry.DecodeCursor(query.Cursor)
		if err != nil {
			return telemetry.RequestPage{}, fmt.Errorf("decode request cursor: %w", err)
		}
		where = append(where, "(started_at < ? OR (started_at = ? AND request_id < ?))")
		args = append(args, startedAt, startedAt, requestID)
	}
	args = append(args, limit+1)
	rows, err := s.db.Query(`SELECT `+requestEventColumns+` FROM request_logs WHERE `+strings.Join(where, " AND ")+` ORDER BY started_at DESC, request_id DESC LIMIT ?`, args...)
	if err != nil {
		return telemetry.RequestPage{}, err
	}
	defer rows.Close()
	items := make([]telemetry.Event, 0, limit+1)
	for rows.Next() {
		event, err := scanRequestEvent(rows)
		if err != nil {
			return telemetry.RequestPage{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return telemetry.RequestPage{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := telemetry.RequestPage{Items: items}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = telemetry.EncodeCursor(last.StartedAt, last.RequestID)
	}
	return page, nil
}

func (s *SQLite) RequestDetails(requestID string) (telemetry.RequestDetails, error) {
	if err := s.PrunePayloads(time.Now().UTC(), -1); err != nil {
		return telemetry.RequestDetails{}, err
	}
	event, err := s.requestEvent(requestID)
	if err != nil {
		return telemetry.RequestDetails{}, err
	}
	observations, err := s.TraceObservations(requestID)
	if err != nil {
		return telemetry.RequestDetails{}, err
	}
	snapshots, err := s.PayloadSnapshots(requestID)
	if err != nil {
		return telemetry.RequestDetails{}, err
	}
	byStage := make(map[telemetry.PayloadStage]telemetry.PayloadSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byStage[snapshot.Stage] = snapshot
	}
	stages := []telemetry.PayloadStage{
		telemetry.PayloadStageClientRequest,
		telemetry.PayloadStageCanonicalRequest,
		telemetry.PayloadStageOCRRequest,
		telemetry.PayloadStageOCRResponse,
		telemetry.PayloadStageVisionRequest,
		telemetry.PayloadStageVisionResponse,
		telemetry.PayloadStageEffectiveCanonicalRequest,
		telemetry.PayloadStageUpstreamRequest,
		telemetry.PayloadStageCanonicalResponse,
	}
	payloads := make([]telemetry.PayloadContent, 0, len(stages))
	for _, stage := range stages {
		snapshot, ok := byStage[stage]
		if !ok {
			payloads = append(payloads, telemetry.PayloadContent{Stage: stage, State: missingPayloadState(event.CaptureStatus)})
			continue
		}
		createdAt, expiresAt := snapshot.CreatedAt, snapshot.ExpiresAt
		content := telemetry.PayloadContent{
			Stage: stage, State: snapshot.CaptureStatus, CaptureMode: snapshot.CaptureMode,
			MediaType: snapshot.MediaType, ContentEncoding: snapshot.ContentEncoding, Headers: snapshot.Headers.Clone(),
			OriginalBytes: snapshot.OriginalBytes, StoredBytes: snapshot.StoredBytes,
			Truncated: snapshot.Truncated, TruncationReason: snapshot.TruncationReason,
			RedactionCount: snapshot.RedactionCount, SHA256: snapshot.SHA256, Error: snapshot.Error,
			CreatedAt: &createdAt, ExpiresAt: &expiresAt,
		}
		if len(snapshot.Body) > 0 && json.Valid(snapshot.Body) {
			content.Body = append(json.RawMessage(nil), snapshot.Body...)
		}
		payloads = append(payloads, content)
	}
	return telemetry.RequestDetails{Request: event, Observations: observations, Payloads: payloads}, nil
}

func missingPayloadState(captureStatus string) string {
	switch captureStatus {
	case telemetry.CaptureStatusNotCaptured, telemetry.CaptureStatusDropped, telemetry.CaptureStatusExpired:
		return captureStatus
	default:
		return "missing"
	}
}

func (s *SQLite) QuerySessions(query telemetry.SessionQuery) (telemetry.SessionPage, error) {
	limit := boundedPageLimit(query.Limit)
	where := []string{"session_id IS NOT NULL", "session_id <> ''"}
	args := []any{}
	for _, filter := range []struct{ column, value string }{
		{"session_id", query.SessionID}, {"agent_id", query.AgentID}, {"principal_name", query.PrincipalName}, {"project_id", query.ProjectID},
	} {
		if strings.TrimSpace(filter.value) != "" {
			where = append(where, filter.column+" = ?")
			args = append(args, strings.TrimSpace(filter.value))
		}
	}
	if trimmed := strings.TrimSpace(query.Query); trimmed != "" {
		like := "%" + trimmed + "%"
		where = append(where, "(session_id LIKE ? OR session_name LIKE ? OR agent_id LIKE ? OR project_id LIKE ?)")
		args = append(args, like, like, like, like)
	}
	having := ""
	if query.Cursor != "" {
		updatedAt, sessionID, err := telemetry.DecodeCursor(query.Cursor)
		if err != nil {
			return telemetry.SessionPage{}, fmt.Errorf("decode session cursor: %w", err)
		}
		having = " HAVING (MAX(started_at) < ? OR (MAX(started_at) = ? AND session_id < ?))"
		args = append(args, updatedAt, updatedAt, sessionID)
	}
	args = append(args, limit+1)
	rows, err := s.db.Query(`SELECT session_id, MAX(started_at) FROM request_logs WHERE `+strings.Join(where, " AND ")+` GROUP BY session_id`+having+` ORDER BY MAX(started_at) DESC, session_id DESC LIMIT ?`, args...)
	if err != nil {
		return telemetry.SessionPage{}, err
	}
	defer rows.Close()
	type position struct {
		sessionID string
		updatedAt time.Time
	}
	positions := []position{}
	for rows.Next() {
		var item position
		var rawUpdatedAt any
		if err := rows.Scan(&item.sessionID, &rawUpdatedAt); err != nil {
			return telemetry.SessionPage{}, err
		}
		item.updatedAt, err = parseSQLiteAggregateTime(rawUpdatedAt)
		if err != nil {
			return telemetry.SessionPage{}, err
		}
		positions = append(positions, item)
	}
	if err := rows.Err(); err != nil {
		return telemetry.SessionPage{}, err
	}
	hasMore := len(positions) > limit
	if hasMore {
		positions = positions[:limit]
	}
	page := telemetry.SessionPage{Items: make([]telemetry.SessionSummary, 0, len(positions))}
	for _, item := range positions {
		summary, err := s.sessionSummary(item.sessionID)
		if errors.Is(err, telemetry.ErrRequestNotFound) {
			// Periodic retention may remove the final request in a session after
			// the grouped cursor query. Treat it as an expired result, not a
			// transient failure of the whole page.
			continue
		}
		if err != nil {
			return telemetry.SessionPage{}, err
		}
		page.Items = append(page.Items, summary)
	}
	if hasMore && len(positions) > 0 {
		last := positions[len(positions)-1]
		page.NextCursor = telemetry.EncodeCursor(last.updatedAt, last.sessionID)
	}
	return page, nil
}

func (s *SQLite) sessionSummary(sessionID string) (telemetry.SessionSummary, error) {
	var summary telemetry.SessionSummary
	var rawStartedAt, rawUpdatedAt any
	err := s.db.QueryRow(`SELECT
session_id, COALESCE(MAX(session_name), ''), COALESCE(MAX(session_kind), ''), COALESCE(MAX(session_path), ''),
COALESCE(MAX(agent_id), ''), COALESCE(MAX(agent_name), ''), COALESCE(MAX(principal_name), ''), COALESCE(MAX(project_id), ''),
MIN(started_at), MAX(started_at), COUNT(*), SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), COALESCE(SUM(total_tokens), 0)
FROM request_logs WHERE session_id = ? GROUP BY session_id`, sessionID).Scan(
		&summary.SessionID, &summary.SessionName, &summary.SessionKind, &summary.SessionPath,
		&summary.AgentID, &summary.AgentName, &summary.PrincipalName, &summary.ProjectID,
		&rawStartedAt, &rawUpdatedAt, &summary.RequestCount, &summary.ErrorCount, &summary.TotalTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return telemetry.SessionSummary{}, telemetry.ErrRequestNotFound
	}
	if err != nil {
		return telemetry.SessionSummary{}, err
	}
	if summary.StartedAt, err = parseSQLiteAggregateTime(rawStartedAt); err != nil {
		return telemetry.SessionSummary{}, err
	}
	if summary.UpdatedAt, err = parseSQLiteAggregateTime(rawUpdatedAt); err != nil {
		return telemetry.SessionSummary{}, err
	}
	timeline, err := s.SessionRequests(sessionID)
	if err != nil {
		return telemetry.SessionSummary{}, err
	}
	if len(timeline) > 0 {
		summary.FirstRequestID = timeline[0].RequestID
		summary.LastRequestID = timeline[len(timeline)-1].RequestID
	}
	return summary, nil
}

func (s *SQLite) SessionRequests(sessionID string) ([]telemetry.Event, error) {
	rows, err := s.db.Query(`SELECT `+requestEventColumns+` FROM request_logs WHERE session_id = ? ORDER BY started_at, request_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []telemetry.Event{}
	for rows.Next() {
		event, err := scanRequestEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *SQLite) DeleteRequestContent(requestID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = ?`, requestID).Scan(&exists); err != nil {
		return false, err
	}
	if exists == 0 {
		return false, telemetry.ErrRequestNotFound
	}
	result, err := tx.Exec(`DELETE FROM payload_snapshots WHERE request_id = ?`, requestID)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE request_logs SET capture_status = ?, capture_truncated = 0 WHERE request_id = ?`, telemetry.CaptureStatusExpired, requestID); err != nil {
		return false, err
	}
	deleted, _ := result.RowsAffected()
	return deleted > 0, tx.Commit()
}

func (s *SQLite) RequestDiff(requestID, baseRequestID string) (telemetry.RequestDiff, error) {
	current, err := s.requestEvent(requestID)
	if err != nil {
		return telemetry.RequestDiff{}, err
	}
	candidates := []telemetry.Event{}
	if current.SessionID != "" {
		candidates, err = s.SessionRequests(current.SessionID)
		if err != nil {
			return telemetry.RequestDiff{}, err
		}
	}
	for _, candidateID := range []string{baseRequestID, current.ParentRequestID} {
		if candidateID == "" {
			continue
		}
		candidate, candidateErr := s.requestEvent(candidateID)
		if candidateErr == nil {
			candidates = append(candidates, candidate)
		} else if baseRequestID != "" && candidateID == baseRequestID {
			return telemetry.RequestDiff{}, candidateErr
		}
	}
	base, selection, ok := telemetry.SelectDiffBase(current, candidates, baseRequestID)
	if !ok {
		return telemetry.NewNoBaseDiff(current), nil
	}
	baseBody, baseState, err := s.canonicalRequestPayload(base)
	if err != nil {
		return telemetry.RequestDiff{}, err
	}
	if baseState != "available" {
		return telemetry.NewUnavailableDiff(current, base, selection, "base", baseState), nil
	}
	currentBody, currentState, err := s.canonicalRequestPayload(current)
	if err != nil {
		return telemetry.RequestDiff{}, err
	}
	if currentState != "available" {
		return telemetry.NewUnavailableDiff(current, base, selection, "current", currentState), nil
	}
	diff, err := telemetry.DiffCanonicalRequests(base, current, baseBody, currentBody)
	if err != nil {
		return telemetry.NewUnavailableDiff(current, base, selection, "both", "invalid"), nil
	}
	diff.BaseSelection = selection
	return diff, nil
}

func (s *SQLite) canonicalRequestPayload(event telemetry.Event) ([]byte, string, error) {
	snapshots, err := s.PayloadSnapshots(event.RequestID)
	if err != nil {
		return nil, "", err
	}
	for _, snapshot := range snapshots {
		if snapshot.Stage != telemetry.PayloadStageCanonicalRequest {
			continue
		}
		if snapshot.CaptureStatus == telemetry.CaptureStatusTruncated || snapshot.CaptureStatus == telemetry.CaptureStatusDropped {
			return nil, snapshot.CaptureStatus, nil
		}
		if len(snapshot.Body) == 0 {
			return nil, missingPayloadState(event.CaptureStatus), nil
		}
		return snapshot.Body, "available", nil
	}
	return nil, missingPayloadState(event.CaptureStatus), nil
}

func boundedPageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func parseSQLiteAggregateTime(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed, nil
	case string:
		typed = strings.TrimSpace(typed)
		// Older rows may contain Go's process-local monotonic suffix because the
		// SQLite driver serialized time.Now() through time.Time.String(). Keep
		// those databases readable while new writes normalize the value first.
		if index := strings.LastIndex(typed, " m="); index > 0 {
			typed = typed[:index]
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.999999999 -0700 MST",
			"2006-01-02 15:04:05 -0700 MST",
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05",
		} {
			if parsed, err := time.Parse(layout, typed); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("parse SQLite aggregate time %q", typed)
	case []byte:
		return parseSQLiteAggregateTime(string(typed))
	default:
		return time.Time{}, fmt.Errorf("unsupported SQLite aggregate time %T", value)
	}
}
