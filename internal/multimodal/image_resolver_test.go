package multimodal

import (
	"encoding/base64"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

var onePixelPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}

func TestResolveImagesFromDataURLAndBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{
		{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}},
		{Type: ir.ContentImage, Image: &ir.ImageContent{MediaType: "image/png", Base64: encoded}},
	}}}}
	images, err := ResolveImages(req, ImageLimits{MaxImages: 4, MaxImageBytes: 1024, MaxTotalImageBytes: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0].MediaType != "image/png" || images[0].SHA256 == "" || images[0].SHA256 != images[1].SHA256 {
		t.Fatalf("unexpected images: %+v", images)
	}
}

func TestResolveImagesRejectsRemoteAndMIMEConflict(t *testing.T) {
	remote := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "https://example.com/a.png"}}}}}}
	if _, err := ResolveImages(remote, ImageLimits{}); gatewayCode(err) != "ocr_remote_image_disabled" {
		t.Fatalf("unexpected remote error: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	conflict := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/jpeg;base64," + encoded}}}}}}
	if _, err := ResolveImages(conflict, ImageLimits{}); gatewayCode(err) != "ocr_invalid_image" {
		t.Fatalf("unexpected MIME error: %v", err)
	}
}

func TestResolveImagesEnforcesLimits(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(onePixelPNG)
	req := &ir.Request{Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.ContentImage, Image: &ir.ImageContent{URL: "data:image/png;base64," + encoded}}}}}}
	if _, err := ResolveImages(req, ImageLimits{MaxImageBytes: 4}); gatewayCode(err) != "ocr_image_limit_exceeded" {
		t.Fatalf("unexpected limit error: %v", err)
	}
}

func gatewayCode(err error) string {
	if value, ok := err.(ir.GatewayError); ok {
		return value.Code
	}
	return ""
}
