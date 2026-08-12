package runtime

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

func TestAdminObservabilityLiveRequiresAdminAuthorization(t *testing.T) {
	broker := telemetry.NewLiveBroker(telemetry.LiveBrokerOptions{})
	s := newObservabilityTestServer(t, metrics.MultiSink{broker}, roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{}`), nil
	}))
	s.adminTokenOverride = "admin-token"
	s.snapshot.Store(s.buildSnapshot(s.current().Config))

	response := httptest.NewRecorder()
	s.ControlRoutes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/observability/live", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("live endpoint without admin authorization = %d, want 401", response.Code)
	}
}

func TestAdminObservabilityLiveStreamsReplayFrames(t *testing.T) {
	broker := telemetry.NewLiveBroker(telemetry.LiveBrokerOptions{})
	broker.RequestStarted(telemetry.Event{RequestID: "request-1"})
	broker.RequestUpdated(telemetry.Event{RequestID: "request-1", PrincipalName: "local-codex"}, telemetry.RequestPhaseAuthenticated)
	s := newObservabilityTestServer(t, metrics.MultiSink{broker}, roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{}`), nil
	}))
	s.adminTokenOverride = "admin-token"
	s.snapshot.Store(s.buildSnapshot(s.current().Config))

	server := httptest.NewServer(s.ControlRoutes())
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/admin/observability/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Last-Event-ID", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected SSE response: %d %v", response.StatusCode, response.Header)
	}

	scanner := bufio.NewScanner(response.Body)
	var eventName string
	var meta telemetry.LiveStreamMeta
	var payload telemetry.LiveEvent
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data := []byte(strings.TrimPrefix(line, "data: "))
			if eventName == "stream.meta" {
				if err := json.Unmarshal(data, &meta); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if meta.Epoch == "" || meta.LatestID != 2 || meta.ReplayGap {
		t.Fatalf("unexpected stream metadata: %+v", meta)
	}
	if eventName != "request.updated" || payload.ID != 2 || payload.Phase != telemetry.RequestPhaseAuthenticated || payload.Request.PrincipalName != "local-codex" {
		t.Fatalf("unexpected live frame: event=%q payload=%+v", eventName, payload)
	}
}

func TestRequestPipelinePublishesBoundedLifecycleCheckpoints(t *testing.T) {
	broker := telemetry.NewLiveBroker(telemetry.LiveBrokerOptions{})
	s := newObservabilityTestServer(t, metrics.MultiSink{broker}, roundTrip(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"id":"response-1","model":"raw-chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`), nil
	}))

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	response := httptest.NewRecorder()
	s.DataRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("proxy request = %d: %s", response.Code, response.Body.String())
	}

	subscription := broker.Subscribe(telemetry.LiveCursor{})
	defer subscription.Cancel()
	seen := map[telemetry.RequestPhase]bool{}
	for _, event := range subscription.Replay {
		seen[event.Phase] = true
	}
	for _, phase := range []telemetry.RequestPhase{
		telemetry.RequestPhaseReceived,
		telemetry.RequestPhaseAuthenticated,
		telemetry.RequestPhaseParsed,
		telemetry.RequestPhaseRouted,
		telemetry.RequestPhasePreprocessing,
		telemetry.RequestPhaseUpstreamStarted,
		telemetry.RequestPhaseCompleted,
	} {
		if !seen[phase] {
			t.Fatalf("missing lifecycle phase %q in %+v", phase, subscription.Replay)
		}
	}
	last := subscription.Replay[len(subscription.Replay)-1]
	if last.Kind != telemetry.LiveEventFinished || last.Request.StatusCode != http.StatusOK {
		t.Fatalf("unexpected final lifecycle event: %+v", last)
	}
}
