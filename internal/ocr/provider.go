package ocr

import (
	"context"
	"time"
)

type Image struct {
	Index     int    `json:"index"`
	MediaType string `json:"media_type"`
	Data      []byte `json:"-"`
	SHA256    string `json:"-"`
}

type Result struct {
	Index      int           `json:"index"`
	Text       string        `json:"text"`
	Confidence *float64      `json:"confidence,omitempty"`
	Language   string        `json:"language,omitempty"`
	Duration   time.Duration `json:"-"`
}

type Provider interface {
	Name() string
	Recognize(ctx context.Context, images []Image) ([]Result, error)
	Health(ctx context.Context) error
}

type Error struct {
	StatusCode int
	Code       string
	Message    string
}

func (e Error) Error() string { return e.Message }
