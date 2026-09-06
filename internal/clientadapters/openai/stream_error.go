package openai

import (
	"context"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/httpstream"
	"github.com/a448582655/vibe-proxy/internal/ir"
)

func (ChatAdapter) EncodeStreamError(ctx context.Context, w http.ResponseWriter, failure ir.GatewayError) error {
	writer := httpstream.NewWriter(ctx, w, 5*time.Second)
	defer writer.Close()
	writeSSE(writer, map[string]any{"error": map[string]any{"message": failure.Message, "type": openAIErrorType(failure), "code": failure.Code}})
	return writer.FlushError()
}

func (ResponsesAdapter) EncodeStreamError(ctx context.Context, w http.ResponseWriter, failure ir.GatewayError) error {
	writer := httpstream.NewWriter(ctx, w, 5*time.Second)
	defer writer.Close()
	writeResponsesEvent(writer, "error", map[string]any{"type": "error", "code": failure.Code, "message": failure.Message, "param": nil})
	return writer.FlushError()
}
