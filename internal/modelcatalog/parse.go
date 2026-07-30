package modelcatalog

import (
	"encoding/json"
	"fmt"
	"strings"
)

type apiProvider struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	API    string              `json:"api"`
	Models map[string]apiModel `json:"models"`
}

type apiModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Family           string `json:"family"`
	Attachment       *bool  `json:"attachment"`
	Reasoning        *bool  `json:"reasoning"`
	ToolCall         *bool  `json:"tool_call"`
	StructuredOutput *bool  `json:"structured_output"`
	Temperature      *bool  `json:"temperature"`
	Status           string `json:"status"`
	ReleaseDate      string `json:"release_date"`
	LastUpdated      string `json:"last_updated"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context *int64 `json:"context"`
		Input   *int64 `json:"input"`
		Output  *int64 `json:"output"`
	} `json:"limit"`
}

func parseSnapshot(raw []byte, state State) (*Snapshot, error) {
	var payload map[string]apiProvider
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse models.dev catalog: %w", err)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("parse models.dev catalog: no providers")
	}
	providers := make(map[string]ProviderInfo, len(payload))
	byModel := map[string][]ModelInfo{}
	modelCount := 0
	for key, item := range payload {
		providerID := item.ID
		if providerID == "" {
			providerID = key
		}
		providerID = strings.ToLower(strings.TrimSpace(providerID))
		if providerID == "" {
			continue
		}
		provider := ProviderInfo{ID: providerID, Name: item.Name, API: item.API, Models: map[string]ModelInfo{}}
		for key, model := range item.Models {
			modelID := model.ID
			if modelID == "" {
				modelID = key
			}
			if modelID == "" {
				continue
			}
			info := ModelInfo{
				ProviderID:       providerID,
				ID:               modelID,
				Name:             model.Name,
				Family:           model.Family,
				InputModalities:  normalizedStrings(model.Modalities.Input),
				OutputModalities: normalizedStrings(model.Modalities.Output),
				Attachment:       cloneBool(model.Attachment),
				Reasoning:        cloneBool(model.Reasoning),
				ToolCall:         cloneBool(model.ToolCall),
				StructuredOutput: cloneBool(model.StructuredOutput),
				Temperature:      cloneBool(model.Temperature),
				ContextLimit:     cloneInt64(model.Limit.Context),
				InputLimit:       cloneInt64(model.Limit.Input),
				OutputLimit:      cloneInt64(model.Limit.Output),
				Status:           model.Status,
				ReleaseDate:      model.ReleaseDate,
				LastUpdated:      model.LastUpdated,
			}
			provider.Models[modelID] = info
			byModel[modelID] = append(byModel[modelID], info)
			modelCount++
		}
		providers[providerID] = provider
	}
	state.Providers = len(providers)
	state.Models = modelCount
	state.Stale = false
	state.Error = ""
	return &Snapshot{Providers: providers, byModel: byModel, state: state, raw: append([]byte(nil), raw...)}, nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
