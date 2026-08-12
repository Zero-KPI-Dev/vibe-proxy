package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func DeleteProvider(path string, id string) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, err
	}
	if _, exists := cfg.Providers[id]; !exists {
		return nil, fmt.Errorf("provider %q not found", id)
	}
	delete(cfg.Providers, id)
	// Clean up aliases referencing this provider
	for alias, target := range cfg.Models.Aliases {
		parts := splitAlias(target)
		if len(parts) >= 1 && parts[0] == id {
			if cfg.Multimodal.VisionFallbackModel == alias {
				cfg.Multimodal.VisionFallbackModel = ""
			}
			if cfg.Models.Default == alias {
				cfg.Models.Default = ""
			}
			delete(cfg.Models.Aliases, alias)
		}
	}
	if strings.HasPrefix(cfg.Multimodal.VisionFallbackModel, id+"/") {
		cfg.Multimodal.VisionFallbackModel = ""
	}
	if strings.HasPrefix(cfg.Models.Default, id+"/") {
		cfg.Models.Default = ""
	}
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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

func UpdateProvider(path string, input LocalProviderInput) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
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
	if err := writeConfigFile(path, out); err != nil {
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
	unlock := lockConfigMutation(path)
	defer unlock()
	compiled, err := compileRawValidated(path, []byte(yamlContent))
	if err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, []byte(yamlContent)); err != nil {
		return nil, err
	}
	return compiled, nil
}

func UpsertAlias(path string, alias, target string) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, err
	}
	if cfg.Models.Aliases == nil {
		cfg.Models.Aliases = map[string]string{}
	}
	cfg.Models.Aliases[alias] = target
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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

func DeleteAlias(path string, alias string) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, err
	}
	if cfg.Multimodal.VisionFallbackModel == alias {
		cfg.Multimodal.VisionFallbackModel = ""
	}
	if _, exists := cfg.Models.Aliases[alias]; !exists {
		return nil, fmt.Errorf("model alias %q not found", alias)
	}
	if cfg.Models.Default == alias {
		cfg.Models.Default = ""
	}
	delete(cfg.Models.Aliases, alias)
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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

func UpdateAliasDefaults(path string, defaultModel string, allowRaw bool) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, err
	}
	cfg.Models.Default = defaultModel
	cfg.Models.AllowRaw = allowRaw
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, "", err
	}
	for _, k := range cfg.ClientKeys {
		if k.Name == input.Name {
			return nil, "", fmt.Errorf("client key %q already exists", input.Name)
		}
	}
	rawKey, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}
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
		RawKey:        rawKey,
		KeyPrefix:     clientKeyPrefix(rawKey),
		Enabled:       true,
		AllowedModels: allowed,
		RPM:           rpm,
	})
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, "", err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", err
	}
	if err := writeConfigFile(path, out); err != nil {
		return nil, "", err
	}
	return compiled, rawKey, nil
}

func UpdateClientKey(path string, name string, update ClientKeyUpdate) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
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
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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

func RotateClientKey(path string, name string) (*RuntimeConfig, string, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
		return nil, "", err
	}
	index := -1
	for i, key := range cfg.ClientKeys {
		if key.Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, "", fmt.Errorf("client key %q not found", name)
	}
	rawKey, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}
	hash, err := hashKey(rawKey)
	if err != nil {
		return nil, "", err
	}
	cfg.ClientKeys[index].KeyHash = hash
	cfg.ClientKeys[index].RawKey = rawKey
	cfg.ClientKeys[index].KeyPrefix = clientKeyPrefix(rawKey)
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, "", err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", err
	}
	if err := writeConfigFile(path, out); err != nil {
		return nil, "", err
	}
	return compiled, rawKey, nil
}

func DeleteClientKey(path string, name string) (*RuntimeConfig, error) {
	unlock := lockConfigMutation(path)
	defer unlock()
	cfg, err := readSimpleConfig(path)
	if err != nil {
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
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
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

func compileValidated(cfg SimpleConfig) (*RuntimeConfig, error) {
	compiled, err := CompileSimple(cfg)
	if err != nil {
		return nil, err
	}
	if issues := ValidateRuntime(compiled); HasErrors(issues) {
		return nil, fmt.Errorf("invalid configuration: %+v", issues)
	}
	return compiled, nil
}

func compileRawValidated(path string, b []byte) (*RuntimeConfig, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	currentIsSimple, err := isSimpleConfigYAML(current)
	if err != nil {
		return nil, err
	}
	var compiled *RuntimeConfig
	if currentIsSimple {
		var cfg SimpleConfig
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return nil, err
		}
		compiled, err = compileValidated(cfg)
	} else {
		compiled, err = compileRuntimeYAML(b)
		if err == nil {
			if issues := ValidateRuntime(compiled); HasErrors(issues) {
				err = fmt.Errorf("invalid configuration: %+v", issues)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return compiled, nil
}

func readSimpleConfig(path string) (SimpleConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SimpleConfig{}, err
	}
	isSimple, err := isSimpleConfigYAML(b)
	if err != nil {
		return SimpleConfig{}, err
	}
	if !isSimple {
		return SimpleConfig{}, fmt.Errorf("legacy configuration is read-only; migrate it to the current format before using this editor")
	}
	var cfg SimpleConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return SimpleConfig{}, err
	}
	return cfg, nil
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
