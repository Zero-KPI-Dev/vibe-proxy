package multimodal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
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

type fakeVisionAnalyzer struct {
	calls   int
	request *ir.Request
}

func (a *fakeVisionAnalyzer) Analyze(_ context.Context, input VisionAnalysisRequest) (VisionAnalysisResult, error) {
	a.calls++
	a.request = input.Request
	response := &ir.Response{Model: input.Target.Model, Messages: []ir.Message{{Role: ir.RoleAssistant, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "a ginger cat on a chair"}}}}}
	return VisionAnalysisResult{Evidence: "a ginger cat on a chair", Response: response}, nil
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

func TestProcessorDoesNotInjectOCRPromptIntoTextOnlyRequest(t *testing.T) {
	processor := &Processor{Enabled: true, OCR: &fakeOCRProvider{confidence: 0.9}}
	req := &ir.Request{Messages: []ir.Message{{
		Role:    ir.RoleUser,
		Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "normal text request"}},
	}}}
	result, err := processor.Prepare(context.Background(), req, preprocess.RouteContext{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request != req || len(result.Request.Messages) != 1 ||
		strings.Contains(result.Request.Messages[0].Content[0].Text, "vibe-proxy-ocr") {
		t.Fatalf("text-only request was modified: %+v", result.Request.Messages)
	}
}

func TestDisabledProcessorPreservesExistingImageBehavior(t *testing.T) {
	processor := &Processor{Enabled: false}
	route := preprocess.RouteContext{Target: modelresolver.Target{ProviderID: "local", Model: "text"}, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}}
	got := processor.Decide(imageRequest(), route)
	if got.Mode != RouteLegacyPassthrough || got.Reason != "feature_disabled" {
		t.Fatalf("disabled feature must not change behavior: %+v", got)
	}
	if got.ModelImageSupport != modelcapability.SupportUnsupported || got.CapabilitySource != modelcapability.SourceProviderDefault {
		t.Fatalf("disabled feature must preserve resolved model capability: %+v", got)
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
		if ScanImages(result.Request).Count != 0 || result.Request.Messages[0].Role != ir.RoleUser || !strings.Contains(result.Request.Messages[0].Content[0].Text, OCRSafetyGuard) {
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

func TestProcessorFallsBackToExplicitVisionTarget(t *testing.T) {
	provider := &fakeOCRProvider{confidence: 0.2}
	resolver := modelresolver.New(modelresolver.Config{Aliases: map[string]modelresolver.Alias{"vibe-vision": {Provider: "vision", Model: "vision-model"}}, Providers: []modelresolver.Provider{{ID: "vision", Type: "openai-compatible", BaseURL: "https://vision.example/v1", Models: []string{"vision-model"}}}})
	processor := &Processor{
		Enabled:                true,
		OCR:                    provider,
		MinConfidence:          0.8,
		MinTextChars:           1,
		ImageLimits:            ImageLimits{MaxImages: 1, MaxImageBytes: 1024, MaxTotalImageBytes: 1024},
		TextLimits:             TextLimits{PerImage: 100, Total: 100},
		VisionFallbackModel:    "vibe-vision",
		VisionFallbackStrategy: VisionFallbackTakeover,
		Resolver:               resolver,
		Providers: map[string]config.ProviderConfig{"vision": {
			Type:                "openai-compatible",
			BaseURL:             "https://vision.example/v1",
			DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
		}},
		AdapterCapabilities: func(string) (protocol.Capabilities, bool) { return protocol.Capabilities{Vision: true}, true },
	}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}}}}
	original := modelresolver.Target{ProviderID: "text", ProviderType: "openai-compatible", Model: "text-model"}
	route := preprocess.RouteContext{Target: original, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	result, err := processor.Prepare(context.Background(), req, route)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.ProviderID != "vision" || result.Target.Model != "vision-model" || ScanImages(result.Request).Count != 1 {
		t.Fatalf("unexpected Vision fallback: target=%+v request=%+v", result.Target, result.Request)
	}
	if len(result.Decisions) != 1 || result.Decisions[0].Route != string(RouteVisionFallback) || result.Decisions[0].Reason != "ocr_no_usable_text" {
		t.Fatalf("unexpected fallback decision: %+v", result.Decisions)
	}
}

func TestProcessorVisionAssistKeepsOriginalTargetAndCachesEvidence(t *testing.T) {
	ocrProvider := &fakeOCRProvider{confidence: 0.2}
	visionAnalyzer := &fakeVisionAnalyzer{}
	resolver := modelresolver.New(modelresolver.Config{Aliases: map[string]modelresolver.Alias{"vibe-vision": {Provider: "vision", Model: "vision-model"}}, Providers: []modelresolver.Provider{{ID: "vision", Type: "openai-compatible", BaseURL: "https://vision.example/v1", Models: []string{"vision-model"}}}})
	processor := &Processor{
		Enabled:                true,
		OCR:                    ocrProvider,
		MinConfidence:          0.8,
		MinTextChars:           1,
		ImageLimits:            ImageLimits{MaxImages: 1, MaxImageBytes: 1024, MaxTotalImageBytes: 1024},
		TextLimits:             TextLimits{PerImage: 100, Total: 1000},
		VisionFallbackModel:    "vibe-vision",
		VisionFallbackStrategy: VisionFallbackAssist,
		VisionAnalyzer:         visionAnalyzer,
		VisionCache:            NewVisionCache(4, time.Minute),
		VisionMaxPromptChars:   100,
		VisionMaxOutputTokens:  100,
		Resolver:               resolver,
		Providers: map[string]config.ProviderConfig{"vision": {
			Type:                "openai-compatible",
			BaseURL:             "https://vision.example/v1",
			DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
		}},
		AdapterCapabilities: func(string) (protocol.Capabilities, bool) { return protocol.Capabilities{Vision: true}, true },
	}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{
		{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "historic context stays with original"}}},
		{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "what is shown?"}, {Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}},
	}}
	original := modelresolver.Target{ProviderID: "text", ProviderType: "openai-compatible", Model: "text-model"}
	route := preprocess.RouteContext{Target: original, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	for range 2 {
		result, err := processor.Prepare(context.Background(), req, route)
		if err != nil {
			t.Fatal(err)
		}
		if result.Target != original || ScanImages(result.Request).Count != 0 {
			t.Fatalf("Vision assist must keep original target and replace images: target=%+v request=%+v", result.Target, result.Request)
		}
		encodedResult, _ := json.Marshal(result.Request)
		if !strings.Contains(string(encodedResult), "vibe-proxy-vision") || !strings.Contains(string(encodedResult), "historic context stays with original") {
			t.Fatalf("missing visual evidence or original context: %s", encodedResult)
		}
	}
	if visionAnalyzer.calls != 1 {
		t.Fatalf("Vision analysis calls = %d, want one cached call", visionAnalyzer.calls)
	}
	helperJSON, _ := json.Marshal(visionAnalyzer.request)
	if strings.Contains(string(helperJSON), "historic context stays with original") {
		t.Fatalf("bounded Vision helper received unrelated historical context: %s", helperJSON)
	}
}

func TestProcessorVisionTakeoverRejectsKnownContextOverflow(t *testing.T) {
	catalog := modelcatalog.NewService(modelcatalog.Options{})
	if err := catalog.Import(strings.NewReader(`{"vision":{"id":"vision","models":{"vision-model":{"id":"vision-model","modalities":{"input":["text","image"],"output":["text"]},"limit":{"input":32}}}}}`)); err != nil {
		t.Fatal(err)
	}
	resolver := modelresolver.New(modelresolver.Config{Aliases: map[string]modelresolver.Alias{"vibe-vision": {Provider: "vision", Model: "vision-model"}}, Providers: []modelresolver.Provider{{ID: "vision", Type: "openai-compatible", BaseURL: "https://vision.example/v1", Models: []string{"vision-model"}}}})
	processor := &Processor{
		Enabled:                true,
		OCR:                    &fakeOCRProvider{confidence: 0.1},
		MinConfidence:          0.8,
		MinTextChars:           1,
		ImageLimits:            ImageLimits{MaxImages: 1, MaxImageBytes: 1024, MaxTotalImageBytes: 1024},
		TextLimits:             TextLimits{PerImage: 100, Total: 100},
		VisionFallbackModel:    "vibe-vision",
		VisionFallbackStrategy: VisionFallbackTakeover,
		Catalog:                catalog,
		Resolver:               resolver,
		Providers: map[string]config.ProviderConfig{"vision": {
			Type:                "openai-compatible",
			BaseURL:             "https://vision.example/v1",
			CatalogProvider:     "vision",
			DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
		}},
		AdapterCapabilities: func(string) (protocol.Capabilities, bool) { return protocol.Capabilities{Vision: true}, true },
	}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: strings.Repeat("context ", 100)}, {Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}}}}
	route := preprocess.RouteContext{Target: modelresolver.Target{ProviderID: "text", Model: "text-model"}, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	_, err := processor.Prepare(context.Background(), req, route)
	if gatewayCode(err) != "vision_takeover_context_exceeded" {
		t.Fatalf("unexpected takeover overflow result: %v", err)
	}
}

func TestProcessorDoesNotSendMalformedImageToVisionFallback(t *testing.T) {
	provider := &fakeOCRProvider{confidence: 0.9}
	resolver := modelresolver.New(modelresolver.Config{Aliases: map[string]modelresolver.Alias{"vibe-vision": {Provider: "vision", Model: "vision-model"}}, Providers: []modelresolver.Provider{{ID: "vision", Type: "openai-compatible", BaseURL: "https://vision.example/v1", Models: []string{"vision-model"}}}})
	processor := &Processor{
		Enabled:             true,
		OCR:                 provider,
		MinConfidence:       0.5,
		MinTextChars:        1,
		ImageLimits:         ImageLimits{MaxImages: 1, MaxImageBytes: 1024, MaxTotalImageBytes: 1024},
		TextLimits:          TextLimits{PerImage: 100, Total: 100},
		VisionFallbackModel: "vibe-vision",
		Resolver:            resolver,
		Providers: map[string]config.ProviderConfig{"vision": {
			Type:                "openai-compatible",
			BaseURL:             "https://vision.example/v1",
			DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
		}},
		AdapterCapabilities: func(string) (protocol.Capabilities, bool) { return protocol.Capabilities{Vision: true}, true },
	}
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64,not-valid-base64"}}}}}}
	original := modelresolver.Target{ProviderID: "text", ProviderType: "openai-compatible", Model: "text-model"}
	route := preprocess.RouteContext{Target: original, ProviderConfig: config.ProviderConfig{DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}}, AdapterCapabilities: protocol.Capabilities{Vision: true}}
	result, err := processor.Prepare(context.Background(), req, route)
	if gatewayCode(err) != "ocr_invalid_image" {
		t.Fatalf("malformed image should be rejected before Vision fallback: result=%+v err=%v", result, err)
	}
	if result.Target != original || provider.calls != 0 {
		t.Fatalf("malformed image reached a fallback provider: result=%+v OCR calls=%d", result, provider.calls)
	}
}
