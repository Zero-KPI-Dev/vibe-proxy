package telemetry

import (
	"testing"
	"time"
)

func TestLiveBrokerPublishesLifecycleAndReplaysAfterID(t *testing.T) {
	broker := NewLiveBroker(LiveBrokerOptions{ReplayCapacity: 8})
	started := Event{RequestID: "request-1", StartedAt: time.Now().UTC()}
	broker.RequestStarted(started)
	started.PrincipalName = "local-codex"
	broker.RequestUpdated(started, RequestPhaseAuthenticated)
	started.StatusCode = 200
	broker.RequestFinished(started)

	subscription := broker.Subscribe(1)
	defer subscription.Cancel()
	if len(subscription.Replay) != 2 {
		t.Fatalf("expected two replay events after id 1, got %+v", subscription.Replay)
	}
	if subscription.Replay[0].Kind != LiveEventUpdated || subscription.Replay[0].Phase != RequestPhaseAuthenticated {
		t.Fatalf("unexpected update replay: %+v", subscription.Replay[0])
	}
	if subscription.Replay[1].Kind != LiveEventFinished || subscription.Replay[1].ID != 3 {
		t.Fatalf("unexpected finish replay: %+v", subscription.Replay[1])
	}
}

func TestLiveBrokerCoalescesProgressAndMarksFirstToken(t *testing.T) {
	broker := NewLiveBroker(LiveBrokerOptions{ProgressInterval: time.Hour})
	subscription := broker.Subscribe(0)
	defer subscription.Cancel()

	event := Event{RequestID: "request-1", FirstTokenAt: timePointer(time.Now().UTC()), TTFTMillis: 12}
	broker.Token(event)
	event.Usage.CompletionTokens = 2
	broker.Token(event)

	first := <-subscription.Events
	if first.Kind != LiveEventFirstToken || first.Phase != RequestPhaseStreaming {
		t.Fatalf("unexpected first-token event: %+v", first)
	}
	select {
	case extra := <-subscription.Events:
		t.Fatalf("progress should have been coalesced, got %+v", extra)
	default:
	}
}

func TestLiveBrokerDisconnectsSlowSubscriberWithoutBlockingPublisher(t *testing.T) {
	broker := NewLiveBroker(LiveBrokerOptions{SubscriberCapacity: 1})
	subscription := broker.Subscribe(0)
	broker.RequestStarted(Event{RequestID: "request-1"})
	broker.RequestStarted(Event{RequestID: "request-2"})

	if event := <-subscription.Events; event.Request.RequestID != "request-1" {
		t.Fatalf("unexpected queued event: %+v", event)
	}
	if _, ok := <-subscription.Events; ok {
		t.Fatal("slow subscriber channel should be closed")
	}
	// Cancel is intentionally idempotent after broker-driven disconnect.
	subscription.Cancel()
}

func TestLiveBrokerReplayIsBounded(t *testing.T) {
	broker := NewLiveBroker(LiveBrokerOptions{ReplayCapacity: 2})
	for _, id := range []string{"one", "two", "three"} {
		broker.RequestStarted(Event{RequestID: id})
	}
	subscription := broker.Subscribe(0)
	defer subscription.Cancel()
	if len(subscription.Replay) != 2 || subscription.Replay[0].Request.RequestID != "two" || subscription.Replay[1].Request.RequestID != "three" {
		t.Fatalf("unexpected bounded replay: %+v", subscription.Replay)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
