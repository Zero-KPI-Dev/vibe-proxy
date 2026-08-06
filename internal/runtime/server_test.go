package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
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
	"github.com/a448582655/vibe-proxy/internal/store"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

var testPromOnce sync.Once
var testProm *metrics.Prometheus

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observabilityRecordingSink struct {
	finished     []telemetry.Event
	payloads     []telemetry.PayloadSnapshot
	observations []telemetry.TraceObservation
	failWrites   bool
}

func (s *observabilityRecordingSink) RequestStarted(telemetry.Event) {}
func (s *observabilityRecordingSink) Token(telemetry.Event)          {}
func (s *observabilityRecordingSink) RequestFinished(event telemetry.Event) {
	s.finished = append(s.finished, event)
}
func (s *observabilityRecordingSink) RecordPayload(snapshot telemetry.PayloadSnapshot) error {
	if s.failWrites {
		return errors.New("payload store unavailable")
	}
	s.payloads = append(s.payloads, snapshot)
	return nil
}
func (s *observabilityRecordingSink) RecordObservation(observation telemetry.TraceObservation) error {
	if s.failWrites {
		return errors.New("observation store unavailable")
	}
	s.observations = append(s.observations, observation)
	return nil
}

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

func TestRuntimeHealthzReportsStableServerStartTime(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{}`), nil
	})
	startedAt := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	s.startedAt = startedAt

	readHealth := func() struct {
		OK        bool      `json:"ok"`
		StartedAt time.Time `json:"started_at"`
		LoadedAt  time.Time `json:"loaded_at"`
	} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("unexpected health response: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			OK        bool      `json:"ok"`
			StartedAt time.Time `json:"started_at"`
			LoadedAt  time.Time `json:"loaded_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	first := readHealth()
	if !first.OK || !first.StartedAt.Equal(startedAt) || first.LoadedAt.IsZero() {
		t.Fatalf("unexpected health payload: %+v", first)
	}
	s.snapshot.Store(s.buildSnapshot(s.current().Config))
	second := readHealth()
	if !second.StartedAt.Equal(startedAt) || second.LoadedAt.Before(first.LoadedAt) {
		t.Fatalf("server start time changed with the config snapshot: first=%+v second=%+v", first, second)
	}
}

func TestDataAndControlRoutesAreIsolated(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{}`), nil
	})

	data := s.DataRoutes()
	for _, target := range []string{"/admin/config/snapshot", "/auth/status", "/metrics", "/desktop/bootstrap/nonce", "/"} {
		response := httptest.NewRecorder()
		data.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("data route %s = %d, want 404", target, response.Code)
		}
	}
	health := httptest.NewRecorder()
	data.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("data health = %d: %s", health.Code, health.Body.String())
	}
	models := httptest.NewRecorder()
	data.ServeHTTP(models, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if models.Code == http.StatusNotFound {
		t.Fatal("data listener did not mount /v1/models")
	}

	control := s.ControlRoutes()
	for _, target := range []string{"/v1", "/v1/models", "/v1/chat/completions", "/v1/responses", "/v1/messages", "/anthropic", "/anthropic/v1/messages"} {
		response := httptest.NewRecorder()
		control.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("control route %s = %d, want 404", target, response.Code)
		}
	}
	for _, target := range []string{"/", "/auth/status", "/metrics", "/healthz"} {
		response := httptest.NewRecorder()
		control.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code == http.StatusNotFound {
			t.Fatalf("control listener did not mount %s", target)
		}
	}
}

func TestAdminSnapshotIncludesConfiguredListenerAddresses(t *testing.T) {
	cfg, err := config.CompileSimple(config.SimpleConfig{Server: config.ServerConfig{
		Listen:      "0.0.0.0:9080",
		AdminListen: "127.0.0.1:9081",
	}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := NewWithOptions("", cfg, metrics.MultiSink{}, testProm, Options{AdminTokenOverride: "admin-token"})
	request := adminJSONRequest(http.MethodGet, "/admin/config/snapshot", "")
	response := httptest.NewRecorder()
	s.ControlRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Server struct {
			Listen      string `json:"listen"`
			AdminListen string `json:"admin_listen"`
		} `json:"server"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Server.Listen != "0.0.0.0:9080" || payload.Server.AdminListen != "127.0.0.1:9081" {
		t.Fatalf("listener snapshot = %+v", payload.Server)
	}
}

func TestRuntimeRecordsPayloadStagesAndIgnoresRecorderFailure(t *testing.T) {
	tests := []struct {
		name       string
		failWrites bool
	}{
		{name: "records stages"},
		{name: "telemetry failure does not change response", failWrites: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := &observabilityRecordingSink{failWrites: test.failWrites}
			s := newObservabilityTestServer(t, sink, func(r *http.Request) (*http.Response, error) {
				if got := r.Header.Get("Authorization"); got != "" {
					t.Fatalf("unexpected upstream authorization in auth:none test: %q", got)
				}
				return jsonResponse(http.StatusOK, `{"id":"chatcmpl-1","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hello"}],"api_key":"body-secret"}`))
			request.Header.Set("Authorization", "Bearer vibe-local-dev-key")
			request.Header.Set("User-Agent", "codex/1")
			response := httptest.NewRecorder()
			s.Routes().ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "world") {
				t.Fatalf("response changed by telemetry: %d %s", response.Code, response.Body.String())
			}
			if test.failWrites {
				return
			}
			stages := map[telemetry.PayloadStage]telemetry.PayloadSnapshot{}
			for _, payload := range sink.payloads {
				stages[payload.Stage] = payload
				if strings.Contains(string(payload.Body), "body-secret") || payload.Headers.Get("Authorization") != "" {
					t.Fatalf("payload leaked a secret: %+v", payload)
				}
			}
			for _, stage := range []telemetry.PayloadStage{
				telemetry.PayloadStageClientRequest,
				telemetry.PayloadStageCanonicalRequest,
				telemetry.PayloadStageUpstreamRequest,
				telemetry.PayloadStageCanonicalResponse,
			} {
				if _, ok := stages[stage]; !ok {
					t.Fatalf("missing payload stage %q: %+v", stage, sink.payloads)
				}
			}
			if len(sink.observations) == 0 || len(sink.finished) != 1 || sink.finished[0].CaptureStatus == telemetry.CaptureStatusNotCaptured {
				t.Fatalf("missing observation/capture summary: observations=%+v finished=%+v", sink.observations, sink.finished)
			}
		})
	}
}

func TestRuntimeMarksDisabledResponseCaptureAsNotCaptured(t *testing.T) {
	sink := &observabilityRecordingSink{}
	s := newObservabilityTestServer(t, sink, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"id":"chatcmpl-1","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"world"},"finish_reason":"stop"}]}`), nil
	})
	cfg := *s.current().Config
	cfg.Observability.Capture.CaptureResponse = false
	s.applyRuntimeConfig(&cfg)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
	for _, payload := range sink.payloads {
		if payload.Stage == telemetry.PayloadStageCanonicalResponse {
			if payload.CaptureStatus != telemetry.CaptureStatusNotCaptured || len(payload.Body) != 0 || payload.MediaType != "" {
				t.Fatalf("disabled response capture was not explicit: %+v", payload)
			}
			return
		}
	}
	t.Fatalf("missing not-captured response marker: %+v", sink.payloads)
}

func TestRuntimeStreamingCaptureStoresCanonicalResponseNotSSE(t *testing.T) {
	sink := &observabilityRecordingSink{}
	s := newObservabilityTestServer(t, sink, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
					"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
					"data: [DONE]\n\n",
			)),
		}, nil
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stream response: %d %s", response.Code, response.Body.String())
	}
	for _, payload := range sink.payloads {
		if payload.Stage != telemetry.PayloadStageCanonicalResponse {
			continue
		}
		body := string(payload.Body)
		if !strings.Contains(body, "hello") || strings.Contains(body, "data:") || strings.Contains(body, "chat.completion.chunk") {
			t.Fatalf("stream capture is not canonical: %s", body)
		}
		return
	}
	t.Fatalf("missing canonical stream response: %+v", sink.payloads)
}

func TestObservabilityAdminHandlersQueryDetailDiffSessionsAndDeleteContent(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "admin-observability.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC().Truncate(time.Second)
	completed := now.Add(time.Second)
	for _, event := range []telemetry.Event{
		{RequestID: "request-1", SessionID: "session-1", SessionName: "Trace work", AgentID: "codex", PrincipalName: "alice", StartedAt: now, CompletedAt: &completed, StatusCode: 200, CaptureMode: "structured", CaptureStatus: telemetry.CaptureStatusCaptured},
		{RequestID: "request-2", SessionID: "session-1", ParentRequestID: "request-1", AgentID: "codex", PrincipalName: "alice", StartedAt: now.Add(time.Second), CompletedAt: &completed, StatusCode: 200, CaptureMode: "structured", CaptureStatus: telemetry.CaptureStatusCaptured},
	} {
		database.RequestFinished(event)
	}
	for _, payload := range []telemetry.PayloadSnapshot{
		{RequestID: "request-1", Stage: telemetry.PayloadStageCanonicalRequest, CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured, Body: []byte(`{"requested_model":"vibe","messages":[]}`), CreatedAt: now},
		{RequestID: "request-2", Stage: telemetry.PayloadStageCanonicalRequest, CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured, Body: []byte(`{"requested_model":"vibe","messages":[{"role":"user","content":[]}]}`), CreatedAt: now},
	} {
		if err := database.RecordPayload(payload); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.CompileSimple(config.SimpleConfig{})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := NewWithOptions("", cfg, metrics.MultiSink{database}, testProm, Options{AdminTokenOverride: "admin-token"})

	for _, target := range []string{
		"/admin/observability/requests?limit=1&agent_id=codex",
		"/admin/observability/requests/request-2",
		"/admin/observability/requests/request-2/diff",
		"/admin/observability/sessions?agent_id=codex",
		"/admin/observability/sessions/session-1",
	} {
		response := httptest.NewRecorder()
		s.Routes().ServeHTTP(response, adminJSONRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("GET %s = %d headers=%v body=%s", target, response.Code, response.Header(), response.Body.String())
		}
	}

	deleteResponse := httptest.NewRecorder()
	s.Routes().ServeHTTP(deleteResponse, adminJSONRequest(http.MethodDelete, "/admin/observability/requests/request-2/content", ""))
	if deleteResponse.Code != http.StatusOK || deleteResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("delete content = %d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	detailResponse := httptest.NewRecorder()
	s.Routes().ServeHTTP(detailResponse, adminJSONRequest(http.MethodGet, "/admin/observability/requests/request-2", ""))
	if !strings.Contains(detailResponse.Body.String(), `"state":"expired"`) {
		t.Fatalf("detail did not report expired capture: %s", detailResponse.Body.String())
	}
	other, err := database.PayloadSnapshots("request-1")
	if err != nil || len(other) != 1 {
		t.Fatalf("exact deletion removed another request: %+v err=%v", other, err)
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

func TestRuntimeTracksIdentityAndRequestShape(t *testing.T) {
	recent := telemetry.NewRecentStore(10)
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"id":"chatcmpl_identity","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
	})
	s.sink = recent

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("X-Vibe-Agent-ID", "codex")
	req.Header.Set("X-Vibe-Agent-Name", "Codex CLI")
	req.Header.Set("X-Vibe-Session-ID", "session-42")
	req.Header.Set("X-Vibe-Project-ID", "vibe-proxy")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	events := recent.Recent(10)
	if len(events) != 1 {
		t.Fatalf("request was not tracked: %+v", events)
	}
	event := events[0]
	if event.RequestID == "" || event.RequestID != w.Header().Get("X-Vibe-Proxy-Request-ID") || event.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("request and trace identity missing: %+v", event)
	}
	if event.PrincipalName != "test" || event.ClientName != "test" || event.AgentID != "codex" || event.SessionID != "session-42" || event.ProjectID != "vibe-proxy" {
		t.Fatalf("principal or caller identity missing: %+v", event)
	}
	if event.InitialProvider != "mockai" || event.InitialModel != "raw-chat" || event.ChannelID != "mockai" || event.UpstreamModel != "raw-chat" {
		t.Fatalf("route summary missing: %+v", event)
	}
	if event.RequestShape.InputMessageCount != 1 || event.RequestShape.InputBlockCount != 2 || event.RequestShape.InputImageCount != 1 || event.RequestShape.InputToolCount != 1 || event.RequestShape.InputTextChars != 4 {
		t.Fatalf("input shape missing: %+v", event.RequestShape)
	}
	if event.RequestShape.OutputMessageCount != 1 || event.RequestShape.OutputTextChars != 2 || event.FinishReason != "stop" || event.UpstreamRequestID != "chatcmpl_identity" {
		t.Fatalf("output summary missing: %+v", event)
	}
}

func TestRuntimeTracksEarlyFailuresWithoutRequestContent(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		body          string
		wantStatus    int
		wantCode      string
	}{
		{name: "authentication", authorization: "Bearer wrong", body: `{"secret":"must-not-appear"}`, wantStatus: http.StatusUnauthorized, wantCode: "invalid_api_key"},
		{name: "parse", authorization: "Bearer vibe-local-dev-key", body: `{"secret":"must-not-appear"`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "resolution", authorization: "Bearer vibe-local-dev-key", body: `{"model":"missing-model","messages":[]}`, wantStatus: http.StatusNotFound, wantCode: "model_not_found:missing-model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recent := telemetry.NewRecentStore(10)
			s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
				t.Fatal("early failure reached upstream")
				return nil, nil
			})
			s.sink = recent
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(test.body))
			req.Header.Set("Authorization", test.authorization)
			w := httptest.NewRecorder()
			s.Routes().ServeHTTP(w, req)
			if w.Code != test.wantStatus {
				t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
			}
			events := recent.Recent(10)
			if len(events) != 1 || events[0].CompletedAt == nil || events[0].StatusCode != test.wantStatus || events[0].ErrorCode != test.wantCode {
				t.Fatalf("early failure was not finished: %+v", events)
			}
			if events[0].RequestID == "" || events[0].RequestID != w.Header().Get("X-Vibe-Proxy-Request-ID") {
				t.Fatalf("early failure has no stable request id: %+v headers=%v", events[0], w.Header())
			}
			raw, _ := json.Marshal(events[0])
			if strings.Contains(string(raw), "must-not-appear") || strings.Contains(string(raw), test.authorization) {
				t.Fatalf("request content or credential leaked into telemetry: %s", raw)
			}
		})
	}
}

func TestRuntimeTracksEarlyFailureDuringModelAuthorization(t *testing.T) {
	recent := telemetry.NewRecentStore(10)
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("authorization failure reached upstream")
		return nil, nil
	})
	s.sink = recent
	s.current().Config.ClientKeys[0].AllowedModels = []string{"vibe-coder"}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
	}
	events := recent.Recent(10)
	if len(events) != 1 || events[0].StatusCode != http.StatusForbidden || events[0].ErrorCode != "model_not_allowed" || events[0].PrincipalName != "test" {
		t.Fatalf("authorization failure was not tracked: %+v", events)
	}
}

func TestRuntimeDoesNotMergeUpstreamReasoningIntoVisibleContent(t *testing.T) {
	s := newTestServer(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"id":"chatcmpl_reasoning","model":"raw-chat","choices":[{"message":{"role":"assistant","reasoning_content":"private chain of thought","content":"final answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`), nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	message := payload.Choices[0].Message
	if message.Content != "final answer" || message.ReasoningContent != "private chain of thought" {
		t.Fatalf("reasoning leaked into normal content: %s", w.Body.String())
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
	if len(events) != len(requests)+1 {
		t.Fatalf("admin playground requests were not all tracked: %+v", events)
	}
	adminEvents := 0
	sawRejectedPublicRequest := false
	for _, event := range events {
		if event.ErrorCode == "invalid_api_key" {
			sawRejectedPublicRequest = true
			continue
		}
		if event.ClientName != "admin-playground" || !requestIDs[event.RequestID] {
			t.Fatalf("admin playground request was not tracked correctly: %+v", events)
		}
		adminEvents++
	}
	if adminEvents != len(requests) || !sawRejectedPublicRequest || len(requestIDs) != len(requests) {
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
		Multimodal: config.MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision", VisionFallbackStrategy: "takeover", OCR: config.OCRConfig{Provider: "http", Endpoint: "http://ocr.local/v1/ocr", Auth: upstreamauth.Profile{Type: "none"}}},
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "vision ok") || w.Header().Get("X-Vibe-Proxy-Image-Fallback") != "vision" || w.Header().Get("X-Vibe-Proxy-Vision-Strategy") != "takeover" {
		t.Fatalf("unexpected Vision fallback response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	events := recent.Recent(10)
	if len(events) != 1 || events[0].Transformation == nil || events[0].Transformation.MultimodalRoute != "vision_fallback" || events[0].Transformation.RouteReason != "ocr_no_usable_text" || events[0].Transformation.ModelImageSupport != "unsupported" || events[0].Transformation.OriginalProvider != "text" || events[0].Transformation.EffectiveProvider != "vision" || events[0].Transformation.OCRFailureCode != "ocr_no_usable_text" {
		t.Fatalf("unexpected Vision telemetry: %+v", events)
	}
}

func TestRuntimeVisionAssistExtractsEvidenceThenUsesOriginalModel(t *testing.T) {
	cfg, err := config.CompileSimple(config.SimpleConfig{
		Observability: config.ObservabilityConfig{Capture: config.ObservabilityCaptureConfig{Mode: "structured", MaxSnapshotBytes: 512 << 10, CaptureResponse: true}},
		ClientKeys:    []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}},
		Multimodal:    config.MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision", OCR: config.OCRConfig{Provider: "http", Endpoint: "http://ocr.local/v1/ocr", Auth: upstreamauth.Profile{Type: "none"}}},
		Providers: map[string]config.ProviderConfig{
			"text":   {Type: "openai-compatible", BaseURL: "https://text.example/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"text-model"}, DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported}},
			"vision": {Type: "openai-compatible", BaseURL: "https://vision.example/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"vision-model"}, DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportSupported}},
		},
		Models: config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "text/text-model", "vibe-vision": "vision/vision-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &observabilityRecordingSink{}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, sink, testProm)
	s.SetOCRHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"results":[{"index":0,"text":"uncertain","confidence":0.1}]}`), nil
	})})
	var calls atomic.Int32
	s.SetHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		switch calls.Add(1) {
		case 1:
			if r.URL.String() != "https://vision.example/v1/chat/completions" || !strings.Contains(string(body), `"model":"vision-model"`) || !strings.Contains(string(body), `"image_url"`) {
				t.Fatalf("unexpected Vision assist request: %s %s", r.URL, body)
			}
			if strings.Contains(string(body), "historic context must stay with primary") {
				t.Fatalf("Vision helper received the full conversation: %s", body)
			}
			return jsonResponse(200, `{"id":"vision-evidence","model":"vision-model","choices":[{"message":{"role":"assistant","content":"A ginger cat is sitting on a blue chair."},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`), nil
		case 2:
			if r.URL.String() != "https://text.example/v1/chat/completions" || !strings.Contains(string(body), `"model":"text-model"`) {
				t.Fatalf("original model was not used after Vision assist: %s %s", r.URL, body)
			}
			if strings.Contains(string(body), `"image_url"`) || !strings.Contains(string(body), "vibe-proxy-vision") || !strings.Contains(string(body), "A ginger cat is sitting on a blue chair") || !strings.Contains(string(body), "historic context must stay with primary") {
				t.Fatalf("primary request did not contain preserved context plus Vision evidence: %s", body)
			}
			return jsonResponse(200, `{"id":"primary-answer","model":"text-model","choices":[{"message":{"role":"assistant","content":"primary model answered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":4,"total_tokens":104}}`), nil
		default:
			t.Fatalf("unexpected extra provider call: %s", r.URL)
			return nil, nil
		}
	})})
	encoded := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"historic context must stay with primary"},{"role":"user","content":[{"type":"text","text":"What is in this image?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,`+encoded+`"}}]}]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "primary model answered") || calls.Load() != 2 || w.Header().Get("X-Vibe-Proxy-Vision-Strategy") != "assist" {
		t.Fatalf("unexpected Vision assist response: calls=%d status=%d body=%s", calls.Load(), w.Code, w.Body.String())
	}
	if len(sink.finished) != 1 || sink.finished[0].Transformation == nil || sink.finished[0].Transformation.EffectiveProvider != "text" {
		t.Fatalf("Vision assist must report the original model as final target: %+v", sink.finished)
	}
	stages := map[telemetry.PayloadStage]bool{}
	for _, payload := range sink.payloads {
		stages[payload.Stage] = true
	}
	for _, stage := range []telemetry.PayloadStage{telemetry.PayloadStageOCRRequest, telemetry.PayloadStageOCRResponse, telemetry.PayloadStageVisionRequest, telemetry.PayloadStageVisionResponse, telemetry.PayloadStageEffectiveCanonicalRequest, telemetry.PayloadStageUpstreamRequest} {
		if !stages[stage] {
			t.Fatalf("missing capture stage %s: %+v", stage, stages)
		}
	}
	if len(sink.observations) < 3 {
		t.Fatalf("expected OCR, Vision, and primary observations: %+v", sink.observations)
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
	if !strings.Contains(testW.Body.String(), `"enabled":true`) || !strings.Contains(testW.Body.String(), `"active":false`) || !strings.Contains(testW.Body.String(), `"warning":"multimodal_not_active"`) {
		t.Fatalf("OCR test should distinguish a successful engine test from inactive runtime configuration: %s", testW.Body.String())
	}

	disabledBody := `{"enabled":false,"provider":"http","endpoint":"http://new-ocr.local/v1/ocr","auth_type":"api_key_header","api_key_source":"literal","api_key":"unsaved-secret","header":"x-ocr-key","min_confidence":0.55,"min_text_chars":4,"max_images":4}`
	disabledReq := httptest.NewRequest(http.MethodPost, "/admin/multimodal/ocr/test", strings.NewReader(disabledBody))
	disabledReq.Header.Set("Authorization", "Bearer admin-token")
	disabledW := httptest.NewRecorder()
	s.Routes().ServeHTTP(disabledW, disabledReq)
	if disabledW.Code != http.StatusOK || !strings.Contains(disabledW.Body.String(), `"enabled":false`) || !strings.Contains(disabledW.Body.String(), `"active":false`) || !strings.Contains(disabledW.Body.String(), `"warning":"multimodal_disabled"`) {
		t.Fatalf("disabled OCR test should return an activation warning: %d %s", disabledW.Code, disabledW.Body.String())
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

func TestRuntimeProviderTestReportsUnsupportedModelListing(t *testing.T) {
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
	if testW.Code != 200 || !strings.Contains(testW.Body.String(), `"ok":true`) || !strings.Contains(testW.Body.String(), `"reachable":true`) || !strings.Contains(testW.Body.String(), `"model_listing":"unsupported"`) || !strings.Contains(testW.Body.String(), `"status":404`) {
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

func TestRuntimeAdminModelCatalogOfflineImport(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	cfg, err := config.CompileSimple(config.SimpleConfig{Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"}, Providers: map[string]config.ProviderConfig{}, Models: config.ModelsConfig{AllowRaw: true}})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, metrics.MultiSink{}, testProm)

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("catalog", "api.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, `{"offline":{"id":"offline","models":{"offline-vision":{"id":"offline-vision","modalities":{"input":["text","image"],"output":["text"]}}}}}`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/model-catalog/import", &upload)
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"models":1`) || !strings.Contains(w.Body.String(), `"origin":"upload"`) {
		t.Fatalf("unexpected catalog import response: %d %s", w.Code, w.Body.String())
	}
	if match := s.catalog.Lookup("offline", "", "", "offline-vision"); match.ImageInput != modelcapability.SupportSupported {
		t.Fatalf("imported model capability unavailable: %#v", match)
	}

	invalid := adminJSONRequest(http.MethodPost, "/admin/model-catalog/import", `{"invalid":`)
	invalidW := httptest.NewRecorder()
	s.Routes().ServeHTTP(invalidW, invalid)
	if invalidW.Code != http.StatusBadRequest || !strings.Contains(invalidW.Body.String(), `"catalog_import_failed"`) {
		t.Fatalf("unexpected invalid catalog import response: %d %s", invalidW.Code, invalidW.Body.String())
	}
	if state := s.catalog.State(); state.Models != 1 || state.Origin != "upload" {
		t.Fatalf("invalid import replaced usable catalog: %#v", state)
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

func TestRuntimeAdminDynamicErrorsAreValidJSON(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)

	create := func() *httptest.ResponseRecorder {
		req := adminJSONRequest(http.MethodPost, "/admin/client-keys", `{"name":"agent","rpm":10}`)
		response := httptest.NewRecorder()
		s.Routes().ServeHTTP(response, req)
		return response
	}
	if first := create(); first.Code != http.StatusOK {
		t.Fatalf("initial client key create failed: %d %s", first.Code, first.Body.String())
	}
	second := create()
	if second.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate key error, got %d %s", second.Code, second.Body.String())
	}
	if contentType := second.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("unexpected content type %q", contentType)
	}
	if !json.Valid(second.Body.Bytes()) {
		t.Fatalf("dynamic error is not valid JSON: %s", second.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(second.Body.Bytes(), &payload); err != nil || !strings.Contains(payload["error"], `"agent"`) {
		t.Fatalf("unexpected error payload: %v, %s", err, second.Body.String())
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

func TestRuntimeReloadRejectsInvalidRuntimeAndKeepsCurrentSnapshot(t *testing.T) {
	t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
	path := writeAdminTestConfig(t)
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New(path, cfg, metrics.MultiSink{}, testProm)
	if err := os.WriteFile(path, []byte("version: vibeproxy.io/v1alpha1\nsecurity:\n  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN\nproviders:\n  bad:\n    type: openai-compatible\n    base_url: /relative\n    auth: {type: none}\nmodels:\n  allow_raw: true\n  aliases: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	req := adminJSONRequest(http.MethodPost, "/admin/config/reload", "")
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || !json.Valid(response.Body.Bytes()) {
		t.Fatalf("unexpected invalid reload response: %d %s", response.Code, response.Body.String())
	}
	if _, exists := s.current().Config.Providers["mockai"]; !exists {
		t.Fatalf("invalid reload replaced the current snapshot: %+v", s.current().Config.Providers)
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

func newObservabilityTestServer(t *testing.T, sink telemetry.EventSink, rt roundTrip) *Server {
	t.Helper()
	cfg, err := config.CompileSimple(config.SimpleConfig{
		Observability: config.ObservabilityConfig{
			Capture: config.ObservabilityCaptureConfig{
				Mode:             "structured",
				MaxSnapshotBytes: 256 << 10,
				CaptureResponse:  true,
				HeaderAllowlist:  []string{"user-agent", "authorization"},
			},
		},
		ClientKeys: []config.ClientKeyConfig{{Name: "test", KeyHash: "$2a$10$AXRkz.6y44ygdJFk6L1/IO0aRVp9zRMfXDJoBCBsroxVac/Lovvz6", Enabled: true, AllowedModels: []string{"*"}, RPM: 1000}},
		Providers:  map[string]config.ProviderConfig{"mockai": {Type: "openai-compatible", BaseURL: "https://mock.openai/v1", Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"raw-chat"}}},
		Models:     config.ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe-fast": "mockai/raw-chat"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	testPromOnce.Do(func() { testProm = metrics.New() })
	s := New("", cfg, sink, testProm)
	s.SetHTTPClient(&http.Client{Transport: rt})
	return s
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
