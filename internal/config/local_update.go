package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
	"gopkg.in/yaml.v3"
)

type LocalProviderInput struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	BaseURL         string   `json:"base_url"`
	CatalogProvider string   `json:"catalog_provider"`
	APIKeyEnv       string   `json:"api_key_env"`
	APIKey          string   `json:"api_key"`
	APIKeySource    string   `json:"api_key_source"`
	AuthType        string   `json:"auth_type"`
	Header          string   `json:"header"`
	Models          []string `json:"models"`
	Alias           string   `json:"alias"`
	AliasModel      string   `json:"alias_model"`
	DefaultModel    string   `json:"default_model"`
	MaxConcurrency  int      `json:"max_concurrency"`
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
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return CompileSimple(cfg)
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
	return ProviderConfig{
		Type:            providerType,
		BaseURL:         input.BaseURL,
		CatalogProvider: input.CatalogProvider,
		Auth:            auth,
		Models:          input.Models,
		MaxConcurrency:  input.MaxConcurrency,
	}, nil
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
