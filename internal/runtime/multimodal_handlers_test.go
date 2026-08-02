package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/protocol"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

type noVisionProviderAdapter struct {
	protocol.ProviderAdapter
}

func (noVisionProviderAdapter) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{}
}

type visionFallbackModelResponse struct {
	Target     string                 `json:"target"`
	ProviderID string                 `json:"provider_id"`
	Model      string                 `json:"model"`
	Source     modelcapability.Source `json:"source"`
}

const catalogVisionFixture = `{
	"moonshotai": {
		"id": "moonshotai",
		"models": {
			"catalog-vision": {"id":"catalog-vision","modalities":{"input":["text","image"],"output":["text"]}}
		}
	}
}`

func writeCatalogVisionAdminConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `version: vibeproxy.io/v1alpha1
security:
  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN
storage:
  sqlite_path: ":memory:"
providers:
  gateway:
    type: openai-compatible
    base_url: https://gateway.example/v1
    catalog_provider: moonshotai
    auth: {type: none}
    models: [catalog-vision, unknown]
models:
  allow_raw: true
  aliases: {}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuntimeAdminMultimodalListsEffectiveVisionFallbackModels(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{
		Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"},
		Providers: map[string]config.ProviderConfig{
			"gateway": {
				Type:            "openai-compatible",
				BaseURL:         "https://gateway.example/v1",
				CatalogProvider: "moonshotai",
				Auth:            upstreamauth.Profile{Type: "none"},
				Models:          []string{"unknown", "manual-vision", "blocked", "catalog-vision"},
				ModelCapabilities: map[string]modelcapability.ModelCapabilities{
					"blocked":       {ImageInput: modelcapability.SupportUnsupported},
					"manual-vision": {ImageInput: modelcapability.SupportSupported},
				},
			},
			"defaulted": {
				Type:                "anthropic",
				BaseURL:             "https://defaulted.example/v1",
				CatalogProvider:     "moonshotai",
				Auth:                upstreamauth.Profile{Type: "none"},
				Models:              []string{"default-vision"},
				DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported},
			},
			"ambiguous-proxy": {
				Type:    "openai-compatible",
				BaseURL: "https://proxy.example/v1",
				Auth:    upstreamauth.Profile{Type: "none"},
				Models:  []string{"shared"},
			},
			"transportless": {
				Type:            "transportless",
				BaseURL:         "https://transportless.example/v1",
				CatalogProvider: "moonshotai",
				Auth:            upstreamauth.Profile{Type: "none"},
				Models:          []string{"catalog-vision"},
			},
		},
		Models: config.ModelsConfig{AllowRaw: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)
	s.providerAdapters["transportless"] = noVisionProviderAdapter{ProviderAdapter: s.providerAdapters["openai-compatible"]}
	if err := s.catalog.Import(strings.NewReader(`{
		"moonshotai": {
			"id": "moonshotai",
			"models": {
				"catalog-vision": {"id":"catalog-vision","modalities":{"input":["text","image"],"output":["text"]}},
				"blocked": {"id":"blocked","modalities":{"input":["text","image"],"output":["text"]}},
				"manual-vision": {"id":"manual-vision","modalities":{"input":["text"],"output":["text"]}},
				"default-vision": {"id":"default-vision","modalities":{"input":["text"],"output":["text"]}},
				"shared": {"id":"shared","modalities":{"input":["text","image"],"output":["text"]}}
			}
		},
		"other": {
			"id": "other",
			"models": {
				"shared": {"id":"shared","modalities":{"input":["text"],"output":["text"]}}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	req := adminJSONRequest(http.MethodGet, "/admin/multimodal", "")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/multimodal = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		VisionFallbackModels []visionFallbackModelResponse `json:"vision_fallback_models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []visionFallbackModelResponse{
		{Target: "defaulted/default-vision", ProviderID: "defaulted", Model: "default-vision", Source: modelcapability.SourceProviderDefault},
		{Target: "gateway/catalog-vision", ProviderID: "gateway", Model: "catalog-vision", Source: modelcapability.SourceModelsDev},
		{Target: "gateway/manual-vision", ProviderID: "gateway", Model: "manual-vision", Source: modelcapability.SourceModelOverride},
	}
	if !reflect.DeepEqual(response.VisionFallbackModels, want) {
		t.Fatalf("candidates = %#v, want %#v", response.VisionFallbackModels, want)
	}
}

func TestRuntimeAdminMultimodalValidatesVisionFallbackBeforeWriting(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeCatalogVisionAdminConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	if err := s.catalog.Import(strings.NewReader(catalogVisionFixture)); err != nil {
		t.Fatal(err)
	}

	valid := adminJSONRequest(http.MethodPut, "/admin/multimodal", `{"enabled":true,"provider":"builtin","vision_fallback_model":"gateway/catalog-vision"}`)
	validW := httptest.NewRecorder()
	s.Routes().ServeHTTP(validW, valid)
	if validW.Code != http.StatusOK {
		t.Fatalf("valid save = %d: %s", validW.Code, validW.Body.String())
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := adminJSONRequest(http.MethodPut, "/admin/multimodal", `{"enabled":true,"provider":"builtin","vision_fallback_model":"gateway/unknown"}`)
	invalidW := httptest.NewRecorder()
	s.Routes().ServeHTTP(invalidW, invalid)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if invalidW.Code != http.StatusBadRequest || !strings.Contains(invalidW.Body.String(), "vision_fallback_invalid") {
		t.Fatalf("invalid save = %d: %s", invalidW.Code, invalidW.Body.String())
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid save overwrote config")
	}
}

func TestRuntimeAdminMultimodalRejectsVisionFallbackWithoutImageTransportBeforeWriting(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeCatalogVisionAdminConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	if err := s.catalog.Import(strings.NewReader(catalogVisionFixture)); err != nil {
		t.Fatal(err)
	}
	s.providerAdapters["openai-compatible"] = noVisionProviderAdapter{ProviderAdapter: s.providerAdapters["openai-compatible"]}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	request := adminJSONRequest(http.MethodPut, "/admin/multimodal", `{"enabled":true,"provider":"builtin","vision_fallback_model":"gateway/catalog-vision"}`)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, request)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "vision_fallback_invalid") {
		t.Fatalf("save without image transport = %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(before, after) {
		t.Fatal("transport-invalid save overwrote config")
	}
}
