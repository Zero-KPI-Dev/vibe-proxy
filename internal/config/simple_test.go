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
