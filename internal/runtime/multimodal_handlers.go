package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ocr"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

const ocrTestImageBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func (s *Server) adminMultimodal(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, multimodalAdminSnapshot(s.current().Config.Multimodal))
	case http.MethodPut:
		if s.cfgPath == "" {
			http.Error(w, `{"error":"config path not writable"}`, http.StatusBadRequest)
			return
		}
		var input config.MultimodalAdminInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		next, err := config.SaveMultimodal(s.cfgPath, input)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": "invalid_multimodal_config", "message": err.Error()})
			return
		}
		s.snapshot.Store(s.buildSnapshot(next))
		s.writeJSON(w, map[string]any{"ok": true, "multimodal": multimodalAdminSnapshot(next.Multimodal), "loaded_at": s.current().LoadedAt})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminOCRTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.adminAuthorize(w, r) {
		return
	}
	var input config.MultimodalAdminInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	cfg, err := config.BuildMultimodal(input, &s.current().Config.Multimodal)
	if err != nil || cfg.OCR.Endpoint == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid_ocr_config", "message": "OCR endpoint and valid authentication are required."})
		return
	}
	data, _ := base64.StdEncoding.DecodeString(ocrTestImageBase64)
	hash := sha256.Sum256(data)
	provider := ocr.NewHTTPProvider(ocr.HTTPOptions{Endpoint: cfg.OCR.Endpoint, Auth: cfg.OCR.Auth, Client: s.ocrHTTPClient})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	results, recognizeErr := provider.Recognize(ctx, []ocr.Image{{Index: 0, MediaType: "image/png", Data: data, SHA256: hex.EncodeToString(hash[:])}})
	latency := time.Since(started).Milliseconds()
	if recognizeErr != nil {
		status := http.StatusBadGateway
		code := "ocr_test_failed"
		if value, ok := recognizeErr.(ocr.Error); ok {
			status = value.StatusCode
			code = value.Code
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": code, "latency_ms": latency})
		return
	}
	hasText := false
	for _, result := range results {
		if strings.TrimSpace(result.Text) != "" {
			hasText = true
			break
		}
	}
	s.writeJSON(w, map[string]any{"ok": true, "provider": provider.Name(), "latency_ms": latency, "result_count": len(results), "has_text": hasText})
}

func multimodalAdminSnapshot(cfg config.MultimodalConfig) map[string]any {
	authType, keySource, keyEnv, header := authProfileMeta(cfg.OCR.Auth)
	return map[string]any{
		"enabled":               cfg.Enabled,
		"strategy":              cfg.Strategy,
		"endpoint":              cfg.OCR.Endpoint,
		"auth_type":             authType,
		"api_key_source":        keySource,
		"api_key_env":           keyEnv,
		"header":                header,
		"vision_fallback_model": cfg.VisionFallbackModel,
		"min_confidence":        cfg.OCR.MinConfidence,
		"min_text_chars":        cfg.OCR.MinTextChars,
		"max_images":            cfg.OCR.MaxImages,
	}
}

func authProfileMeta(profile upstreamauth.Profile) (authType, keySource, keyEnv, header string) {
	authType = profile.Type
	if authType == "" {
		authType = "none"
	}
	var secret upstreamauth.SecretRef
	switch authType {
	case "bearer":
		secret = profile.Token
	case "api_key_header":
		secret = profile.Value
		header = profile.Header
	}
	raw := string(secret)
	switch {
	case strings.HasPrefix(raw, "env:"):
		keySource = "env"
		keyEnv = strings.TrimPrefix(raw, "env:")
	case raw != "":
		keySource = "literal"
	}
	return
}
