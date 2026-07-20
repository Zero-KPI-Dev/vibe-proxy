package multimodal

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/ocr"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

func imageRequest() *ir.Request {
	return &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{Base64: "AA=="}}}}}}
}

type fakeOCRProvider struct {
	confidence float64
	calls      int
}

func (p *fakeOCRProvider) Name() string                 { return "fake" }
func (p *fakeOCRProvider) Health(context.Context) error { return nil }
func (p *fakeOCRProvider) Recognize(_ context.Context, images []ocr.Image) ([]ocr.Result, error) {
	p.calls++
	return []ocr.Result{{Index: images[0].Index, Text: "recognized text", Confidence: &p.confidence}}, nil
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

func TestProcessorAppliesOCRAndUsesCache(t *testing.T) {
	provider := &fakeOCRProvider{confidence: 0.9}
	processor := &Processor{
		Enabled:       true,
		OCR:           provider,
		OCRTimeout:    time.Second,
		MinConfidence: 0.5,
		MinTextChars:  4,
		ImageLimits:   ImageLimits{MaxImages: 2, MaxImageBytes: 1024, MaxTotalImageBytes: 2048},
		TextLimits:    TextLimits{PerImage: 100, Total: 100},
		Cache:         NewOCRCache(4, time.Minute),
		cachePrefix:   "test",
	}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}}}}
	route := preprocess.RouteContext{Target: modelresolver.Target{ProviderID: "local", Model: "text"}, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	for range 2 {
		result, err := processor.Prepare(context.Background(), req, route)
		if err != nil {
			t.Fatal(err)
		}
		if ScanImages(result.Request).Count != 0 || result.Request.Messages[0].Role != ir.RoleSystem {
			t.Fatalf("request was not normalized: %+v", result.Request.Messages)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("OCR calls = %d, want 1", provider.calls)
	}
}

func TestProcessorRejectsLowConfidenceOCR(t *testing.T) {
	provider := &fakeOCRProvider{confidence: 0.2}
	processor := &Processor{Enabled: true, OCR: provider, MinConfidence: 0.8, MinTextChars: 1, ImageLimits: ImageLimits{MaxImages: 1, MaxImageBytes: 1024, MaxTotalImageBytes: 1024}, TextLimits: TextLimits{PerImage: 100, Total: 100}}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}}}}
	route := preprocess.RouteContext{Target: modelresolver.Target{ProviderID: "local", Model: "text"}, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	_, err := processor.Prepare(context.Background(), req, route)
	if gatewayCode(err) != "ocr_no_usable_text" {
		t.Fatalf("unexpected low confidence error: %v", err)
	}
}
