package multimodal

import (
	"context"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
)

type RouteMode string

const (
	RouteDirectText        RouteMode = "direct_text"
	RouteDirectVision      RouteMode = "direct_vision"
	RouteLegacyPassthrough RouteMode = "legacy_passthrough"
	RouteOCRFallback       RouteMode = "ocr_fallback"
	RouteVisionFallback    RouteMode = "vision_fallback"
	RouteRejected          RouteMode = "rejected"
)

type Decision struct {
	Mode              RouteMode                    `json:"mode"`
	Reason            string                       `json:"reason,omitempty"`
	InputImageCount   int                          `json:"input_image_count"`
	OriginalTarget    modelresolver.Target         `json:"original_target"`
	EffectiveTarget   modelresolver.Target         `json:"effective_target"`
	ModelImageSupport modelcapability.SupportState `json:"model_image_support"`
	CapabilitySource  modelcapability.Source       `json:"capability_source"`
	CatalogMatch      modelcatalog.MatchStatus     `json:"catalog_match,omitempty"`
	Degraded          bool                         `json:"degraded"`
}

type Processor struct {
	Enabled bool
	Catalog *modelcatalog.Service
}

func (p *Processor) Name() string { return "multimodal_fallback" }

func (p *Processor) Decide(req *ir.Request, route preprocess.RouteContext) Decision {
	scan := ScanImages(req)
	decision := Decision{Mode: RouteDirectText, InputImageCount: scan.Count, OriginalTarget: route.Target, EffectiveTarget: route.Target}
	if scan.Count == 0 {
		decision.Reason = "no_images"
		return decision
	}
	if !p.Enabled {
		decision.Mode = RouteLegacyPassthrough
		decision.Reason = "feature_disabled"
		decision.ModelImageSupport = modelcapability.SupportUnknown
		decision.CapabilitySource = modelcapability.SourceUnknown
		return decision
	}

	var catalog *modelcatalog.Snapshot
	if p.Catalog != nil {
		catalog = p.Catalog.Snapshot()
	}
	resolved := ResolveCapabilities(route.Target.ProviderID, route.Target.Model, route.ProviderConfig, catalog)
	decision.ModelImageSupport = resolved.Capabilities.ImageInput
	decision.CapabilitySource = resolved.Source
	decision.CatalogMatch = resolved.CatalogMatch
	switch resolved.Capabilities.ImageInput {
	case modelcapability.SupportSupported:
		if !route.AdapterCapabilities.Vision {
			decision.Mode = RouteRejected
			decision.Reason = "image_transport_unsupported"
			return decision
		}
		decision.Mode = RouteDirectVision
		decision.Reason = "model_supports_images"
	case modelcapability.SupportUnsupported:
		decision.Mode = RouteRejected
		decision.Reason = "multimodal_unsupported"
	default:
		decision.Mode = RouteLegacyPassthrough
		decision.Reason = "capability_unknown"
	}
	return decision
}

func (p *Processor) Prepare(_ context.Context, req *ir.Request, route preprocess.RouteContext) (preprocess.Result, error) {
	decision := p.Decide(req, route)
	result := preprocess.Result{
		Request: req,
		Target:  route.Target,
		Decisions: []preprocess.Decision{{
			Processor: p.Name(),
			Route:     string(decision.Mode),
			Reason:    decision.Reason,
			Attributes: map[string]any{
				"input_image_count":   decision.InputImageCount,
				"model_image_support": decision.ModelImageSupport,
				"capability_source":   decision.CapabilitySource,
				"catalog_match":       decision.CatalogMatch,
			},
		}},
	}
	if decision.Mode != RouteRejected {
		return result, nil
	}
	status := 400
	if decision.Reason == "image_transport_unsupported" {
		status = 500
	}
	return result, ir.GatewayError{StatusCode: status, Kind: "multimodal_error", Code: decision.Reason, Message: multimodalErrorMessage(decision.Reason)}
}

func multimodalErrorMessage(code string) string {
	if code == "image_transport_unsupported" {
		return "The selected provider adapter cannot encode image input."
	}
	return "The selected model does not support image input and no fallback is configured."
}
