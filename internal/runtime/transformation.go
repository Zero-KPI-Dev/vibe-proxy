package runtime

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/preprocess"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

func transformationSummary(decisions []preprocess.Decision, original, effective modelresolver.Target) *telemetry.TransformationSummary {
	for _, decision := range decisions {
		if decision.Processor != "multimodal_fallback" {
			continue
		}
		summary := &telemetry.TransformationSummary{
			MultimodalRoute:   decision.Route,
			RouteReason:       decision.Reason,
			OriginalProvider:  original.ProviderID,
			OriginalModel:     original.Model,
			EffectiveProvider: effective.ProviderID,
			EffectiveModel:    effective.Model,
		}
		summary.InputImages = attributeInt(decision.Attributes, "input_image_count")
		summary.ModelImageSupport = attributeString(decision.Attributes, "model_image_support")
		summary.CapabilitySource = attributeString(decision.Attributes, "capability_source")
		summary.CatalogMatch = attributeString(decision.Attributes, "catalog_match")
		summary.OCRProvider = attributeString(decision.Attributes, "ocr_provider")
		summary.OCRProcessed = attributeInt(decision.Attributes, "ocr_processed")
		summary.OCRCacheHits = attributeInt(decision.Attributes, "ocr_cache_hits")
		summary.OCRLatencyMS = int64(attributeInt(decision.Attributes, "ocr_latency_ms"))
		summary.OCRFailureCode = attributeString(decision.Attributes, "ocr_error_code")
		if confidence, ok := attributeFloat(decision.Attributes, "ocr_min_confidence"); ok {
			summary.OCRMinConfidence = &confidence
		}
		return summary
	}
	return nil
}

func applyTransformationHeaders(w http.ResponseWriter, summary *telemetry.TransformationSummary) {
	if summary == nil {
		return
	}
	switch summary.MultimodalRoute {
	case "ocr_fallback":
		w.Header().Set("X-Vibe-Proxy-Image-Fallback", "ocr")
		w.Header().Set("X-Vibe-Proxy-OCR-Images", strconv.Itoa(summary.OCRProcessed))
		w.Header().Add("Warning", `299 vibe-proxy "Image input was degraded to OCR text"`)
	case "vision_fallback":
		w.Header().Set("X-Vibe-Proxy-Image-Fallback", "vision")
	}
}

func attributeString(values map[string]any, key string) string {
	if values == nil || values[key] == nil {
		return ""
	}
	return fmt.Sprint(values[key])
}

func attributeInt(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func attributeFloat(values map[string]any, key string) (float64, bool) {
	if values == nil {
		return 0, false
	}
	switch value := values[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	default:
		return 0, false
	}
}
