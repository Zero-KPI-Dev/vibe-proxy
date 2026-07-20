package multimodal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Enabled       bool
	Catalog       *modelcatalog.Service
	OCR           ocr.Provider
	OCRTimeout    time.Duration
	MinConfidence float64
	MinTextChars  int
	ImageLimits   ImageLimits
	TextLimits    TextLimits
	Cache         *OCRCache
	cachePrefix   string
}

func NewProcessor(cfg config.MultimodalConfig, catalog *modelcatalog.Service, client *http.Client) *Processor {
	processor := &Processor{
		Enabled:       cfg.Enabled,
		Catalog:       catalog,
		OCRTimeout:    cfg.OCR.Timeout.Duration,
		MinConfidence: cfg.OCR.MinConfidence,
		MinTextChars:  cfg.OCR.MinTextChars,
		ImageLimits: ImageLimits{
			MaxImages:          cfg.OCR.MaxImages,
			MaxImageBytes:      cfg.OCR.MaxImageBytes,
			MaxTotalImageBytes: cfg.OCR.MaxTotalImageBytes,
			RemoteImages:       cfg.OCR.RemoteImages,
		},
		TextLimits: TextLimits{PerImage: cfg.OCR.MaxTextCharsPerImage, Total: cfg.OCR.MaxTextCharsTotal},
	}
	if !cfg.Enabled || cfg.OCR.Provider != "http" || cfg.OCR.Endpoint == "" {
		return processor
	}
	processor.OCR = ocr.NewHTTPProvider(ocr.HTTPOptions{Endpoint: cfg.OCR.Endpoint, Auth: cfg.OCR.Auth, Client: client})
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
		if p.OCR != nil {
			decision.Mode = RouteOCRFallback
			decision.Reason = "model_does_not_support_images"
			decision.Degraded = true
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
			processed, hits, latency, minConfidence, err := p.applyOCR(ctx, req)
			if err != nil {
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
		}
		result.Decisions = []preprocess.Decision{{Processor: p.Name(), Route: string(decision.Mode), Reason: decision.Reason, Attributes: attributes}}
		return result, nil
	}
	status := 400
	if decision.Reason == "image_transport_unsupported" {
		status = 500
	}
	return result, ir.GatewayError{StatusCode: status, Kind: "multimodal_error", Code: decision.Reason, Message: multimodalErrorMessage(decision.Reason)}
}

func (p *Processor) applyOCR(ctx context.Context, req *ir.Request) (*ir.Request, int, time.Duration, *float64, error) {
	images, err := ResolveImages(req, p.ImageLimits)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	if len(images) == 0 {
		return req, 0, 0, nil, nil
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
			return nil, cacheHits, time.Since(started), minConfidence, normalizeOCRError(recognizeErr)
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
				return nil, cacheHits, time.Since(started), minConfidence, ir.GatewayError{StatusCode: 422, Kind: "multimodal_error", Code: "ocr_no_usable_text", Message: "OCR confidence is below the configured threshold."}
			}
		}
		totalTextChars += utf8.RuneCountInString(strings.TrimSpace(result.Text))
		results = append(results, result)
	}
	if totalTextChars < p.MinTextChars {
		return nil, cacheHits, time.Since(started), minConfidence, ir.GatewayError{StatusCode: 422, Kind: "multimodal_error", Code: "ocr_no_usable_text", Message: "OCR did not extract enough usable text from the images."}
	}
	processed, err := NormalizeOCRRequest(req, results, p.TextLimits)
	if err != nil {
		return nil, cacheHits, time.Since(started), minConfidence, err
	}
	return processed, cacheHits, time.Since(started), minConfidence, nil
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
	return "The selected model does not support image input and no fallback is configured."
}
