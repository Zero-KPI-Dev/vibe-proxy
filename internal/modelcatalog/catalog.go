package modelcatalog

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/modelcapability"
)

const DefaultSourceURL = "https://models.dev/api.json"

type SupportState = modelcapability.SupportState

const (
	SupportUnknown     = modelcapability.SupportUnknown
	SupportSupported   = modelcapability.SupportSupported
	SupportUnsupported = modelcapability.SupportUnsupported
)

type MatchStatus string

const (
	MatchNotFound      MatchStatus = "not_found"
	MatchAmbiguous     MatchStatus = "ambiguous"
	MatchExactProvider MatchStatus = "exact_provider"
	MatchExactPrefixed MatchStatus = "exact_prefixed"
	MatchExactUnique   MatchStatus = "exact_unique"
	MatchConsensus     MatchStatus = "consensus"
)

type ModelInfo struct {
	ProviderID       string   `json:"provider_id"`
	ID               string   `json:"id"`
	Name             string   `json:"name,omitempty"`
	Family           string   `json:"family,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	Attachment       *bool    `json:"attachment,omitempty"`
	Reasoning        *bool    `json:"reasoning,omitempty"`
	ToolCall         *bool    `json:"tool_call,omitempty"`
	StructuredOutput *bool    `json:"structured_output,omitempty"`
	Temperature      *bool    `json:"temperature,omitempty"`
	ContextLimit     *int64   `json:"context_limit,omitempty"`
	InputLimit       *int64   `json:"input_limit,omitempty"`
	OutputLimit      *int64   `json:"output_limit,omitempty"`
	Status           string   `json:"status,omitempty"`
	ReleaseDate      string   `json:"release_date,omitempty"`
	LastUpdated      string   `json:"last_updated,omitempty"`
}

func (m ModelInfo) ImageInput() SupportState {
	if len(m.InputModalities) == 0 {
		return SupportUnknown
	}
	for _, modality := range m.InputModalities {
		if strings.EqualFold(modality, "image") {
			return SupportSupported
		}
	}
	return SupportUnsupported
}

type Candidate struct {
	ProviderID string       `json:"provider_id"`
	ModelID    string       `json:"model_id"`
	Name       string       `json:"name,omitempty"`
	ImageInput SupportState `json:"image_input"`
}

type Match struct {
	RequestedModel string       `json:"requested_model"`
	Status         MatchStatus  `json:"status"`
	Source         string       `json:"source"`
	ImageInput     SupportState `json:"image_input"`
	Model          *ModelInfo   `json:"model,omitempty"`
	Candidates     []Candidate  `json:"candidates,omitempty"`
}

type State struct {
	SourceURL string    `json:"source_url"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	Stale     bool      `json:"stale"`
	Providers int       `json:"providers"`
	Models    int       `json:"models"`
	Error     string    `json:"error,omitempty"`
}

type ProviderInfo struct {
	ID     string
	Name   string
	API    string
	Models map[string]ModelInfo
}

type Snapshot struct {
	Providers map[string]ProviderInfo
	byModel   map[string][]ModelInfo
	state     State
	raw       []byte
}

func emptySnapshot(sourceURL string) *Snapshot {
	return &Snapshot{
		Providers: map[string]ProviderInfo{},
		byModel:   map[string][]ModelInfo{},
		state:     State{SourceURL: sourceURL, Stale: true},
	}
}

func (s *Snapshot) State() State { return s.state }

func (s *Snapshot) Lookup(providerHint, vibeProviderID, baseURL, modelID string) Match {
	modelID = strings.TrimSpace(modelID)
	result := Match{RequestedModel: modelID, Status: MatchNotFound, Source: "models_dev", ImageInput: SupportUnknown}
	if modelID == "" {
		return result
	}

	for _, providerID := range orderedProviderHints(providerHint, vibeProviderID, baseURL, s.Providers) {
		if model, ok := s.providerModel(providerID, modelID); ok {
			return exactMatch(modelID, MatchExactProvider, model)
		}
	}

	if slash := strings.IndexByte(modelID, '/'); slash > 0 && slash < len(modelID)-1 {
		providerID := strings.ToLower(modelID[:slash])
		if model, ok := s.providerModel(providerID, modelID[slash+1:]); ok {
			return exactMatch(modelID, MatchExactPrefixed, model)
		}
	}

	candidates := append([]ModelInfo(nil), s.byModel[modelID]...)
	if len(candidates) == 0 {
		return result
	}
	if len(candidates) == 1 {
		return exactMatch(modelID, MatchExactUnique, candidates[0])
	}

	result.Candidates = candidateSummaries(candidates)
	imageSupport := candidates[0].ImageInput()
	for _, candidate := range candidates[1:] {
		if candidate.ImageInput() != imageSupport {
			result.Status = MatchAmbiguous
			return result
		}
	}

	consensus := consensusModel(candidates)
	result.Status = MatchConsensus
	result.ImageInput = imageSupport
	result.Model = &consensus
	return result
}

func (s *Snapshot) providerModel(providerID, modelID string) (ModelInfo, bool) {
	provider, ok := s.Providers[strings.ToLower(strings.TrimSpace(providerID))]
	if !ok {
		return ModelInfo{}, false
	}
	model, ok := provider.Models[modelID]
	return model, ok
}

func exactMatch(requested string, status MatchStatus, model ModelInfo) Match {
	copy := model
	return Match{
		RequestedModel: requested,
		Status:         status,
		Source:         "models_dev",
		ImageInput:     model.ImageInput(),
		Model:          &copy,
	}
}

func candidateSummaries(models []ModelInfo) []Candidate {
	out := make([]Candidate, 0, len(models))
	for _, model := range models {
		out = append(out, Candidate{ProviderID: model.ProviderID, ModelID: model.ID, Name: model.Name, ImageInput: model.ImageInput()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderID == out[j].ProviderID {
			return out[i].ModelID < out[j].ModelID
		}
		return out[i].ProviderID < out[j].ProviderID
	})
	return out
}

func consensusModel(models []ModelInfo) ModelInfo {
	base := models[0]
	base.ProviderID = ""
	base.Name = consensusString(models, func(m ModelInfo) string { return m.Name })
	base.Family = consensusString(models, func(m ModelInfo) string { return m.Family })
	base.InputModalities = consensusStrings(models, func(m ModelInfo) []string { return m.InputModalities })
	base.OutputModalities = consensusStrings(models, func(m ModelInfo) []string { return m.OutputModalities })
	base.Attachment = consensusBool(models, func(m ModelInfo) *bool { return m.Attachment })
	base.Reasoning = consensusBool(models, func(m ModelInfo) *bool { return m.Reasoning })
	base.ToolCall = consensusBool(models, func(m ModelInfo) *bool { return m.ToolCall })
	base.StructuredOutput = consensusBool(models, func(m ModelInfo) *bool { return m.StructuredOutput })
	base.Temperature = consensusBool(models, func(m ModelInfo) *bool { return m.Temperature })
	base.ContextLimit = consensusInt64(models, func(m ModelInfo) *int64 { return m.ContextLimit })
	base.InputLimit = consensusInt64(models, func(m ModelInfo) *int64 { return m.InputLimit })
	base.OutputLimit = consensusInt64(models, func(m ModelInfo) *int64 { return m.OutputLimit })
	base.Status = consensusString(models, func(m ModelInfo) string { return m.Status })
	base.ReleaseDate = consensusString(models, func(m ModelInfo) string { return m.ReleaseDate })
	base.LastUpdated = consensusString(models, func(m ModelInfo) string { return m.LastUpdated })
	return base
}

func consensusString(models []ModelInfo, get func(ModelInfo) string) string {
	value := get(models[0])
	for _, model := range models[1:] {
		if get(model) != value {
			return ""
		}
	}
	return value
}

func consensusStrings(models []ModelInfo, get func(ModelInfo) []string) []string {
	value := normalizedStrings(get(models[0]))
	for _, model := range models[1:] {
		if strings.Join(normalizedStrings(get(model)), "\x00") != strings.Join(value, "\x00") {
			return nil
		}
	}
	return value
}

func consensusBool(models []ModelInfo, get func(ModelInfo) *bool) *bool {
	first := get(models[0])
	if first == nil {
		return nil
	}
	for _, model := range models[1:] {
		value := get(model)
		if value == nil || *value != *first {
			return nil
		}
	}
	copy := *first
	return &copy
}

func consensusInt64(models []ModelInfo, get func(ModelInfo) *int64) *int64 {
	first := get(models[0])
	if first == nil {
		return nil
	}
	for _, model := range models[1:] {
		value := get(model)
		if value == nil || *value != *first {
			return nil
		}
	}
	copy := *first
	return &copy
}

func normalizedStrings(values []string) []string {
	out := append([]string(nil), values...)
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	sort.Strings(out)
	return out
}

func orderedProviderHints(explicit, vibeProviderID, baseURL string, providers map[string]ProviderInfo) []string {
	seen := map[string]bool{}
	result := []string{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	add(explicit)
	add(vibeProviderID)
	if inferred := inferProviderFromBaseURL(baseURL, providers); inferred != "" {
		add(inferred)
	}
	return result
}

func inferProviderFromBaseURL(baseURL string, providers map[string]ProviderInfo) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	official := map[string]string{
		"api.openai.com":                    "openai",
		"api.anthropic.com":                 "anthropic",
		"generativelanguage.googleapis.com": "google",
		"api.x.ai":                          "xai",
		"api.mistral.ai":                    "mistral",
		"api.deepseek.com":                  "deepseek",
	}
	if providerID := official[host]; providerID != "" {
		return providerID
	}
	for id, provider := range providers {
		apiURL, err := url.Parse(provider.API)
		if err == nil && apiURL.Hostname() != "" && strings.EqualFold(apiURL.Hostname(), host) {
			return id
		}
	}
	return ""
}
