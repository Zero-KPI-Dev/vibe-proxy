package config

import "testing"

func TestCompileSimpleConfig(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{"anthropic": {Type: "anthropic", APIKey: "env:ANTHROPIC_API_KEY", Models: []string{"claude-sonnet"}}, "deepseek": {Type: "openai-compatible", BaseURL: "https://api.deepseek.com/v1", APIKey: "env:DEEPSEEK_API_KEY", Models: []string{"deepseek-chat"}}}, Models: ModelsConfig{Default: "vibe-coder", AllowRaw: true, Aliases: map[string]string{"vibe-coder": "anthropic/claude-sonnet"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "127.0.0.1:8080" {
		t.Fatalf("unexpected listen: %s", cfg.Server.Listen)
	}
	if cfg.Providers["anthropic"].Auth.Type != "api_key_header" {
		t.Fatalf("unexpected anthropic auth: %+v", cfg.Providers["anthropic"].Auth)
	}
	if cfg.Providers["deepseek"].Auth.Type != "bearer" {
		t.Fatalf("unexpected deepseek auth: %+v", cfg.Providers["deepseek"].Auth)
	}
	if cfg.ModelResolver.Aliases["vibe-coder"].Provider != "anthropic" {
		t.Fatalf("alias not compiled: %+v", cfg.ModelResolver.Aliases)
	}
}

func TestCompileSimpleRetainsAgentProfilesInDeterministicOrder(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{AgentProfiles: map[string]AgentProfileConfig{
		"cursor": {Detect: map[string]string{"user_agent": "*cursor*"}, DefaultModel: "fast"},
		"aider":  {Detect: map[string]string{"user_agent": "*aider*"}, DefaultModel: "smart"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AgentProfiles) != 2 {
		t.Fatalf("agent profiles = %+v, want 2", cfg.AgentProfiles)
	}
	if cfg.AgentProfiles[0].ID != "aider" || cfg.AgentProfiles[1].ID != "cursor" {
		t.Fatalf("agent profiles are not deterministic: %+v", cfg.AgentProfiles)
	}
	if cfg.AgentProfiles[0].DefaultModel != "smart" {
		t.Fatalf("profile fields were not retained: %+v", cfg.AgentProfiles[0])
	}
}

func TestCompileSimpleAppliesObservabilityCaptureDefaults(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Observability.Capture.Mode != "metadata" {
		t.Fatalf("capture mode = %q, want metadata", cfg.Observability.Capture.Mode)
	}
	if cfg.Observability.Capture.MaxSnapshotBytes != 256<<10 {
		t.Fatalf("max snapshot bytes = %d", cfg.Observability.Capture.MaxSnapshotBytes)
	}
	if cfg.Observability.Capture.ImagePayloads != "metadata" {
		t.Fatalf("image payload policy = %q", cfg.Observability.Capture.ImagePayloads)
	}
	if cfg.Observability.Retention.ContentDays != 3 || cfg.Observability.Retention.MaxContentStorageMB != 512 {
		t.Fatalf("retention defaults = %+v", cfg.Observability.Retention)
	}
}

func TestCompileSimpleRetainsExplicitObservabilityCapture(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Observability: ObservabilityConfig{
		Capture: ObservabilityCaptureConfig{
			Mode:             "raw",
			MaxSnapshotBytes: 64 << 10,
			CaptureResponse:  true,
			CaptureReasoning: true,
			HeaderAllowlist:  []string{"x-request-id"},
		},
		Retention: ObservabilityRetentionConfig{SummariesDays: 7, ContentDays: 2, MaxContentStorageMB: 128},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Observability.Capture.Mode != "raw" || !cfg.Observability.Capture.CaptureResponse || !cfg.Observability.Capture.CaptureReasoning {
		t.Fatalf("explicit capture settings lost: %+v", cfg.Observability.Capture)
	}
	if got := cfg.Observability.Capture.HeaderAllowlist; len(got) != 1 || got[0] != "x-request-id" {
		t.Fatalf("header allowlist = %#v", got)
	}
}
