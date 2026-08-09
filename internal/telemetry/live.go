package telemetry

import (
	"sync"
	"time"
)

const (
	DefaultLiveReplayCapacity     = 512
	DefaultLiveSubscriberCapacity = 64
	DefaultLiveProgressInterval   = 350 * time.Millisecond
)

type RequestPhase string

const (
	RequestPhaseReceived        RequestPhase = "received"
	RequestPhaseAuthenticated   RequestPhase = "authenticated"
	RequestPhaseParsed          RequestPhase = "parsed"
	RequestPhaseRouted          RequestPhase = "routed"
	RequestPhasePreprocessing   RequestPhase = "preprocessing"
	RequestPhaseUpstreamStarted RequestPhase = "upstream_started"
	RequestPhaseStreaming       RequestPhase = "streaming"
	RequestPhaseCompleted       RequestPhase = "completed"
)

type LiveEventKind string

const (
	LiveEventStarted    LiveEventKind = "started"
	LiveEventUpdated    LiveEventKind = "updated"
	LiveEventFirstToken LiveEventKind = "first_token"
	LiveEventProgress   LiveEventKind = "progress"
	LiveEventFinished   LiveEventKind = "finished"
	LiveEventFailed     LiveEventKind = "failed"
)

type LiveEvent struct {
	ID        uint64        `json:"id"`
	Kind      LiveEventKind `json:"kind"`
	Phase     RequestPhase  `json:"phase"`
	EmittedAt time.Time     `json:"emitted_at"`
	Request   Event         `json:"request"`
}

// RequestUpdateSink receives bounded lifecycle checkpoints without changing the
// compatibility EventSink contract used by persistence and metrics sinks.
type RequestUpdateSink interface {
	RequestUpdated(Event, RequestPhase)
}

func PublishRequestUpdate(sink EventSink, event Event, phase RequestPhase) {
	if updater, ok := sink.(RequestUpdateSink); ok {
		updater.RequestUpdated(event, phase)
	}
}

type LiveSubscription struct {
	Replay []LiveEvent
	Events <-chan LiveEvent
	cancel func()
}

func (s LiveSubscription) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

type LiveEventSource interface {
	Subscribe(afterID uint64) LiveSubscription
	LatestID() uint64
}

type LiveEventSourceProvider interface {
	LiveEventSource() LiveEventSource
}

type LiveBrokerOptions struct {
	ReplayCapacity     int
	SubscriberCapacity int
	ProgressInterval   time.Duration
}

// LiveBroker is an in-memory, non-blocking delivery sink. SQLite remains the
// durable source of truth; this broker only carries a bounded replay window for
// connected control-plane clients.
type LiveBroker struct {
	mu                 sync.Mutex
	nextID             uint64
	nextSubscriberID   uint64
	replayCapacity     int
	subscriberCapacity int
	progressInterval   time.Duration
	replay             []LiveEvent
	subscribers        map[uint64]chan LiveEvent
	firstTokenSeen     map[string]bool
	lastProgress       map[string]time.Time
}

func NewLiveBroker(options LiveBrokerOptions) *LiveBroker {
	replayCapacity := options.ReplayCapacity
	if replayCapacity <= 0 {
		replayCapacity = DefaultLiveReplayCapacity
	}
	subscriberCapacity := options.SubscriberCapacity
	if subscriberCapacity <= 0 {
		subscriberCapacity = DefaultLiveSubscriberCapacity
	}
	progressInterval := options.ProgressInterval
	if progressInterval <= 0 {
		progressInterval = DefaultLiveProgressInterval
	}
	return &LiveBroker{
		replayCapacity:     replayCapacity,
		subscriberCapacity: subscriberCapacity,
		progressInterval:   progressInterval,
		subscribers:        map[uint64]chan LiveEvent{},
		firstTokenSeen:     map[string]bool{},
		lastProgress:       map[string]time.Time{},
	}
}

func (b *LiveBroker) LiveEventSource() LiveEventSource { return b }

func (b *LiveBroker) RequestStarted(event Event) {
	b.publish(LiveEventStarted, RequestPhaseReceived, event, time.Now().UTC())
}

func (b *LiveBroker) RequestUpdated(event Event, phase RequestPhase) {
	b.publish(LiveEventUpdated, phase, event, time.Now().UTC())
}

func (b *LiveBroker) Token(event Event) {
	now := time.Now().UTC()
	b.mu.Lock()
	if !b.firstTokenSeen[event.RequestID] {
		b.firstTokenSeen[event.RequestID] = true
		b.lastProgress[event.RequestID] = now
		b.publishLocked(LiveEventFirstToken, RequestPhaseStreaming, event, now)
		b.mu.Unlock()
		return
	}
	if now.Sub(b.lastProgress[event.RequestID]) < b.progressInterval {
		b.mu.Unlock()
		return
	}
	b.lastProgress[event.RequestID] = now
	b.publishLocked(LiveEventProgress, RequestPhaseStreaming, event, now)
	b.mu.Unlock()
}

func (b *LiveBroker) RequestFinished(event Event) {
	now := time.Now().UTC()
	b.mu.Lock()
	delete(b.firstTokenSeen, event.RequestID)
	delete(b.lastProgress, event.RequestID)
	kind := LiveEventFinished
	if event.StatusCode >= 400 || event.ErrorCode != "" {
		kind = LiveEventFailed
	}
	b.publishLocked(kind, RequestPhaseCompleted, event, now)
	b.mu.Unlock()
}

func (b *LiveBroker) LatestID() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextID
}

func (b *LiveBroker) Subscribe(afterID uint64) LiveSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	replay := make([]LiveEvent, 0, len(b.replay))
	for _, event := range b.replay {
		if event.ID > afterID {
			replay = append(replay, event)
		}
	}
	b.nextSubscriberID++
	id := b.nextSubscriberID
	stream := make(chan LiveEvent, b.subscriberCapacity)
	b.subscribers[id] = stream
	return LiveSubscription{
		Replay: replay,
		Events: stream,
		cancel: func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if current, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(current)
			}
		},
	}
}

func (b *LiveBroker) publish(kind LiveEventKind, phase RequestPhase, event Event, now time.Time) {
	b.mu.Lock()
	b.publishLocked(kind, phase, event, now)
	b.mu.Unlock()
}

func (b *LiveBroker) publishLocked(kind LiveEventKind, phase RequestPhase, event Event, now time.Time) {
	b.nextID++
	live := LiveEvent{
		ID:        b.nextID,
		Kind:      kind,
		Phase:     phase,
		EmittedAt: now,
		Request:   cloneEvent(event),
	}
	b.replay = append(b.replay, live)
	if len(b.replay) > b.replayCapacity {
		copy(b.replay, b.replay[len(b.replay)-b.replayCapacity:])
		b.replay = b.replay[:b.replayCapacity]
	}
	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- live:
		default:
			delete(b.subscribers, id)
			close(subscriber)
		}
	}
}

func cloneEvent(event Event) Event {
	clone := event
	if event.Transformation != nil {
		transformation := *event.Transformation
		clone.Transformation = &transformation
	}
	return clone
}
