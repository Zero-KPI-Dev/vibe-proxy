package config

import (
	"errors"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct{ time.Duration }

func (d Duration) MarshalYAML() (any, error) {
	if d.Duration == 0 {
		return "0s", nil
	}
	return d.Duration.String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Security      SecurityConfig      `yaml:"security"`
	Storage       StorageConfig       `yaml:"storage"`
	Observability ObservabilityConfig `yaml:"observability,omitempty"`
	ClientKeys    []ClientKeyConfig   `yaml:"client_keys"`
	ModelRoutes   []ModelRoute        `yaml:"model_routes"`
	Channels      []ChannelConfig     `yaml:"channels"`
}

type ServerConfig struct {
	Listen       string   `yaml:"listen"`
	ReadTimeout  Duration `yaml:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout"`
	IdleTimeout  Duration `yaml:"idle_timeout"`
}

type SecurityConfig struct {
	AdminBearerTokenEnv string `yaml:"admin_bearer_token_env"`
	MasterKeyEnv        string `yaml:"master_key_env"`
}

type StorageConfig struct {
	SQLitePath    string `yaml:"sqlite_path"`
	RetentionDays int    `yaml:"retention_days"`
}

// ObservabilityConfig controls local trace capture independently from the
// request-summary retention used by the compatibility telemetry surface.
type ObservabilityConfig struct {
	Capture   ObservabilityCaptureConfig   `yaml:"capture,omitempty"`
	Retention ObservabilityRetentionConfig `yaml:"retention,omitempty"`
}

type ObservabilityCaptureConfig struct {
	Mode             string   `yaml:"mode,omitempty"`
	MaxSnapshotBytes int      `yaml:"max_snapshot_bytes,omitempty"`
	CaptureResponse  bool     `yaml:"capture_response,omitempty"`
	CaptureReasoning bool     `yaml:"capture_reasoning,omitempty"`
	ImagePayloads    string   `yaml:"image_payloads,omitempty"`
	HeaderAllowlist  []string `yaml:"header_allowlist,omitempty"`
}

type ObservabilityRetentionConfig struct {
	SummariesDays       int `yaml:"summaries_days,omitempty"`
	ContentDays         int `yaml:"content_days,omitempty"`
	MaxContentStorageMB int `yaml:"max_content_storage_mb,omitempty"`
}

type ClientKeyConfig struct {
	Name          string   `yaml:"name"`
	KeyHash       string   `yaml:"key_hash"`
	KeyPrefix     string   `yaml:"key_prefix,omitempty"`
	Enabled       bool     `yaml:"enabled"`
	AllowedModels []string `yaml:"allowed_models"`
	RPM           int      `yaml:"rpm"`
}

type ModelRoute struct {
	Name      string         `yaml:"name"`
	Match     string         `yaml:"match"`
	Channels  []string       `yaml:"channels"`
	Overrides map[string]any `yaml:"overrides"`
}

type ChannelConfig struct {
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name"`
	Protocol       string            `yaml:"protocol"`
	BaseURL        string            `yaml:"base_url"`
	APIKeyEnv      string            `yaml:"api_key_env"`
	EncryptedKey   string            `yaml:"encrypted_key"`
	Weight         int               `yaml:"weight"`
	MaxConcurrency int               `yaml:"max_concurrency"`
	Timeout        Duration          `yaml:"timeout"`
	Region         string            `yaml:"region"`
	Models         map[string]string `yaml:"models"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Storage.SQLitePath == "" {
		cfg.Storage.SQLitePath = "./vibe-proxy.db"
	}
	if cfg.Storage.RetentionDays <= 0 {
		cfg.Storage.RetentionDays = 14
	}
	applyObservabilityDefaults(&cfg.Observability, cfg.Storage.RetentionDays)
	if len(cfg.Channels) == 0 {
		return nil, errors.New("at least one channel is required")
	}
	return &cfg, nil
}
