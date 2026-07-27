package ocr

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == BuiltinWorkerArgument {
		if err := RunBuiltinWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBuiltinProviderRunsOCRInIsolatedExecutable(t *testing.T) {
	data, err := os.ReadFile("testdata/builtin-text.png")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewBuiltinProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	results, err := provider.Recognize(ctx, []Image{{Index: 3, MediaType: "image/png", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Index != 3 ||
		!strings.Contains(results[0].Text, "vibe-proxy") ||
		results[0].Confidence == nil || *results[0].Confidence < 0.55 {
		t.Fatalf("unexpected isolated OCR result: %+v", results)
	}
}
