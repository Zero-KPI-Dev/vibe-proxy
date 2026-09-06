package anthropic

import (
	"context"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/httpstream"
	"github.com/a448582655/vibe-proxy/internal/ir"
)

func (MessagesAdapter) EncodeStreamError(ctx context.Context, w http.ResponseWriter, failure ir.GatewayError) error {
	writer := httpstream.NewWriter(ctx, w, 5*time.Second)
	defer writer.Close()
	writeEvent(writer, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": failure.Message, "code": failure.Code}})
	return writer.FlushError()
}
