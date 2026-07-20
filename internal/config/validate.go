package config

import (
	"fmt"
	"net/url"
	"strings"
)

type ValidationIssue struct {
	Level   string `json:"level"`
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateRuntime(cfg *RuntimeConfig) []ValidationIssue {
	issues := []ValidationIssue{}
	if cfg == nil {
		return []ValidationIssue{{Level: "error", Path: "", Code: "nil_config", Message: "Configuration is empty."}}
	}
	if cfg.Server.Listen == "" {
		issues = append(issues, issue("error", "server.listen", "missing_listen", "Server listen address is required."))
	}
	if len(cfg.Providers) == 0 {
		issues = append(issues, issue("warning", "providers", "missing_providers", "No providers are configured yet; data-plane requests will fail until one is added."))
	}
	for id, p := range cfg.Providers {
		path := "providers." + id
		if strings.TrimSpace(id) == "" {
			issues = append(issues, issue("error", "providers", "empty_provider_id", "Provider id cannot be empty."))
		}
		if p.Type == "" {
			issues = append(issues, issue("error", path+".type", "missing_provider_type", "Provider type is required."))
		}
		if p.Type != "anthropic" && p.Type != "openai-compatible" {
			issues = append(issues, issue("error", path+".type", "unsupported_provider_type", fmt.Sprintf("Provider type %q is not supported yet.", p.Type)))
		}
		if p.BaseURL == "" {
			issues = append(issues, issue("error", path+".base_url", "missing_base_url", "Provider base_url is required."))
		} else if _, err := url.ParseRequestURI(p.BaseURL); err != nil {
			issues = append(issues, issue("error", path+".base_url", "invalid_base_url", "Provider base_url must be a valid absolute URL."))
		}
		if p.Auth.Type == "" {
			issues = append(issues, issue("warning", path+".auth", "missing_auth", "Provider auth is not configured; this is only valid for local/private providers without auth."))
		}
		if p.Auth.Type == "bearer" && p.Auth.Token == "" {
			issues = append(issues, issue("error", path+".auth.token", "missing_bearer_token", "Bearer auth requires token."))
		}
		if p.Auth.Type == "api_key_header" && (p.Auth.Header == "" || p.Auth.Value == "") {
			issues = append(issues, issue("error", path+".auth", "invalid_api_key_header", "API key header auth requires header and value."))
		}
		if !p.DefaultCapabilities.ImageInput.Valid() {
			issues = append(issues, issue("error", path+".default_capabilities.image_input", "invalid_image_input_capability", "image_input must be unknown, supported, or unsupported."))
		}
		for model, capabilities := range p.ModelCapabilities {
			if !capabilities.ImageInput.Valid() {
				issues = append(issues, issue("error", path+".model_capabilities."+model+".image_input", "invalid_image_input_capability", "image_input must be unknown, supported, or unsupported."))
			}
		}
	}
	for alias, target := range cfg.ModelResolver.Aliases {
		if alias == "" {
			issues = append(issues, issue("error", "models.aliases", "empty_alias", "Model alias cannot be empty."))
			continue
		}
		if target.Provider == "" || target.Model == "" {
			issues = append(issues, issue("error", "models.aliases."+alias, "invalid_alias_target", "Alias target must include provider and model."))
			continue
		}
		if _, ok := cfg.Providers[target.Provider]; !ok {
			issues = append(issues, issue("error", "models.aliases."+alias, "alias_provider_not_found", "Alias points to an unknown provider."))
		}
	}
	return issues
}

func HasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}
func issue(level, path, code, message string) ValidationIssue {
	return ValidationIssue{Level: level, Path: path, Code: code, Message: message}
}
