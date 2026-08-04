package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAsyncRecorderEvictsPayloadsBeforeObservationsAndSummaries(t *testing.T) {
	sink := &blockingRecorderSink{entered: make(chan struct{}), release: make(chan struct{})}
	recorder := NewAsyncRecorder(sink, 2)
	if err := recorder.RecordPayload(PayloadSnapshot{RequestID: "first", Stage: PayloadStageClientRequest}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("recorder worker did not start")
	}

	_ = recorder.RecordPayload(PayloadSnapshot{RequestID: "evicted", Stage: PayloadStageClientRequest})
	_ = recorder.RecordObservation(TraceObservation{ObservationID: "observation"})
	recorder.RequestFinished(Event{RequestID: "summary"})

	stats := recorder.Stats()
	if stats.DroppedPayloads != 1 || stats.DroppedObservations != 0 || stats.DroppedSummaries != 0 {
		t.Fatalf("priority eviction stats = %+v", stats)
	}
	close(sink.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}

	got := sink.operations()
	want := []string{"payload:first:captured", "summary:summary", "payload:evicted:dropped", "observation:observation"}
	if len(got) != len(want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("operations = %v, want %v", got, want)
		}
	}
}

func TestAsyncRecorderContainsSinkFailuresAndFlushesOnClose(t *testing.T) {
	sink := &failingRecorderSink{}
	recorder := NewAsyncRecorder(sink, 4)
	if err := recorder.RecordPayload(PayloadSnapshot{RequestID: "payload"}); err != nil {
		t.Fatalf("write failure escaped recorder: %v", err)
	}
	recorder.RequestFinished(Event{RequestID: "summary"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if stats := recorder.Stats(); stats.WriteFailures != 2 {
		t.Fatalf("write failure stats = %+v", stats)
	}
}

type blockingRecorderSink struct {
	mu       sync.Mutex
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	recorded []string
}

func (s *blockingRecorderSink) RequestStarted(Event) {}
func (s *blockingRecorderSink) Token(Event)          {}
func (s *blockingRecorderSink) RequestFinished(event Event) {
	s.append("summary:" + event.RequestID)
}
func (s *blockingRecorderSink) RecordPayload(snapshot PayloadSnapshot) error {
	if snapshot.RequestID == "first" {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	status := snapshot.CaptureStatus
	if status == "" {
		status = CaptureStatusCaptured
	}
	s.append("payload:" + snapshot.RequestID + ":" + status)
	return nil
}
func (s *blockingRecorderSink) RecordObservation(observation TraceObservation) error {
	s.append("observation:" + observation.ObservationID)
	return nil
}
func (s *blockingRecorderSink) append(value string) {
	s.mu.Lock()
	s.recorded = append(s.recorded, value)
	s.mu.Unlock()
}
func (s *blockingRecorderSink) operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recorded...)
}

type failingRecorderSink struct{}

func (*failingRecorderSink) RequestStarted(Event)  {}
func (*failingRecorderSink) RequestFinished(Event) {}
func (*failingRecorderSink) Token(Event)           {}
func (*failingRecorderSink) RecordRequest(Event) error {
	return errors.New("summary write failed")
}
func (*failingRecorderSink) RecordPayload(PayloadSnapshot) error {
	return errors.New("payload write failed")
}
func (*failingRecorderSink) RecordObservation(TraceObservation) error {
	return errors.New("observation write failed")
}
