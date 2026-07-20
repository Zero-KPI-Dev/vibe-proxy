package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func DeleteProvider(path string, id string) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	delete(cfg.Providers, id)
	// Clean up aliases referencing this provider
	for alias, target := range cfg.Models.Aliases {
		parts := splitAlias(target)
		if len(parts) >= 1 && parts[0] == id {
			delete(cfg.Models.Aliases, alias)
		}
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

func UpdateProvider(path string, input LocalProviderInput) (*RuntimeConfig, error) {
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
	existing := cfg.Providers[input.ID]
	provider, err := BuildLocalProvider(input, &existing)
	if err != nil {
		return nil, err
	}
	cfg.Providers[input.ID] = provider
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

func RawConfig(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func SaveRawConfig(path string, yamlContent string) (*RuntimeConfig, error) {
	var cfg SimpleConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
		return nil, err
	}
	compiled, err := CompileSimple(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(yamlContent), 0600); err != nil {
		return nil, err
	}
	return compiled, nil
}

func UpsertAlias(path string, alias, target string) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.Models.Aliases == nil {
		cfg.Models.Aliases = map[string]string{}
	}
	cfg.Models.Aliases[alias] = target
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return CompileSimple(cfg)
}

func DeleteAlias(path string, alias string) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	delete(cfg.Models.Aliases, alias)
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return CompileSimple(cfg)
}

func UpdateAliasDefaults(path string, defaultModel string, allowRaw bool) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	cfg.Models.Default = defaultModel
	cfg.Models.AllowRaw = allowRaw
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return CompileSimple(cfg)
}

type ClientKeyInput struct {
	Name          string   `json:"name"`
	AllowedModels []string `json:"allowed_models"`
	RPM           int      `json:"rpm"`
}

type ClientKeyUpdate struct {
	Enabled       *bool    `json:"enabled,omitempty"`
	AllowedModels []string `json:"allowed_models,omitempty"`
	RPM           *int     `json:"rpm,omitempty"`
}

func UpsertClientKey(path string, input ClientKeyInput) (*RuntimeConfig, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, "", err
	}
	for _, k := range cfg.ClientKeys {
		if k.Name == input.Name {
			return nil, "", fmt.Errorf("client key %q already exists", input.Name)
		}
	}
	rawKey := generateAPIKey()
	hash, err := hashKey(rawKey)
	if err != nil {
		return nil, "", err
	}
	allowed := input.AllowedModels
	if allowed == nil {
		allowed = []string{"*"}
	}
	rpm := input.RPM
	if rpm <= 0 {
		rpm = 60
	}
	cfg.ClientKeys = append(cfg.ClientKeys, ClientKeyConfig{
		Name:          input.Name,
		KeyHash:       hash,
		Enabled:       true,
		AllowedModels: allowed,
		RPM:           rpm,
	})
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, "", err
	}
	compiled, err := CompileSimple(cfg)
	if err != nil {
		return nil, "", err
	}
	return compiled, rawKey, nil
}

func UpdateClientKey(path string, name string, update ClientKeyUpdate) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	found := false
	for i, k := range cfg.ClientKeys {
		if k.Name == name {
			found = true
			if update.Enabled != nil {
				cfg.ClientKeys[i].Enabled = *update.Enabled
			}
			if update.AllowedModels != nil {
				cfg.ClientKeys[i].AllowedModels = update.AllowedModels
			}
			if update.RPM != nil {
				cfg.ClientKeys[i].RPM = *update.RPM
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("client key %q not found", name)
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

func DeleteClientKey(path string, name string) (*RuntimeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	filtered := make([]ClientKeyConfig, 0, len(cfg.ClientKeys))
	found := false
	for _, k := range cfg.ClientKeys {
		if k.Name != name {
			filtered = append(filtered, k)
		} else {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("client key %q not found", name)
	}
	cfg.ClientKeys = filtered
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return nil, err
	}
	return CompileSimple(cfg)
}

func splitAlias(target string) []string {
	parts := make([]string, 0, 2)
	current := ""
	for _, c := range target {
		if c == '/' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
