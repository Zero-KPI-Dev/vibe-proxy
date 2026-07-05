package modelresolver

import (
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestResolveAliasAndRawModel(t *testing.T) {
	r := New(Config{AllowRaw: true, Aliases: map[string]Alias{"vibe-coder": {Provider: "anthropic", Model: "claude-sonnet"}}, Providers: []Provider{{ID: "anthropic", Type: "anthropic", BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet"}}, {ID: "deepseek", Type: "openai-compatible", BaseURL: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat"}}}})
	alias, err := r.Resolve(&ir.Request{RequestedModel: "vibe-coder"})
	if err != nil {
		t.Fatalf("alias resolve failed: %v", err)
	}
	if alias.ProviderID != "anthropic" || alias.Model != "claude-sonnet" || !alias.IsAlias {
		t.Fatalf("unexpected alias target: %+v", alias)
	}
	raw, err := r.Resolve(&ir.Request{RequestedModel: "deepseek-chat"})
	if err != nil {
		t.Fatalf("raw resolve failed: %v", err)
	}
	if raw.ProviderID != "deepseek" || raw.Model != "deepseek-chat" || raw.IsAlias {
		t.Fatalf("unexpected raw target: %+v", raw)
	}
}
