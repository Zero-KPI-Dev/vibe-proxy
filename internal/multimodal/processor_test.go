package multimodal

import (
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

func imageRequest() *ir.Request {
	return &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{Base64: "AA=="}}}}}}
}

func TestCapabilityPriority(t *testing.T) {
	provider := config.ProviderConfig{
		DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported},
		ModelCapabilities: map[string]modelcapability.ModelCapabilities{
			"vision": {ImageInput: modelcapability.SupportSupported},
		},
	}
	got := ResolveCapabilities("local", "vision", provider, nil)
	if got.Capabilities.ImageInput != modelcapability.SupportSupported || got.Source != modelcapability.SourceModelOverride {
		t.Fatalf("model override should win: %+v", got)
	}
	got = ResolveCapabilities("local", "text", provider, nil)
	if got.Capabilities.ImageInput != modelcapability.SupportUnsupported || got.Source != modelcapability.SourceProviderDefault {
		t.Fatalf("provider default should win: %+v", got)
	}
}

func TestRouteDecisionMatrixSkeleton(t *testing.T) {
	target := modelresolver.Target{ProviderID: "local", Model: "model"}
	route := preprocess.RouteContext{Target: target, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	processor := &Processor{Enabled: true}

	if got := processor.Decide(&ir.Request{}, route); got.Mode != RouteDirectText {
		t.Fatalf("no image should be direct text: %+v", got)
	}
	if got := processor.Decide(imageRequest(), route); got.Mode != RouteLegacyPassthrough {
		t.Fatalf("unknown capability should retain legacy passthrough: %+v", got)
	}
	route.ProviderConfig.DefaultCapabilities.ImageInput = modelcapability.SupportUnsupported
	if got := processor.Decide(imageRequest(), route); got.Mode != RouteRejected || got.Reason != "multimodal_unsupported" {
		t.Fatalf("unsupported model without fallback should reject: %+v", got)
	}
	route.ProviderConfig.DefaultCapabilities.ImageInput = modelcapability.SupportSupported
	if got := processor.Decide(imageRequest(), route); got.Mode != RouteDirectVision {
		t.Fatalf("supported model should use vision: %+v", got)
	}
	route.AdapterCapabilities.Vision = false
	if got := processor.Decide(imageRequest(), route); got.Mode != RouteRejected || got.Reason != "image_transport_unsupported" {
		t.Fatalf("adapter without image transport should reject: %+v", got)
	}
}

func TestDisabledProcessorPreservesExistingImageBehavior(t *testing.T) {
	processor := &Processor{Enabled: false}
	route := preprocess.RouteContext{Target: modelresolver.Target{ProviderID: "local", Model: "text"}, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}}
	got := processor.Decide(imageRequest(), route)
	if got.Mode != RouteLegacyPassthrough || got.Reason != "feature_disabled" {
		t.Fatalf("disabled feature must not change behavior: %+v", got)
	}
}

func TestScanImagesIncludesToolResults(t *testing.T) {
	req := imageRequest()
	req.Messages = append(req.Messages, ir.Message{Role: ir.RoleTool, Content: []ir.ContentBlock{{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{Base64: "AA=="}}}}}}})
	if got := ScanImages(req).Count; got != 2 {
		t.Fatalf("image count = %d, want 2", got)
	}
}
