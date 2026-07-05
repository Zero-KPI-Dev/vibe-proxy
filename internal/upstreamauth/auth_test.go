package upstreamauth

import (
	"net/http"
	"testing"
)

func TestApplyCustomHeadersAndQuery(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat", nil)
	p := Profile{Type: "custom_headers", Headers: map[string]SecretRef{"X-Tenant-ID": "literal:team-a", "X-Api-Token": "literal:secret"}}
	if err := p.Apply(r); err != nil {
		t.Fatalf("apply headers: %v", err)
	}
	if r.Header.Get("X-Tenant-ID") != "team-a" || r.Header.Get("X-Api-Token") != "secret" {
		t.Fatalf("headers not applied: %v", r.Header)
	}
	q := Profile{Type: "custom_query", Query: map[string]SecretRef{"api-version": "literal:2026-01-01"}}
	if err := q.Apply(r); err != nil {
		t.Fatalf("apply query: %v", err)
	}
	if r.URL.Query().Get("api-version") != "2026-01-01" {
		t.Fatalf("query not applied: %s", r.URL.RawQuery)
	}
}
