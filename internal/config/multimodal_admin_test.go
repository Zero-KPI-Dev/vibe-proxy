package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveMultimodalPreservesLiteralSecretOnEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `version: vibeproxy.io/v1alpha1
providers:
  text:
    type: openai-compatible
    base_url: http://127.0.0.1:3000/v1
    auth:
      type: none
    models: [text-model]
    default_capabilities:
      image_input: unsupported
models:
  allow_raw: true
  aliases: {}
multimodal:
  enabled: true
  ocr:
    provider: http
    endpoint: http://127.0.0.1:32180/v1/ocr
    auth:
      type: bearer
      token: literal:secret
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := SaveMultimodal(path, MultimodalAdminInput{Enabled: true, Endpoint: "http://127.0.0.1:32180/v1/ocr", AuthType: "bearer", APIKeySource: "literal", MinConfidence: 0.6, MinTextChars: 5, MaxImages: 3})
	if err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "literal:secret") || !strings.Contains(string(written), "min_confidence: 0.6") {
		t.Fatalf("unexpected written config: %s", written)
	}
}

func TestBuildMultimodalLiteralAuth(t *testing.T) {
	got, err := BuildMultimodal(MultimodalAdminInput{Enabled: true, Endpoint: "http://ocr.local/v1/ocr", AuthType: "api_key_header", APIKeySource: "literal", APIKey: "secret", Header: "x-ocr-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.OCR.Auth.Type != "api_key_header" || got.OCR.Auth.Value != "literal:secret" || got.OCR.Auth.Header != "x-ocr-key" {
		t.Fatalf("unexpected auth: %+v", got.OCR.Auth)
	}
}

func TestBuildMultimodalDefaultsToBuiltinAndExternalOverridesIt(t *testing.T) {
	builtin, err := BuildMultimodal(MultimodalAdminInput{Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if builtin.OCR.Provider != "builtin" || builtin.OCR.Endpoint != "" || builtin.OCR.Auth.Type != "" {
		t.Fatalf("unexpected built-in config: %+v", builtin.OCR)
	}

	external, err := BuildMultimodal(MultimodalAdminInput{
		Enabled:  true,
		Provider: "http",
		Endpoint: "http://ocr.local/v1/ocr",
		AuthType: "none",
	}, &builtin)
	if err != nil {
		t.Fatal(err)
	}
	if external.OCR.Provider != "http" || external.OCR.Endpoint != "http://ocr.local/v1/ocr" || external.OCR.Auth.Type != "none" {
		t.Fatalf("unexpected external config: %+v", external.OCR)
	}
}

func TestBuildMultimodalRejectsUnknownProvider(t *testing.T) {
	_, err := BuildMultimodal(MultimodalAdminInput{Enabled: true, Provider: "mystery"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported OCR provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
