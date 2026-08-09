package telemetry

import (
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/types"
)

func TestRecentStoreTracksActiveAndRecent(t *testing.T) {
	s := NewRecentStore(2)
	s.RequestStarted(Event{RequestID: "1", VirtualModel: "a"})
	s.RequestUpdated(Event{RequestID: "1", VirtualModel: "a", PrincipalName: "local-codex"}, RequestPhaseAuthenticated)
	if active := s.Active(); len(active) != 1 || active[0].PrincipalName != "local-codex" {
		t.Fatalf("expected enriched active request: %+v", active)
	}
	s.Token(Event{RequestID: "1", VirtualModel: "a", TTFTMillis: 10})
	s.RequestFinished(Event{RequestID: "1", VirtualModel: "a", StatusCode: 200})
	if len(s.Active()) != 0 {
		t.Fatalf("expected no active requests")
	}
	if got := s.Recent(10); len(got) != 1 || got[0].RequestID != "1" || got[0].StatusCode != 200 {
		t.Fatalf("unexpected recent: %+v", got)
	}
	s.RequestFinished(Event{RequestID: "2"})
	s.RequestFinished(Event{RequestID: "3"})
	if got := s.Recent(10); len(got) != 2 || got[0].RequestID != "3" || got[1].RequestID != "2" {
		t.Fatalf("capacity/order failed: %+v", got)
	}
}

func TestTrackerRecordsTotalDuration(t *testing.T) {
	started := time.Now().Add(-25 * time.Millisecond)
	tracker := NewTracker(Event{RequestID: "duration", StartedAt: started}, nil)
	event := tracker.Finish(200, types.Usage{}, "")
	if event.CompletedAt == nil || event.DurationMillis < 20 {
		t.Fatalf("total duration was not recorded: %+v", event)
	}
}
