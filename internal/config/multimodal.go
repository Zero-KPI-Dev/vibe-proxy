package config

import (
	"time"

	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

type MultimodalConfig struct {
	Enabled             bool      `yaml:"enabled" json:"enabled"`
	Strategy            string    `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	OCR                 OCRConfig `yaml:"ocr,omitempty" json:"ocr,omitempty"`
	VisionFallbackModel string    `yaml:"vision_fallback_model,omitempty" json:"vision_fallback_model,omitempty"`
}

type OCRConfig struct {
	Provider             string               `yaml:"provider,omitempty" json:"provider,omitempty"`
	Endpoint             string               `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Timeout              Duration             `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MinConfidence        float64              `yaml:"min_confidence,omitempty" json:"min_confidence,omitempty"`
	MinTextChars         int                  `yaml:"min_text_chars,omitempty" json:"min_text_chars,omitempty"`
	MaxImages            int                  `yaml:"max_images,omitempty" json:"max_images,omitempty"`
	MaxImageBytes        int64                `yaml:"max_image_bytes,omitempty" json:"max_image_bytes,omitempty"`
	MaxTotalImageBytes   int64                `yaml:"max_total_image_bytes,omitempty" json:"max_total_image_bytes,omitempty"`
	MaxTextCharsPerImage int                  `yaml:"max_text_chars_per_image,omitempty" json:"max_text_chars_per_image,omitempty"`
	MaxTextCharsTotal    int                  `yaml:"max_text_chars_total,omitempty" json:"max_text_chars_total,omitempty"`
	RemoteImages         bool                 `yaml:"remote_images,omitempty" json:"remote_images,omitempty"`
	Cache                OCRCacheConfig       `yaml:"cache,omitempty" json:"cache,omitempty"`
	Auth                 upstreamauth.Profile `yaml:"auth,omitempty" json:"-"`
}

type OCRCacheConfig struct {
	Enabled    *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MaxEntries int      `yaml:"max_entries,omitempty" json:"max_entries,omitempty"`
	TTL        Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`
}

func (c OCRCacheConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func applyMultimodalDefaults(cfg *MultimodalConfig) {
	if cfg.Strategy == "" {
		cfg.Strategy = "ocr_then_vision"
	}
	if cfg.OCR.Provider == "" && cfg.OCR.Endpoint != "" {
		cfg.OCR.Provider = "http"
	}
	if cfg.OCR.Timeout.Duration == 0 {
		cfg.OCR.Timeout.Duration = 15 * time.Second
	}
	if cfg.OCR.MinConfidence == 0 {
		cfg.OCR.MinConfidence = 0.55
	}
	if cfg.OCR.MinTextChars == 0 {
		cfg.OCR.MinTextChars = 4
	}
	if cfg.OCR.MaxImages == 0 {
		cfg.OCR.MaxImages = 4
	}
	if cfg.OCR.MaxImageBytes == 0 {
		cfg.OCR.MaxImageBytes = 5 << 20
	}
	if cfg.OCR.MaxTotalImageBytes == 0 {
		cfg.OCR.MaxTotalImageBytes = 12 << 20
	}
	if cfg.OCR.MaxTextCharsPerImage == 0 {
		cfg.OCR.MaxTextCharsPerImage = 8000
	}
	if cfg.OCR.MaxTextCharsTotal == 0 {
		cfg.OCR.MaxTextCharsTotal = 16000
	}
	if cfg.OCR.Cache.MaxEntries == 0 {
		cfg.OCR.Cache.MaxEntries = 256
	}
	if cfg.OCR.Cache.TTL.Duration == 0 {
		cfg.OCR.Cache.TTL.Duration = 24 * time.Hour
	}
}
