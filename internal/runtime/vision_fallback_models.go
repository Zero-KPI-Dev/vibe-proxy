package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/multimodal"
)

type visionFallbackModel struct {
	Target     string                 `json:"target"`
	ProviderID string                 `json:"provider_id"`
	Model      string                 `json:"model"`
	Source     modelcapability.Source `json:"source"`
}

func (s *Server) visionFallbackModels(cfg *config.RuntimeConfig) []visionFallbackModel {
	result := make([]visionFallbackModel, 0)
	if cfg == nil {
		return result
	}
	catalog := s.catalog.Snapshot()
	for providerID, provider := range cfg.Providers {
		adapter, ok := s.providerAdapters[provider.Type]
		if !ok || !adapter.Capabilities().Vision {
			continue
		}
		for _, model := range provider.Models {
			resolved := multimodal.ResolveCapabilities(providerID, model, provider, catalog)
			if resolved.Capabilities.ImageInput != modelcapability.SupportSupported {
				continue
			}
			result = append(result, visionFallbackModel{
				Target:     providerID + "/" + model,
				ProviderID: providerID,
				Model:      model,
				Source:     resolved.Source,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Target < result[j].Target
	})
	return result
}

func (s *Server) validateVisionFallbackSelection(cfg *config.RuntimeConfig, requested string) error {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return nil
	}
	target, err := modelresolver.New(cfg.ModelResolver).Resolve(&ir.Request{RequestedModel: requested})
	if err != nil {
		return fmt.Errorf("vision_fallback_invalid: resolve target: %w", err)
	}
	provider, ok := cfg.Providers[target.ProviderID]
	if !ok {
		return fmt.Errorf("vision_fallback_invalid: provider is not configured")
	}
	resolved := multimodal.ResolveCapabilities(target.ProviderID, target.Model, provider, s.catalog.Snapshot())
	if resolved.Capabilities.ImageInput != modelcapability.SupportSupported {
		return fmt.Errorf("vision_fallback_invalid: model does not support image input")
	}
	adapter, ok := s.providerAdapters[provider.Type]
	if !ok || !adapter.Capabilities().Vision {
		return fmt.Errorf("vision_fallback_invalid: provider adapter cannot transport images")
	}
	return nil
}
