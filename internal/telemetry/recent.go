package telemetry

import "sync"

type RecentStore struct {
	mu       sync.RWMutex
	capacity int
	items    []Event
	active   map[string]Event
}

func NewRecentStore(capacity int) *RecentStore {
	if capacity <= 0 {
		capacity = 100
	}
	return &RecentStore{capacity: capacity, active: map[string]Event{}}
}

func (s *RecentStore) RequestStarted(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = map[string]Event{}
	}
	s.active[e.RequestID] = e
}

func (s *RecentStore) Token(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = map[string]Event{}
	}
	s.active[e.RequestID] = e
}

func (s *RecentStore) RequestUpdated(e Event, _ RequestPhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = map[string]Event{}
	}
	s.active[e.RequestID] = e
}

func (s *RecentStore) RequestFinished(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, e.RequestID)
	s.items = append([]Event{e}, s.items...)
	if len(s.items) > s.capacity {
		s.items = s.items[:s.capacity]
	}
}

func (s *RecentStore) Recent(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.items) {
		limit = len(s.items)
	}
	out := make([]Event, limit)
	copy(out, s.items[:limit])
	return out
}

func (s *RecentStore) Active() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, 0, len(s.active))
	for _, e := range s.active {
		out = append(out, e)
	}
	return out
}
