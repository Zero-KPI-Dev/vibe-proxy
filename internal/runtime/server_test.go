package runtime

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

var testPromOnce sync.Once
var testProm *metrics.Prometheus

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMetricsRange(t *testing.T) {
	tests := []struct {
		value  string
		window time.Duration
		bucket time.Duration
		ok     bool
	}{
		{value: "", window: time.Hour, bucket: 5 * time.Minute, ok: true},
		{value: "1h", window: time.Hour, bucket: 5 * time.Minute, ok: true},
		{value: "6h", window: 6 * time.Hour, bucket: 30 * time.Minute, ok: true},
		{value: "24h", window: 24 * time.Hour, bucket: time.Hour, ok: true},
		{value: "7d", window: 7 * 24 * time.Hour, bucket: 6 * time.Hour, ok: true},
		{value: "invalid", ok: false},
	}
	for _, test := range tests {
		window, bucket, ok := metricsRange(test.value)
		if ok != test.ok || window != test.window || bucket != test.bucket {
			t.Fatalf("metricsRange(%q) = %s, %s, %v", test.value, window, bucket, ok)
		}
	}
}

func TestRuntimeModelsEndpoint(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) { return jsonResponse(200, `{}`), nil })
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"vibe-coder"`) || !strings.Contains(w.Body.String(), `"id":"raw-chat"`) {
		t.Fatalf("unexpected models response: %d %s", w.Code, w.Body.String())
	}
}

func TestRuntimeOpenAIChatToOpenAICompatible(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://mock.openai/v1/chat/completions" {
			t.Fatalf("unexpected upstream url: %s", r.URL.String())
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"raw-chat"`) {
			t.Fatalf("model not resolved: %s", body)
		}
		return jsonResponse(200, `{"id":"chatcmpl_1","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"content":"ok"`) {
		t.Fatalf("unexpected response code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRuntimeAdminPlaygroundUsesFullPipelineWithoutDataPlaneKey(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{
		Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"},
		Providers: map[string]config.ProviderConfig{"mockai": {
			Type:    "openai-compatible",
			BaseURL: "https://mock.openai/v1",
			Auth:    upstreamauth.Profile{Type: "none"},
			Models:  []string{"raw-chat"},
		}},
		Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	recent := telemetry.NewRecentStore(10)
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{recent}, testProm)
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://mock.openai/v1/chat/completions" {
			t.Fatalf("unexpected upstream URL: %s", r.URL.String())
		}
		return jsonResponse(200, `{"id":"chatcmpl_admin","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"admin playground ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
	})})

	unauthorized := httptest.NewRequest(http.MethodPost, "/admin/playground/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`))
	unauthorizedW := httptest.NewRecorder()
	s.Routes().ServeHTTP(unauthorizedW, unauthorized)
	if unauthorizedW.Code != http.StatusUnauthorized {
		t.Fatalf("admin playground accepted missing admin token: %d %s", unauthorizedW.Code, unauthorizedW.Body.String())
	}

	publicWithAdminToken := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`))
	publicWithAdminToken.Header.Set("Authorization", "Bearer admin-token")
	publicW := httptest.NewRecorder()
	s.Routes().ServeHTTP(publicW, publicWithAdminToken)
	if publicW.Code != http.StatusUnauthorized || !strings.Contains(publicW.Body.String(), `"code":"invalid_api_key"`) {
		t.Fatalf("public data plane unexpectedly trusted admin token: %d %s", publicW.Code, publicW.Body.String())
	}

	requests := []struct {
		path string
		body string
		want string
	}{
		{"/admin/playground/v1/chat/completions", `{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`, "admin playground ok"},
		{"/admin/playground/v1/responses", `{"model":"vibe-fast","input":"hi"}`, `"object":"response"`},
		{"/admin/playground/anthropic/v1/messages", `{"model":"vibe-fast","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`, `"type":"message"`},
	}
	requestIDs := map[string]bool{}
	for _, item := range requests {
		req := adminJSONRequest(http.MethodPost, item.path, item.body)
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), item.want) {
			t.Fatalf("unexpected admin playground response for %s: %d %s", item.path, w.Code, w.Body.String())
		}
		requestID := w.Header().Get("X-Vibe-Proxy-Request-ID")
		if requestID == "" {
			t.Fatalf("admin playground response did not expose a request ID: %v", w.Header())
		}
		requestIDs[requestID] = true
	}
	events := recent.Recent(10)
	if len(events) != len(requests) {
		t.Fatalf("admin playground requests were not all tracked: %+v", events)
	}
	for _, event := range events {
		if event.ClientName != "admin-playground" || !requestIDs[event.RequestID] {
			t.Fatalf("admin playground request was not tracked correctly: %+v", events)
		}
	}
	if len(requestIDs) != len(requests) {
		t.Fatalf("admin playground request was not tracked correctly: %+v", events)
	}
}

func TestRuntimeDisabledMultimodalPreprocessorPreservesImages(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"image_url":{"url":"data:image/png;base64,AA=="}`) {
			t.Fatalf("image was not preserved: %s", body)
		}
		return jsonResponse(200, `{"id":"chatcmpl_image","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vibe-fast","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`)))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected response code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRuntimeOCRFallbackForAllClientProtocolsAndCache(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{
		Security:   config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"},
		ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}},
		Multimodal: config.MultimodalConfig{Enabled: true, OCR: config.OCRConfig{Provider: "http", Endpoint: "http://ocr.local/v1/ocr", Auth: upstreamauth.Profile{Type: "none"}}},
		Providers: map[string]config.ProviderConfig{"mockai": {
			Type:                "openai-compatible",
			BaseURL:             "https://mock.openai/v1",
			Auth:                upstreamauth.Profile{Type: "none"},
			Models:              []string{"raw-chat"},
			DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported},
		}},
		Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	recent := telemetry.NewRecentStore(20)
	s := New("", cfg, metrics.MultiSink{recent}, testProm)
	var ocrCalls atomic.Int32
	s.SetOCRHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		ocrCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"media_type":"image/png"`) {
			t.Fatalf("unexpected OCR request: %s", body)
		}
		return jsonResponse(200, `{"results":[{"index":0,"text":"recognized invoice 123","confidence":0.96,"language":"en"}]}`), nil
	})})
	var upstreamCalls atomic.Int32
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		upstreamCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "image_url") || !strings.Contains(string(body), "recognized invoice 123") || !strings.Contains(string(body), "untrusted user data") {
			t.Fatalf("upstream did not receive normalized OCR text: %s", body)
		}
		if strings.Contains(string(body), `"stream":true`) {
			time.Sleep(2 * time.Millisecond)
			return jsonResponse(200, "data: {\"id\":\"chatcmpl_ocr_stream\",\"choices\":[{\"delta\":{\"content\":\"ocr stream\"},\"finish_reason\":\"\"}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"), nil
		}
		return jsonResponse(200, `{"id":"chatcmpl_ocr","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ocr ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
	})})
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	encoded := base64.StdEncoding.EncodeToString(png)
	requests := []struct {
		path string
		body string
	}{
		{"/v1/chat/completions", `{"model":"vibe-fast","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + encoded + `"}}]}]}`},
		{"/v1/responses", `{"model":"vibe-fast","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + encoded + `"}]}]}`},
		{"/anthropic/v1/messages", `{"model":"vibe-fast","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + encoded + `"}}]}]}`},
		{"/v1/chat/completions", `{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + encoded + `"}}]}]}`},
	}
	for _, item := range requests {
		req := httptest.NewRequest(http.MethodPost, item.path, strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s failed: %d %s", item.path, w.Code, w.Body.String())
		}
		if w.Header().Get("X-Vibe-Proxy-Image-Fallback") != "ocr" || w.Header().Get("X-Vibe-Proxy-OCR-Images") != "1" || !strings.Contains(w.Header().Get("Warning"), "degraded to OCR text") {
			t.Fatalf("missing OCR response headers for %s: %v", item.path, w.Header())
		}
	}
	remoteReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/private.png"}}]}]}`))
	remoteReq.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	remoteW := httptest.NewRecorder()
	s.Routes().ServeHTTP(remoteW, remoteReq)
	if remoteW.Code != http.StatusBadRequest || !strings.Contains(remoteW.Body.String(), `"code":"ocr_remote_image_disabled"`) {
		t.Fatalf("unexpected remote image error: %d %s", remoteW.Code, remoteW.Body.String())
	}
	if ocrCalls.Load() != 1 || upstreamCalls.Load() != 4 {
		t.Fatalf("unexpected call counts: OCR=%d upstream=%d", ocrCalls.Load(), upstreamCalls.Load())
	}
	var sawOCR, sawCache, sawRejected, sawStreamTiming bool
	for _, event := range recent.Recent(20) {
		if event.FirstTokenAt != nil && event.TTFTMillis > 0 {
			sawStreamTiming = true
		}
		if event.Transformation == nil {
			continue
		}
		switch event.Transformation.MultimodalRoute {
		case "ocr_fallback":
			sawOCR = event.Transformation.OCRProcessed == 1 && event.Transformation.OCRProvider == "http"
			if event.Transformation.OCRCacheHits == 1 {
				sawCache = true
			}
		case "rejected":
			if event.Transformation.OCRFailureCode == "ocr_remote_image_disabled" {
				sawRejected = true
			}
		}
	}
	if !sawOCR || !sawCache || !sawRejected {
		t.Fatalf("missing structured OCR telemetry: %+v", recent.Recent(20))
	}
	if !sawStreamTiming {
		t.Fatalf("missing streaming latency telemetry: %+v", recent.Recent(20))
	}
}

func TestRuntimeFallsBackFromOCRToVisionProvider(t *testing.T) {
	cfg, err := config.CompileSimple(config.SimpleConfig{
		ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}},
		Multimodal: config.MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision", OCR: config.OCRConfig{Provider: "http", Endpoint: "http://ocr.local/v1/ocr", Auth: upstreamauth.Profile{Type: "none"}}},
		Providers: map[string]config.ProviderConfig{
			"text":   {Type: "openai-compatible", BaseURL: "https://text.example/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"text-model"}, DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}},
			"vision": {Type: "openai-compatible", BaseURL: "https://vision.example/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"vision-model"}, DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported}},
		},
		Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "text/text-model", "vibe-vision": "vision/vision-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if issues := config.ValidateRuntime(cfg); config.HasErrors(issues) {
		t.Fatalf("invalid test config: %+v", issues)
	}
	recent := telemetry.NewRecentStore(10)
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{recent}, testProm)
	s.SetOCRHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"results":[{"index":0,"text":"uncertain","confidence":0.1}]}`), nil
	})})
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://vision.example/v1/chat/completions" {
			t.Fatalf("request did not switch to Vision provider: %s", r.URL.String())
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"vision-model"`) || !strings.Contains(string(body), `"image_url"`) {
			t.Fatalf("Vision fallback did not preserve original image: %s", body)
		}
		return jsonResponse(200, `{"id":"chatcmpl_vision","model":"vision-model","choices":[{"message":{"role":"assistant","content":"vision ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
	})})
	encoded := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,`+encoded+`"}}]}]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "vision ok") || w.Header().Get("X-Vibe-Proxy-Image-Fallback") != "vision" {
		t.Fatalf("unexpected Vision fallback response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	events := recent.Recent(10)
	if len(events) != 1 || events[0].Transformation == nil || events[0].Transformation.MultimodalRoute != "vision_fallback" || events[0].Transformation.RouteReason != "ocr_no_usable_text" || events[0].Transformation.ModelImageSupport != "unsupported" || events[0].Transformation.OriginalProvider != "text" || events[0].Transformation.EffectiveProvider != "vision" || events[0].Transformation.OCRFailureCode != "ocr_no_usable_text" {
		t.Fatalf("unexpected Vision telemetry: %+v", events)
	}
}

func TestRuntimeAdminConfiguresAndTestsOCRWithoutLeakingSecret(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	content := `version: vibeproxy.io/v1alpha1
security:
  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN
providers:
  text:
    type: openai-compatible
    base_url: http://127.0.0.1:3000/v1
    auth:
      type: none
    models: [text-model]
    default_capabilities:
      image_input: unsupported
models:
  allow_raw: true
  aliases: {}
multimodal:
  enabled: false
  ocr:
    provider: http
    endpoint: http://old-ocr.local/v1/ocr
    auth:
      type: bearer
      token: literal:old-secret
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	s.SetOCRHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("x-ocr-key") != "unsaved-secret" {
			t.Fatalf("unsaved OCR auth not applied: %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"data_base64"`) || strings.Contains(string(body), "unsaved-secret") {
			t.Fatalf("unexpected OCR test body: %s", body)
		}
		return jsonResponse(200, `{"results":[{"index":0,"text":"ok","confidence":0.99}]}`), nil
	})})

	getReq := httptest.NewRequest(http.MethodGet, "/admin/multimodal", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getW := httptest.NewRecorder()
	s.Routes().ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK || strings.Contains(getW.Body.String(), "old-secret") || !strings.Contains(getW.Body.String(), `"api_key_source":"literal"`) {
		t.Fatalf("unexpected multimodal snapshot: %d %s", getW.Code, getW.Body.String())
	}

	testBody := `{"enabled":true,"endpoint":"http://new-ocr.local/v1/ocr","auth_type":"api_key_header","api_key_source":"literal","api_key":"unsaved-secret","header":"x-ocr-key","min_confidence":0.55,"min_text_chars":4,"max_images":4}`
	testReq := httptest.NewRequest(http.MethodPost, "/admin/multimodal/ocr/test", strings.NewReader(testBody))
	testReq.Header.Set("Authorization", "Bearer admin-token")
	testW := httptest.NewRecorder()
	s.Routes().ServeHTTP(testW, testReq)
	if testW.Code != http.StatusOK || !strings.Contains(testW.Body.String(), `"ok":true`) || strings.Contains(testW.Body.String(), "unsaved-secret") {
		t.Fatalf("unexpected OCR test response: %d %s", testW.Code, testW.Body.String())
	}

	saveBody := `{"enabled":true,"endpoint":"http://new-ocr.local/v1/ocr","auth_type":"bearer","api_key_source":"env","api_key_env":"OCR_TOKEN","min_confidence":0.6,"min_text_chars":5,"max_images":3}`
	saveReq := httptest.NewRequest(http.MethodPut, "/admin/multimodal", strings.NewReader(saveBody))
	saveReq.Header.Set("Authorization", "Bearer admin-token")
	saveW := httptest.NewRecorder()
	s.Routes().ServeHTTP(saveW, saveReq)
	if saveW.Code != http.StatusOK || !strings.Contains(saveW.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected OCR save response: %d %s", saveW.Code, saveW.Body.String())
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "env:OCR_TOKEN") || strings.Contains(string(written), "unsaved-secret") || !strings.Contains(string(written), "min_confidence: 0.6") {
		t.Fatalf("unexpected saved OCR config: %s", written)
	}
}

func TestRuntimeResponsesToOpenAICompatible(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"id":"chatcmpl_2","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"response ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`), nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"model":"vibe-fast","input":"hi"}`)))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `response ok`) || !strings.Contains(w.Body.String(), `"object":"response"`) {
		t.Fatalf("unexpected response code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRuntimeAnthropicMessagesToAnthropicProvider(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://mock.anthropic/v1/messages" {
			t.Fatalf("unexpected upstream url: %s", r.URL.String())
		}
		if r.Header.Get("x-api-key") != "test-anthropic-key" {
			t.Fatalf("missing provider auth: %v", r.Header)
		}
		return jsonResponse(200, `{"id":"msg_1","model":"claude-raw","stop_reason":"end_turn","content":[{"type":"text","text":"anthropic ok"}],"usage":{"input_tokens":1,"output_tokens":2}}`), nil
	})
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", bytes.NewReader([]byte(`{"model":"vibe-coder","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `anthropic ok`) || !strings.Contains(w.Body.String(), `"type":"message"`) {
		t.Fatalf("unexpected response code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRuntimeAdminRecentRequests(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	recent := telemetry.NewRecentStore(10)
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}}, Providers: map[string]config.ProviderConfig{"mockai": {Type: "openai-compatible", BaseURL: "https://mock.openai/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"raw-chat"}}}, Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{recent}, testProm)
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"id":"chatcmpl_1","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
	})})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("request failed: %d %s", w.Code, w.Body.String())
	}
	adminReq := httptest.NewRequest(http.MethodGet, "/admin/requests/recent", nil)
	adminReq.Header.Set("Authorization", "Bearer admin-token")
	adminW := httptest.NewRecorder()
	s.Routes().ServeHTTP(adminW, adminReq)
	if adminW.Code != 200 || !strings.Contains(adminW.Body.String(), `"recent"`) || !strings.Contains(adminW.Body.String(), `"vibe-fast"`) {
		t.Fatalf("unexpected admin response: %d %s", adminW.Code, adminW.Body.String())
	}
}

func TestRuntimeAdminValidateAndProviderTest(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}}, Providers: map[string]config.ProviderConfig{"mockai": {Type: "openai-compatible", BaseURL: "https://mock.openai/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"raw-chat"}}}, Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != "https://mock.openai/v1/models" {
			t.Fatalf("provider test used wrong probe target: %s %s", r.Method, r.URL.String())
		}
		return jsonResponse(200, `{"object":"list","data":[]}`), nil
	})})

	validateReq := httptest.NewRequest(http.MethodGet, "/admin/config/validate", nil)
	validateReq.Header.Set("Authorization", "Bearer admin-token")
	validateW := httptest.NewRecorder()
	s.Routes().ServeHTTP(validateW, validateReq)
	if validateW.Code != 200 || !strings.Contains(validateW.Body.String(), `"valid":true`) {
		t.Fatalf("unexpected validate response: %d %s", validateW.Code, validateW.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/admin/providers/test?id=mockai", nil)
	testReq.Header.Set("Authorization", "Bearer admin-token")
	testW := httptest.NewRecorder()
	s.Routes().ServeHTTP(testW, testReq)
	if testW.Code != 200 || !strings.Contains(testW.Body.String(), `"ok":true`) || !strings.Contains(testW.Body.String(), `"status":200`) || !strings.Contains(testW.Body.String(), `"target":"https://mock.openai/v1/models"`) {
		t.Fatalf("unexpected provider test response: %d %s", testW.Code, testW.Body.String())
	}
}

func TestRuntimeProviderTestTreats404AsFailure(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}}, Providers: map[string]config.ProviderConfig{"mockai": {Type: "openai-compatible", BaseURL: "https://mock.openai/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"raw-chat"}}}, Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(404, `{"error":"not found"}`), nil
	})})

	testReq := httptest.NewRequest(http.MethodPost, "/admin/providers/test?id=mockai", nil)
	testReq.Header.Set("Authorization", "Bearer admin-token")
	testW := httptest.NewRecorder()
	s.Routes().ServeHTTP(testW, testReq)
	if testW.Code != 200 || !strings.Contains(testW.Body.String(), `"ok":false`) || !strings.Contains(testW.Body.String(), `"status":404`) {
		t.Fatalf("unexpected provider test response: %d %s", testW.Code, testW.Body.String())
	}
}

func TestRuntimeAdminLocalConfigure(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(path, []byte("version: vibeproxy.io/v1alpha1\nsecurity:\n  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN\nclient_keys: []\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	body := bytes.NewReader([]byte(`{"id":"newapi","type":"openai-compatible","base_url":"http://127.0.0.1:3000/v1","api_key_env":"NEW_API_KEY","models":["deepseek-v4-flash"],"alias":"vibe-chat","alias_model":"deepseek-v4-flash","default_model":"vibe-chat"}`))
	req := httptest.NewRequest(http.MethodPost, "/admin/local/configure", body)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected configure response: %d %s", w.Code, w.Body.String())
	}
	snap := s.current()
	if _, ok := snap.Config.Providers["newapi"]; !ok {
		t.Fatalf("provider not loaded into snapshot")
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "vibe-chat") || !strings.Contains(string(written), "NEW_API_KEY") {
		t.Fatalf("config was not written: %s", written)
	}
}

func TestRuntimeAdminLocalConfigureAcceptsLiteralAPIKey(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	if err := os.WriteFile(path, []byte("version: vibeproxy.io/v1alpha1\nsecurity:\n  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN\nclient_keys: []\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	body := bytes.NewReader([]byte(`{"id":"direct","type":"openai-compatible","base_url":"http://127.0.0.1:3000/v1","auth_type":"bearer","api_key_source":"literal","api_key":"sk-direct-secret","models":["deepseek-v4-flash"]}`))
	req := httptest.NewRequest(http.MethodPost, "/admin/local/configure", body)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected configure response: %d %s", w.Code, w.Body.String())
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "literal:sk-direct-secret") {
		t.Fatalf("literal secret was not written: %s", written)
	}
	snapshotReq := httptest.NewRequest(http.MethodGet, "/admin/config/snapshot", nil)
	snapshotReq.Header.Set("Authorization", "Bearer admin-token")
	snapshotW := httptest.NewRecorder()
	s.Routes().ServeHTTP(snapshotW, snapshotReq)
	if snapshotW.Code != 200 {
		t.Fatalf("unexpected snapshot response: %d %s", snapshotW.Code, snapshotW.Body.String())
	}
	if strings.Contains(snapshotW.Body.String(), "sk-direct-secret") {
		t.Fatalf("snapshot leaked literal secret: %s", snapshotW.Body.String())
	}
	if !strings.Contains(snapshotW.Body.String(), `"api_key_source":"literal"`) {
		t.Fatalf("snapshot did not expose key source metadata: %s", snapshotW.Body.String())
	}
}

func TestRuntimeAdminProviderModelsProbeUsesUnsavedFormAuth(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, Providers: map[string]config.ProviderConfig{}, Models: config.ModelsConfig{AllowRaw: true}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != "https://mock.openai/v1/models" {
			t.Fatalf("unexpected models probe: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer sk-direct-secret" {
			t.Fatalf("probe did not use form auth: %v", r.Header)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"z-model"},{"id":"a-model"}]}`), nil
	})})
	s.SetCatalogHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://models.dev/api.json" {
			t.Fatalf("unexpected catalog url: %s", r.URL.String())
		}
		return jsonResponse(200, `{"draft":{"id":"draft","models":{"a-model":{"id":"a-model","name":"A Model","reasoning":true,"tool_call":true,"modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":128000,"output":16000}}}}}`), nil
	})})

	req := adminJSONRequest(http.MethodPost, "/admin/providers/models", `{"id":"draft","type":"openai-compatible","base_url":"https://mock.openai/v1","auth_type":"bearer","api_key_source":"literal","api_key":"sk-direct-secret"}`)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"a-model"`) || !strings.Contains(w.Body.String(), `"z-model"`) {
		t.Fatalf("unexpected provider models response: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"model_details"`) || !strings.Contains(w.Body.String(), `"image_input":"supported"`) || !strings.Contains(w.Body.String(), `"context_limit":128000`) {
		t.Fatalf("provider models response missing catalog details: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-direct-secret") {
		t.Fatalf("provider models response leaked secret: %s", w.Body.String())
	}
	if len(s.current().Config.Providers) != 0 {
		t.Fatalf("models probe unexpectedly saved provider")
	}
}

func TestRuntimeAdminModelCatalogRefreshStatusAndLookup(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, Providers: map[string]config.ProviderConfig{}, Models: config.ModelsConfig{AllowRaw: true}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)
	s.SetCatalogHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		resp := jsonResponse(200, `{"openai":{"id":"openai","models":{"gpt-vision":{"id":"gpt-vision","name":"GPT Vision","tool_call":true,"modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":128000,"output":16000}}}}}`)
		resp.Header.Set("ETag", `"catalog-v1"`)
		return resp, nil
	})})

	refresh := adminJSONRequest(http.MethodPost, "/admin/model-catalog/refresh", "")
	refreshW := httptest.NewRecorder()
	s.Routes().ServeHTTP(refreshW, refresh)
	if refreshW.Code != http.StatusOK || !strings.Contains(refreshW.Body.String(), `"models":1`) || !strings.Contains(refreshW.Body.String(), `"etag":"\"catalog-v1\""`) {
		t.Fatalf("unexpected refresh response: %d %s", refreshW.Code, refreshW.Body.String())
	}

	status := adminJSONRequest(http.MethodGet, "/admin/model-catalog/status", "")
	statusW := httptest.NewRecorder()
	s.Routes().ServeHTTP(statusW, status)
	if statusW.Code != http.StatusOK || !strings.Contains(statusW.Body.String(), `"providers":1`) {
		t.Fatalf("unexpected status response: %d %s", statusW.Code, statusW.Body.String())
	}

	lookup := adminJSONRequest(http.MethodPost, "/admin/model-catalog/lookup", `{"provider_id":"newapi","catalog_provider":"openai","models":["gpt-vision","private-model"]}`)
	lookupW := httptest.NewRecorder()
	s.Routes().ServeHTTP(lookupW, lookup)
	if lookupW.Code != http.StatusOK || !strings.Contains(lookupW.Body.String(), `"gpt-vision":{"requested_model":"gpt-vision","status":"exact_provider"`) || !strings.Contains(lookupW.Body.String(), `"private-model":{"requested_model":"private-model","status":"not_found"`) {
		t.Fatalf("unexpected lookup response: %d %s", lookupW.Code, lookupW.Body.String())
	}
}

func TestRuntimeAdminProviderCRUD(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)

	createBody := `{"id":"localai","type":"openai-compatible","base_url":"http://127.0.0.1:3000/v1","auth_type":"none","models":["gpt-test"],"alias":"vibe-test","alias_model":"gpt-test","default_model":"vibe-test"}`
	createReq := adminJSONRequest(http.MethodPost, "/admin/providers", createBody)
	createW := httptest.NewRecorder()
	s.Routes().ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK || !strings.Contains(createW.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected provider create response: %d %s", createW.Code, createW.Body.String())
	}
	if _, ok := s.current().Config.Providers["localai"]; !ok {
		t.Fatalf("created provider not loaded")
	}

	updateBody := `{"id":"localai","type":"openai-compatible","base_url":"http://127.0.0.1:3001/v1","auth_type":"none","models":["gpt-test-2"],"max_concurrency":7}`
	updateReq := adminJSONRequest(http.MethodPut, "/admin/providers", updateBody)
	updateW := httptest.NewRecorder()
	s.Routes().ServeHTTP(updateW, updateReq)
	if updateW.Code != http.StatusOK || s.current().Config.Providers["localai"].BaseURL != "http://127.0.0.1:3001/v1" {
		t.Fatalf("unexpected provider update response: %d %s", updateW.Code, updateW.Body.String())
	}

	deleteReq := adminJSONRequest(http.MethodDelete, "/admin/providers", `{"id":"localai"}`)
	deleteW := httptest.NewRecorder()
	s.Routes().ServeHTTP(deleteW, deleteReq)
	if deleteW.Code != http.StatusOK || strings.Contains(deleteW.Body.String(), "error") {
		t.Fatalf("unexpected provider delete response: %d %s", deleteW.Code, deleteW.Body.String())
	}
	if _, ok := s.current().Config.Providers["localai"]; ok {
		t.Fatalf("deleted provider still loaded")
	}
}

func TestRuntimeAdminAliasAndClientKeyCRUD(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)

	aliasCreate := adminJSONRequest(http.MethodPost, "/admin/aliases", `{"alias":"vibe-fast","target":"mockai/raw-chat"}`)
	aliasCreateW := httptest.NewRecorder()
	s.Routes().ServeHTTP(aliasCreateW, aliasCreate)
	if aliasCreateW.Code != http.StatusOK {
		t.Fatalf("unexpected alias create response: %d %s", aliasCreateW.Code, aliasCreateW.Body.String())
	}

	aliasList := adminJSONRequest(http.MethodGet, "/admin/aliases", "")
	aliasListW := httptest.NewRecorder()
	s.Routes().ServeHTTP(aliasListW, aliasList)
	if aliasListW.Code != http.StatusOK || !strings.Contains(aliasListW.Body.String(), `"vibe-fast":"mockai/raw-chat"`) {
		t.Fatalf("unexpected alias list response: %d %s", aliasListW.Code, aliasListW.Body.String())
	}

	defaultReq := adminJSONRequest(http.MethodPut, "/admin/aliases/default", `{"default_model":"vibe-fast","allow_raw":false}`)
	defaultW := httptest.NewRecorder()
	s.Routes().ServeHTTP(defaultW, defaultReq)
	if defaultW.Code != http.StatusOK || s.current().Config.ModelResolver.DefaultModel != "vibe-fast" || s.current().Config.ModelResolver.AllowRaw {
		t.Fatalf("unexpected alias defaults response: %d %s", defaultW.Code, defaultW.Body.String())
	}

	keyCreate := adminJSONRequest(http.MethodPost, "/admin/client-keys", `{"name":"agent","allowed_models":["vibe-fast"],"rpm":10}`)
	keyCreateW := httptest.NewRecorder()
	s.Routes().ServeHTTP(keyCreateW, keyCreate)
	if keyCreateW.Code != http.StatusOK || !strings.Contains(keyCreateW.Body.String(), `"raw_key":"sk-`) {
		t.Fatalf("unexpected client key create response: %d %s", keyCreateW.Code, keyCreateW.Body.String())
	}

	keyUpdate := adminJSONRequest(http.MethodPut, "/admin/client-keys/agent", `{"enabled":false,"allowed_models":["*"],"rpm":20}`)
	keyUpdateW := httptest.NewRecorder()
	s.Routes().ServeHTTP(keyUpdateW, keyUpdate)
	if keyUpdateW.Code != http.StatusOK || s.current().Config.ClientKeys[len(s.current().Config.ClientKeys)-1].Enabled {
		t.Fatalf("unexpected client key update response: %d %s", keyUpdateW.Code, keyUpdateW.Body.String())
	}

	keyDelete := adminJSONRequest(http.MethodDelete, "/admin/client-keys/agent", "")
	keyDeleteW := httptest.NewRecorder()
	s.Routes().ServeHTTP(keyDeleteW, keyDelete)
	if keyDeleteW.Code != http.StatusOK {
		t.Fatalf("unexpected client key delete response: %d %s", keyDeleteW.Code, keyDeleteW.Body.String())
	}

	aliasDelete := adminJSONRequest(http.MethodDelete, "/admin/aliases/vibe-fast", "")
	aliasDeleteW := httptest.NewRecorder()
	s.Routes().ServeHTTP(aliasDeleteW, aliasDelete)
	if aliasDeleteW.Code != http.StatusOK {
		t.Fatalf("unexpected alias delete response: %d %s", aliasDeleteW.Code, aliasDeleteW.Body.String())
	}
	if _, ok := s.current().Config.ModelResolver.Aliases["vibe-fast"]; ok {
		t.Fatalf("deleted alias still loaded")
	}
}

func TestRuntimeAdminRawConfigRejectsInvalidYAMLWithoutOverwriting(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)

	req := adminJSONRequest(http.MethodPut, "/admin/config/raw", `{"yaml":"models:\n  aliases:\n    bad: missing-slash"}`)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid raw config to be rejected, got %d %s", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid raw config overwrote file:\n%s", after)
	}
}

func writeAdminTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap.yaml")
	content := "version: vibeproxy.io/v1alpha1\nsecurity:\n  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN\nclient_keys: []\nproviders:\n  mockai:\n    type: openai-compatible\n    base_url: https://mock.openai/v1\n    auth:\n      type: none\n    models:\n      - raw-chat\nmodels:\n  allow_raw: true\n  aliases: {}\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func adminJSONRequest(method, target, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newTestServer(t *testing.T, rt roundTrip) *Server {
	t.Helper()
	cfg, err := config.CompileSimple(config.SimpleConfig{ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}}, Providers: map[string]config.ProviderConfig{"anthropic": {Type: "anthropic", BaseURL: "https://mock.anthropic", Auth: upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: "literal:test-anthropic-key"}, Models: []string{"claude-raw"}}, "mockai": {Type: "openai-compatible", BaseURL: "https://mock.openai/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"raw-chat"}}}, Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-coder": "anthropic/claude-raw", "vibe-fast": "mockai/raw-chat"}}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, nil, testProm)
	s.SetHTTPClient(&http.Client{Transport: rt})
	return s
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
