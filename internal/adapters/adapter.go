package adapters

import (
	"context"
	"net/http"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

type ProxyRequest struct {
	RawBody       []byte
	ClientHeaders http.Header
	VirtualModel  string
	UpstreamModel string
	Stream        bool
	Overrides     map[string]any
}

type Adapter interface {
	Protocol() types.Protocol
	TransformRequest(ctx context.Context, req ProxyRequest, ch config.ChannelConfig, apiKey string) (*http.Request, *types.GatewayError)
	HandleResponse(ctx context.Context, upstream *http.Response, client http.ResponseWriter, req ProxyRequest, tracker *telemetry.Tracker) *types.GatewayError
	NormalizeError(err *types.GatewayError) []byte
}
