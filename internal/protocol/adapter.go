package protocol

import (
	"context"
	"net/http"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
)

type ClientAdapter interface {
	Name() string
	Protocol() ir.Protocol
	Detect(r *http.Request) bool
	ParseRequest(ctx context.Context, r *http.Request) (*ir.Request, error)
	EncodeUnary(ctx context.Context, w http.ResponseWriter, resp *ir.Response) error
	EncodeStream(ctx context.Context, w http.ResponseWriter, events <-chan ir.StreamEvent) error
	EncodeError(ctx context.Context, w http.ResponseWriter, err ir.GatewayError) error
}

type ProviderAdapter interface {
	Name() string
	Protocol() ir.Protocol
	BuildRequest(ctx context.Context, req *ir.Request, target modelresolver.Target) (*http.Request, error)
	ParseUnary(ctx context.Context, resp *http.Response) (*ir.Response, error)
	ParseStream(ctx context.Context, resp *http.Response) (<-chan ir.StreamEvent, error)
	NormalizeError(ctx context.Context, resp *http.Response) ir.GatewayError
	Capabilities() Capabilities
}

type Capabilities struct {
	Streaming         bool `json:"streaming"`
	Tools             bool `json:"tools"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	Vision            bool `json:"vision"`
	Files             bool `json:"files"`
	JSONMode          bool `json:"json_mode"`
	StructuredOutput  bool `json:"structured_output"`
	PromptCache       bool `json:"prompt_cache"`
	TrailingUsage     bool `json:"trailing_usage"`
}
