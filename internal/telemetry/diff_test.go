package telemetry

import (
	"testing"
	"time"
)

func TestDiffCanonicalRequestsClassifiesConversationAndRouteChanges(t *testing.T) {
	baseEvent := Event{RequestID: "base", ChannelID: "provider-a", UpstreamModel: "model-a"}
	currentEvent := Event{RequestID: "current", ChannelID: "provider-b", UpstreamModel: "model-b"}
	base := []byte(`{"requested_model":"vibe-chat","messages":[{"role":"user","content":[{"type":"text","text":"one"}]}],"tools":[{"name":"search","parameters":{"type":"object"}}]}`)

	tests := []struct {
		name          string
		current       string
		appendOnly    bool
		contextShrink bool
		added         int
		removed       int
		rewritten     int
		toolChanged   bool
	}{
		{
			name:       "append-only turns",
			current:    `{"requested_model":"vibe-chat","messages":[{"role":"user","content":[{"type":"text","text":"one"}]},{"role":"assistant","content":[{"type":"text","text":"two"}]}],"tools":[{"name":"search","parameters":{"type":"object"}}]}`,
			appendOnly: true, added: 1,
		},
		{
			name:      "rewrite",
			current:   `{"requested_model":"vibe-chat","messages":[{"role":"user","content":[{"type":"text","text":"changed"}]}],"tools":[{"name":"search","parameters":{"type":"object"}}]}`,
			rewritten: 1,
		},
		{
			name:          "context shrink and tool change",
			current:       `{"requested_model":"vibe-reasoner","messages":[],"tools":[{"name":"browser","parameters":{"type":"object"}}]}`,
			contextShrink: true, removed: 1, toolChanged: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := DiffCanonicalRequests(baseEvent, currentEvent, base, []byte(test.current))
			if err != nil {
				t.Fatal(err)
			}
			if diff.AppendOnly != test.appendOnly || diff.ContextShrunk != test.contextShrink || diff.MessagesAdded != test.added || diff.MessagesRemoved != test.removed || diff.MessagesRewritten != test.rewritten {
				t.Fatalf("conversation diff = %+v", diff)
			}
			if test.toolChanged && len(diff.ToolChanges) == 0 {
				t.Fatalf("missing tool changes: %+v", diff)
			}
			if diff.ProviderChange == nil || diff.ModelChange == nil {
				t.Fatalf("missing provider/model route changes: %+v", diff)
			}
		})
	}
}

func TestSelectDiffBaseUsesExplicitThenParentThenChronologicalPrevious(t *testing.T) {
	started := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	completedA := started.Add(-2 * time.Second)
	completedB := started.Add(-time.Second)
	events := []Event{
		{RequestID: "a", SessionID: "session-1", StartedAt: started.Add(-4 * time.Second), CompletedAt: &completedA},
		{RequestID: "b", SessionID: "session-1", StartedAt: started.Add(-3 * time.Second), CompletedAt: &completedB},
		{RequestID: "parent", SessionID: "session-1", StartedAt: started.Add(-time.Second)},
		{RequestID: "explicit", SessionID: "other", StartedAt: started.Add(time.Second)},
	}
	current := Event{RequestID: "c", SessionID: "session-1", ParentRequestID: "parent", StartedAt: started}

	if base, source, ok := SelectDiffBase(current, events, "explicit"); !ok || base.RequestID != "explicit" || source != "explicit" {
		t.Fatalf("explicit selection = %+v %q %v", base, source, ok)
	}
	if base, source, ok := SelectDiffBase(current, events, ""); !ok || base.RequestID != "parent" || source != "parent" {
		t.Fatalf("parent selection = %+v %q %v", base, source, ok)
	}
	current.ParentRequestID = ""
	if base, source, ok := SelectDiffBase(current, events, ""); !ok || base.RequestID != "b" || source != "previous" {
		t.Fatalf("completed chronological selection = %+v %q %v", base, source, ok)
	}
}

func TestSelectDiffBaseIgnoresOverlappingAndIncompleteRequests(t *testing.T) {
	started := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	completedBefore := started.Add(-time.Second)
	completedAfter := started.Add(time.Second)
	events := []Event{
		{RequestID: "completed", SessionID: "session-1", StartedAt: started.Add(-4 * time.Second), CompletedAt: &completedBefore},
		{RequestID: "overlapping", SessionID: "session-1", StartedAt: started.Add(-2 * time.Second), CompletedAt: &completedAfter},
		{RequestID: "incomplete", SessionID: "session-1", StartedAt: started.Add(-1500 * time.Millisecond)},
	}
	current := Event{RequestID: "current", SessionID: "session-1", StartedAt: started}

	base, source, ok := SelectDiffBase(current, events, "")
	if !ok || base.RequestID != "completed" || source != "previous" {
		t.Fatalf("diff base = %+v %q %v, want completed previous request", base, source, ok)
	}
}

func TestSelectDiffBaseRejectsCrossSessionParent(t *testing.T) {
	started := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	completed := started.Add(-time.Second)
	current := Event{RequestID: "current", SessionID: "session-1", ParentRequestID: "parent", StartedAt: started}
	parent := Event{RequestID: "parent", SessionID: "session-2", StartedAt: started.Add(-2 * time.Second), CompletedAt: &completed}

	if base, source, ok := SelectDiffBase(current, []Event{parent}, ""); ok {
		t.Fatalf("cross-session parent selected as %q: %+v", source, base)
	}
}
