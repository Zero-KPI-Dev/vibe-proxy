package httpstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

type pipeWriter struct {
	net.Conn
	header  http.Header
	started chan struct{}
}

func (w *pipeWriter) Header() http.Header { return w.header }
func (w *pipeWriter) WriteHeader(int)     {}
func (w *pipeWriter) Write(p []byte) (int, error) {
	if w.started != nil {
		close(w.started)
		w.started = nil
	}
	return w.Conn.Write(p)
}
func (*pipeWriter) FlushError() error { return nil }

func TestWriterInterruptsBlockedSocket(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle_timeout", true: "cancelled"}[cancelRequest], func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			base := &pipeWriter{Conn: server, header: make(http.Header), started: started}
			timeout := 30 * time.Millisecond
			if cancelRequest {
				timeout = time.Minute
			}
			writer := NewWriter(ctx, base, timeout)
			done := make(chan error, 1)
			go func() {
				_, err := writer.Write([]byte("blocked until deadline"))
				writer.Close()
				done <- err
			}()
			<-started
			if cancelRequest {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("blocked write was reported as successful")
				}
			case <-time.After(time.Second):
				t.Fatal("blocked write did not stop")
			}
			// A completed cancellation callback must not poison keep-alive reuse.
			go io.Copy(io.Discard, client)
			if _, err := server.Write([]byte("next request")); err != nil {
				t.Fatalf("stale deadline: %v", err)
			}
		})
	}
}

func TestWriterPreservesCancellationCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	failure := errors.New("first token timeout")
	cancel(failure)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	w := NewWriter(ctx, &pipeWriter{Conn: server, header: make(http.Header)}, time.Second)
	defer w.Close()
	if _, err := w.Write([]byte("not sent")); !errors.Is(err, failure) {
		t.Fatalf("lost cause: %v", err)
	}
}
