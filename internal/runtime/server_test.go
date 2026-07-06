package runtime

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

var testPromOnce sync.Once
var testProm *metrics.Prometheus

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
