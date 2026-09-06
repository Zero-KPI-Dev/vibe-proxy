package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestProviderProgressTimeoutsLoadAndValidate(t *testing.T) {
	var simple SimpleConfig
	if err := yaml.Unmarshal([]byte(`providers:
  local:
    type: openai-compatible
    base_url: http://127.0.0.1:3000/v1
    auth: {type: none}
    timeout: 5m
    first_token_timeout: 90s
    stream_idle_timeout: 45s
`), &simple); err != nil {
		t.Fatal(err)
	}
	cfg, err := CompileSimple(simple)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Providers["local"]
	if p.Timeout.Duration != 5*time.Minute || p.FirstTokenTimeout.Duration != 90*time.Second || p.StreamIdleTimeout.Duration != 45*time.Second {
		t.Fatalf("timeouts not loaded: %+v", p)
	}
	if issues := ValidateRuntime(cfg); HasErrors(issues) {
		t.Fatalf("valid timeouts rejected: %+v", issues)
	}
	p.FirstTokenTimeout.Duration = -time.Second
	cfg.Providers["local"] = p
	found := false
	for _, issue := range ValidateRuntime(cfg) {
		if issue.Code == "invalid_provider_timeout" {
			found = true
		}
	}
	if !found {
		t.Fatal("negative timeout accepted")
	}
}
