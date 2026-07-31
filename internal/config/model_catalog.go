package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateModelCatalogProxy atomically persists the optional models.dev proxy.
// An empty value restores the standard HTTP_PROXY/HTTPS_PROXY environment
// behavior.
func UpdateModelCatalogProxy(path, proxyURL string) (*RuntimeConfig, error) {
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
	cfg.ModelCatalog.ProxyURL = strings.TrimSpace(proxyURL)
	compiled, err := compileValidated(cfg)
	if err != nil {
		return nil, err
	}
	output, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, output); err != nil {
		return nil, err
	}
	return compiled, nil
}
