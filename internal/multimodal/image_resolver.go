package multimodal

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/a448582655/vibe-proxy/internal/ir"
	"github.com/a448582655/vibe-proxy/internal/ocr"
)

type ImageLimits struct {
	MaxImages          int
	MaxImageBytes      int64
	MaxTotalImageBytes int64
	RemoteImages       bool
}

func ResolveImages(req *ir.Request, limits ImageLimits) ([]ocr.Image, error) {
	images := []ocr.Image{}
	var total int64
	if req == nil {
		return images, nil
	}
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == ir.ContentImage {
				if message.Role != ir.RoleUser {
					return nil, imageError("ocr_invalid_image", "OCR fallback only accepts images in user messages.")
				}
				image, err := resolveImage(len(images), block.Image, limits)
				if err != nil {
					return nil, err
				}
				images = append(images, image)
				total += int64(len(image.Data))
			}
			if block.ToolResult != nil {
				for _, nested := range block.ToolResult.Content {
					if nested.Type == ir.ContentImage {
						return nil, imageError("ocr_invalid_image", "OCR fallback does not accept images inside tool results yet.")
					}
				}
			}
			if limits.MaxImages > 0 && len(images) > limits.MaxImages {
				return nil, imageError("ocr_image_limit_exceeded", "The request contains too many images for OCR fallback.")
			}
			if limits.MaxTotalImageBytes > 0 && total > limits.MaxTotalImageBytes {
				return nil, imageError("ocr_image_limit_exceeded", "The total image size exceeds the OCR fallback limit.")
			}
		}
	}
	return images, nil
}

func resolveImage(index int, input *ir.ImageContent, limits ImageLimits) (ocr.Image, error) {
	if input == nil {
		return ocr.Image{}, imageError("ocr_invalid_image", "Image content is missing.")
	}
	mediaType := normalizeMediaType(input.MediaType)
	encoded := input.Base64
	if input.URL != "" {
		if !strings.HasPrefix(strings.ToLower(input.URL), "data:") {
			if !limits.RemoteImages {
				return ocr.Image{}, imageError("ocr_remote_image_disabled", "Remote image URLs are disabled for OCR fallback.")
			}
			return ocr.Image{}, imageError("ocr_remote_image_disabled", "Remote image fetching is not available in this release.")
		}
		var err error
		mediaType, encoded, err = parseDataURL(input.URL)
		if err != nil {
			return ocr.Image{}, err
		}
	}
	if encoded == "" || mediaType == "" {
		return ocr.Image{}, imageError("ocr_invalid_image", "Image MIME type and base64 data are required.")
	}
	if !allowedImageMediaType(mediaType) {
		return ocr.Image{}, imageError("ocr_invalid_image", "The image MIME type is not supported for OCR fallback.")
	}
	if limits.MaxImageBytes > 0 && int64(len(encoded)) > (limits.MaxImageBytes*4/3)+8 {
		return ocr.Image{}, imageError("ocr_image_limit_exceeded", "An image exceeds the OCR fallback size limit.")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return ocr.Image{}, imageError("ocr_invalid_image", "Image data is not valid base64.")
	}
	if limits.MaxImageBytes > 0 && int64(len(data)) > limits.MaxImageBytes {
		return ocr.Image{}, imageError("ocr_image_limit_exceeded", "An image exceeds the OCR fallback size limit.")
	}
	detected := normalizeMediaType(http.DetectContentType(data))
	if !mediaTypesCompatible(mediaType, detected) {
		return ocr.Image{}, imageError("ocr_invalid_image", fmt.Sprintf("Image data does not match declared MIME type %s.", mediaType))
	}
	hash := sha256.Sum256(data)
	return ocr.Image{Index: index, MediaType: mediaType, Data: data, SHA256: hex.EncodeToString(hash[:])}, nil
}

func parseDataURL(value string) (string, string, error) {
	comma := strings.IndexByte(value, ',')
	if comma <= len("data:") {
		return "", "", imageError("ocr_invalid_image", "Image data URL is invalid.")
	}
	metadata := value[len("data:"):comma]
	parts := strings.Split(metadata, ";")
	if len(parts) != 2 || !strings.EqualFold(parts[1], "base64") {
		return "", "", imageError("ocr_invalid_image", "Only base64 image data URLs are supported.")
	}
	return normalizeMediaType(parts[0]), value[comma+1:], nil
}

func normalizeMediaType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}

func allowedImageMediaType(value string) bool {
	switch value {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func mediaTypesCompatible(declared, detected string) bool {
	return normalizeMediaType(declared) == normalizeMediaType(detected)
}

func imageError(code, message string) ir.GatewayError {
	return ir.GatewayError{StatusCode: 400, Kind: "multimodal_error", Code: code, Message: message}
}
