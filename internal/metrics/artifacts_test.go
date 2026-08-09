package metrics

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExternalObservabilityArtifactsMatchMetricContract(t *testing.T) {
	prometheusConfig, err := os.ReadFile("../../deploy/observability/prometheus.yml")
	if err != nil {
		t.Fatal(err)
	}
	config := string(prometheusConfig)
	if !strings.Contains(config, `targets: ["127.0.0.1:8081"]`) {
		t.Fatalf("sample must scrape the loopback control plane: %s", config)
	}
	if strings.Contains(config, `0.0.0.0`) {
		t.Fatal("sample must not expose or scrape the public data-plane listener")
	}

	dashboardJSON, err := os.ReadFile("../../deploy/observability/grafana/vibe-proxy-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(dashboardJSON, &dashboard); err != nil {
		t.Fatalf("invalid Grafana dashboard JSON: %v", err)
	}
	dashboardSource := string(dashboardJSON)
	for _, metric := range []string{
		"vibe_proxy_requests_total",
		"vibe_proxy_ttft_seconds_bucket",
		"vibe_proxy_tpot_seconds_bucket",
		"vibe_proxy_tps_bucket",
		"vibe_proxy_tokens_total",
		"vibe_proxy_prompt_cache_requests_total",
		"vibe_proxy_prompt_cache_tokens_total",
	} {
		if !strings.Contains(dashboardSource, metric) {
			t.Errorf("Grafana dashboard does not query %s", metric)
		}
	}
	for _, averageMetric := range []string{
		"vibe_proxy_ttft_seconds_sum",
		"vibe_proxy_tpot_seconds_sum",
		"vibe_proxy_tps_sum",
	} {
		if !strings.Contains(dashboardSource, averageMetric) {
			t.Errorf("Grafana dashboard does not expose the average for %s", averageMetric)
		}
	}
	for _, forbiddenLabel := range []string{"request_id=", "session_id=", "agent_id=", "principal_name=", "client_key"} {
		if strings.Contains(dashboardSource, forbiddenLabel) {
			t.Errorf("Grafana dashboard uses forbidden high-cardinality label %q", forbiddenLabel)
		}
	}
}
