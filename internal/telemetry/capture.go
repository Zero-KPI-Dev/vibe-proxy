package telemetry

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type CaptureMode string

const (
	CaptureModeOff        CaptureMode = "off"
	CaptureModeMetadata   CaptureMode = "metadata"
	CaptureModeStructured CaptureMode = "structured"
	CaptureModeRaw        CaptureMode = "raw"
)

const (
	CaptureStatusNotCaptured = "not_captured"
	CaptureStatusCaptured    = "captured"
	CaptureStatusRedacted    = "redacted"
	CaptureStatusTruncated   = "truncated"
	CaptureStatusDropped     = "dropped"
)

type CapturePolicy struct {
	Mode             CaptureMode
	MaxSnapshotBytes int
	CaptureResponse  bool
	CaptureReasoning bool
	ImagePayloads    string
	HeaderAllowlist  []string
}

type CaptureResult struct {
	Mode             CaptureMode
	Status           string
	Body             []byte
	Headers          http.Header
	OriginalBytes    int
	StoredBytes      int
	RedactionCount   int
	Truncated        bool
	TruncationReason string
	SHA256           string
	Error            string
}

// CapturePayload decodes JSON before applying mandatory redaction. The
// requestMode corresponds to X-Vibe-Capture and may only reduce configured
// capture to metadata or off; it can never elevate capture.
func CapturePayload(body []byte, headers http.Header, policy CapturePolicy, requestMode string) CaptureResult {
	mode := resolveCaptureMode(policy.Mode, requestMode)
	result := CaptureResult{
		Mode:          mode,
		Status:        CaptureStatusNotCaptured,
		Headers:       captureHeaders(headers, policy.HeaderAllowlist),
		OriginalBytes: len(body),
	}
	if mode == CaptureModeOff || mode == CaptureModeMetadata {
		return result
	}

	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		result.Status = CaptureStatusDropped
		result.Error = "invalid_json"
		return result
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		result.Status = CaptureStatusDropped
		result.Error = "multiple_json_values"
		return result
	}

	sanitized, redactions := sanitizeCapturedValue(decoded, "", false, policy.CaptureReasoning)
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		result.Status = CaptureStatusDropped
		result.Error = "encode_sanitized_json"
		return result
	}
	result.RedactionCount = redactions
	result.Status = CaptureStatusCaptured
	if redactions > 0 {
		result.Status = CaptureStatusRedacted
	}
	limit := policy.MaxSnapshotBytes
	if limit <= 0 {
		limit = 256 << 10
	}
	if len(encoded) > limit {
		encoded = boundedJSONPreview(encoded, limit)
		result.Status = CaptureStatusTruncated
		result.Truncated = true
		result.TruncationReason = "max_snapshot_bytes"
	}
	result.Body = encoded
	result.StoredBytes = len(encoded)
	digest := sha256.Sum256(encoded)
	result.SHA256 = hex.EncodeToString(digest[:])
	return result
}

func resolveCaptureMode(configured CaptureMode, requested string) CaptureMode {
	switch configured {
	case CaptureModeOff, CaptureModeMetadata, CaptureModeStructured, CaptureModeRaw:
	default:
		configured = CaptureModeMetadata
	}
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "off":
		return CaptureModeOff
	case "metadata":
		if configured == CaptureModeStructured || configured == CaptureModeRaw {
			return CaptureModeMetadata
		}
	}
	return configured
}

func EffectiveCaptureMode(configured CaptureMode, requested string) CaptureMode {
	return resolveCaptureMode(configured, requested)
}

func captureHeaders(source http.Header, allowlist []string) http.Header {
	result := make(http.Header)
	for _, allowed := range allowlist {
		name := http.CanonicalHeaderKey(strings.TrimSpace(allowed))
		if name == "" || sensitiveCaptureHeader(name) {
			continue
		}
		for _, value := range source.Values(name) {
			value = strings.TrimSpace(value)
			if value == "" || strings.ContainsAny(value, "\r\n") {
				continue
			}
			if len(value) > 1024 {
				value = value[:1024]
			}
			result.Add(name, value)
		}
	}
	return result
}

func sensitiveCaptureHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
		return true
	default:
		return false
	}
}

func sanitizeCapturedValue(value any, key string, binaryContainer, captureReasoning bool) (any, int) {
	if sensitiveCaptureKey(key) {
		return "[REDACTED]", 1
	}
	if !captureReasoning && reasoningCaptureKey(key) {
		return "[REASONING_OMITTED]", 1
	}
	switch typed := value.(type) {
	case map[string]any:
		isBinary := binaryContainer || describesBinaryPayload(typed)
		isReasoning := !captureReasoning && describesReasoningPayload(typed)
		result := make(map[string]any, len(typed))
		redactions := 0
		for childKey, childValue := range typed {
			if isBinary && (strings.EqualFold(childKey, "data") || strings.EqualFold(childKey, "base64")) {
				result[childKey] = binaryDescriptor(childValue, mediaTypeFromMap(typed))
				redactions++
				continue
			}
			if isReasoning && (strings.EqualFold(childKey, "text") || strings.EqualFold(childKey, "content")) {
				result[childKey] = "[REASONING_OMITTED]"
				redactions++
				continue
			}
			clean, count := sanitizeCapturedValue(childValue, childKey, isBinary, captureReasoning)
			result[childKey] = clean
			redactions += count
		}
		return result, redactions
	case []any:
		result := make([]any, len(typed))
		redactions := 0
		for index, child := range typed {
			clean, count := sanitizeCapturedValue(child, key, binaryContainer, captureReasoning)
			result[index] = clean
			redactions += count
		}
		return result, redactions
	case string:
		if descriptor, ok := dataURLDescriptor(typed); ok {
			return descriptor, 1
		}
		return typed, 0
	default:
		return value, 0
	}
}

func reasoningCaptureKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	switch normalized {
	case "reasoning", "reasoning_content", "thinking", "thinking_content", "chain_of_thought":
		return true
	default:
		return false
	}
}

func sensitiveCaptureKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "api_key", "apikey", "token", "access_token", "refresh_token", "password", "passwd", "secret", "client_secret":
		return true
	}
	for _, suffix := range []string{"_api_key", "_token", "_password", "_secret"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func describesBinaryPayload(value map[string]any) bool {
	for _, key := range []string{"type", "media_type", "mime_type"} {
		raw, _ := value[key].(string)
		normalized := strings.ToLower(raw)
		if strings.Contains(normalized, "image") || strings.Contains(normalized, "file") || strings.HasPrefix(normalized, "audio/") || strings.HasPrefix(normalized, "video/") {
			return true
		}
	}
	return false
}

func describesReasoningPayload(value map[string]any) bool {
	raw, _ := value["type"].(string)
	switch strings.ToLower(raw) {
	case "reasoning", "thinking", "analysis":
		return true
	default:
		return false
	}
}

func mediaTypeFromMap(value map[string]any) string {
	for _, key := range []string{"media_type", "mime_type"} {
		if mediaType, _ := value[key].(string); mediaType != "" {
			return mediaType
		}
	}
	return "application/octet-stream"
}

func binaryDescriptor(value any, mediaType string) map[string]any {
	raw, _ := value.(string)
	bytes := decodedBase64(raw)
	return map[string]any{
		"_capture":   "IMAGE_DATA_OMITTED",
		"media_type": mediaType,
		"bytes":      len(bytes),
		"sha256":     digestBytes(bytes),
	}
}

func dataURLDescriptor(value string) (map[string]any, bool) {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return nil, false
	}
	header, data, ok := strings.Cut(value[5:], ",")
	if !ok {
		return map[string]any{"_capture": "IMAGE_DATA_OMITTED", "media_type": "unknown", "bytes": 0}, true
	}
	mediaType := strings.Split(header, ";")[0]
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	payload := []byte(data)
	if strings.Contains(strings.ToLower(header), ";base64") {
		payload = decodedBase64(data)
	}
	return map[string]any{"_capture": "IMAGE_DATA_OMITTED", "media_type": mediaType, "bytes": len(payload), "sha256": digestBytes(payload)}, true
}

func decodedBase64(value string) []byte {
	value = strings.TrimSpace(value)
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded
	}
	return []byte(value)
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func boundedJSONPreview(encoded []byte, limit int) []byte {
	if limit <= 0 {
		return nil
	}
	low, high := 0, len(encoded)
	var best []byte
	for low <= high {
		mid := low + (high-low)/2
		preview := string(encoded[:mid])
		for !utf8.ValidString(preview) && mid > 0 {
			mid--
			preview = string(encoded[:mid])
		}
		candidate, _ := json.Marshal(map[string]any{
			"_capture_truncated": true,
			"preview":            preview,
		})
		if len(candidate) <= limit {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if len(best) > 0 {
		return best
	}
	fallback := []byte(fmt.Sprintf(`{"_capture_truncated":true}`))
	if len(fallback) <= limit {
		return fallback
	}
	if limit >= 2 {
		return []byte("{}")
	}
	return nil
}
