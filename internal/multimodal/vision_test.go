package multimodal

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

func TestVisionAssistScopesHelperAndEvidenceToLatestImageMessage(t *testing.T) {
	req := &ir.Request{Messages: []ir.Message{
		{Role: ir.RoleUser, Content: []ir.ContentBlock{
			{Type: ir.ContentText, Text: "old image question"},
			{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "https://example.invalid/old.png"}},
		}},
		{Role: ir.RoleAssistant, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "old answer"}}},
		{Role: ir.RoleUser, Content: []ir.ContentBlock{
			{Type: ir.ContentText, Text: "latest image question"},
			{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "https://example.invalid/latest.png"}},
		}},
	}}
	target := modelresolver.Target{ProviderID: "vision", Model: "vision-model"}

	helper, question := BuildVisionAssistRequest(req, target, 4000, 1024)
	helperJSON, err := json.Marshal(helper)
	if err != nil {
		t.Fatal(err)
	}
	if question != "latest image question" || strings.Contains(string(helperJSON), "old.png") || strings.Contains(string(helperJSON), "old image question") || strings.Contains(string(helperJSON), "old answer") {
		t.Fatalf("helper included unrelated historical content: question=%q request=%s", question, helperJSON)
	}
	if !strings.Contains(string(helperJSON), "latest.png") || !strings.Contains(string(helperJSON), "latest image question") {
		t.Fatalf("helper omitted latest image-local request: %s", helperJSON)
	}

	processed, err := NormalizeVisionRequest(req, "latest visual evidence", target.ProviderID, target.Model, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if ScanImages(processed).Count != 0 {
		t.Fatalf("text-only primary request retained images: %+v", processed.Messages)
	}
	historical := messageTextForTest(processed.Messages[0])
	latest := messageTextForTest(processed.Messages[2])
	if !strings.Contains(historical, "historical_image_not_analyzed") || strings.Contains(historical, "latest visual evidence") || strings.Contains(historical, VisionSafetyGuard) {
		t.Fatalf("historical image received latest evidence or lost omission marker: %s", historical)
	}
	if !strings.Contains(latest, "latest visual evidence") || !strings.Contains(latest, VisionSafetyGuard) || !strings.Contains(latest, `<vibe-proxy-vision`) {
		t.Fatalf("latest image position did not receive protected evidence: %s", latest)
	}
}

func TestVisionAssistCacheIdentityIgnoresHistoricalImages(t *testing.T) {
	latest := base64.StdEncoding.EncodeToString(onePixelPNG)
	request := func(historical string) *ir.Request {
		return &ir.Request{Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{Base64: historical, MediaType: "image/png"}}}},
			{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentText, Text: "latest"}, {Type: ir.ContentImage, Image: &ir.ImageContent{Base64: latest, MediaType: "image/png"}}}},
		}}
	}
	limits := ImageLimits{MaxImages: 4, MaxImageBytes: 1024, MaxTotalImageBytes: 4096}
	target := modelresolver.Target{ProviderID: "vision", Model: "vision-model"}
	first, err := visionAssistImageIdentities(request(base64.StdEncoding.EncodeToString([]byte("old-a"))), limits)
	if err != nil {
		t.Fatal(err)
	}
	second, err := visionAssistImageIdentities(request(base64.StdEncoding.EncodeToString([]byte("old-b"))), limits)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey, secondKey := VisionCacheKey(target, first, "latest"), VisionCacheKey(target, second, "latest"); firstKey != secondKey {
		t.Fatalf("historical image changed latest-message cache identity: %s != %s", firstKey, secondKey)
	}
}

func TestEstimateRequestTokensIncludesNonMessageContext(t *testing.T) {
	imageData := strings.Repeat("a", 32*1024)
	request := &ir.Request{
		RequestedModel: "vision-model",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{
			{Type: ir.ContentText, Text: "analyze"},
			{Type: ir.ContentImage, Image: &ir.ImageContent{MediaType: "image/png", Base64: imageData}},
			{Type: ir.ContentToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call-1", Content: []ir.ContentBlock{
				{Type: ir.ContentText, Text: strings.Repeat("result", 1024)},
			}}},
		}}},
		Tools: []ir.Tool{{
			Name: "large-tool", Description: strings.Repeat("description", 8192),
			Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		}},
		ToolChoice:     &ir.ToolChoice{Type: "function", Name: "large-tool"},
		ResponseFormat: &ir.ResponseFormat{Type: "json_schema", JSONSchema: json.RawMessage(`{"type":"object","required":["answer"]}`)},
		Stop:           []string{"END"},
		Metadata:       map[string]string{"conversation": strings.Repeat("metadata", 1024)},
		VendorExtensions: map[string]any{
			"provider_context": strings.Repeat("extension", 1024),
		},
	}

	estimated := estimateRequestTokens(request)
	if estimated < 25_000 {
		t.Fatalf("non-message context was not included in takeover estimate: %d", estimated)
	}
	if request.Messages[0].Content[1].Image.Base64 != imageData {
		t.Fatal("token estimation mutated the original image payload")
	}
	withoutContext := *request
	withoutContext.Tools = nil
	withoutContext.ToolChoice = nil
	withoutContext.ResponseFormat = nil
	withoutContext.Metadata = nil
	withoutContext.VendorExtensions = nil
	if smaller := estimateRequestTokens(&withoutContext); smaller >= estimated {
		t.Fatalf("removing non-message context did not reduce estimate: full=%d reduced=%d", estimated, smaller)
	}
}

func messageTextForTest(message ir.Message) string {
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == ir.ContentText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
