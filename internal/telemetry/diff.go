package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type ValueChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type DiffChange struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type RequestDiff struct {
	State                string       `json:"state"`
	UnavailableSide      string       `json:"unavailable_side,omitempty"`
	RequestID            string       `json:"request_id"`
	BaseRequestID        string       `json:"base_request_id,omitempty"`
	BaseSelection        string       `json:"base_selection,omitempty"`
	AppendOnly           bool         `json:"append_only"`
	ContextShrunk        bool         `json:"context_shrunk"`
	CommonPrefix         int          `json:"common_prefix_messages"`
	MessagesAdded        int          `json:"messages_added"`
	MessagesRemoved      int          `json:"messages_removed"`
	MessagesRewritten    int          `json:"messages_rewritten"`
	RequestedModelChange *ValueChange `json:"requested_model_change,omitempty"`
	ModelChange          *ValueChange `json:"model_change,omitempty"`
	ProviderChange       *ValueChange `json:"provider_change,omitempty"`
	ToolChanges          []DiffChange `json:"tool_changes"`
}

func DiffCanonicalRequests(baseEvent, currentEvent Event, baseJSON, currentJSON []byte) (RequestDiff, error) {
	diff := RequestDiff{
		State:         "available",
		RequestID:     currentEvent.RequestID,
		BaseRequestID: baseEvent.RequestID,
		ToolChanges:   []DiffChange{},
	}
	var base, current map[string]any
	if err := json.Unmarshal(baseJSON, &base); err != nil {
		return diff, fmt.Errorf("decode base canonical request: %w", err)
	}
	if err := json.Unmarshal(currentJSON, &current); err != nil {
		return diff, fmt.Errorf("decode current canonical request: %w", err)
	}
	baseMessages := anySlice(base["messages"])
	currentMessages := anySlice(current["messages"])
	diff.CommonPrefix = commonJSONPrefix(baseMessages, currentMessages)
	minimum := len(baseMessages)
	if len(currentMessages) < minimum {
		minimum = len(currentMessages)
	}
	for index := diff.CommonPrefix; index < minimum; index++ {
		if jsonSignature(baseMessages[index]) != jsonSignature(currentMessages[index]) {
			diff.MessagesRewritten++
		}
	}
	if len(currentMessages) > len(baseMessages) {
		diff.MessagesAdded = len(currentMessages) - len(baseMessages)
	}
	if len(baseMessages) > len(currentMessages) {
		diff.MessagesRemoved = len(baseMessages) - len(currentMessages)
		diff.ContextShrunk = true
	}
	diff.AppendOnly = diff.CommonPrefix == len(baseMessages) && len(currentMessages) > len(baseMessages)

	baseRequested, _ := base["requested_model"].(string)
	currentRequested, _ := current["requested_model"].(string)
	if baseRequested != currentRequested {
		diff.RequestedModelChange = &ValueChange{Before: baseRequested, After: currentRequested}
	}
	if baseEvent.UpstreamModel != currentEvent.UpstreamModel {
		diff.ModelChange = &ValueChange{Before: baseEvent.UpstreamModel, After: currentEvent.UpstreamModel}
	}
	if baseEvent.ChannelID != currentEvent.ChannelID {
		diff.ProviderChange = &ValueChange{Before: baseEvent.ChannelID, After: currentEvent.ChannelID}
	}
	diff.ToolChanges = diffTools(anySlice(base["tools"]), anySlice(current["tools"]))
	return diff, nil
}

func SelectDiffBase(current Event, candidates []Event, explicitRequestID string) (Event, string, bool) {
	byID := make(map[string]Event, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.RequestID] = candidate
	}
	if explicitRequestID != "" {
		base, ok := byID[explicitRequestID]
		return base, "explicit", ok && base.RequestID != current.RequestID
	}
	if current.ParentRequestID != "" {
		if base, ok := byID[current.ParentRequestID]; ok {
			return base, "parent", true
		}
	}
	var previous Event
	found := false
	for _, candidate := range candidates {
		if candidate.RequestID == current.RequestID || current.SessionID == "" || candidate.SessionID != current.SessionID {
			continue
		}
		if compareEventPosition(candidate, current) >= 0 {
			continue
		}
		if !found || compareEventPosition(candidate, previous) > 0 {
			previous = candidate
			found = true
		}
	}
	return previous, "previous", found
}

func compareEventPosition(left, right Event) int {
	if left.StartedAt.Before(right.StartedAt) {
		return -1
	}
	if left.StartedAt.After(right.StartedAt) {
		return 1
	}
	if left.RequestID < right.RequestID {
		return -1
	}
	if left.RequestID > right.RequestID {
		return 1
	}
	return 0
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func commonJSONPrefix(base, current []any) int {
	limit := len(base)
	if len(current) < limit {
		limit = len(current)
	}
	for index := 0; index < limit; index++ {
		if jsonSignature(base[index]) != jsonSignature(current[index]) {
			return index
		}
	}
	return limit
}

func jsonSignature(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func diffTools(base, current []any) []DiffChange {
	baseTools := toolSignatures(base)
	currentTools := toolSignatures(current)
	names := make([]string, 0, len(baseTools)+len(currentTools))
	seen := map[string]bool{}
	for name := range baseTools {
		names = append(names, name)
		seen[name] = true
	}
	for name := range currentTools {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	changes := []DiffChange{}
	for _, name := range names {
		before, beforeOK := baseTools[name]
		after, afterOK := currentTools[name]
		switch {
		case !beforeOK:
			changes = append(changes, DiffChange{Kind: "added", Path: "tools." + name, After: after})
		case !afterOK:
			changes = append(changes, DiffChange{Kind: "removed", Path: "tools." + name, Before: before})
		case before != after:
			changes = append(changes, DiffChange{Kind: "changed", Path: "tools." + name, Before: before, After: after})
		}
	}
	return changes
}

func toolSignatures(tools []any) map[string]string {
	result := map[string]string{}
	for index, item := range tools {
		tool, _ := item.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "" {
			name = fmt.Sprintf("#%d", index)
		}
		result[name] = jsonSignature(tool)
	}
	return result
}

func NewUnavailableDiff(current, base Event, selection, side, state string) RequestDiff {
	return RequestDiff{
		State:           state,
		UnavailableSide: side,
		RequestID:       current.RequestID,
		BaseRequestID:   base.RequestID,
		BaseSelection:   selection,
		ToolChanges:     []DiffChange{},
	}
}

func NewNoBaseDiff(current Event) RequestDiff {
	return RequestDiff{State: "no_base", RequestID: current.RequestID, ToolChanges: []DiffChange{}}
}
