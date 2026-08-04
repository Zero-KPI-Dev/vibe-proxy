package runtime

import (
	"context"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/multimodal"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

// runtimeVisionAnalyzer performs a small, unary helper invocation against an
// already configured Vision-capable provider. It deliberately receives a
// bounded helper request instead of the original conversation.
type runtimeVisionAnalyzer struct {
	server   *Server
	config   map[string]providerRuntime
	adapters map[string]protocol.ProviderAdapter
}

type providerRuntime struct {
	MaxConcurrency int
	Timeout        time.Duration
	Auth           func(*http.Request) *ir.GatewayError
}

func newRuntimeVisionAnalyzer(server *Server, cfg map[string]providerRuntime) *runtimeVisionAnalyzer {
	return &runtimeVisionAnalyzer{server: server, config: cfg, adapters: server.providerAdapters}
}

func (a *runtimeVisionAnalyzer) Analyze(ctx context.Context, input multimodal.VisionAnalysisRequest) (multimodal.VisionAnalysisResult, error) {
	provider, ok := a.config[input.Target.ProviderID]
	if !ok {
		return multimodal.VisionAnalysisResult{}, ir.GatewayError{StatusCode: 503, Kind: "config_error", Code: "vision_fallback_invalid", Message: "Vision fallback provider is not configured."}
	}
	adapter := a.adapters[input.Target.ProviderType]
	if adapter == nil {
		return multimodal.VisionAnalysisResult{}, ir.GatewayError{StatusCode: 501, Kind: "config_error", Code: "vision_fallback_invalid", Message: "Vision fallback provider adapter is not available."}
	}
	if !a.server.acquire(input.Target.ProviderID, provider.MaxConcurrency) {
		return multimodal.VisionAnalysisResult{}, ir.GatewayError{StatusCode: 429, Kind: "rate_limit_error", Code: "vision_provider_busy", Message: "Vision fallback provider is busy.", RetryAfter: "1"}
	}
	defer a.server.release(input.Target.ProviderID)

	requestCtx := ctx
	cancel := func() {}
	if provider.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, provider.Timeout)
	}
	defer cancel()
	started := time.Now()
	upstream, err := adapter.BuildRequest(requestCtx, input.Request, input.Target)
	if err != nil {
		return multimodal.VisionAnalysisResult{Latency: time.Since(started)}, err
	}
	body, _ := requestBodyCopy(upstream)
	result := multimodal.VisionAnalysisResult{UpstreamRequest: body, UpstreamMediaType: upstream.Header.Get("Content-Type")}
	if authErr := provider.Auth(upstream); authErr != nil {
		result.Latency = time.Since(started)
		return result, *authErr
	}
	response, err := a.server.httpClient.Do(upstream)
	if err != nil {
		result.Latency = time.Since(started)
		if requestCtx.Err() != nil {
			return result, ir.GatewayError{StatusCode: 504, Kind: "upstream_error", Code: "vision_fallback_timeout", Message: "Vision evidence extraction timed out."}
		}
		return result, ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "vision_fallback_unavailable", Message: "Could not connect to the Vision fallback provider."}
	}
	parsed, err := adapter.ParseUnary(requestCtx, response)
	result.Latency = time.Since(started)
	if err != nil {
		return result, err
	}
	result.Response = parsed
	result.Evidence = visionResponseText(parsed)
	if result.Evidence == "" {
		return result, ir.GatewayError{StatusCode: 502, Kind: "multimodal_error", Code: "vision_no_usable_evidence", Message: "Vision fallback did not return usable visual evidence."}
	}
	return result, nil
}

func visionResponseText(response *ir.Response) string {
	if response == nil {
		return ""
	}
	value := ""
	for _, message := range response.Messages {
		for _, block := range message.Content {
			if block.Type != ir.ContentText || block.Text == "" {
				continue
			}
			if value != "" {
				value += "\n"
			}
			value += block.Text
		}
	}
	return value
}
