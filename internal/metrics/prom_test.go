package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

func TestPrometheusExportsGenerationAndPromptCacheMetrics(t *testing.T) {
	sink := NewWithRegistry(prometheus.NewRegistry())
	sink.RequestFinished(telemetry.Event{
		VirtualModel: "vibe-chat",
		ChannelID:    "local",
		StatusCode:   http.StatusOK,
		TTFTMillis:   250,
		TPOTMillis:   20,
		TPS:          50,
		Usage: types.Usage{
			PromptTokens:         100,
			CompletionTokens:     25,
			CacheReadTokens:      60,
			CacheWriteTokens:     10,
			CacheMetricsReported: true,
		},
	})
	sink.RequestFinished(telemetry.Event{
		VirtualModel: "vibe-chat",
		ChannelID:    "local",
		StatusCode:   http.StatusBadGateway,
		Usage:        types.Usage{PromptTokens: 5},
	})

	response := httptest.NewRecorder()
	sink.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`vibe_proxy_requests_total{channel="local",model="vibe-chat",status="200"} 1`,
		`vibe_proxy_requests_total{channel="local",model="vibe-chat",status="502"} 1`,
		`vibe_proxy_prompt_cache_requests_total{channel="local",model="vibe-chat",state="reported"} 1`,
		`vibe_proxy_prompt_cache_requests_total{channel="local",model="vibe-chat",state="not_reported"} 1`,
		`vibe_proxy_prompt_cache_tokens_total{channel="local",kind="eligible",model="vibe-chat"} 100`,
		`vibe_proxy_prompt_cache_tokens_total{channel="local",kind="read",model="vibe-chat"} 60`,
		`vibe_proxy_prompt_cache_tokens_total{channel="local",kind="write",model="vibe-chat"} 10`,
		`vibe_proxy_ttft_seconds_sum{channel="local",model="vibe-chat"} 0.25`,
		`vibe_proxy_tpot_seconds_sum{channel="local",model="vibe-chat"} 0.02`,
		`vibe_proxy_tps_bucket{channel="local",model="vibe-chat",le="40"} 0`,
		`vibe_proxy_tps_bucket{channel="local",model="vibe-chat",le="80"} 1`,
		`vibe_proxy_tps_sum{channel="local",model="vibe-chat"} 50`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics output missing %q\n%s", expected, body)
		}
	}
}

func TestPrometheusDoesNotExportHighCardinalityIdentityLabels(t *testing.T) {
	sink := NewWithRegistry(prometheus.NewRegistry())
	sink.RequestFinished(telemetry.Event{
		RequestID:       "request-secret-dimension",
		SessionID:       "session-secret-dimension",
		AgentID:         "agent-secret-dimension",
		PrincipalName:   "client-key-name",
		ClientKeyPrefix: "vibe_1234567",
		VirtualModel:    "model",
		ChannelID:       "provider",
		StatusCode:      http.StatusOK,
	})
	response := httptest.NewRecorder()
	sink.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, forbidden := range []string{"request-secret-dimension", "session-secret-dimension", "agent-secret-dimension", "client-key-name", "vibe_1234567"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("high-cardinality identity %q leaked into Prometheus output", forbidden)
		}
	}
}
