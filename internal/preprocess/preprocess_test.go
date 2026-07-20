package preprocess

import (
	"context"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

type testProcessor struct {
	name string
	text string
}

func (p testProcessor) Name() string { return p.name }
func (p testProcessor) Prepare(_ context.Context, req *ir.Request, route RouteContext) (Result, error) {
	copy := *req
	copy.Metadata = map[string]string{"step": p.text}
	return Result{Request: &copy, Target: route.Target, Decisions: []Decision{{Processor: p.name, Route: p.text}}}, nil
}

func TestPipelineRunsProcessorsInOrder(t *testing.T) {
	req := &ir.Request{}
	target := modelresolver.Target{ProviderID: "provider"}
	result, err := New(testProcessor{name: "one", text: "first"}, testProcessor{name: "two", text: "second"}).Prepare(context.Background(), req, RouteContext{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request == req || result.Request.Metadata["step"] != "second" {
		t.Fatalf("unexpected pipeline result: %+v", result.Request)
	}
	if len(result.Decisions) != 2 || result.Decisions[0].Processor != "one" || result.Decisions[1].Processor != "two" {
		t.Fatalf("unexpected decisions: %+v", result.Decisions)
	}
}
