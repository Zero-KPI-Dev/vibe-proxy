package ocr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPProviderContractAndAuth(t *testing.T) {
	provider := NewHTTPProvider(HTTPOptions{
		Endpoint: "https://ocr.example/v1/ocr",
		Auth:     upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: "literal:secret"},
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost || r.Header.Get("x-api-key") != "secret" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected OCR request: %s %v", r.Method, r.Header)
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"data_base64":"aGVsbG8="`) || strings.Contains(string(body), "secret") {
				t.Fatalf("unexpected OCR body: %s", body)
			}
			return response(200, `{"results":[{"index":3,"text":"invoice","confidence":0.9,"language":"en"}]}`), nil
		})},
	})
	results, err := provider.Recognize(context.Background(), []Image{{Index: 3, MediaType: "image/png", Data: []byte("hello")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Index != 3 || results[0].Text != "invoice" || results[0].Confidence == nil || *results[0].Confidence != 0.9 {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestHTTPProviderRejectsInvalidResponse(t *testing.T) {
	provider := NewHTTPProvider(HTTPOptions{Endpoint: "https://ocr.example/v1/ocr", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(200, `{"results":[{"index":99,"text":"bad"}]}`), nil
	})}})
	_, err := provider.Recognize(context.Background(), []Image{{Index: 0, MediaType: "image/png", Data: []byte("hello")}})
	if got, ok := err.(Error); !ok || got.Code != "ocr_invalid_response" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
