// Package httpstream bounds writes to slow clients and preserves I/O errors
// across protocol encoders, including failures reported only by Flush.
package httpstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const DefaultWriteTimeout = 30 * time.Second

// Writer is used by a single encoder goroutine. Only write deadlines are
// changed by the cancellation callback; that callback never writes a response.
type Writer struct {
	http.ResponseWriter
	ctx        context.Context
	controller *http.ResponseController
	timeout    time.Duration
	err        error
	deadlineMu sync.Mutex
	stop       func() bool
	cancelDone chan struct{}
}

func NewWriter(ctx context.Context, w http.ResponseWriter, timeout time.Duration) *Writer {
	if timeout <= 0 {
		timeout = DefaultWriteTimeout
	}
	writer := &Writer{ResponseWriter: w, ctx: ctx, controller: http.NewResponseController(w), timeout: timeout, cancelDone: make(chan struct{})}
	writer.stop = context.AfterFunc(ctx, func() {
		defer close(writer.cancelDone)
		writer.deadlineMu.Lock()
		defer writer.deadlineMu.Unlock()
		// Cancelling an upstream context alone cannot interrupt a blocked
		// downstream Write/Flush. Expire the actual socket write deadline.
		_ = writer.controller.SetWriteDeadline(time.Now())
	})
	return writer
}

func (w *Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *Writer) prepare() error {
	if err := w.Err(); err != nil {
		return err
	}
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	if err := context.Cause(w.ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(w.timeout)
	if limit, ok := w.ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := w.controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.err = err
	}
	return w.err
}

func (w *Writer) Write(p []byte) (int, error) {
	if err := w.prepare(); err != nil {
		return 0, err
	}
	n, err := w.ResponseWriter.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *Writer) FlushError() error {
	if err := w.prepare(); err != nil {
		return err
	}
	w.err = w.controller.Flush()
	return w.err
}

func (w *Writer) Flush() { _ = w.FlushError() }

func (w *Writer) Err() error {
	if w.err != nil {
		return w.err
	}
	return context.Cause(w.ctx)
}

// Close waits for a running deadline callback before the HTTP handler can
// return and the connection can be reused by another request.
func (w *Writer) Close() {
	if !w.stop() {
		<-w.cancelDone
	}
	_ = w.controller.SetWriteDeadline(time.Time{})
}
