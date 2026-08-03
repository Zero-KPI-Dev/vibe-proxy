package config

import (
	"testing"

	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

func TestValidateRuntimeDetectsInvalidProviderAndAlias(t *testing.T) {
	cfg := &RuntimeConfig{Providers: map[string]ProviderConfig{"bad": {Type: "weird", BaseURL: "not url"}}, ModelResolver: modelresolver.Config{Aliases: map[string]modelresolver.Alias{"x": {Provider: "missing", Model: "model"}}}}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected errors: %+v", issues)
	}
	var sawType, sawAlias bool
	for _, i := range issues {
		if i.Code == "unsupported_provider_type" {
			sawType = true
		}
		if i.Code == "alias_provider_not_found" {
			sawAlias = true
		}
	}
	if !sawType || !sawAlias {
		t.Fatalf("missing expected issues: %+v", issues)
	}
}

func TestValidateRuntimeRejectsInvalidImageCapability(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{
		"local": {
			Type:    "openai-compatible",
			BaseURL: "http://127.0.0.1:3000/v1",
			Auth:    upstreamauth.Profile{Type: "none"},
			DefaultCapabilities: modelcapability.ModelCapabilities{
				ImageInput: "maybe",
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected invalid capability error: %+v", issues)
	}
	for _, issue := range issues {
		if issue.Code == "invalid_image_input_capability" {
			return
		}
	}
	t.Fatalf("missing capability validation issue: %+v", issues)
}

func TestValidateRuntimeAcceptsSimpleConfig(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{"anthropic": {Type: "anthropic", APIKey: "env:ANTHROPIC_API_KEY", Auth: upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: "env:ANTHROPIC_API_KEY"}, Models: []string{"claude"}}}, Models: ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe": "anthropic/claude"}}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if HasErrors(issues) {
		t.Fatalf("unexpected validation errors: %+v", issues)
	}
}

func TestValidateRuntimeAllowsBootstrapWithoutProviders(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{}, Models: ModelsConfig{AllowRaw: true}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if HasErrors(issues) {
		t.Fatalf("bootstrap should not have validation errors: %+v", issues)
	}
	if len(issues) == 0 || issues[0].Code != "missing_providers" {
		t.Fatalf("expected missing providers warning: %+v", issues)
	}
}

func TestValidateRuntimeAcceptsHTTPOCRFallback(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Multimodal: MultimodalConfig{
		Enabled: true,
		OCR: OCRConfig{
			Provider: "http",
			Endpoint: "http://127.0.0.1:32180/v1/ocr",
			Auth:     upstreamauth.Profile{Type: "none"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateRuntime(cfg); HasErrors(issues) {
		t.Fatalf("unexpected OCR validation errors: %+v", issues)
	}
	if cfg.Multimodal.OCR.MaxImages != 4 || cfg.Multimodal.OCR.Cache.MaxEntries != 256 || !cfg.Multimodal.OCR.Cache.IsEnabled() {
		t.Fatalf("OCR defaults not applied: %+v", cfg.Multimodal.OCR)
	}
	if cfg.Multimodal.VisionFallbackStrategy != "assist" || cfg.Multimodal.VisionAssist.MaxPromptChars != 4000 || cfg.Multimodal.VisionAssist.MaxOutputTokens != 1024 || !cfg.Multimodal.VisionAssist.Cache.IsEnabled() {
		t.Fatalf("Vision assist defaults not applied: %+v", cfg.Multimodal)
	}
}

func TestValidateRuntimeDefaultsToBuiltinOCRFallback(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Multimodal: MultimodalConfig{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Multimodal.OCR.Provider != "builtin" {
		t.Fatalf("expected built-in OCR default: %+v", cfg.Multimodal.OCR)
	}
	if issues := ValidateRuntime(cfg); HasErrors(issues) {
		t.Fatalf("unexpected built-in OCR validation errors: %+v", issues)
	}
}

func TestValidateRuntimeRejectsUnsafeOCRConfig(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Multimodal: MultimodalConfig{
		Enabled: true,
		OCR:     OCRConfig{Provider: "http", Endpoint: "file:///tmp/ocr"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected invalid OCR endpoint: %+v", issues)
	}
	for _, issue := range issues {
		if issue.Code == "invalid_ocr_endpoint" {
			return
		}
	}
	t.Fatalf("missing endpoint issue: %+v", issues)
}

func TestValidateRuntimeRequiresExplicitVisionFallbackCapability(t *testing.T) {
	base := SimpleConfig{
		Multimodal: MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision"},
		Providers: map[string]ProviderConfig{"vision": {
			Type:    "openai-compatible",
			BaseURL: "https://vision.example/v1",
			Auth:    upstreamauth.Profile{Type: "none"},
			Models:  []string{"vision-model"},
			DefaultCapabilities: modelcapability.ModelCapabilities{
				ImageInput: modelcapability.SupportSupported,
			},
		}},
		Models: ModelsConfig{Aliases: map[string]string{"vibe-vision": "vision/vision-model"}},
	}
	cfg, err := CompileSimple(base)
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateRuntime(cfg); HasErrors(issues) {
		t.Fatalf("valid Vision fallback rejected: %+v", issues)
	}
	provider := base.Providers["vision"]
	provider.DefaultCapabilities.ImageInput = modelcapability.SupportUnknown
	base.Providers["vision"] = provider
	cfg, err = CompileSimple(base)
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected explicit Vision capability error: %+v", issues)
	}
}

func TestValidateRuntimeVisionFallbackCapabilityPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		provider      ProviderConfig
		wantCode      string
		wantLevel     string
		wantHasErrors bool
	}{
		{
			name: "provider default supported",
			provider: ProviderConfig{
				DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
			},
		},
		{
			name: "model override supported",
			provider: ProviderConfig{
				DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported},
				ModelCapabilities: map[string]modelcapability.ModelCapabilities{
					"vision-model": {ImageInput: modelcapability.SupportSupported},
				},
			},
		},
		{
			name: "model override unsupported",
			provider: ProviderConfig{
				DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
				ModelCapabilities: map[string]modelcapability.ModelCapabilities{
					"vision-model": {ImageInput: modelcapability.SupportUnsupported},
				},
			},
			wantCode:      "vision_fallback_invalid",
			wantLevel:     "error",
			wantHasErrors: true,
		},
		{
			name: "model override unknown",
			provider: ProviderConfig{
				DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
				ModelCapabilities: map[string]modelcapability.ModelCapabilities{
					"vision-model": {ImageInput: modelcapability.SupportUnknown},
				},
			},
			wantCode:      "vision_fallback_invalid",
			wantLevel:     "error",
			wantHasErrors: true,
		},
		{
			name:          "capability absent",
			provider:      ProviderConfig{},
			wantCode:      "vision_fallback_unverified",
			wantLevel:     "warning",
			wantHasErrors: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := test.provider
			provider.Type = "openai-compatible"
			provider.BaseURL = "https://vision.example/v1"
			provider.Auth = upstreamauth.Profile{Type: "none"}
			provider.Models = []string{"vision-model"}
			cfg, err := CompileSimple(SimpleConfig{
				Multimodal: MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision"},
				Providers:  map[string]ProviderConfig{"vision": provider},
				Models:     ModelsConfig{Aliases: map[string]string{"vibe-vision": "vision/vision-model"}},
			})
			if err != nil {
				t.Fatal(err)
			}

			issues := ValidateRuntime(cfg)
			if got := HasErrors(issues); got != test.wantHasErrors {
				t.Fatalf("HasErrors() = %v, want %v; issues: %+v", got, test.wantHasErrors, issues)
			}
			if test.wantCode == "" {
				if hasIssueCode(issues, "vision_fallback_invalid") || hasIssueCode(issues, "vision_fallback_unverified") {
					t.Fatalf("unexpected Vision fallback issue: %+v", issues)
				}
				return
			}
			for _, issue := range issues {
				if issue.Code == test.wantCode {
					if issue.Level != test.wantLevel {
						t.Fatalf("issue level = %q, want %q: %+v", issue.Level, test.wantLevel, issues)
					}
					return
				}
			}
			t.Fatalf("missing %s issue: %+v", test.wantCode, issues)
		})
	}
}

func TestValidateRuntimeRejectsRelativeProviderURLAndUnsupportedAuth(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{
		"bad": {
			Type:    "openai-compatible",
			BaseURL: "/relative",
			Auth:    upstreamauth.Profile{Type: "magic"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !hasIssueCode(issues, "invalid_base_url") || !hasIssueCode(issues, "unsupported_auth_type") {
		t.Fatalf("missing URL or auth validation issues: %+v", issues)
	}
}

func TestValidateRuntimeRejectsUnresolvableDefaultModel(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{
		Providers: map[string]ProviderConfig{
			"local": {
				Type:    "openai-compatible",
				BaseURL: "http://127.0.0.1:3000/v1",
				Auth:    upstreamauth.Profile{Type: "none"},
				Models:  []string{"chat"},
			},
		},
		Models: ModelsConfig{Default: "missing", AllowRaw: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateRuntime(cfg); !hasIssueCode(issues, "default_model_invalid") {
		t.Fatalf("missing default model validation issue: %+v", issues)
	}
}

func TestValidateRuntimeRejectsInvalidAndDuplicateClientKeys(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{
		ClientKeys: []ClientKeyConfig{
			{Name: "agent", KeyHash: "hash", Enabled: true, AllowedModels: []string{"*"}, RPM: 60},
			{Name: "agent", Enabled: true, RPM: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	for _, code := range []string{"duplicate_client_key_name", "missing_client_key_hash", "missing_allowed_models", "invalid_client_key_rpm"} {
		if !hasIssueCode(issues, code) {
			t.Fatalf("missing %s validation issue: %+v", code, issues)
		}
	}
}

func TestValidateRuntimeRejectsUnsafeAgentProfileDetectors(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{AgentProfiles: map[string]AgentProfileConfig{
		"empty":  {},
		"secret": {Detect: map[string]string{"header.authorization": "Bearer *"}},
		"broken": {Detect: map[string]string{"user_agent": "["}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	for _, code := range []string{"missing_agent_detector", "sensitive_agent_detector", "invalid_agent_detector_pattern"} {
		if !hasIssueCode(issues, code) {
			t.Fatalf("missing %s validation issue: %+v", code, issues)
		}
	}
}

func TestValidateRuntimeRejectsUnsafeObservabilityCapture(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Observability: ObservabilityConfig{
		Capture: ObservabilityCaptureConfig{
			Mode:             "everything",
			MaxSnapshotBytes: 8 << 20,
			ImagePayloads:    "full",
			HeaderAllowlist:  []string{"authorization", "cookie"},
		},
		Retention: ObservabilityRetentionConfig{SummariesDays: 2, ContentDays: 3, MaxContentStorageMB: -1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	for _, code := range []string{
		"invalid_capture_mode",
		"invalid_capture_size",
		"invalid_image_payload_policy",
		"sensitive_capture_header",
		"invalid_content_retention",
		"invalid_content_quota",
	} {
		if !hasIssueCode(issues, code) {
			t.Fatalf("missing %s validation issue: %+v", code, issues)
		}
	}
}

func TestValidateRuntimeRejectsCredentialLikeCaptureHeaders(t *testing.T) {
	for _, header := range []string{"x-auth-token", "api-key", "x-client-secret", "x-private-key", "x-credentials"} {
		t.Run(header, func(t *testing.T) {
			cfg, err := CompileSimple(SimpleConfig{Observability: ObservabilityConfig{
				Capture: ObservabilityCaptureConfig{HeaderAllowlist: []string{header}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if issues := ValidateRuntime(cfg); !hasIssueCode(issues, "sensitive_capture_header") {
				t.Fatalf("credential-like header %q was accepted: %+v", header, issues)
			}
		})
	}
}

func TestValidateRuntimeWarnsForRawObservabilityCapture(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Observability: ObservabilityConfig{
		Capture: ObservabilityCaptureConfig{Mode: "raw"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if HasErrors(issues) || !hasIssueCode(issues, "raw_capture_sensitive") {
		t.Fatalf("raw capture warning missing: %+v", issues)
	}
}

func hasIssueCode(issues []ValidationIssue, code string) bool {
	for _, validationIssue := range issues {
		if validationIssue.Code == code {
			return true
		}
	}
	return false
}
