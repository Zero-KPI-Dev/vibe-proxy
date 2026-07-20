package preprocess

import (
	"context"

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
	Request   *ir.Request
	Target    modelresolver.Target
	Decisions []Decision
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
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
