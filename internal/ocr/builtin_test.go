package ocr

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseHOCRPreservesLinesAndConfidence(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body>
<span class='ocr_line'>
  <span class='ocrx_word' title='bbox 0 0 10 10; x_wconf 93'>内</span>
  <span class='ocrx_word' title='bbox 11 0 20 10; x_wconf 30'>置</span>
  <span class='ocrx_word' title='bbox 21 0 40 10; x_wconf 96'>OCR</span>
</span>
<span class='ocr_line'>
  <span class='ocrx_word' title='bbox 0 20 50 30; x_wconf 90'>Invoice</span>
  <span class='ocrx_word' title='bbox 51 20 70 30; x_wconf 92'>42</span>
</span>
</body></html>`
	text, confidence, err := parseHOCR(input)
	if err != nil {
		t.Fatal(err)
	}
	if text != "内置OCR\nInvoice 42" {
		t.Fatalf("unexpected OCR text: %q", text)
	}
	if confidence == nil || *confidence < 0.80 || *confidence > 0.90 {
		t.Fatalf("unexpected confidence: %v", confidence)
	}
}

func TestBuiltinProviderRecognizesChineseAndEnglishWithoutSystemOCR(t *testing.T) {
	data, err := os.ReadFile("testdata/builtin-text.png")
	if err != nil {
		t.Fatal(err)
	}
	provider := newBuiltinEngineProvider()
	defer provider.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := provider.Health(ctx); err != nil {
		t.Fatalf("built-in OCR health failed: %v", err)
	}
	results, err := provider.Recognize(ctx, []Image{{Index: 7, MediaType: "image/png", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Index != 7 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !strings.Contains(results[0].Text, "vibe-proxy") ||
		!strings.Contains(results[0].Text, "内置OCR") ||
		!strings.Contains(results[0].Text, "Invoice total: 42 dollars") {
		t.Fatalf("unexpected recognized text: %q", results[0].Text)
	}
	if results[0].Confidence == nil || *results[0].Confidence < 0.55 {
		t.Fatalf("unexpected recognized confidence: %v", results[0].Confidence)
	}
	if results[0].Language != BuiltinLanguage || results[0].Duration <= 0 {
		t.Fatalf("missing result metadata: %+v", results[0])
	}

	var blank bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			canvas.Set(x, y, color.RGBA{R: 238, G: 242, B: 255, A: 255})
		}
	}
	if err := png.Encode(&blank, canvas); err != nil {
		t.Fatal(err)
	}
	blankResults, err := provider.Recognize(ctx, []Image{{Index: 0, MediaType: "image/png", Data: blank.Bytes()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(blankResults) != 1 || strings.TrimSpace(blankResults[0].Text) != "" || blankResults[0].Confidence != nil {
		t.Fatalf("blank image should not produce usable OCR: %+v", blankResults)
	}
}

func TestBuiltinProviderRejectsMalformedImage(t *testing.T) {
	provider := newBuiltinEngineProvider()
	defer provider.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := provider.Recognize(ctx, []Image{{Index: 0, MediaType: "image/png", Data: []byte("not-an-image")}})
	value, ok := err.(Error)
	if !ok || value.Code != "ocr_invalid_image" || value.StatusCode != 400 {
		t.Fatalf("unexpected malformed image error: %#v", err)
	}
}
