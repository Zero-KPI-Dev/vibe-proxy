package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
	"gopkg.in/yaml.v3"
)

type MultimodalAdminInput struct {
	Enabled             bool    `json:"enabled"`
	Provider            string  `json:"provider"`
	Endpoint            string  `json:"endpoint"`
	AuthType            string  `json:"auth_type"`
	APIKeySource        string  `json:"api_key_source"`
	APIKeyEnv           string  `json:"api_key_env"`
	APIKey              string  `json:"api_key"`
	Header              string  `json:"header"`
	VisionFallbackModel string  `json:"vision_fallback_model"`
	MinConfidence       float64 `json:"min_confidence"`
	MinTextChars        int     `json:"min_text_chars"`
	MaxImages           int     `json:"max_images"`
}

func SaveMultimodal(path string, input MultimodalAdminInput) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	next, err := BuildMultimodal(input, &cfg.Multimodal)
	if err != nil {
		return nil, err
	}
	cfg.Multimodal = next
	compiled, err := CompileSimple(cfg)
	if err != nil {
		return nil, err
	}
	if issues := ValidateRuntime(compiled); HasErrors(issues) {
		return nil, fmt.Errorf("invalid multimodal configuration: %+v", issues)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, out); err != nil {
		return nil, err
	}
	return compiled, nil
}

func BuildMultimodal(input MultimodalAdminInput, existing *MultimodalConfig) (MultimodalConfig, error) {
	result := MultimodalConfig{
		Enabled:             input.Enabled,
		Strategy:            "ocr_then_vision",
		VisionFallbackModel: strings.TrimSpace(input.VisionFallbackModel),
	}
	if existing != nil {
		result.OCR.Timeout = existing.OCR.Timeout
		result.OCR.MaxImageBytes = existing.OCR.MaxImageBytes
		result.OCR.MaxTotalImageBytes = existing.OCR.MaxTotalImageBytes
		result.OCR.MaxTextCharsPerImage = existing.OCR.MaxTextCharsPerImage
		result.OCR.MaxTextCharsTotal = existing.OCR.MaxTextCharsTotal
		result.OCR.RemoteImages = existing.OCR.RemoteImages
		result.OCR.Cache = existing.OCR.Cache
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		if endpoint != "" {
			// Backward compatibility for older control planes that did not
			// submit an explicit provider.
			provider = "http"
		} else {
			provider = "builtin"
		}
	}
	switch provider {
	case "builtin":
		result.OCR.Provider = "builtin"
	case "http":
		result.OCR.Provider = "http"
		result.OCR.Endpoint = endpoint
		auth, err := buildMultimodalAuth(input, existing)
		if err != nil {
			return MultimodalConfig{}, err
		}
		result.OCR.Auth = auth
	default:
		return MultimodalConfig{}, fmt.Errorf("unsupported OCR provider %q", provider)
	}
	if input.MinConfidence > 0 {
		result.OCR.MinConfidence = input.MinConfidence
	}
	if input.MinTextChars > 0 {
		result.OCR.MinTextChars = input.MinTextChars
	}
	if input.MaxImages > 0 {
		result.OCR.MaxImages = input.MaxImages
	}
	return result, nil
}

func buildMultimodalAuth(input MultimodalAdminInput, existing *MultimodalConfig) (upstreamauth.Profile, error) {
	authType := input.AuthType
	if authType == "" || authType == "none" {
		return upstreamauth.Profile{Type: "none"}, nil
	}
	if authType != "bearer" && authType != "api_key_header" {
		return upstreamauth.Profile{}, fmt.Errorf("unsupported OCR auth type %q", authType)
	}
	var current upstreamauth.SecretRef
	if existing != nil {
		switch existing.OCR.Auth.Type {
		case "bearer":
			current = existing.OCR.Auth.Token
		case "api_key_header":
			current = existing.OCR.Auth.Value
		}
	}
	var secret upstreamauth.SecretRef
	switch input.APIKeySource {
	case "literal":
		if input.APIKey != "" {
			secret = upstreamauth.SecretRef("literal:" + input.APIKey)
		} else if strings.HasPrefix(string(current), "literal:") {
			secret = current
		}
	case "env", "":
		if input.APIKeyEnv != "" {
			secret = upstreamauth.SecretRef("env:" + input.APIKeyEnv)
		} else if strings.HasPrefix(string(current), "env:") {
			secret = current
		}
	default:
		return upstreamauth.Profile{}, fmt.Errorf("unsupported OCR key source %q", input.APIKeySource)
	}
	if secret == "" {
		return upstreamauth.Profile{}, fmt.Errorf("OCR authentication secret is required")
	}
	if authType == "bearer" {
		return upstreamauth.Profile{Type: "bearer", Token: secret}, nil
	}
	header := strings.TrimSpace(input.Header)
	if header == "" && existing != nil && existing.OCR.Auth.Type == "api_key_header" {
		header = existing.OCR.Auth.Header
	}
	if header == "" {
		header = "x-api-key"
	}
	return upstreamauth.Profile{Type: "api_key_header", Header: header, Value: secret}, nil
}
