package telemetry

import "testing"

func TestRecentStoreTracksActiveAndRecent(t *testing.T) {
	s := NewRecentStore(2)
	s.RequestStarted(Event{RequestID: "1", VirtualModel: "a"})
	if len(s.Active()) != 1 {
		t.Fatalf("expected active request")
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
