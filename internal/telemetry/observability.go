package telemetry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var ErrRequestNotFound = errors.New("observability request not found")

type MetricsSummary struct {
	TotalRequests int64 `json:"total_requests"`
	TodayTokens   struct {
		Prompt     int64 `json:"prompt"`
		Completion int64 `json:"completion"`
		Total      int64 `json:"total"`
	} `json:"today_tokens"`
}

type MetricPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	Requests         int64     `json:"requests"`
	Errors           int64     `json:"errors"`
	TTFTP50          int64     `json:"ttft_p50"`
	TTFTP95          int64     `json:"ttft_p95"`
	TTFTP99          int64     `json:"ttft_p99"`
	TPOTP50          float64   `json:"tpot_p50"`
	TPOTP95          float64   `json:"tpot_p95"`
	TokensPrompt     int64     `json:"tokens_prompt"`
	TokensCompletion int64     `json:"tokens_completion"`
}

type RequestQuery struct {
	Limit         int
	Cursor        string
	AgentID       string
	PrincipalName string
	SessionID     string
	ProjectID     string
	Model         string
	Provider      string
	Protocol      string
	StatusClass   int
	CaptureStatus string
	Query         string
	From          *time.Time
	To            *time.Time
}

type RequestPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

type SessionQuery struct {
	Limit         int
	Cursor        string
	SessionID     string
	AgentID       string
	PrincipalName string
	ProjectID     string
	Query         string
}

type SessionSummary struct {
	SessionID      string    `json:"session_id"`
	SessionName    string    `json:"session_name,omitempty"`
	SessionKind    string    `json:"session_kind,omitempty"`
	SessionPath    string    `json:"session_path,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	AgentName      string    `json:"agent_name,omitempty"`
	PrincipalName  string    `json:"principal_name,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	FirstRequestID string    `json:"first_request_id"`
	LastRequestID  string    `json:"last_request_id"`
	RequestCount   int64     `json:"request_count"`
	ErrorCount     int64     `json:"error_count"`
	TotalTokens    int64     `json:"total_tokens"`
}

type SessionPage struct {
	Items      []SessionSummary `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type PayloadContent struct {
	Stage            PayloadStage    `json:"stage"`
	State            string          `json:"state"`
	CaptureMode      CaptureMode     `json:"capture_mode,omitempty"`
	MediaType        string          `json:"media_type,omitempty"`
	ContentEncoding  string          `json:"content_encoding,omitempty"`
	Headers          http.Header     `json:"headers,omitempty"`
	Body             json.RawMessage `json:"body,omitempty"`
	OriginalBytes    int             `json:"original_bytes,omitempty"`
	StoredBytes      int             `json:"stored_bytes,omitempty"`
	Truncated        bool            `json:"truncated,omitempty"`
	TruncationReason string          `json:"truncation_reason,omitempty"`
	RedactionCount   int             `json:"redaction_count,omitempty"`
	SHA256           string          `json:"sha256,omitempty"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        *time.Time      `json:"created_at,omitempty"`
	ExpiresAt        *time.Time      `json:"expires_at,omitempty"`
}

type RequestDetails struct {
	Request      Event              `json:"request"`
	Observations []TraceObservation `json:"observations"`
	Payloads     []PayloadContent   `json:"payloads"`
}

type cursorPosition struct {
	StartedAt time.Time `json:"started_at"`
	RequestID string    `json:"request_id"`
}

func EncodeCursor(startedAt time.Time, requestID string) string {
	encoded, _ := json.Marshal(cursorPosition{StartedAt: startedAt.UTC(), RequestID: requestID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func DecodeCursor(cursor string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	var position cursorPosition
	if err := json.Unmarshal(decoded, &position); err != nil {
		return time.Time{}, "", err
	}
	if position.StartedAt.IsZero() || position.RequestID == "" {
		return time.Time{}, "", errors.New("invalid observability cursor")
	}
	return position.StartedAt, position.RequestID, nil
}

// ObservabilityReader exposes persisted telemetry without coupling the runtime
// package to a concrete storage backend.
type ObservabilityReader interface {
	MetricsSummary(todayStart time.Time) (MetricsSummary, error)
	MetricsHistory(since time.Time, bucket time.Duration) ([]MetricPoint, error)
	RecentFinished(limit int) ([]Event, error)
	QueryRequests(RequestQuery) (RequestPage, error)
	RequestDetails(requestID string) (RequestDetails, error)
	QuerySessions(SessionQuery) (SessionPage, error)
	SessionRequests(sessionID string) ([]Event, error)
	RequestDiff(requestID, baseRequestID string) (RequestDiff, error)
}

type ObservabilityReaderProvider interface {
	ObservabilityReader() ObservabilityReader
}
