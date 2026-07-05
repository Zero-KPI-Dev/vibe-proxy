package config

import (
	"os"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
	"gopkg.in/yaml.v3"
)

type LocalProviderInput struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	BaseURL        string   `json:"base_url"`
	APIKeyEnv      string   `json:"api_key_env"`
	AuthType       string   `json:"auth_type"`
	Header         string   `json:"header"`
	Models         []string `json:"models"`
	Alias          string   `json:"alias"`
	AliasModel     string   `json:"alias_model"`
	DefaultModel   string   `json:"default_model"`
	MaxConcurrency int      `json:"max_concurrency"`
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
	secret := upstreamauth.SecretRef("env:" + input.APIKeyEnv)
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
	case "none":
	default:
		auth.Type = "bearer"
		auth.Token = secret
	}
	cfg.Providers[input.ID] = ProviderConfig{Type: providerType, BaseURL: input.BaseURL, Auth: auth, Models: input.Models, MaxConcurrency: input.MaxConcurrency}
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
