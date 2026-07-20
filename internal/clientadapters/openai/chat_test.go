package openai

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func TestParseChatImageURL(t *testing.T) {
	body := []byte(`{"model":"vision","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`)
	r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req, err := (ChatAdapter{}).ParseRequest(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 {
		t.Fatalf("unexpected content: %+v", req.Messages)
	}
	image := req.Messages[0].Content[1]
	if image.Type != ir.ContentImage || image.Image == nil || image.Image.URL != "data:image/png;base64,AA==" {
		t.Fatalf("image not parsed: %+v", image)
	}
}
