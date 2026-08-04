package telemetry

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestExtractRequestIdentityPrecedence(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/messages", nil)
	r.Header.Set("User-Agent", "Cursor/1.2")
	r.Header.Set("Baggage", "gen_ai.agent.id=baggage-agent,gen_ai.agent.name=Baggage,gen_ai.conversation.id=baggage-session")
	r.Header.Set("X-Vibe-Agent-ID", " header-agent ")
	r.Header.Set("X-Vibe-Agent-Name", " Header Agent ")
	r.Header.Set("X-Vibe-Agent-Version", " 2.0 ")
	r.Header.Set("X-Vibe-Session-ID", " header-session ")
	r.Header.Set("X-Vibe-Session-Name", " Fix auth ")
	r.Header.Set("X-Vibe-Session-Kind", " coding ")
	r.Header.Set("X-Vibe-Session-Path", " /review/auth ")
	r.Header.Set("X-Vibe-Project-ID", " vibe-proxy ")
	r.Header.Set("X-Vibe-Parent-Request-ID", " request-before-this ")

	got := ExtractRequestIdentity(r, map[string]string{
		"agent_id": "protocol-agent", "session_id": "protocol-session",
	}, []AgentProfile{{ID: "profile-agent", Detect: map[string]string{"user_agent": "*cursor*"}}})

	if got.AgentID != "header-agent" || got.AgentName != "Header Agent" || got.AgentVersion != "2.0" {
		t.Fatalf("explicit agent did not win: %+v", got)
	}
	if got.AgentSource != "header" || got.AgentConfidence != "explicit" {
		t.Fatalf("unexpected classification: %+v", got)
	}
	if got.SessionID != "header-session" || got.SessionName != "Fix auth" || got.SessionKind != "coding" || got.SessionPath != "/review/auth" {
		t.Fatalf("explicit session did not win: %+v", got)
	}
	if got.ProjectID != "vibe-proxy" || got.ParentRequestID != "request-before-this" {
		t.Fatalf("missing explicit correlation fields: %+v", got)
	}
}

func TestExtractRequestIdentityUsesBaggageThenMetadata(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	r.Header.Set("Baggage", "gen_ai.agent.id=codex-cli,gen_ai.agent.name=Codex%20CLI,gen_ai.conversation.id=session-42,vibe.project.id=project-7")
	got := ExtractRequestIdentity(r, map[string]string{"agent_id": "metadata-agent", "session_id": "metadata-session"}, nil)
	if got.AgentID != "codex-cli" || got.AgentName != "Codex CLI" || got.AgentSource != "baggage" {
		t.Fatalf("baggage agent not extracted: %+v", got)
	}
	if got.SessionID != "session-42" || got.ProjectID != "project-7" {
		t.Fatalf("baggage correlation not extracted: %+v", got)
	}

	r.Header.Del("Baggage")
	got = ExtractRequestIdentity(r, map[string]string{"agent_id": "metadata-agent", "agent_name": "Metadata Agent", "session_id": "metadata-session"}, nil)
	if got.AgentID != "metadata-agent" || got.AgentSource != "protocol_metadata" || got.SessionID != "metadata-session" {
		t.Fatalf("protocol metadata not extracted: %+v", got)
	}
}

func TestExtractRequestIdentityUsesProfileAndBuiltinUserAgent(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/messages", nil)
	r.Header.Set("User-Agent", "my-wrapper Cursor/1.2")
	got := ExtractRequestIdentity(r, nil, []AgentProfile{{ID: "configured-cursor", Name: "Team Cursor", Detect: map[string]string{"user_agent": "*cursor*"}}})
	if got.AgentID != "configured-cursor" || got.AgentName != "Team Cursor" || got.AgentSource != "profile" || got.AgentConfidence != "configured" {
		t.Fatalf("profile not applied: %+v", got)
	}

	r.Header.Set("User-Agent", "claude-code/1.0")
	got = ExtractRequestIdentity(r, nil, nil)
	if got.AgentID != "claude-code" || got.AgentSource != "user_agent" || got.AgentConfidence != "heuristic" {
		t.Fatalf("built-in user agent not detected: %+v", got)
	}
	if got.SessionID != "" {
		t.Fatalf("user agent must not fabricate a session: %+v", got)
	}
}

func TestExtractRequestIdentityDoesNotMatchMissingProfileHeader(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/messages", nil)
	profile := AgentProfile{ID: "configured-agent", Detect: map[string]string{"header.x-agent-client": "*"}}
	got := ExtractRequestIdentity(r, nil, []AgentProfile{profile})
	if got.AgentID != "unknown" || got.AgentSource != "unknown" {
		t.Fatalf("missing detector header matched wildcard profile: %+v", got)
	}

	r.Header.Set("X-Agent-Client", "codex")
	got = ExtractRequestIdentity(r, nil, []AgentProfile{profile})
	if got.AgentID != "configured-agent" || got.AgentSource != "profile" {
		t.Fatalf("present detector header did not match wildcard profile: %+v", got)
	}
}

func TestExtractRequestIdentityTraceparent(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/messages", nil)
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	got := ExtractRequestIdentity(r, nil, nil)
	if got.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || got.ParentSpanID != "00f067aa0ba902b7" {
		t.Fatalf("valid traceparent not accepted: %+v", got)
	}
	if got.SpanID == "" || got.SpanID == got.ParentSpanID {
		t.Fatalf("gateway span was not generated: %+v", got)
	}

	r.Header.Set("Traceparent", "00-00000000000000000000000000000000-0000000000000000-01")
	got = ExtractRequestIdentity(r, nil, nil)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got.TraceID) || strings.Trim(got.TraceID, "0") == "" {
		t.Fatalf("invalid traceparent did not produce a trace id: %+v", got)
	}
	if got.ParentSpanID != "" {
		t.Fatalf("invalid traceparent leaked parent span: %+v", got)
	}
}

func TestExtractRequestIdentityRejectsUnsafeValuesAndDoesNotInventSession(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/messages", nil)
	r.Header.Set("X-Vibe-Agent-ID", strings.Repeat("x", 201))
	r.Header.Set("X-Vibe-Agent-Name", "unsafe\x00name")
	r.Header.Set("User-Agent", "ordinary-http-client")
	got := ExtractRequestIdentity(r, nil, nil)
	if got.AgentID != "unknown" || got.AgentName != "" || got.AgentSource != "unknown" {
		t.Fatalf("unsafe identity accepted: %+v", got)
	}
	if got.SessionID != "" {
		t.Fatalf("session was fabricated: %+v", got)
	}
}
