package multimodal

import (
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
)

type CapabilityResolution struct {
	Capabilities modelcapability.ModelCapabilities `json:"capabilities"`
	Source       modelcapability.Source            `json:"source"`
	CatalogMatch modelcatalog.MatchStatus          `json:"catalog_match,omitempty"`
}

func ResolveCapabilities(providerID, modelID string, provider config.ProviderConfig, catalog *modelcatalog.Snapshot) CapabilityResolution {
	if override, ok := provider.ModelCapabilities[modelID]; ok && override.ImageInput != "" {
		return CapabilityResolution{Capabilities: normalizedCapabilities(override), Source: modelcapability.SourceModelOverride}
	}
	if provider.DefaultCapabilities.ImageInput != "" {
		return CapabilityResolution{Capabilities: normalizedCapabilities(provider.DefaultCapabilities), Source: modelcapability.SourceProviderDefault}
	}
	if catalog != nil {
		match := catalog.Lookup(provider.CatalogProvider, providerID, provider.BaseURL, modelID)
		if match.Status != modelcatalog.MatchNotFound && match.Status != modelcatalog.MatchAmbiguous {
			return CapabilityResolution{
				Capabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportState(match.ImageInput).Effective()},
				Source:       modelcapability.SourceModelsDev,
				CatalogMatch: match.Status,
			}
		}
	}
	return CapabilityResolution{Capabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnknown}, Source: modelcapability.SourceUnknown}
}

func normalizedCapabilities(value modelcapability.ModelCapabilities) modelcapability.ModelCapabilities {
	value.ImageInput = value.ImageInput.Effective()
	return value
}
