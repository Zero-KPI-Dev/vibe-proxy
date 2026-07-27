package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

const (
	// BuiltinWorkerArgument is handled before the normal vibe-proxy CLI flags.
	// It is exported so cmd/vibe-proxy can enter the isolated OCR worker.
	BuiltinWorkerArgument = "__vibe_proxy_builtin_ocr_worker"

	maxBuiltinWorkerRequestBytes  = 64 << 20
	maxBuiltinWorkerResponseBytes = 4 << 20
)

// BuiltinProvider invokes the embedded WASM engine in a short-lived copy of
// the current executable. This keeps the installation single-binary while
// ensuring Tesseract's WASM memory is returned to the OS after recognition.
// A one-slot semaphore prevents distinct concurrent images from creating
// multiple high-memory workers.
type BuiltinProvider struct {
	executable    string
	executableErr error
	slot          chan struct{}
}

func NewBuiltinProvider() *BuiltinProvider {
	executable, err := os.Executable()
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	return &BuiltinProvider{executable: executable, executableErr: err, slot: slot}
}

func (p *BuiltinProvider) Name() string { return BuiltinProviderName }

func (p *BuiltinProvider) Health(ctx context.Context) error {
	_, err := p.runWorker(ctx, builtinWorkerRequest{Health: true})
	return err
}

func (p *BuiltinProvider) Recognize(ctx context.Context, images []Image) ([]Result, error) {
	request := builtinWorkerRequest{Images: make([]builtinWorkerImage, 0, len(images))}
	for _, image := range images {
		request.Images = append(request.Images, builtinWorkerImage{
			Index:     image.Index,
			MediaType: image.MediaType,
			Data:      image.Data,
			SHA256:    image.SHA256,
		})
	}
	started := time.Now()
	response, err := p.runWorker(ctx, request)
	if err != nil {
		return nil, err
	}
	duration := time.Since(started)
	for index := range response.Results {
		response.Results[index].Duration = duration
	}
	return response.Results, nil
}

func (p *BuiltinProvider) runWorker(ctx context.Context, request builtinWorkerRequest) (builtinWorkerResponse, error) {
	if p == nil || p.executableErr != nil || p.executable == "" {
		return builtinWorkerResponse{}, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR could not locate the vibe-proxy executable."}
	}
	select {
	case <-ctx.Done():
		return builtinWorkerResponse{}, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR was cancelled before recognition started."}
	case <-p.slot:
	}
	defer func() { p.slot <- struct{}{} }()

	payload, err := json.Marshal(request)
	if err != nil {
		return builtinWorkerResponse{}, Error{StatusCode: 500, Code: "ocr_request_failed", Message: "Built-in OCR could not encode its worker request."}
	}
	command := exec.CommandContext(ctx, p.executable, BuiltinWorkerArgument)
	command.Env = []string{"LANG=C.UTF-8"}
	command.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	runErr := command.Run()
	if ctx.Err() != nil {
		return builtinWorkerResponse{}, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR recognition timed out or was cancelled."}
	}
	if stdout.Len() > maxBuiltinWorkerResponseBytes {
		return builtinWorkerResponse{}, Error{StatusCode: 503, Code: "ocr_invalid_response", Message: "Built-in OCR worker returned too much data."}
	}
	var response builtinWorkerResponse
	decodeErr := json.Unmarshal(stdout.Bytes(), &response)
	if response.Error != nil {
		return builtinWorkerResponse{}, *response.Error
	}
	if runErr != nil || decodeErr != nil {
		return builtinWorkerResponse{}, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR worker did not complete successfully."}
	}
	return response, nil
}

type builtinWorkerImage struct {
	Index     int    `json:"index"`
	MediaType string `json:"media_type"`
	Data      []byte `json:"data_base64"`
	SHA256    string `json:"sha256,omitempty"`
}

type builtinWorkerRequest struct {
	Health bool                 `json:"health,omitempty"`
	Images []builtinWorkerImage `json:"images,omitempty"`
}

type builtinWorkerResponse struct {
	Results []Result `json:"results,omitempty"`
	Error   *Error   `json:"error,omitempty"`
}

// RunBuiltinWorker executes the isolated side of BuiltinProvider. The caller
// must only invoke it after matching BuiltinWorkerArgument, before loading the
// normal runtime configuration or opening SQLite.
func RunBuiltinWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, maxBuiltinWorkerRequestBytes+1))
	var request builtinWorkerRequest
	if err := decoder.Decode(&request); err != nil {
		responseErr := Error{StatusCode: 400, Code: "ocr_invalid_request", Message: "Built-in OCR worker received an invalid request."}
		_ = json.NewEncoder(output).Encode(builtinWorkerResponse{Error: &responseErr})
		return err
	}
	engine := newBuiltinEngineProvider()
	defer engine.Close()

	if request.Health {
		if err := engine.Health(ctx); err != nil {
			return encodeBuiltinWorkerError(output, err)
		}
		return json.NewEncoder(output).Encode(builtinWorkerResponse{})
	}
	images := make([]Image, 0, len(request.Images))
	for _, image := range request.Images {
		images = append(images, Image{
			Index:     image.Index,
			MediaType: image.MediaType,
			Data:      image.Data,
			SHA256:    image.SHA256,
		})
	}
	results, err := engine.Recognize(ctx, images)
	if err != nil {
		return encodeBuiltinWorkerError(output, err)
	}
	return json.NewEncoder(output).Encode(builtinWorkerResponse{Results: results})
}

func encodeBuiltinWorkerError(output io.Writer, err error) error {
	var providerErr Error
	if !errors.As(err, &providerErr) {
		providerErr = Error{StatusCode: 503, Code: "ocr_unavailable", Message: "Built-in OCR worker failed."}
	}
	if encodeErr := json.NewEncoder(output).Encode(builtinWorkerResponse{Error: &providerErr}); encodeErr != nil {
		return encodeErr
	}
	return nil
}
