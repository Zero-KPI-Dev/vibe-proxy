package modelresolver

import (
	"fmt"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

type Provider struct {
	ID       string
	Type     string
	BaseURL  string
	Models   []string
	Priority int
}

type Alias struct {
	Provider string
	Model    string
}

type Config struct {
	DefaultModel string
	AllowRaw     bool
	Aliases      map[string]Alias
	Providers    []Provider
}

type Target struct {
	ProviderID   string
	ProviderType string
	BaseURL      string
	Model        string
	Requested    string
	IsAlias      bool
}

type Resolver struct{ cfg Config }

func New(cfg Config) *Resolver { return &Resolver{cfg: cfg} }

func (r *Resolver) Resolve(req *ir.Request) (Target, *ir.GatewayError) {
	model := strings.TrimSpace(req.RequestedModel)
	if model == "" {
		model = r.cfg.DefaultModel
	}
	if model == "" {
		return Target{}, modelErr("missing_model")
	}
	if alias, ok := r.cfg.Aliases[model]; ok {
		p, ok := r.provider(alias.Provider)
		if !ok {
			return Target{}, modelErr("alias_provider_not_found")
		}
		return Target{ProviderID: p.ID, ProviderType: p.Type, BaseURL: p.BaseURL, Model: alias.Model, Requested: model, IsAlias: true}, nil
	}
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		p, ok := r.provider(parts[0])
		if ok && parts[1] != "" {
			return Target{ProviderID: p.ID, ProviderType: p.Type, BaseURL: p.BaseURL, Model: parts[1], Requested: model}, nil
		}
	}
	if r.cfg.AllowRaw {
		if p, ok := r.findRaw(model); ok {
			return Target{ProviderID: p.ID, ProviderType: p.Type, BaseURL: p.BaseURL, Model: model, Requested: model}, nil
		}
	}
	return Target{}, modelErr(fmt.Sprintf("model_not_found:%s", model))
}

func (r *Resolver) provider(id string) (Provider, bool) {
	for _, p := range r.cfg.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

func (r *Resolver) findRaw(model string) (Provider, bool) {
	var selected Provider
	for _, p := range r.cfg.Providers {
		for _, m := range p.Models {
			if m == model || m == "*" {
				if selected.ID == "" || p.Priority > selected.Priority {
					selected = p
				}
			}
		}
	}
	return selected, selected.ID != ""
}

func modelErr(code string) *ir.GatewayError {
	return &ir.GatewayError{StatusCode: 404, Kind: "model_error", Code: code, Message: "No provider route is configured for the requested model."}
}
