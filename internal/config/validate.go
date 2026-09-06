package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/sensitive"
)

type ValidationIssue struct {
	Level   string `json:"level"`
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateRuntime(cfg *RuntimeConfig) []ValidationIssue {
	issues := []ValidationIssue{}
	if cfg == nil {
		return []ValidationIssue{{Level: "error", Path: "", Code: "nil_config", Message: "Configuration is empty."}}
	}
	if cfg.Server.Listen == "" {
		issues = append(issues, issue("error", "server.listen", "missing_listen", "Server listen address is required."))
	}
	dataPort, dataPortOK := validateListenAddress(&issues, "server.listen", cfg.Server.Listen, false)
	adminPort, adminPortOK := validateListenAddress(&issues, "server.admin_listen", cfg.Server.AdminListen, true)
	if dataPortOK && adminPortOK && dataPort != 0 && dataPort == adminPort {
		issues = append(issues, issue("error", "server.admin_listen", "listener_port_conflict", "Data-plane and control-plane listeners must use different ports."))
	}
	if proxyURL := strings.TrimSpace(cfg.ModelCatalog.ProxyURL); proxyURL != "" && !isAbsoluteHTTPURL(proxyURL) {
		issues = append(issues, issue("error", "model_catalog.proxy_url", "invalid_proxy_url", "Model catalog proxy_url must be a valid absolute HTTP or HTTPS URL."))
	}
	if len(cfg.Providers) == 0 {
		issues = append(issues, issue("warning", "providers", "missing_providers", "No providers are configured yet; data-plane requests will fail until one is added."))
	}
	for id, p := range cfg.Providers {
		path := "providers." + id
		if strings.TrimSpace(id) == "" {
			issues = append(issues, issue("error", "providers", "empty_provider_id", "Provider id cannot be empty."))
		}
		if p.Type == "" {
			issues = append(issues, issue("error", path+".type", "missing_provider_type", "Provider type is required."))
		}
		if p.Type != "anthropic" && p.Type != "openai-compatible" {
			issues = append(issues, issue("error", path+".type", "unsupported_provider_type", fmt.Sprintf("Provider type %q is not supported yet.", p.Type)))
		}
		if p.BaseURL == "" {
			issues = append(issues, issue("error", path+".base_url", "missing_base_url", "Provider base_url is required."))
		} else if !isAbsoluteHTTPURL(p.BaseURL) {
			issues = append(issues, issue("error", path+".base_url", "invalid_base_url", "Provider base_url must be a valid absolute URL."))
		}
		switch p.Auth.Type {
		case "":
			issues = append(issues, issue("warning", path+".auth", "missing_auth", "Provider auth is not configured; this is only valid for local/private providers without auth."))
		case "none":
		case "bearer":
			if p.Auth.Token == "" {
				issues = append(issues, issue("error", path+".auth.token", "missing_bearer_token", "Bearer auth requires token."))
			}
		case "api_key_header":
			if strings.TrimSpace(p.Auth.Header) == "" || p.Auth.Value == "" {
				issues = append(issues, issue("error", path+".auth", "invalid_api_key_header", "API key header auth requires header and value."))
			}
		case "custom_headers":
			if len(p.Auth.Headers) == 0 {
				issues = append(issues, issue("error", path+".auth.headers", "missing_custom_headers", "Custom header auth requires at least one header."))
			}
			for header, value := range p.Auth.Headers {
				if strings.TrimSpace(header) == "" || value == "" {
					issues = append(issues, issue("error", path+".auth.headers", "invalid_custom_header", "Custom header names and values cannot be empty."))
					break
				}
			}
		case "custom_query":
			if len(p.Auth.Query) == 0 {
				issues = append(issues, issue("error", path+".auth.query", "missing_custom_query", "Custom query auth requires at least one parameter."))
			}
			for name, value := range p.Auth.Query {
				if strings.TrimSpace(name) == "" || value == "" {
					issues = append(issues, issue("error", path+".auth.query", "invalid_custom_query", "Custom query parameter names and values cannot be empty."))
					break
				}
			}
		default:
			issues = append(issues, issue("error", path+".auth.type", "unsupported_auth_type", fmt.Sprintf("Provider auth type %q is not supported.", p.Auth.Type)))
		}
		for field, duration := range map[string]time.Duration{"timeout": p.Timeout.Duration, "first_token_timeout": p.FirstTokenTimeout.Duration, "stream_idle_timeout": p.StreamIdleTimeout.Duration} {
			if duration < 0 {
				issues = append(issues, issue("error", path+"."+field, "invalid_provider_timeout", "Provider timeouts must not be negative; omit them to use defaults."))
			}
		}
		if !p.DefaultCapabilities.ImageInput.Valid() {
			issues = append(issues, issue("error", path+".default_capabilities.image_input", "invalid_image_input_capability", "image_input must be unknown, supported, or unsupported."))
		}
		for model, capabilities := range p.ModelCapabilities {
			if !capabilities.ImageInput.Valid() {
				issues = append(issues, issue("error", path+".model_capabilities."+model+".image_input", "invalid_image_input_capability", "image_input must be unknown, supported, or unsupported."))
			}
		}
	}
	for alias, target := range cfg.ModelResolver.Aliases {
		if alias == "" {
			issues = append(issues, issue("error", "models.aliases", "empty_alias", "Model alias cannot be empty."))
			continue
		}
		if target.Provider == "" || target.Model == "" {
			issues = append(issues, issue("error", "models.aliases."+alias, "invalid_alias_target", "Alias target must include provider and model."))
			continue
		}
		if _, ok := cfg.Providers[target.Provider]; !ok {
			issues = append(issues, issue("error", "models.aliases."+alias, "alias_provider_not_found", "Alias points to an unknown provider."))
		}
	}
	if cfg.ModelResolver.DefaultModel != "" {
		resolver := modelresolver.New(cfg.ModelResolver)
		if _, resolveErr := resolver.Resolve(&ir.Request{RequestedModel: cfg.ModelResolver.DefaultModel}); resolveErr != nil {
			issues = append(issues, issue("error", "models.default", "default_model_invalid", "Default model cannot be resolved."))
		}
	}
	seenClientKeys := make(map[string]struct{}, len(cfg.ClientKeys))
	for i, clientKey := range cfg.ClientKeys {
		path := fmt.Sprintf("client_keys.%d", i)
		name := strings.TrimSpace(clientKey.Name)
		if name == "" {
			issues = append(issues, issue("error", path+".name", "missing_client_key_name", "Client key name is required."))
		} else if _, exists := seenClientKeys[name]; exists {
			issues = append(issues, issue("error", path+".name", "duplicate_client_key_name", "Client key names must be unique."))
		} else {
			seenClientKeys[name] = struct{}{}
		}
		if strings.TrimSpace(clientKey.KeyHash) == "" {
			issues = append(issues, issue("error", path+".key_hash", "missing_client_key_hash", "Client key hash is required."))
		} else if clientKey.RawKey != "" && bcrypt.CompareHashAndPassword([]byte(clientKey.KeyHash), []byte(clientKey.RawKey)) != nil {
			issues = append(issues, issue("error", path+".raw_key", "client_key_raw_hash_mismatch", "Client key raw value does not match its hash."))
		}
		if clientKey.RawKey != "" && clientKey.KeyPrefix != clientKeyPrefix(clientKey.RawKey) {
			issues = append(issues, issue("error", path+".key_prefix", "client_key_prefix_mismatch", "Client key prefix does not match its raw value."))
		}
		if len(clientKey.AllowedModels) == 0 {
			issues = append(issues, issue("error", path+".allowed_models", "missing_allowed_models", "Client key must allow at least one model or wildcard."))
		}
		if clientKey.RPM <= 0 {
			issues = append(issues, issue("error", path+".rpm", "invalid_client_key_rpm", "Client key RPM must be greater than zero."))
		}
	}
	issues = append(issues, validateAgentProfiles(cfg.AgentProfiles)...)
	issues = append(issues, validateObservability(cfg.Observability)...)
	issues = append(issues, validateMultimodal(cfg.Multimodal)...)
	issues = append(issues, validateVisionFallback(cfg)...)
	return issues
}

func validateListenAddress(issues *[]ValidationIssue, configPath, raw string, loopbackOnly bool) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		if loopbackOnly {
			*issues = append(*issues, issue("error", configPath, "missing_admin_listen", "Control-plane listen address is required."))
		}
		return 0, false
	}
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		*issues = append(*issues, issue("error", configPath, "invalid_listen_address", "Listen address must include a valid host and port."))
		return 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		*issues = append(*issues, issue("error", configPath, "invalid_listen_port", "Listen port must be between 0 and 65535."))
		return 0, false
	}
	if loopbackOnly {
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.IsLoopback() {
			*issues = append(*issues, issue("error", configPath, "admin_listen_not_loopback", "Control-plane listen address must use a literal loopback IP."))
			return 0, false
		}
	}
	return port, true
}

func validateObservability(cfg ObservabilityConfig) []ValidationIssue {
	issues := []ValidationIssue{}
	switch cfg.Capture.Mode {
	case "metadata", "structured":
	case "raw":
		issues = append(issues, issue("warning", "observability.capture.mode", "raw_capture_sensitive", "Raw capture preserves the wire JSON shape after mandatory redaction and should be enabled only when needed."))
	default:
		issues = append(issues, issue("error", "observability.capture.mode", "invalid_capture_mode", "Capture mode must be metadata, structured, or raw."))
	}
	if cfg.Capture.MaxSnapshotBytes < 1<<10 || cfg.Capture.MaxSnapshotBytes > 4<<20 {
		issues = append(issues, issue("error", "observability.capture.max_snapshot_bytes", "invalid_capture_size", "Snapshot size must be between 1 KiB and 4 MiB."))
	}
	if cfg.Capture.ImagePayloads != "metadata" {
		issues = append(issues, issue("error", "observability.capture.image_payloads", "invalid_image_payload_policy", "Image payloads must use metadata-only capture."))
	}
	if len(cfg.Capture.HeaderAllowlist) > 32 {
		issues = append(issues, issue("error", "observability.capture.header_allowlist", "invalid_capture_header", "At most 32 capture headers may be configured."))
	}
	for index, rawHeader := range cfg.Capture.HeaderAllowlist {
		header := strings.ToLower(strings.TrimSpace(rawHeader))
		headerPath := fmt.Sprintf("observability.capture.header_allowlist.%d", index)
		if isSensitiveHeader(header) {
			issues = append(issues, issue("error", headerPath, "sensitive_capture_header", "Credential and cookie headers can never be captured."))
			continue
		}
		if !isValidHTTPHeaderName(header) || len(header) > 100 {
			issues = append(issues, issue("error", headerPath, "invalid_capture_header", "Capture header names must be valid HTTP field names of at most 100 characters."))
		}
	}
	if cfg.Retention.SummariesDays < 1 || cfg.Retention.SummariesDays > 3650 {
		issues = append(issues, issue("error", "observability.retention.summaries_days", "invalid_summary_retention", "Summary retention must be between 1 and 3650 days."))
	}
	if cfg.Retention.ContentDays < 1 || cfg.Retention.ContentDays > cfg.Retention.SummariesDays {
		issues = append(issues, issue("error", "observability.retention.content_days", "invalid_content_retention", "Content retention must be positive and no longer than summary retention."))
	}
	if cfg.Retention.MaxContentStorageMB < 1 || cfg.Retention.MaxContentStorageMB > 102400 {
		issues = append(issues, issue("error", "observability.retention.max_content_storage_mb", "invalid_content_quota", "Content storage quota must be between 1 MiB and 100 GiB."))
	}
	return issues
}

func isSensitiveHeader(header string) bool {
	return sensitive.IsCredentialName(header)
}

func isValidHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}

func validateAgentProfiles(profiles []AgentProfileConfig) []ValidationIssue {
	issues := []ValidationIssue{}
	for index, profile := range profiles {
		basePath := fmt.Sprintf("agent_profiles.%s", profile.ID)
		if strings.TrimSpace(profile.ID) == "" || len(profile.ID) > 100 {
			issues = append(issues, issue("error", fmt.Sprintf("agent_profiles.%d", index), "invalid_agent_profile_id", "Agent profile id must contain 1 to 100 characters."))
		}
		if len(profile.Detect) == 0 {
			issues = append(issues, issue("error", basePath+".detect", "missing_agent_detector", "Agent profile requires at least one detector."))
			continue
		}
		for detector, pattern := range profile.Detect {
			detectorPath := basePath + ".detect." + detector
			if detector != "user_agent" && !strings.HasPrefix(detector, "header.") {
				issues = append(issues, issue("error", detectorPath, "unsupported_agent_detector", "Agent detector must be user_agent or an allowlisted header."))
				continue
			}
			if strings.HasPrefix(detector, "header.") {
				header := strings.ToLower(strings.TrimPrefix(detector, "header."))
				if sensitive.IsCredentialName(header) {
					issues = append(issues, issue("error", detectorPath, "sensitive_agent_detector", "Sensitive credential headers cannot classify an Agent."))
				}
				if !isValidHTTPHeaderName(header) || len(header) > 100 {
					issues = append(issues, issue("error", detectorPath, "invalid_agent_detector_header", "Agent detector headers must be valid HTTP field names of at most 100 characters."))
				}
			}
			if strings.TrimSpace(pattern) == "" || len(pattern) > 200 {
				issues = append(issues, issue("error", detectorPath, "invalid_agent_detector_pattern", "Agent detector pattern must contain 1 to 200 characters."))
				continue
			}
			if _, err := path.Match(strings.ToLower(pattern), "validation-value"); err != nil {
				issues = append(issues, issue("error", detectorPath, "invalid_agent_detector_pattern", "Agent detector pattern is not a valid glob."))
			}
		}
	}
	return issues
}

func isAbsoluteHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func validateMultimodal(cfg MultimodalConfig) []ValidationIssue {
	if !cfg.Enabled {
		return nil
	}
	issues := []ValidationIssue{}
	if cfg.Strategy != "ocr_then_vision" {
		issues = append(issues, issue("error", "multimodal.strategy", "unsupported_multimodal_strategy", "Only ocr_then_vision is supported."))
	}
	switch cfg.VisionFallbackStrategy {
	case "assist", "takeover", "reject":
	default:
		issues = append(issues, issue("error", "multimodal.vision_fallback_strategy", "unsupported_vision_fallback_strategy", "Vision fallback strategy must be assist, takeover, or reject."))
	}
	if cfg.VisionAssist.MaxPromptChars <= 0 || cfg.VisionAssist.MaxPromptChars > 50000 {
		issues = append(issues, issue("error", "multimodal.vision_assist.max_prompt_chars", "invalid_vision_assist_limit", "Vision assist prompt text must be between 1 and 50000 characters."))
	}
	if cfg.VisionAssist.MaxOutputTokens <= 0 || cfg.VisionAssist.MaxOutputTokens > 8192 {
		issues = append(issues, issue("error", "multimodal.vision_assist.max_output_tokens", "invalid_vision_assist_limit", "Vision assist output must be between 1 and 8192 tokens."))
	}
	if cfg.VisionAssist.Cache.IsEnabled() && (cfg.VisionAssist.Cache.MaxEntries <= 0 || cfg.VisionAssist.Cache.MaxEntries > 4096 || cfg.VisionAssist.Cache.TTL.Duration <= 0) {
		issues = append(issues, issue("error", "multimodal.vision_assist.cache", "invalid_vision_assist_cache", "Vision assist cache requires a positive TTL and 1 to 4096 entries."))
	}
	hasOCR := cfg.OCR.Provider != ""
	if !hasOCR && cfg.VisionFallbackModel == "" {
		issues = append(issues, issue("error", "multimodal", "missing_multimodal_fallback", "A built-in OCR provider, HTTP OCR endpoint, or Vision fallback model is required when multimodal fallback is enabled."))
	}
	if hasOCR {
		switch cfg.OCR.Provider {
		case "builtin":
			// The model and WASM runtime are embedded in the executable.
		case "http":
			if cfg.OCR.Endpoint == "" {
				issues = append(issues, issue("error", "multimodal.ocr.endpoint", "missing_ocr_endpoint", "OCR endpoint is required for provider: http."))
			} else if endpoint, err := url.Parse(cfg.OCR.Endpoint); err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
				issues = append(issues, issue("error", "multimodal.ocr.endpoint", "invalid_ocr_endpoint", "OCR endpoint must be an absolute HTTP or HTTPS URL."))
			}
		default:
			issues = append(issues, issue("error", "multimodal.ocr.provider", "unsupported_ocr_provider", "OCR provider must be builtin or http."))
		}
	}
	if cfg.OCR.Timeout.Duration <= 0 || cfg.OCR.Timeout.Duration > 2*time.Minute {
		issues = append(issues, issue("error", "multimodal.ocr.timeout", "invalid_ocr_timeout", "OCR timeout must be greater than zero and no more than 2 minutes."))
	}
	if cfg.OCR.MinConfidence < 0 || cfg.OCR.MinConfidence > 1 {
		issues = append(issues, issue("error", "multimodal.ocr.min_confidence", "invalid_ocr_confidence", "OCR min_confidence must be between 0 and 1."))
	}
	if cfg.OCR.MinTextChars <= 0 || cfg.OCR.MinTextChars > 1000 {
		issues = append(issues, issue("error", "multimodal.ocr.min_text_chars", "invalid_ocr_min_text", "OCR min_text_chars must be between 1 and 1000."))
	}
	if cfg.OCR.MaxImages <= 0 || cfg.OCR.MaxImages > 16 {
		issues = append(issues, issue("error", "multimodal.ocr.max_images", "invalid_ocr_image_limit", "OCR max_images must be between 1 and 16."))
	}
	if cfg.OCR.MaxImageBytes <= 0 || cfg.OCR.MaxImageBytes > 20<<20 {
		issues = append(issues, issue("error", "multimodal.ocr.max_image_bytes", "invalid_ocr_image_limit", "OCR max_image_bytes must be between 1 and 20 MiB."))
	}
	if cfg.OCR.MaxTotalImageBytes <= 0 || cfg.OCR.MaxTotalImageBytes > 64<<20 || cfg.OCR.MaxTotalImageBytes < cfg.OCR.MaxImageBytes {
		issues = append(issues, issue("error", "multimodal.ocr.max_total_image_bytes", "invalid_ocr_image_limit", "OCR total image limit must be at least the single-image limit and no more than 64 MiB."))
	}
	if cfg.OCR.MaxTextCharsPerImage <= 0 || cfg.OCR.MaxTextCharsPerImage > 50000 || cfg.OCR.MaxTextCharsTotal < cfg.OCR.MaxTextCharsPerImage || cfg.OCR.MaxTextCharsTotal > 200000 {
		issues = append(issues, issue("error", "multimodal.ocr.max_text_chars_total", "invalid_ocr_text_limit", "OCR text limits are invalid or exceed the hard limit."))
	}
	if hasOCR && cfg.OCR.Cache.IsEnabled() && (cfg.OCR.Cache.MaxEntries <= 0 || cfg.OCR.Cache.MaxEntries > 4096 || cfg.OCR.Cache.TTL.Duration <= 0) {
		issues = append(issues, issue("error", "multimodal.ocr.cache", "invalid_ocr_cache", "OCR cache requires a positive TTL and 1 to 4096 entries."))
	}
	return issues
}

func validateVisionFallback(cfg *RuntimeConfig) []ValidationIssue {
	if cfg == nil || !cfg.Multimodal.Enabled || cfg.Multimodal.VisionFallbackModel == "" {
		return nil
	}
	resolver := modelresolver.New(cfg.ModelResolver)
	target, resolveErr := resolver.Resolve(&ir.Request{RequestedModel: cfg.Multimodal.VisionFallbackModel})
	if resolveErr != nil {
		return []ValidationIssue{issue("error", "multimodal.vision_fallback_model", "vision_fallback_invalid", "Vision fallback model cannot be resolved.")}
	}
	provider, ok := cfg.Providers[target.ProviderID]
	if !ok {
		return []ValidationIssue{issue("error", "multimodal.vision_fallback_model", "vision_fallback_invalid", "Vision fallback provider is not configured.")}
	}
	support := provider.DefaultCapabilities.ImageInput
	explicit := support != ""
	if model, exists := provider.ModelCapabilities[target.Model]; exists && model.ImageInput != "" {
		support = model.ImageInput
		explicit = true
	}
	if !explicit {
		return []ValidationIssue{issue("warning", "multimodal.vision_fallback_model", "vision_fallback_unverified", "Vision fallback image support will be verified from models.dev at runtime.")}
	}
	if support != modelcapability.SupportSupported {
		return []ValidationIssue{issue("error", "multimodal.vision_fallback_model", "vision_fallback_invalid", "Vision fallback model is explicitly marked as not supporting image input.")}
	}
	return nil
}

func HasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}
func issue(level, path, code, message string) ValidationIssue {
	return ValidationIssue{Level: level, Path: path, Code: code, Message: message}
}
