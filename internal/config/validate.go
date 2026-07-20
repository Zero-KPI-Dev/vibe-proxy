package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
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
		} else if _, err := url.ParseRequestURI(p.BaseURL); err != nil {
			issues = append(issues, issue("error", path+".base_url", "invalid_base_url", "Provider base_url must be a valid absolute URL."))
		}
		if p.Auth.Type == "" {
			issues = append(issues, issue("warning", path+".auth", "missing_auth", "Provider auth is not configured; this is only valid for local/private providers without auth."))
		}
		if p.Auth.Type == "bearer" && p.Auth.Token == "" {
			issues = append(issues, issue("error", path+".auth.token", "missing_bearer_token", "Bearer auth requires token."))
		}
		if p.Auth.Type == "api_key_header" && (p.Auth.Header == "" || p.Auth.Value == "") {
			issues = append(issues, issue("error", path+".auth", "invalid_api_key_header", "API key header auth requires header and value."))
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
	issues = append(issues, validateMultimodal(cfg.Multimodal)...)
	issues = append(issues, validateVisionFallback(cfg)...)
	return issues
}

func validateMultimodal(cfg MultimodalConfig) []ValidationIssue {
	if !cfg.Enabled {
		return nil
	}
	issues := []ValidationIssue{}
	if cfg.Strategy != "ocr_then_vision" {
		issues = append(issues, issue("error", "multimodal.strategy", "unsupported_multimodal_strategy", "Only ocr_then_vision is supported."))
	}
	hasOCR := cfg.OCR.Provider != "" || cfg.OCR.Endpoint != ""
	if !hasOCR && cfg.VisionFallbackModel == "" {
		issues = append(issues, issue("error", "multimodal", "missing_multimodal_fallback", "An HTTP OCR endpoint or Vision fallback model is required when multimodal fallback is enabled."))
	}
	if hasOCR {
		if cfg.OCR.Provider != "http" {
			issues = append(issues, issue("error", "multimodal.ocr.provider", "unsupported_ocr_provider", "The first OCR release requires provider: http."))
		}
		if cfg.OCR.Endpoint == "" {
			issues = append(issues, issue("error", "multimodal.ocr.endpoint", "missing_ocr_endpoint", "OCR endpoint is required when OCR fallback is configured."))
		} else if endpoint, err := url.Parse(cfg.OCR.Endpoint); err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			issues = append(issues, issue("error", "multimodal.ocr.endpoint", "invalid_ocr_endpoint", "OCR endpoint must be an absolute HTTP or HTTPS URL."))
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
	if model, exists := provider.ModelCapabilities[target.Model]; exists && model.ImageInput != "" {
		support = model.ImageInput
	}
	if support != modelcapability.SupportSupported {
		return []ValidationIssue{issue("error", "multimodal.vision_fallback_model", "vision_fallback_invalid", "Vision fallback model must be explicitly marked image_input: supported.")}
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
