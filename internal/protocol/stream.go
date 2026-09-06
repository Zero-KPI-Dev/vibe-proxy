package protocol

import (
	"context"
	"net/http"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

// StreamErrorEncoder reports a failure after the HTTP status has been sent.
// It must not append a successful completion marker after the error.
type StreamErrorEncoder interface {
	EncodeStreamError(context.Context, http.ResponseWriter, ir.GatewayError) error
}

func UnexpectedStreamEnd(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return ir.GatewayError{StatusCode: 502, Kind: "upstream_error", Code: "stream_interrupted", Message: "Upstream stream ended without a completion event."}
}
