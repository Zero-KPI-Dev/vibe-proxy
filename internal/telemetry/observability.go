package telemetry

import "time"

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

// ObservabilityReader exposes persisted telemetry without coupling the runtime
// package to a concrete storage backend.
type ObservabilityReader interface {
	MetricsSummary(todayStart time.Time) (MetricsSummary, error)
	MetricsHistory(since time.Time, bucket time.Duration) ([]MetricPoint, error)
	RecentFinished(limit int) ([]Event, error)
}

type ObservabilityReaderProvider interface {
	ObservabilityReader() ObservabilityReader
}
