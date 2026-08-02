package telemetry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxIdentityValueLength = 200

// AgentProfile is a pre-validated, deterministic Agent classifier.
// All configured detectors must match before a profile is selected.
type AgentProfile struct {
	ID     string
	Name   string
	Detect map[string]string
}

type RequestIdentity struct {
	TraceID         string
	SpanID          string
	ParentSpanID    string
	SessionID       string
	SessionName     string
	SessionKind     string
	SessionPath     string
	ParentRequestID string
	AgentID         string
	AgentName       string
	AgentVersion    string
	AgentSource     string
	AgentConfidence string
	ProjectID       string
}

type agentSignature struct {
	id      string
	name    string
	needles []string
}

var builtInAgentSignatures = []agentSignature{
	{id: "claude-code", name: "Claude Code", needles: []string{"claude-code", "claude code"}},
	{id: "codex", name: "Codex", needles: []string{"codex-cli", "openai-codex"}},
	{id: "cursor", name: "Cursor", needles: []string{"cursor"}},
	{id: "opencode", name: "OpenCode", needles: []string{"opencode"}},
	{id: "cline", name: "Cline", needles: []string{"cline"}},
	{id: "aider", name: "Aider", needles: []string{"aider"}},
}

// ExtractRequestIdentity classifies diagnostic identity without changing the
// authenticated principal or authorization inputs.
func ExtractRequestIdentity(r *http.Request, protocolMetadata map[string]string, profiles []AgentProfile) RequestIdentity {
	identity := newTraceIdentity(r.Header.Get("Traceparent"))
	baggage := parseBaggage(r.Header.Values("Baggage"))

	if id, name, version, ok := headerAgent(r); ok {
		identity.AgentID, identity.AgentName, identity.AgentVersion = id, name, version
		identity.AgentSource, identity.AgentConfidence = "header", "explicit"
	} else if id, name, version, ok := baggageAgent(baggage); ok {
		identity.AgentID, identity.AgentName, identity.AgentVersion = id, name, version
		identity.AgentSource, identity.AgentConfidence = "baggage", "explicit"
	} else if id, name, version, ok := metadataAgent(protocolMetadata); ok {
		identity.AgentID, identity.AgentName, identity.AgentVersion = id, name, version
		identity.AgentSource, identity.AgentConfidence = "protocol_metadata", "explicit"
	} else if profile, ok := matchAgentProfile(r, profiles); ok {
		identity.AgentID = cleanIdentityValue(profile.ID)
		identity.AgentName = cleanIdentityValue(profile.Name)
		if identity.AgentName == "" {
			identity.AgentName = identity.AgentID
		}
		identity.AgentSource, identity.AgentConfidence = "profile", "configured"
	} else if signature, ok := matchBuiltInAgent(r.UserAgent()); ok {
		identity.AgentID, identity.AgentName = signature.id, signature.name
		identity.AgentSource, identity.AgentConfidence = "user_agent", "heuristic"
	} else {
		identity.AgentID = "unknown"
		identity.AgentSource = "unknown"
		identity.AgentConfidence = "unknown"
	}

	identity.SessionID = firstClean(
		r.Header.Get("X-Vibe-Session-ID"),
		baggage["gen_ai.conversation.id"],
		protocolMetadata["session_id"],
		protocolMetadata["conversation_id"],
	)
	identity.SessionName = firstClean(r.Header.Get("X-Vibe-Session-Name"), baggage["vibe.session.name"], protocolMetadata["session_name"])
	identity.SessionKind = firstClean(r.Header.Get("X-Vibe-Session-Kind"), baggage["vibe.session.kind"], protocolMetadata["session_kind"])
	identity.SessionPath = firstClean(r.Header.Get("X-Vibe-Session-Path"), baggage["vibe.session.path"], protocolMetadata["session_path"])
	identity.ProjectID = firstClean(r.Header.Get("X-Vibe-Project-ID"), baggage["vibe.project.id"], protocolMetadata["project_id"])
	identity.ParentRequestID = firstClean(r.Header.Get("X-Vibe-Parent-Request-ID"), baggage["vibe.parent_request.id"], protocolMetadata["parent_request_id"])
	return identity
}

// EnrichRequestIdentity applies protocol-native metadata after parsing. It
// only overrides lower-confidence profile, User-Agent, or unknown Agent data;
// explicit HTTP headers and baggage remain authoritative.
func EnrichRequestIdentity(identity RequestIdentity, metadata map[string]string) RequestIdentity {
	if id, name, version, ok := metadataAgent(metadata); ok &&
		(identity.AgentSource == "profile" || identity.AgentSource == "user_agent" || identity.AgentSource == "unknown") {
		identity.AgentID, identity.AgentName, identity.AgentVersion = id, name, version
		identity.AgentSource, identity.AgentConfidence = "protocol_metadata", "explicit"
	}
	if identity.SessionID == "" {
		identity.SessionID = firstClean(metadata["session_id"], metadata["conversation_id"])
	}
	if identity.SessionName == "" {
		identity.SessionName = cleanIdentityValue(metadata["session_name"])
	}
	if identity.SessionKind == "" {
		identity.SessionKind = cleanIdentityValue(metadata["session_kind"])
	}
	if identity.SessionPath == "" {
		identity.SessionPath = cleanIdentityValue(metadata["session_path"])
	}
	if identity.ProjectID == "" {
		identity.ProjectID = cleanIdentityValue(metadata["project_id"])
	}
	if identity.ParentRequestID == "" {
		identity.ParentRequestID = cleanIdentityValue(metadata["parent_request_id"])
	}
	return identity
}

func headerAgent(r *http.Request) (string, string, string, bool) {
	id := cleanIdentityValue(r.Header.Get("X-Vibe-Agent-ID"))
	name := cleanIdentityValue(r.Header.Get("X-Vibe-Agent-Name"))
	version := cleanIdentityValue(r.Header.Get("X-Vibe-Agent-Version"))
	return id, name, version, id != "" || name != "" || version != ""
}

func baggageAgent(baggage map[string]string) (string, string, string, bool) {
	id := cleanIdentityValue(baggage["gen_ai.agent.id"])
	name := cleanIdentityValue(baggage["gen_ai.agent.name"])
	version := cleanIdentityValue(baggage["gen_ai.agent.version"])
	return id, name, version, id != "" || name != "" || version != ""
}

func metadataAgent(metadata map[string]string) (string, string, string, bool) {
	id := cleanIdentityValue(metadata["agent_id"])
	name := cleanIdentityValue(metadata["agent_name"])
	version := cleanIdentityValue(metadata["agent_version"])
	return id, name, version, id != "" || name != "" || version != ""
}

func matchAgentProfile(r *http.Request, profiles []AgentProfile) (AgentProfile, bool) {
	for _, profile := range profiles {
		if cleanIdentityValue(profile.ID) == "" || len(profile.Detect) == 0 {
			continue
		}
		matched := true
		for detector, pattern := range profile.Detect {
			var value string
			switch {
			case detector == "user_agent":
				value = r.UserAgent()
			case strings.HasPrefix(detector, "header."):
				value = r.Header.Get(strings.TrimPrefix(detector, "header."))
			default:
				matched = false
			}
			if !matchGlob(pattern, value) {
				matched = false
			}
		}
		if matched {
			return profile, true
		}
	}
	return AgentProfile{}, false
}

func matchGlob(pattern, value string) bool {
	pattern = regexp.QuoteMeta(strings.ToLower(strings.TrimSpace(pattern)))
	pattern = strings.ReplaceAll(pattern, `\*`, `.*`)
	pattern = strings.ReplaceAll(pattern, `\?`, `.`)
	matched, err := regexp.MatchString("^"+pattern+"$", strings.ToLower(value))
	return err == nil && matched
}

func matchBuiltInAgent(userAgent string) (agentSignature, bool) {
	normalized := strings.ToLower(userAgent)
	for _, signature := range builtInAgentSignatures {
		for _, needle := range signature.needles {
			if strings.Contains(normalized, needle) {
				return signature, true
			}
		}
	}
	return agentSignature{}, false
}

func parseBaggage(values []string) map[string]string {
	allowed := map[string]struct{}{
		"gen_ai.agent.id": {}, "gen_ai.agent.name": {}, "gen_ai.agent.version": {},
		"gen_ai.conversation.id": {}, "vibe.project.id": {}, "vibe.session.name": {},
		"vibe.session.kind": {}, "vibe.session.path": {}, "vibe.parent_request.id": {},
	}
	result := make(map[string]string)
	for _, header := range values {
		for _, member := range strings.Split(header, ",") {
			member = strings.TrimSpace(strings.SplitN(member, ";", 2)[0])
			parts := strings.SplitN(member, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			if _, ok := allowed[key]; !ok {
				continue
			}
			decoded, err := url.PathUnescape(strings.TrimSpace(parts[1]))
			if err != nil {
				continue
			}
			if value := cleanIdentityValue(decoded); value != "" {
				result[key] = value
			}
		}
	}
	return result
}

func firstClean(values ...string) string {
	for _, value := range values {
		if cleaned := cleanIdentityValue(value); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func cleanIdentityValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxIdentityValueLength {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return value
}

func newTraceIdentity(traceparent string) RequestIdentity {
	identity := RequestIdentity{SpanID: randomHex(8)}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(traceparent)), "-")
	if len(parts) == 4 && parts[0] != "ff" && len(parts[0]) == 2 && validHex(parts[0], 2, false) &&
		validHex(parts[1], 32, true) && validHex(parts[2], 16, true) && validHex(parts[3], 2, false) {
		identity.TraceID = parts[1]
		identity.ParentSpanID = parts[2]
	} else {
		identity.TraceID = randomHex(16)
	}
	return identity
}

func validHex(value string, length int, nonzero bool) bool {
	if len(value) != length {
		return false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return false
	}
	return !nonzero || strings.Trim(value, "0") != ""
}

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		copy(b, sum[:])
	}
	return hex.EncodeToString(b)
}
