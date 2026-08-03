package multimodal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelcatalog"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/ocr"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
	"github.com/a448582655/vibe-proxy/internal/protocol"
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

const (
	VisionFallbackAssist   = "assist"
	VisionFallbackTakeover = "takeover"
	VisionFallbackReject   = "reject"
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

type OCRRequestDiagnostic struct {
	Provider string                  `json:"provider"`
	Endpoint string                  `json:"endpoint,omitempty"`
	Images   []resolvedImageIdentity `json:"images"`
}

type OCRResponseDiagnostic struct {
	Provider  string       `json:"provider"`
	Results   []ocr.Result `json:"results,omitempty"`
	CacheHits int          `json:"cache_hits"`
	LatencyMS int64        `json:"latency_ms"`
	ErrorCode string       `json:"error_code,omitempty"`
}

type Processor struct {
	Enabled                bool
	Catalog                *modelcatalog.Service
	OCR                    ocr.Provider
	OCREndpoint            string
	OCRTimeout             time.Duration
	MinConfidence          float64
	MinTextChars           int
	ImageLimits            ImageLimits
	TextLimits             TextLimits
	Cache                  *OCRCache
	cachePrefix            string
	VisionFallbackModel    string
	VisionFallbackStrategy string
	VisionAnalyzer         VisionAnalyzer
	VisionCache            *VisionCache
	VisionMaxPromptChars   int
	VisionMaxOutputTokens  int
	Resolver               *modelresolver.Resolver
	Providers              map[string]config.ProviderConfig
	AdapterCapabilities    func(providerType string) (protocol.Capabilities, bool)
}

type ProcessorOptions struct {
	Config              config.MultimodalConfig
	Catalog             *modelcatalog.Service
	Client              *http.Client
	BuiltinOCR          ocr.Provider
	Resolver            *modelresolver.Resolver
	Providers           map[string]config.ProviderConfig
	AdapterCapabilities func(providerType string) (protocol.Capabilities, bool)
	VisionAnalyzer      VisionAnalyzer
}

func NewProcessor(opts ProcessorOptions) *Processor {
	cfg := opts.Config
	processor := &Processor{
		Enabled:       cfg.Enabled,
		Catalog:       opts.Catalog,
		OCRTimeout:    cfg.OCR.Timeout.Duration,
		OCREndpoint:   cfg.OCR.Endpoint,
		MinConfidence: cfg.OCR.MinConfidence,
		MinTextChars:  cfg.OCR.MinTextChars,
		ImageLimits: ImageLimits{
			MaxImages:          cfg.OCR.MaxImages,
			MaxImageBytes:      cfg.OCR.MaxImageBytes,
			MaxTotalImageBytes: cfg.OCR.MaxTotalImageBytes,
			RemoteImages:       cfg.OCR.RemoteImages,
		},
		TextLimits:             TextLimits{PerImage: cfg.OCR.MaxTextCharsPerImage, Total: cfg.OCR.MaxTextCharsTotal},
		VisionFallbackModel:    cfg.VisionFallbackModel,
		VisionFallbackStrategy: cfg.VisionFallbackStrategy,
		VisionAnalyzer:         opts.VisionAnalyzer,
		VisionMaxPromptChars:   cfg.VisionAssist.MaxPromptChars,
		VisionMaxOutputTokens:  cfg.VisionAssist.MaxOutputTokens,
		Resolver:               opts.Resolver,
		Providers:              opts.Providers,
		AdapterCapabilities:    opts.AdapterCapabilities,
	}
	if !cfg.Enabled {
		return processor
	}
	switch cfg.OCR.Provider {
	case "builtin":
		processor.OCR = opts.BuiltinOCR
		if processor.OCR == nil {
			processor.OCR = ocr.NewBuiltinProvider()
		}
	case "http":
		if cfg.OCR.Endpoint != "" {
			processor.OCR = ocr.NewHTTPProvider(ocr.HTTPOptions{Endpoint: cfg.OCR.Endpoint, Auth: cfg.OCR.Auth, Client: opts.Client})
		}
	}
	if cfg.VisionAssist.Cache.IsEnabled() {
		processor.VisionCache = NewVisionCache(cfg.VisionAssist.Cache.MaxEntries, cfg.VisionAssist.Cache.TTL.Duration)
	}
	if processor.OCR == nil {
		return processor
	}
	if cfg.OCR.Cache.IsEnabled() {
		processor.Cache = NewOCRCache(cfg.OCR.Cache.MaxEntries, cfg.OCR.Cache.TTL.Duration)
	}
	fingerprint := sha256.Sum256([]byte(cfg.OCR.Provider + "\x00" + cfg.OCR.Endpoint))
	processor.cachePrefix = hex.EncodeToString(fingerprint[:])
	return processor
}

func (p *Processor) Name() string { return "multimodal_fallback" }

func (p *Processor) Decide(req *ir.Request, route preprocess.RouteContext) Decision {
	scan := ScanImages(req)
	decision := Decision{Mode: RouteDirectText, InputImageCount: scan.Count, OriginalTarget: route.Target, EffectiveTarget: route.Target}
	if scan.Count == 0 {
		decision.Reason = "no_images"
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

	if !p.Enabled {
		decision.Mode = RouteLegacyPassthrough
		decision.Reason = "feature_disabled"
		return decision
	}

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
		if p.OCR != nil {
			decision.Mode = RouteOCRFallback
			decision.Reason = "model_does_not_support_images"
			decision.Degraded = true
		} else if p.VisionFallbackModel != "" && p.VisionFallbackStrategy != VisionFallbackReject {
			fallback, err := p.resolveVisionFallback(req, route.Target)
			if err != nil {
				decision.Mode = RouteRejected
				decision.Reason = "vision_fallback_invalid"
			} else {
				decision.Mode = RouteVisionFallback
				decision.Reason = "ocr_unavailable"
				if p.VisionFallbackStrategy == VisionFallbackTakeover {
					decision.EffectiveTarget = fallback
				}
			}
		} else {
			decision.Mode = RouteRejected
			decision.Reason = "multimodal_unsupported"
		}
	default:
		decision.Mode = RouteLegacyPassthrough
		decision.Reason = "capability_unknown"
	}
	return decision
}

func (p *Processor) Prepare(ctx context.Context, req *ir.Request, route preprocess.RouteContext) (preprocess.Result, error) {
	decision := p.Decide(req, route)
	attributes := map[string]any{
		"input_image_count":   decision.InputImageCount,
		"model_image_support": decision.ModelImageSupport,
		"capability_source":   decision.CapabilitySource,
		"catalog_match":       decision.CatalogMatch,
	}
	result := preprocess.Result{Request: req, Target: route.Target}
	if decision.Mode != RouteRejected {
		if decision.Mode == RouteOCRFallback {
			attributes["ocr_provider"] = p.OCR.Name()
			identities, _ := imageIdentities(req, p.ImageLimits)
			result.Diagnostics = append(result.Diagnostics, preprocess.Diagnostic{Stage: "ocr_request", Value: OCRRequestDiagnostic{Provider: p.OCR.Name(), Endpoint: p.OCREndpoint, Images: identities}})
			ocrStarted := time.Now().UTC()
			processed, ocrResults, hits, latency, minConfidence, err := p.applyOCR(ctx, req)
			ocrCompleted := time.Now().UTC()
			ocrStatus := "ok"
			ocrCode := ""
			if err != nil {
				ocrStatus = "error"
				ocrCode = gatewayErrorCode(err)
			}
			result.Diagnostics = append(result.Diagnostics, preprocess.Diagnostic{Stage: "ocr_response", Value: OCRResponseDiagnostic{Provider: p.OCR.Name(), Results: ocrResults, CacheHits: hits, LatencyMS: latency.Milliseconds(), ErrorCode: ocrCode}})
			result.Observations = append(result.Observations, preprocess.Observation{Type: "preprocess", Name: "ocr.invoke", StartedAt: ocrStarted, CompletedAt: ocrCompleted, Status: ocrStatus, ErrorCode: ocrCode, Attributes: map[string]any{"provider": p.OCR.Name(), "image_count": decision.InputImageCount, "cache_hits": hits}})
			if err != nil {
				attributes["ocr_error_code"] = gatewayErrorCode(err)
				attributes["ocr_latency_ms"] = latency.Milliseconds()
				attributes["ocr_cache_hits"] = hits
				if minConfidence != nil {
					attributes["ocr_min_confidence"] = *minConfidence
				}
				if p.VisionFallbackModel != "" && p.VisionFallbackStrategy != VisionFallbackReject && canUseVisionFallback(err) {
					return p.applyVisionFallback(ctx, result, req, route, attributes, gatewayErrorCode(err))
				}
				result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: gatewayErrorCode(err), Attributes: attributes}}
				return result, err
			}
			result.Request = processed
			attributes["ocr_provider"] = p.OCR.Name()
			attributes["ocr_processed"] = decision.InputImageCount
			attributes["ocr_cache_hits"] = hits
			attributes["ocr_latency_ms"] = latency.Milliseconds()
			if minConfidence != nil {
				attributes["ocr_min_confidence"] = *minConfidence
			}
		} else if decision.Mode == RouteVisionFallback {
			return p.applyVisionFallback(ctx, result, req, route, attributes, decision.Reason)
		}
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(decision.Mode), Reason: decision.Reason, Attributes: attributes}}
		return result, nil
	}
	status := 400
	if decision.Reason == "image_transport_unsupported" {
		status = 500
	}
	result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(decision.Mode), Reason: decision.Reason, Attributes: attributes}}
	return result, ir.GatewayError{StatusCode: status, Kind: "multimodal_error", Code: decision.Reason, Message: multimodalErrorMessage(decision.Reason)}
}

func (p *Processor) applyVisionFallback(ctx context.Context, result preprocess.Result, req *ir.Request, route preprocess.RouteContext, attributes map[string]any, reason string) (preprocess.Result, error) {
	fallback, fallbackErr := p.resolveVisionFallback(req, route.Target)
	if fallbackErr != nil {
		attributes["vision_fallback_error"] = "invalid_target"
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: "vision_fallback_invalid", Attributes: attributes}}
		return result, ir.GatewayError{StatusCode: 500, Kind: "multimodal_error", Code: "vision_fallback_invalid", Message: "The configured Vision fallback model is invalid or does not explicitly support image input."}
	}
	strategy := p.VisionFallbackStrategy
	if strategy == "" {
		strategy = VisionFallbackAssist
	}
	attributes["vision_strategy"] = strategy
	attributes["vision_provider"] = fallback.ProviderID
	attributes["vision_model"] = fallback.Model

	if strategy == VisionFallbackTakeover {
		if estimated, limit, exceeded := p.takeoverContextLimit(req, fallback); exceeded {
			attributes["vision_estimated_input_tokens"] = estimated
			attributes["vision_input_limit"] = limit
			result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: "vision_takeover_context_exceeded", Attributes: attributes}}
			return result, ir.GatewayError{StatusCode: 422, Kind: "multimodal_error", Code: "vision_takeover_context_exceeded", Message: "The request is too large for the configured Vision takeover model. Use assist mode or reduce the context."}
		}
		result.Target = fallback
		attributes["effective_provider"] = fallback.ProviderID
		attributes["effective_model"] = fallback.Model
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteVisionFallback), Reason: reason, Attributes: attributes}}
		return result, nil
	}
	if strategy != VisionFallbackAssist || p.VisionAnalyzer == nil {
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: "vision_fallback_unavailable", Attributes: attributes}}
		return result, ir.GatewayError{StatusCode: 503, Kind: "multimodal_error", Code: "vision_fallback_unavailable", Message: "Vision assist is not available."}
	}

	helper, question := BuildVisionAssistRequest(req, fallback, p.VisionMaxPromptChars, p.VisionMaxOutputTokens)
	identities, identityErr := imageIdentities(req, p.ImageLimits)
	if identityErr != nil {
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: gatewayErrorCode(identityErr), Attributes: attributes}}
		return result, identityErr
	}
	cacheKey := VisionCacheKey(fallback, identities, question)
	visionStarted := time.Now().UTC()
	analysis, cacheHit, analysisErr := p.VisionCache.Do(ctx, cacheKey, func() (VisionAnalysisResult, error) {
		return p.VisionAnalyzer.Analyze(ctx, VisionAnalysisRequest{Target: fallback, Request: helper})
	})
	visionCompleted := time.Now().UTC()
	requestValue := VisionRequestDiagnostic{Strategy: strategy, PromptVersion: visionAssistPromptVersion, Provider: fallback.ProviderID, Model: fallback.Model, CanonicalRequest: helper}
	if len(analysis.UpstreamRequest) > 0 {
		requestValue.UpstreamRequest = decodeJSONOrString(analysis.UpstreamRequest)
	}
	result.Diagnostics = append(result.Diagnostics, preprocess.Diagnostic{Stage: "vision_request", Value: requestValue})
	visionStatus := "ok"
	visionCode := ""
	if analysisErr != nil {
		visionStatus = "error"
		visionCode = visionErrorCode(analysisErr)
	}
	result.Diagnostics = append(result.Diagnostics, preprocess.Diagnostic{Stage: "vision_response", Value: VisionResponseDiagnostic{Strategy: strategy, Provider: fallback.ProviderID, Model: fallback.Model, CacheHit: cacheHit, LatencyMS: visionCompleted.Sub(visionStarted).Milliseconds(), Evidence: analysis.Evidence, Response: analysis.Response, ErrorCode: visionCode}})
	result.Observations = append(result.Observations, preprocess.Observation{Type: "preprocess", Name: "vision.analyze", StartedAt: visionStarted, CompletedAt: visionCompleted, Status: visionStatus, ErrorCode: visionCode, Attributes: map[string]any{"provider": fallback.ProviderID, "model": fallback.Model, "strategy": strategy, "cache_hit": cacheHit, "image_count": len(identities)}})
	attributes["vision_cache_hit"] = cacheHit
	attributes["vision_latency_ms"] = visionCompleted.Sub(visionStarted).Milliseconds()
	if analysisErr != nil {
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: visionCode, Attributes: attributes}}
		return result, analysisErr
	}
	processed, normalizeErr := NormalizeVisionRequest(req, analysis.Evidence, fallback.ProviderID, fallback.Model, p.TextLimits.Total)
	if normalizeErr != nil {
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteRejected), Reason: gatewayErrorCode(normalizeErr), Attributes: attributes}}
		return result, normalizeErr
	}
	result.Request = processed
	attributes["vision_evidence_chars"] = utf8.RuneCountInString(analysis.Evidence)
	attributes["effective_provider"] = route.Target.ProviderID
	attributes["effective_model"] = route.Target.Model
	result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(RouteVisionFallback), Reason: reason, Attributes: attributes}}
	return result, nil
}

func (p *Processor) takeoverContextLimit(req *ir.Request, target modelresolver.Target) (estimated, limit int64, exceeded bool) {
	if p.Catalog == nil {
		return 0, 0, false
	}
	provider, ok := p.Providers[target.ProviderID]
	if !ok {
		return 0, 0, false
	}
	match := p.Catalog.Snapshot().Lookup(provider.CatalogProvider, target.ProviderID, provider.BaseURL, target.Model)
	if match.Model == nil {
		return 0, 0, false
	}
	if match.Model.InputLimit != nil {
		limit = *match.Model.InputLimit
	} else if match.Model.ContextLimit != nil {
		limit = *match.Model.ContextLimit
	}
	if limit <= 0 {
		return 0, 0, false
	}
	estimated = estimateRequestTokens(req)
	return estimated, limit, estimated > limit
}

func (p *Processor) resolveVisionFallback(req *ir.Request, original modelresolver.Target) (modelresolver.Target, error) {
	if p.VisionFallbackModel == "" || p.Resolver == nil {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	copy := *req
	copy.RequestedModel = p.VisionFallbackModel
	target, resolveErr := p.Resolver.Resolve(&copy)
	if resolveErr != nil || target.ProviderID == "" || (target.ProviderID == original.ProviderID && target.Model == original.Model) {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	provider, ok := p.Providers[target.ProviderID]
	if !ok {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	var catalog *modelcatalog.Snapshot
	if p.Catalog != nil {
		catalog = p.Catalog.Snapshot()
	}
	capability := ResolveCapabilities(target.ProviderID, target.Model, provider, catalog)
	if capability.Capabilities.ImageInput != modelcapability.SupportSupported {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	if p.AdapterCapabilities == nil {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	adapter, ok := p.AdapterCapabilities(target.ProviderType)
	if !ok || !adapter.Vision {
		return modelresolver.Target{}, ir.GatewayError{Code: "vision_fallback_invalid"}
	}
	return target, nil
}

func gatewayErrorCode(err error) string {
	var gateway ir.GatewayError
	if errors.As(err, &gateway) {
		return gateway.Code
	}
	var provider ocr.Error
	if errors.As(err, &provider) {
		return provider.Code
	}
	return "ocr_unavailable"
}

func visionErrorCode(err error) string {
	code := gatewayErrorCode(err)
	if code == "ocr_unavailable" {
		return "vision_fallback_unavailable"
	}
	return code
}

func canUseVisionFallback(err error) bool {
	// A malformed image is a client error, not an OCR quality or availability
	// failure. Forwarding it to a Vision provider would bypass the gateway's
	// deterministic input validation and turn a 400 into an upstream request.
	switch gatewayErrorCode(err) {
	case "ocr_invalid_image", "ocr_image_limit_exceeded":
		return false
	default:
		return true
	}
}

func (p *Processor) applyOCR(ctx context.Context, req *ir.Request) (*ir.Request, []ocr.Result, int, time.Duration, *float64, error) {
	images, err := ResolveImages(req, p.ImageLimits)
	if err != nil {
		return nil, nil, 0, 0, nil, err
	}
	if len(images) == 0 {
		return req, nil, 0, 0, nil, nil
	}
	ocrCtx := ctx
	cancel := func() {}
	if p.OCRTimeout > 0 {
		ocrCtx, cancel = context.WithTimeout(ctx, p.OCRTimeout)
	}
	defer cancel()
	started := time.Now()
	results := make([]ocr.Result, 0, len(images))
	cacheHits := 0
	var minConfidence *float64
	totalTextChars := 0
	for _, image := range images {
		key := p.cachePrefix + ":" + image.SHA256
		result, hit, recognizeErr := p.Cache.Do(ocrCtx, key, func() (ocr.Result, error) {
			recognized, err := p.OCR.Recognize(ocrCtx, []ocr.Image{image})
			if err != nil {
				return ocr.Result{}, err
			}
			if len(recognized) != 1 || recognized[0].Index != image.Index {
				return ocr.Result{}, ocr.Error{StatusCode: 502, Code: "ocr_invalid_response", Message: "OCR did not return exactly one result for the image."}
			}
			result := recognized[0]
			result.Index = 0
			return result, nil
		})
		if recognizeErr != nil {
			return nil, results, cacheHits, time.Since(started), minConfidence, normalizeOCRError(recognizeErr)
		}
		if hit {
			cacheHits++
		}
		result.Index = image.Index
		if result.Confidence != nil {
			if minConfidence == nil || *result.Confidence < *minConfidence {
				value := *result.Confidence
				minConfidence = &value
			}
			if *result.Confidence < p.MinConfidence {
				results = append(results, result)
				return nil, results, cacheHits, time.Since(started), minConfidence, ir.GatewayError{StatusCode: 422, Kind: "multimodal_error", Code: "ocr_no_usable_text", Message: "OCR confidence is below the configured threshold."}
			}
		}
		totalTextChars += utf8.RuneCountInString(strings.TrimSpace(result.Text))
		results = append(results, result)
	}
	if totalTextChars < p.MinTextChars {
		return nil, results, cacheHits, time.Since(started), minConfidence, ir.GatewayError{StatusCode: 422, Kind: "multimodal_error", Code: "ocr_no_usable_text", Message: "OCR did not extract enough usable text from the images."}
	}
	processed, err := NormalizeOCRRequest(req, results, p.TextLimits)
	if err != nil {
		return nil, results, cacheHits, time.Since(started), minConfidence, err
	}
	return processed, results, cacheHits, time.Since(started), minConfidence, nil
}

func normalizeOCRError(err error) error {
	if gateway, ok := err.(ir.GatewayError); ok {
		return gateway
	}
	if provider, ok := err.(ocr.Error); ok {
		return ir.GatewayError{StatusCode: provider.StatusCode, Kind: "multimodal_error", Code: provider.Code, Message: provider.Message}
	}
	if err == context.DeadlineExceeded || err == context.Canceled {
		return ir.GatewayError{StatusCode: 503, Kind: "multimodal_error", Code: "ocr_unavailable", Message: "The OCR request timed out or was canceled."}
	}
	return ir.GatewayError{StatusCode: 503, Kind: "multimodal_error", Code: "ocr_unavailable", Message: "The OCR service is unavailable."}
}

func multimodalErrorMessage(code string) string {
	if code == "image_transport_unsupported" {
		return "The selected provider adapter cannot encode image input."
	}
	if code == "vision_fallback_invalid" {
		return "The configured Vision fallback model is invalid or does not support image input."
	}
	return "The selected model does not support image input and no fallback is configured."
}
