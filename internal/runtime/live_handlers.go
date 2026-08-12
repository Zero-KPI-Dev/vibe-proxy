package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

const liveHeartbeatInterval = 15 * time.Second

func (s *Server) adminObservabilityLive(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.live == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "live observability is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeJSONError(w, http.StatusInternalServerError, "streaming is unsupported")
		return
	}
	cursor, err := liveCursor(r)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "invalid last event id")
		return
	}

	subscription := s.live.Subscribe(cursor)
	defer subscription.Cancel()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprint(w, "retry: 1500\n: connected\n\n")
	if err := writeLiveMeta(w, subscription.Meta); err != nil {
		return
	}
	flusher.Flush()
	for _, event := range subscription.Replay {
		if err := writeLiveEvent(w, event); err != nil {
			return
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(liveHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-subscription.Events:
			if !open {
				return
			}
			if err := writeLiveEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case now := <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat %d\n\n", now.Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func liveCursor(r *http.Request) (telemetry.LiveCursor, error) {
	value := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if value == "" {
		value = strings.TrimSpace(r.URL.Query().Get("after"))
	}
	if value == "" {
		return telemetry.LiveCursor{Epoch: strings.TrimSpace(r.Header.Get("X-Vibe-Stream-Epoch"))}, nil
	}
	afterID, err := strconv.ParseUint(value, 10, 64)
	return telemetry.LiveCursor{Epoch: strings.TrimSpace(r.Header.Get("X-Vibe-Stream-Epoch")), AfterID: afterID}, err
}

func writeLiveMeta(w http.ResponseWriter, meta telemetry.LiveStreamMeta) error {
	payload, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: stream.meta\ndata: %s\n\n", payload)
	return err
}

func writeLiveEvent(w http.ResponseWriter, event telemetry.LiveEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: request.%s\ndata: %s\n\n", event.ID, event.Kind, payload)
	return err
}
