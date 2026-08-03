package preprocess

import (
	"context"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/protocol"
)

type RouteContext struct {
	Target              modelresolver.Target
	ProviderConfig      config.ProviderConfig
	AdapterCapabilities protocol.Capabilities
}

type Decision struct {
	Processor  string         `json:"processor"`
	Route      string         `json:"route"`
	Reason     string         `json:"reason,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type Result struct {
	Request      *ir.Request
	Target       modelresolver.Target
	Decisions    []Decision
	Diagnostics  []Diagnostic
	Observations []Observation
}

// Diagnostic is a structured intermediate artifact produced by a preprocessor.
// Runtime capture policy still decides whether its value is persisted.
type Diagnostic struct {
	Stage string
	Value any
}

// Observation describes one bounded preprocessing operation. The runtime turns
// it into the same trace-observation format used for primary provider calls.
type Observation struct {
	Type        string
	Name        string
	StartedAt   time.Time
	CompletedAt time.Time
	Status      string
	ErrorCode   string
	Attributes  map[string]any
}

type RequestPreprocessor interface {
	Name() string
	Prepare(ctx context.Context, req *ir.Request, route RouteContext) (Result, error)
}

type Pipeline struct {
	processors []RequestPreprocessor
}

func New(processors ...RequestPreprocessor) *Pipeline {
	filtered := make([]RequestPreprocessor, 0, len(processors))
	for _, processor := range processors {
		if processor != nil {
			filtered = append(filtered, processor)
		}
	}
	return &Pipeline{processors: filtered}
}

func (p *Pipeline) Prepare(ctx context.Context, req *ir.Request, route RouteContext) (Result, error) {
	result := Result{Request: req, Target: route.Target}
	if p == nil {
		return result, nil
	}
	for _, processor := range p.processors {
		stepRoute := route
		stepRoute.Target = result.Target
		step, err := processor.Prepare(ctx, result.Request, stepRoute)
		if step.Request != nil {
			result.Request = step.Request
		}
		if step.Target.ProviderID != "" {
			result.Target = step.Target
		}
		result.Decisions = append(result.Decisions, step.Decisions...)
		result.Diagnostics = append(result.Diagnostics, step.Diagnostics...)
		result.Observations = append(result.Observations, step.Observations...)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
