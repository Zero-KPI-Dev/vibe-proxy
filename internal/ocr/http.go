package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

const defaultMaxResponseBytes = 2 << 20

type HTTPOptions struct {
	Endpoint         string
	Auth             upstreamauth.Profile
	Client           *http.Client
	MaxResponseBytes int64
}

type HTTPProvider struct {
	endpoint         string
	auth             upstreamauth.Profile
	client           *http.Client
	maxResponseBytes int64
}

func NewHTTPProvider(opts HTTPOptions) *HTTPProvider {
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = defaultMaxResponseBytes
	}
	return &HTTPProvider{endpoint: opts.Endpoint, auth: opts.Auth, client: opts.Client, maxResponseBytes: opts.MaxResponseBytes}
}

func (p *HTTPProvider) Name() string { return "http" }

func (p *HTTPProvider) Recognize(ctx context.Context, images []Image) ([]Result, error) {
	type requestImage struct {
		Index     int    `json:"index"`
		MediaType string `json:"media_type"`
		Data      string `json:"data_base64"`
	}
	payload := struct {
		Images []requestImage `json:"images"`
	}{Images: make([]requestImage, 0, len(images))}
	allowed := make(map[int]bool, len(images))
	for _, image := range images {
		payload.Images = append(payload.Images, requestImage{Index: image.Index, MediaType: image.MediaType, Data: base64.StdEncoding.EncodeToString(image.Data)})
		allowed[image.Index] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, Error{StatusCode: 500, Code: "ocr_request_failed", Message: "Could not encode the OCR request."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, Error{StatusCode: 500, Code: "ocr_invalid_endpoint", Message: "The OCR endpoint is invalid."}
	}
	req.Header.Set("Content-Type", "application/json")
	if authErr := p.auth.Apply(req); authErr != nil {
		return nil, Error{StatusCode: authErr.StatusCode, Code: "ocr_auth_failed", Message: "OCR authentication is not configured correctly."}
	}
	started := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, Error{StatusCode: 503, Code: "ocr_unavailable", Message: "The OCR service is unavailable."}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, Error{StatusCode: 502, Code: "ocr_upstream_rejected", Message: fmt.Sprintf("The OCR service returned HTTP %d.", resp.StatusCode)}
	}
	if resp.ContentLength > p.maxResponseBytes {
		return nil, Error{StatusCode: 502, Code: "ocr_invalid_response", Message: "The OCR response is too large."}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxResponseBytes+1))
	if err != nil || int64(len(raw)) > p.maxResponseBytes {
		return nil, Error{StatusCode: 502, Code: "ocr_invalid_response", Message: "Could not read the OCR response."}
	}
	var decoded struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, Error{StatusCode: 502, Code: "ocr_invalid_response", Message: "The OCR response is not valid JSON."}
	}
	seen := map[int]bool{}
	for i := range decoded.Results {
		result := &decoded.Results[i]
		if !allowed[result.Index] || seen[result.Index] {
			return nil, Error{StatusCode: 502, Code: "ocr_invalid_response", Message: "The OCR response contains an invalid image index."}
		}
		seen[result.Index] = true
		result.Duration = time.Since(started)
	}
	return decoded.Results, nil
}

func (p *HTTPProvider) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.endpoint, nil)
	if err != nil {
		return err
	}
	if authErr := p.auth.Apply(req); authErr != nil {
		return authErr
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("OCR health returned HTTP %d", resp.StatusCode)
	}
	return nil
}
