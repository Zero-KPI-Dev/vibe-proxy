package telemetry

import (
	"context"
	"sync"
)

const DefaultRecorderCapacity = 1024

type recorderKind uint8

const (
	recorderSummary recorderKind = iota
	recorderObservation
	recorderPayload
)

// RequestRecorder is the error-reporting persistence form of RequestFinished.
// EventSink remains source-compatible for lightweight metrics and in-memory
// consumers while durable sinks can report failures to AsyncRecorder.
type RequestRecorder interface {
	RecordRequest(Event) error
}

type RecorderStats struct {
	DroppedSummaries    uint64
	DroppedObservations uint64
	DroppedPayloads     uint64
	WriteFailures       uint64
}

// AsyncRecorder keeps telemetry persistence off the model request path. Its
// single bounded queue is split by priority so a new summary can evict an
// observation or payload, and a new observation can evict a payload. Payloads
// are always the first data discarded under pressure.
type AsyncRecorder struct {
	sink     EventSink
	capacity int

	mu           sync.Mutex
	condition    *sync.Cond
	summaries    []Event
	observations []TraceObservation
	payloads     []PayloadSnapshot
	stats        RecorderStats
	closed       bool
	done         chan struct{}
	closeOnce    sync.Once
}

func NewAsyncRecorder(sink EventSink, capacity int) *AsyncRecorder {
	if capacity <= 0 {
		capacity = DefaultRecorderCapacity
	}
	recorder := &AsyncRecorder{sink: sink, capacity: capacity, done: make(chan struct{})}
	recorder.condition = sync.NewCond(&recorder.mu)
	go recorder.run()
	return recorder
}

// RequestStarted and Token remain the responsibility of immediate in-memory
// and metrics sinks. Durable local observability records the finished summary,
// bounded observations, and payload snapshots.
func (r *AsyncRecorder) RequestStarted(Event) {}
func (r *AsyncRecorder) Token(Event)          {}

func (r *AsyncRecorder) RequestFinished(event Event) {
	r.enqueue(recorderSummary, event, TraceObservation{}, PayloadSnapshot{})
}

func (r *AsyncRecorder) RecordObservation(observation TraceObservation) error {
	r.enqueue(recorderObservation, Event{}, observation, PayloadSnapshot{})
	return nil
}

func (r *AsyncRecorder) RecordPayload(snapshot PayloadSnapshot) error {
	r.enqueue(recorderPayload, Event{}, TraceObservation{}, snapshot)
	return nil
}

func (r *AsyncRecorder) ObservabilityReader() ObservabilityReader {
	if reader, ok := r.sink.(ObservabilityReader); ok {
		return reader
	}
	if provider, ok := r.sink.(ObservabilityReaderProvider); ok {
		return provider.ObservabilityReader()
	}
	return nil
}

func (r *AsyncRecorder) Stats() RecorderStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

func (r *AsyncRecorder) Close(ctx context.Context) error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.condition.Broadcast()
		r.mu.Unlock()
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *AsyncRecorder) enqueue(kind recorderKind, summary Event, observation TraceObservation, payload PayloadSnapshot) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.drop(kind)
		return
	}
	if r.queueLength() >= r.capacity && !r.makeRoom(kind) {
		r.drop(kind)
		return
	}
	switch kind {
	case recorderSummary:
		r.summaries = append(r.summaries, summary)
	case recorderObservation:
		r.observations = append(r.observations, observation)
	case recorderPayload:
		r.payloads = append(r.payloads, payload)
	}
	r.condition.Signal()
}

func (r *AsyncRecorder) makeRoom(kind recorderKind) bool {
	switch kind {
	case recorderSummary:
		if len(r.payloads) > 0 {
			r.payloads = r.payloads[1:]
			r.stats.DroppedPayloads++
			return true
		}
		if len(r.observations) > 0 {
			r.observations = r.observations[1:]
			r.stats.DroppedObservations++
			return true
		}
	case recorderObservation:
		if len(r.payloads) > 0 {
			r.payloads = r.payloads[1:]
			r.stats.DroppedPayloads++
			return true
		}
	}
	return false
}

func (r *AsyncRecorder) drop(kind recorderKind) {
	switch kind {
	case recorderSummary:
		r.stats.DroppedSummaries++
	case recorderObservation:
		r.stats.DroppedObservations++
	case recorderPayload:
		r.stats.DroppedPayloads++
	}
}

func (r *AsyncRecorder) queueLength() int {
	return len(r.summaries) + len(r.observations) + len(r.payloads)
}

func (r *AsyncRecorder) run() {
	defer close(r.done)
	for {
		kind, summary, observation, payload, ok := r.next()
		if !ok {
			return
		}
		var err error
		switch kind {
		case recorderSummary:
			if recorder, supported := r.sink.(RequestRecorder); supported {
				err = recorder.RecordRequest(summary)
			} else if r.sink != nil {
				r.sink.RequestFinished(summary)
			}
		case recorderObservation:
			if recorder, supported := r.sink.(ObservationRecorder); supported {
				err = recorder.RecordObservation(observation)
			}
		case recorderPayload:
			if recorder, supported := r.sink.(PayloadRecorder); supported {
				err = recorder.RecordPayload(payload)
			}
		}
		if err != nil {
			r.mu.Lock()
			r.stats.WriteFailures++
			r.mu.Unlock()
		}
	}
}

func (r *AsyncRecorder) next() (recorderKind, Event, TraceObservation, PayloadSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.queueLength() == 0 && !r.closed {
		r.condition.Wait()
	}
	if r.queueLength() == 0 && r.closed {
		return 0, Event{}, TraceObservation{}, PayloadSnapshot{}, false
	}
	if len(r.summaries) > 0 {
		value := r.summaries[0]
		r.summaries = r.summaries[1:]
		return recorderSummary, value, TraceObservation{}, PayloadSnapshot{}, true
	}
	if len(r.observations) > 0 {
		value := r.observations[0]
		r.observations = r.observations[1:]
		return recorderObservation, Event{}, value, PayloadSnapshot{}, true
	}
	value := r.payloads[0]
	r.payloads = r.payloads[1:]
	return recorderPayload, Event{}, TraceObservation{}, value, true
}
