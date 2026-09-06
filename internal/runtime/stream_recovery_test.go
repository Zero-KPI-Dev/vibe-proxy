package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/protocol"
	provideranthropic "github.com/a448582655/vibe-proxy/internal/provideradapters/anthropic"
	provideropenai "github.com/a448582655/vibe-proxy/internal/provideradapters/openai"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

const healthyStream = "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\ndata: [DONE]\n\n"

func configureRecoveryServer(t *testing.T, upstream *httptest.Server, total, first, idle time.Duration) (*Server, *telemetry.RecentStore) {
	t.Helper()
	s := newTestServer(t, nil)
	cfg := s.current().Config
	p := cfg.Providers["mockai"]
	p.BaseURL, p.MaxConcurrency = upstream.URL, 1
	p.Timeout, p.FirstTokenTimeout, p.StreamIdleTimeout = config.Duration{Duration: total}, config.Duration{Duration: first}, config.Duration{Duration: idle}
	cfg.Providers["mockai"] = p
	for i := range cfg.ModelResolver.Providers {
		if cfg.ModelResolver.Providers[i].ID == "mockai" {
			cfg.ModelResolver.Providers[i].BaseURL = upstream.URL
		}
	}
	s.applyRuntimeConfig(cfg)
	s.SetHTTPClient(upstream.Client())
	recent := telemetry.NewRecentStore(100)
	s.sink, s.recent = recent, recent
	return s, recent
}

func TestRuntimeStalledUpstreamRecoversForAllProtocols(t *testing.T) {
	requests := []struct{ path, body string }{
		{"/v1/chat/completions", `{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"vibe-fast","stream":true,"input":"hi"}`},
		{"/anthropic/v1/messages", `{"model":"vibe-fast","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`},
	}
	for _, request := range requests {
		for _, mode := range []string{"no_headers", "keepalives_only", "idle_after_output", "upstream_error"} {
			t.Run(request.path+"/"+mode, func(t *testing.T) {
				var healthy atomic.Bool
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					io.Copy(io.Discard, r.Body)
					if mode == "no_headers" && !healthy.Load() {
						<-r.Context().Done()
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if healthy.Load() {
						io.WriteString(w, healthyStream)
						return
					}
					if mode == "idle_after_output" {
						io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
					}
					if mode == "upstream_error" {
						io.WriteString(w, "data: {\"error\":{\"message\":\"private upstream detail\"}}\n\n")
					}
					w.(http.Flusher).Flush()
					ticker := time.NewTicker(5 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-r.Context().Done():
							return
						case <-ticker.C:
							if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
								return
							}
							w.(http.Flusher).Flush()
						}
					}
				}))
				defer upstream.Close()
				s, recent := configureRecoveryServer(t, upstream, time.Second, 60*time.Millisecond, 60*time.Millisecond)
				gateway := httptest.NewServer(s.DataRoutes())
				defer gateway.Close()
				client := &http.Client{Timeout: 2 * time.Second}
				defer client.CloseIdleConnections()
				send := func() string {
					req, _ := http.NewRequest(http.MethodPost, gateway.URL+request.path, strings.NewReader(request.body))
					req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					return string(body)
				}
				for iteration := 0; iteration < 3; iteration++ {
					body := send()
					want := "upstream_first_token_timeout"
					if mode == "idle_after_output" {
						want = "upstream_stream_idle_timeout"
					}
					if mode == "upstream_error" {
						want = "upstream_stream_error"
					}
					if !strings.Contains(body, want) {
						t.Fatalf("missing failure %s: %s", want, body)
					}
					if strings.Contains(body, "[DONE]") || strings.Contains(body, "response.completed") || strings.Contains(body, "message_stop") || strings.Contains(body, "private upstream detail") {
						t.Fatalf("invalid failure stream: %s", body)
					}
					if active := recent.Active(); len(active) != 0 {
						t.Fatalf("request stuck active: %d", len(active))
					}
					finished := recent.Recent(1)
					if len(finished) != 1 || finished[0].ErrorCode != want || finished[0].StatusCode < 400 {
						t.Fatalf("failure not observable: %+v", finished)
					}
				}
				healthy.Store(true)
				if body := send(); !strings.Contains(body, "recovered") || strings.Contains(body, "provider_busy") {
					t.Fatalf("capacity did not recover: %s", body)
				}
			})
		}
	}
}

func TestRuntimeNonReadingClientReleasesProviderSlot(t *testing.T) {
	var healthy atomic.Bool
	upstreamStopped := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if healthy.Load() {
			io.WriteString(w, healthyStream)
			return
		}
		defer func() { upstreamStopped <- struct{}{} }()
		chunk := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + strings.Repeat("x", 32<<10) + "\"}}]}\n\n")
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	defer upstream.Close()
	s, recent := configureRecoveryServer(t, upstream, 250*time.Millisecond, time.Second, time.Second)
	gateway := httptest.NewServer(s.DataRoutes())
	defer gateway.Close()
	connection, err := net.Dial("tcp", gateway.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if tcp, ok := connection.(*net.TCPConn); ok {
		tcp.SetReadBuffer(1024)
	}
	body := `{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	fmt.Fprintf(connection, "POST /v1/chat/completions HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer vibe-local-dev-key\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	// Deliberately never read the response. Context cancellation must break a
	// blocked socket write even though the TCP connection itself stays open.
	deadline := time.Now().Add(3 * time.Second)
	for len(recent.Recent(1)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	finished := recent.Recent(1)
	if len(finished) != 1 || finished[0].StatusCode < 400 || len(recent.Active()) != 0 {
		t.Fatalf("blackhole held the request open: %+v", finished)
	}
	select {
	case <-upstreamStopped:
	case <-time.After(time.Second):
		t.Fatal("upstream producer still running")
	}
	healthy.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	w := httptest.NewRecorder()
	s.DataRoutes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "recovered") {
		t.Fatalf("blackhole leaked concurrency slot: %d %s", w.Code, w.Body.String())
	}
}

func TestProviderStreamCancellationAndMalformedFrames(t *testing.T) {
	for _, adapter := range []protocol.ProviderAdapter{provideropenai.Provider{}, provideranthropic.Provider{}} {
		t.Run(adapter.Name(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			reader, writer := io.Pipe()
			defer writer.Close()
			events, err := adapter.ParseStream(ctx, &http.Response{StatusCode: 200, Body: reader})
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case <-events:
			case <-time.After(time.Second):
				t.Fatal("cancelled parser remained blocked reading body")
			}
			for _, frame := range []string{"data: {bad json}\n\n", "data: " + strings.Repeat("x", (1<<20)+1), "event: error\ndata: {\"error\":{\"message\":\"failure\"}}\n\n"} {
				events, err := adapter.ParseStream(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(frame))})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case event := <-events:
					if event.Type != ir.EventError {
						t.Fatalf("invalid frame silently ignored: %+v", event)
					}
				case <-time.After(time.Second):
					t.Fatal("invalid frame hung")
				}
			}
		})
	}
}

func TestRuntimeServerWriteBudgetStillCapsStreamingRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	s, recent := configureRecoveryServer(t, upstream, 5*time.Second, time.Second, time.Second)
	s.current().Config.Server.WriteTimeout = config.Duration{Duration: 40 * time.Millisecond}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
	if _, err := s.authenticator.AuthenticateDataPlane(req, s.current().Config.ClientKeys); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	started := time.Now()
	s.DataRoutes().ServeHTTP(w, req)
	if time.Since(started) > time.Second || w.Code != 504 || !strings.Contains(w.Body.String(), "upstream_timeout") {
		t.Fatalf("server budget was extended: %d %s", w.Code, w.Body.String())
	}
	if len(recent.Active()) != 0 {
		t.Fatal("request remained active after server deadline")
	}
}

func TestRuntimeRepeatedConcurrentStallsRemainBounded(t *testing.T) {
	var active, peak atomic.Int32
	var healthy atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		count := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); count > old; old = peak.Load() {
			if peak.CompareAndSwap(old, count) {
				break
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if healthy.Load() {
			io.WriteString(w, healthyStream)
			return
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	s, recent := configureRecoveryServer(t, upstream, 2*time.Second, 100*time.Millisecond, 100*time.Millisecond)
	p := s.current().Config.Providers["mockai"]
	p.MaxConcurrency = 8
	s.current().Config.Providers["mockai"] = p
	s.sink = metrics.MultiSink{recent, telemetry.NewLiveBroker(telemetry.LiveBrokerOptions{})}
	gateway := httptest.NewServer(s.DataRoutes())
	defer gateway.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	send := func() (int, string, error) {
		req, _ := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"vibe-fast","stream":true,"messages":[{"role":"user","content":"burst"}]}`))
		req.Header.Set("Authorization", "Bearer vibe-local-dev-key")
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), err
	}
	// Warm verification before measuring admission under simultaneous load.
	healthy.Store(true)
	if status, _, err := send(); err != nil || status != 200 {
		t.Fatal(status, err)
	}
	healthy.Store(false)
	for round := 0; round < 5; round++ {
		start := make(chan struct{})
		var requests sync.WaitGroup
		for i := 0; i < 32; i++ {
			requests.Add(1)
			go func() {
				defer requests.Done()
				<-start
				status, body, err := send()
				if err != nil || (status != 429 && status != 504) {
					t.Errorf("stalled burst response: %d %s %v", status, body, err)
				}
			}()
		}
		close(start)
		requests.Wait()
		if len(recent.Active()) != 0 {
			t.Fatalf("round %d left active requests", round)
		}
	}
	// Socket shutdown on the upstream test server can lag our local release;
	// assert the proxy limiter directly as the authoritative capacity boundary.
	for i := 0; i < 8; i++ {
		if !s.acquire("mockai", 8) {
			t.Fatalf("concurrency leak after burst: %d/8 slots free", i)
		}
	}
	if s.acquire("mockai", 8) {
		t.Fatal("admission exceeded configured limit")
	}
	for i := 0; i < 8; i++ {
		s.release("mockai")
	}
	healthy.Store(true)
	if status, body, err := send(); err != nil || status != 200 || !strings.Contains(body, "recovered") {
		t.Fatalf("post-burst request: %d %s %v", status, body, err)
	}
	t.Logf("160 requests across 5 bursts completed; provider slots recovered (peak upstream handlers %d)", peak.Load())
}
