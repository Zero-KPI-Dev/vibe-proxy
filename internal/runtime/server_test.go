package runtime

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

var testPromOnce sync.Once
var testProm *metrics.Prometheus

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
