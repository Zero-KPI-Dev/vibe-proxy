package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
	"gopkg.in/yaml.v3"
)

type LocalProviderInput struct {
	ID                     string            `json:"id"`
	Type                   string            `json:"type"`
	BaseURL                string            `json:"base_url"`
	CatalogProvider        string            `json:"catalog_provider"`
	APIKeyEnv              string            `json:"api_key_env"`
	APIKey                 string            `json:"api_key"`
	APIKeySource           string            `json:"api_key_source"`
	AuthType               string            `json:"auth_type"`
	Header                 string            `json:"header"`
	Models                 []string          `json:"models"`
	Alias                  string            `json:"alias"`
	AliasModel             string            `json:"alias_model"`
	DefaultModel           string            `json:"default_model"`
	MaxConcurrency         int               `json:"max_concurrency"`
	DefaultImageInput      *string           `json:"default_image_input"`
	ModelImageCapabilities map[string]string `json:"model_image_capabilities"`
}

func UpsertLocalProvider(path string, input LocalProviderInput) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	if cfg.Models.Aliases == nil {
		cfg.Models.Aliases = map[string]string{}
	}
	existing := cfg.Providers[input.ID]
	provider, err := BuildLocalProvider(input, &existing)
	if err != nil {
		return nil, err
	}
	cfg.Providers[input.ID] = provider
	if input.Alias != "" && input.AliasModel != "" {
		cfg.Models.Aliases[input.Alias] = input.ID + "/" + input.AliasModel
	}
	if input.DefaultModel != "" {
		cfg.Models.Default = input.DefaultModel
	}
	if cfg.Version == "" {
		cfg.Version = "vibeproxy.io/v1alpha1"
	}
	compiled, err := CompileSimple(cfg)
	if err != nil {
		return nil, err
	}
	if issues := ValidateRuntime(compiled); HasErrors(issues) {
		return nil, fmt.Errorf("invalid provider configuration: %+v", issues)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return compiled, nil
}

func BuildLocalProvider(input LocalProviderInput, existing *ProviderConfig) (ProviderConfig, error) {
	providerType := input.Type
	if providerType == "" {
		providerType = "openai-compatible"
	}
	authType := input.AuthType
	if authType == "" {
		if providerType == "anthropic" {
			authType = "api_key_header"
		} else {
			authType = "bearer"
		}
	}
	auth, err := buildProviderAuth(input, authType, providerType, existing)
	if err != nil {
		return ProviderConfig{}, err
	}
	provider := ProviderConfig{
		Type:            providerType,
		BaseURL:         input.BaseURL,
		CatalogProvider: input.CatalogProvider,
		Auth:            auth,
		Models:          input.Models,
		MaxConcurrency:  input.MaxConcurrency,
	}
	if existing != nil {
		provider.DefaultCapabilities = existing.DefaultCapabilities
		provider.ModelCapabilities = existing.ModelCapabilities
	}
	if input.DefaultImageInput != nil {
		state := modelcapability.SupportState(*input.DefaultImageInput)
		if !state.Valid() {
			return ProviderConfig{}, fmt.Errorf("invalid default image_input capability %q", state)
		}
		provider.DefaultCapabilities.ImageInput = state
	}
	if input.ModelImageCapabilities != nil {
		provider.ModelCapabilities = make(map[string]modelcapability.ModelCapabilities, len(input.ModelImageCapabilities))
		for model, state := range input.ModelImageCapabilities {
			model = strings.TrimSpace(model)
			if model == "" || state == "" {
				continue
			}
			support := modelcapability.SupportState(state)
			if !support.Valid() {
				return ProviderConfig{}, fmt.Errorf("invalid image_input capability %q for model %q", support, model)
			}
			provider.ModelCapabilities[model] = modelcapability.ModelCapabilities{ImageInput: support}
		}
	}
	return provider, nil
}

func buildProviderAuth(input LocalProviderInput, authType string, providerType string, existing *ProviderConfig) (upstreamauth.Profile, error) {
	if authType == "" {
		if providerType == "anthropic" {
			authType = "api_key_header"
		} else {
			authType = "bearer"
		}
	}
	if authType == "none" {
		return upstreamauth.Profile{Type: "none"}, nil
	}
	source := input.APIKeySource
	if source == "" {
		if input.APIKey != "" {
			source = "literal"
		} else {
			source = "env"
		}
	}
	secret, err := providerSecretRef(input, source, authType, existing)
	if err != nil {
		return upstreamauth.Profile{}, err
	}
	auth := upstreamauth.Profile{Type: authType}
	switch authType {
	case "bearer":
		auth.Token = secret
	case "api_key_header":
		auth.Header = input.Header
		if auth.Header == "" {
			auth.Header = "x-api-key"
		}
		auth.Value = secret
	default:
		auth.Type = "bearer"
		auth.Token = secret
	}
	return auth, nil
}

func providerSecretRef(input LocalProviderInput, source string, authType string, existing *ProviderConfig) (upstreamauth.SecretRef, error) {
	switch source {
	case "literal":
		if input.APIKey != "" {
			return upstreamauth.SecretRef("literal:" + input.APIKey), nil
		}
		if existing != nil {
			if preserved := existingSecretRef(*existing, authType); preserved != "" {
				return preserved, nil
			}
		}
		return "", fmt.Errorf("api_key is required when api_key_source is literal")
	case "env", "":
		if input.APIKeyEnv != "" {
			return upstreamauth.SecretRef("env:" + input.APIKeyEnv), nil
		}
		if existing != nil {
			if preserved := existingSecretRef(*existing, authType); strings.HasPrefix(string(preserved), "env:") {
				return preserved, nil
			}
		}
		return "", fmt.Errorf("api_key_env is required when api_key_source is env")
	default:
		return "", fmt.Errorf("unsupported api_key_source %q", source)
	}
}

func existingSecretRef(provider ProviderConfig, authType string) upstreamauth.SecretRef {
	switch authType {
	case "bearer":
		return provider.Auth.Token
	case "api_key_header":
		return provider.Auth.Value
	default:
		return ""
	}
}
