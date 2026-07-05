package adapters

import (
	"encoding/json"

	"github.com/a448582655/vibe-proxy/internal/types"
)

func OpenAIErrorJSON(err *types.GatewayError) []byte {
	if err == nil {
		err = &types.GatewayError{StatusCode: 500, Type: "server_error", Code: "internal_error", Message: "Internal server error."}
	}
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Message, "type": err.Type, "param": nil, "code": err.Code}})
	return b
}
