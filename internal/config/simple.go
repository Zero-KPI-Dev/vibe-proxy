package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
	"gopkg.in/yaml.v3"
)

type SimpleConfig struct {
	Version       string                        `yaml:"version"`
	Server        ServerConfig                  `yaml:"server"`
	Security      SecurityConfig                `yaml:"security"`
	Storage       StorageConfig                 `yaml:"storage"`
	ClientKeys    []ClientKeyConfig             `yaml:"client_keys"`
	Providers     map[string]ProviderConfig     `yaml:"providers"`
	Models        ModelsConfig                  `yaml:"models"`
	AgentProfiles map[string]AgentProfileConfig `yaml:"agent_profiles"`
	Routes        []AdvancedRouteConfig         `yaml:"routes"`
}

type ProviderConfig struct {
	Type            string                 `yaml:"type"`
	BaseURL         string                 `yaml:"base_url"`
	CatalogProvider string                 `yaml:"catalog_provider,omitempty"`
	APIKey          upstreamauth.SecretRef `yaml:"api_key"`
	Auth            upstreamauth.Profile   `yaml:"auth"`
	Models          []string               `yaml:"models"`
	Priority        int                    `yaml:"priority"`
	Timeout         Duration               `yaml:"timeout"`
	MaxConcurrency  int                    `yaml:"max_concurrency"`
}

type ModelsConfig struct {
	Default  string            `yaml:"default"`
	AllowRaw bool              `yaml:"allow_raw"`
	Aliases  map[string]string `yaml:"aliases"`
}

type AgentProfileConfig struct {
	Detect       map[string]string `yaml:"detect"`
	DefaultModel string            `yaml:"default_model"`
}

type AdvancedRouteConfig struct {
	Match   map[string]string     `yaml:"match"`
	Targets []AdvancedRouteTarget `yaml:"targets"`
}

type AdvancedRouteTarget struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Fallback bool   `yaml:"fallback"`
}

type RuntimeConfig struct {
	Server        ServerConfig
	Security      SecurityConfig
	Storage       StorageConfig
	ClientKeys    []ClientKeyConfig
	ModelResolver modelresolver.Config
	Providers     map[string]ProviderConfig
}

func LoadRuntime(path string) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Version   string                    `yaml:"version"`
		Providers map[string]ProviderConfig `yaml:"providers"`
	}
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return nil, err
	}
	if probe.Version != "" || len(probe.Providers) > 0 {
		var cfg SimpleConfig
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return nil, err
		}
		return CompileSimple(cfg)
	}
	legacy, err := Load(path)
	if err != nil {
		return nil, err
	}
	return CompileLegacy(legacy), nil
}

func CompileSimple(cfg SimpleConfig) (*RuntimeConfig, error) {
	if cfg.Version == "" {
		cfg.Version = "vibeproxy.io/v1alpha1"
	}
	applyServerDefaults(&cfg.Server)
	applyStorageDefaults(&cfg.Storage)
	providers := map[string]ProviderConfig{}
	resolverProviders := make([]modelresolver.Provider, 0, len(cfg.Providers))
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := cfg.Providers[id]
		if p.Type == "" {
			p.Type = "openai-compatible"
		}
		if p.BaseURL == "" {
			p.BaseURL = defaultBaseURL(p.Type)
		}
		if p.Timeout.Duration == 0 {
			p.Timeout.Duration = 120 * time.Second
		}
		if p.MaxConcurrency <= 0 {
			p.MaxConcurrency = 32
		}
		if p.Auth.Type == "" && p.APIKey != "" {
			p.Auth = defaultAuthForProvider(p.Type, p.APIKey)
		}
		providers[id] = p
		resolverProviders = append(resolverProviders, modelresolver.Provider{ID: id, Type: p.Type, BaseURL: p.BaseURL, Models: p.Models, Priority: p.Priority})
	}
	aliases := map[string]modelresolver.Alias{}
	for name, target := range cfg.Models.Aliases {
		alias, err := parseAlias(target)
		if err != nil {
			return nil, fmt.Errorf("model alias %q: %w", name, err)
		}
		aliases[name] = alias
	}
	return &RuntimeConfig{Server: cfg.Server, Security: cfg.Security, Storage: cfg.Storage, ClientKeys: cfg.ClientKeys, Providers: providers, ModelResolver: modelresolver.Config{DefaultModel: cfg.Models.Default, AllowRaw: cfg.Models.AllowRaw, Aliases: aliases, Providers: resolverProviders}}, nil
}

func CompileLegacy(cfg *Config) *RuntimeConfig {
	applyServerDefaults(&cfg.Server)
	applyStorageDefaults(&cfg.Storage)
	providers := map[string]ProviderConfig{}
	resolverProviders := make([]modelresolver.Provider, 0, len(cfg.Channels))
	aliases := map[string]modelresolver.Alias{}
	for _, ch := range cfg.Channels {
		ptype := ch.Protocol
		if ptype == "anthropic_messages" {
			ptype = "anthropic"
		}
		if ptype == "openai_chat" {
			ptype = "openai-compatible"
		}
		models := make([]string, 0, len(ch.Models))
		for virtual, raw := range ch.Models {
			models = append(models, raw)
			aliases[virtual] = modelresolver.Alias{Provider: ch.ID, Model: raw}
		}
		providers[ch.ID] = ProviderConfig{Type: ptype, BaseURL: ch.BaseURL, Auth: legacyAuth(ch), Models: models, Priority: ch.Weight, Timeout: ch.Timeout, MaxConcurrency: ch.MaxConcurrency}
		resolverProviders = append(resolverProviders, modelresolver.Provider{ID: ch.ID, Type: ptype, BaseURL: ch.BaseURL, Models: models, Priority: ch.Weight})
	}
	return &RuntimeConfig{Server: cfg.Server, Security: cfg.Security, Storage: cfg.Storage, ClientKeys: cfg.ClientKeys, Providers: providers, ModelResolver: modelresolver.Config{DefaultModel: "", AllowRaw: true, Aliases: aliases, Providers: resolverProviders}}
}

func parseAlias(target string) (modelresolver.Alias, error) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return modelresolver.Alias{}, fmt.Errorf("expected provider/model")
	}
	return modelresolver.Alias{Provider: parts[0], Model: parts[1]}, nil
}

func defaultAuthForProvider(ptype string, ref upstreamauth.SecretRef) upstreamauth.Profile {
	if ptype == "anthropic" {
		return upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: ref}
	}
	return upstreamauth.Profile{Type: "bearer", Token: ref}
}

func legacyAuth(ch ChannelConfig) upstreamauth.Profile {
	if ch.APIKeyEnv == "" {
		return upstreamauth.Profile{Type: "none"}
	}
	ref := upstreamauth.SecretRef("env:" + ch.APIKeyEnv)
	if ch.Protocol == "anthropic_messages" {
		return upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: ref}
	}
	return upstreamauth.Profile{Type: "bearer", Token: ref}
}

func defaultBaseURL(ptype string) string {
	switch ptype {
	case "anthropic":
		return "https://api.anthropic.com"
	case "openai-compatible":
		return "https://api.openai.com/v1"
	default:
		return ""
	}
}

func applyServerDefaults(s *ServerConfig) {
	if s.Listen == "" {
		s.Listen = "127.0.0.1:8080"
	}
	if s.ReadTimeout.Duration == 0 {
		s.ReadTimeout.Duration = 30 * time.Second
	}
	if s.IdleTimeout.Duration == 0 {
		s.IdleTimeout.Duration = 120 * time.Second
	}
}

func applyStorageDefaults(s *StorageConfig) {
	if s.SQLitePath == "" {
		s.SQLitePath = "./vibe-proxy.db"
	}
	if s.RetentionDays <= 0 {
		s.RetentionDays = 14
	}
}
