package ocr

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/danlock/gogosseract"
)

const (
	BuiltinProviderName = "builtin"
	BuiltinEngine       = "tesseract-wasm"
	BuiltinLanguage     = "zh-Hans+en"
)

// builtinTrainingData is the compact chi_sim model from tessdata_fast. Besides
// simplified Chinese it contains the Latin glyphs needed for common English
// screenshots, so the built-in provider only needs one embedded language file.
//
//go:embed assets/chi_sim.traineddata
var builtinTrainingData []byte

//go:embed testdata/builtin-text.png
var builtinSelfTestImage []byte

// SelfTestImage returns a small deterministic Chinese and English image used by
// the admin OCR test endpoint. Returning a copy prevents callers from mutating
// the embedded fixture.
func SelfTestImage() (string, []byte) {
	return "image/png", bytes.Clone(builtinSelfTestImage)
}

// builtinEngineProvider runs Tesseract through WASM in the current process.
// The public BuiltinProvider executes this engine in a short-lived worker
// process so its comparatively large WASM memory arena is returned to the OS
// after each cache miss.
type builtinEngineProvider struct {
	mu     sync.Mutex
	engine *gogosseract.Tesseract
}

func newBuiltinEngineProvider() *builtinEngineProvider {
	return &builtinEngineProvider{}
}

func (p *builtinEngineProvider) Name() string { return BuiltinProviderName }

func (p *builtinEngineProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engine == nil {
		return nil
	}
	err := p.engine.Close(context.Background())
	p.engine = nil
	return err
}

func (p *builtinEngineProvider) Health(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ensureEngine(ctx)
}

func (p *builtinEngineProvider) Recognize(ctx context.Context, images []Image) ([]Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureEngine(ctx); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(images))
	for _, image := range images {
		if err := ctx.Err(); err != nil {
			return nil, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR was cancelled before recognition completed."}
		}
		started := time.Now()
		if err := p.engine.LoadImage(ctx, bytes.NewReader(image.Data), gogosseract.LoadImageOptions{}); err != nil {
			p.resetEngineLocked()
			return nil, Error{StatusCode: 400, Code: "ocr_invalid_image", Message: "Built-in OCR could not decode the image."}
		}
		hocr, err := p.engine.GetHOCR(ctx, nil)
		if err != nil {
			p.resetEngineLocked()
			return nil, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR could not recognize the image."}
		}
		text, confidence, err := parseHOCR(hocr)
		if err != nil {
			p.resetEngineLocked()
			return nil, Error{StatusCode: 503, Code: "ocr_invalid_response", Message: "Built-in OCR returned an invalid result."}
		}
		results = append(results, Result{
			Index:      image.Index,
			Text:       text,
			Confidence: confidence,
			Language:   BuiltinLanguage,
			Duration:   time.Since(started),
		})
	}
	return results, nil
}

func (p *builtinEngineProvider) ensureEngine(ctx context.Context) error {
	if p.engine != nil {
		return nil
	}
	cfg := gogosseract.Config{
		Language:     "chi_sim",
		TrainingData: bytes.NewReader(builtinTrainingData),
		Variables: map[string]string{
			"tessedit_pageseg_mode": "3",
		},
	}
	// These fields are promoted from gogosseract's compile configuration and
	// cannot be set in the composite literal outside that package.
	cfg.Stdout = io.Discard
	cfg.Stderr = io.Discard

	engine, err := gogosseract.New(ctx, cfg)
	if err != nil {
		return Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR could not initialize."}
	}
	p.engine = engine
	return nil
}

func (p *builtinEngineProvider) resetEngineLocked() {
	if p.engine == nil {
		return
	}
	_ = p.engine.Close(context.Background())
	p.engine = nil
}

type hocrWord struct {
	text       string
	confidence int
}

// parseHOCR extracts text and computes a character-weighted mean of Tesseract's
// word confidence values. A nil confidence means no words were recognized.
func parseHOCR(value string) (string, *float64, error) {
	decoder := xml.NewDecoder(strings.NewReader(value))
	var (
		elementClasses []string
		currentWord    strings.Builder
		currentScore   = -1
		lineWords      []hocrWord
		lines          []string
		scoreSum       int
		scoreWeight    int
	)
	flushLine := func() {
		if len(lineWords) == 0 {
			return
		}
		words := make([]string, 0, len(lineWords))
		for _, word := range lineWords {
			words = append(words, word.text)
		}
		if line := joinOCRWords(words); line != "" {
			lines = append(lines, line)
		}
		lineWords = lineWords[:0]
	}

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			class, title := hocrAttributes(typed.Attr)
			elementClasses = append(elementClasses, class)
			switch class {
			case "ocr_line":
				flushLine()
			case "ocrx_word":
				currentWord.Reset()
				currentScore = parseWordConfidence(title)
			}
		case xml.CharData:
			if containsClass(elementClasses, "ocrx_word") {
				currentWord.Write([]byte(typed))
			}
		case xml.EndElement:
			if len(elementClasses) == 0 {
				continue
			}
			class := elementClasses[len(elementClasses)-1]
			elementClasses = elementClasses[:len(elementClasses)-1]
			switch class {
			case "ocrx_word":
				word := strings.TrimSpace(currentWord.String())
				if word != "" {
					lineWords = append(lineWords, hocrWord{text: word, confidence: currentScore})
					if currentScore >= 0 {
						weight := utf8.RuneCountInString(word)
						if weight < 1 {
							weight = 1
						}
						scoreSum += currentScore * weight
						scoreWeight += weight
					}
				}
				currentWord.Reset()
				currentScore = -1
			case "ocr_line":
				flushLine()
			}
		}
	}
	flushLine()

	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if scoreWeight == 0 {
		return text, nil, nil
	}
	confidence := float64(scoreSum) / float64(scoreWeight) / 100
	return text, &confidence, nil
}

func hocrAttributes(attributes []xml.Attr) (class, title string) {
	for _, attribute := range attributes {
		switch attribute.Name.Local {
		case "class":
			class = attribute.Value
		case "title":
			title = attribute.Value
		}
	}
	return class, title
}

func parseWordConfidence(title string) int {
	fields := strings.Fields(title)
	for index, field := range fields {
		if field != "x_wconf" || index+1 >= len(fields) {
			continue
		}
		raw := strings.TrimRight(fields[index+1], ";")
		value, err := strconv.Atoi(raw)
		if err == nil && value >= 0 && value <= 100 {
			return value
		}
	}
	return -1
}

func containsClass(classes []string, expected string) bool {
	for index := len(classes) - 1; index >= 0; index-- {
		if classes[index] == expected {
			return true
		}
	}
	return false
}

func joinOCRWords(words []string) string {
	var builder strings.Builder
	var previous rune
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		first, _ := utf8.DecodeRuneInString(word)
		if builder.Len() > 0 && shouldSeparateOCRWords(previous, first) {
			builder.WriteByte(' ')
		}
		builder.WriteString(word)
		previous, _ = utf8.DecodeLastRuneInString(word)
	}
	return builder.String()
}

func shouldSeparateOCRWords(previous, next rune) bool {
	if isCJK(previous) || isCJK(next) {
		return false
	}
	if strings.ContainsRune(",.;:!?)]}、。；：！？", next) {
		return false
	}
	if strings.ContainsRune("([{", previous) {
		return false
	}
	return true
}

func isCJK(value rune) bool {
	return unicode.In(value, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
