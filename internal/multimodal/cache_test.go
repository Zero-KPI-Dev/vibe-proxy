package multimodal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ocr"
)

func TestOCRCacheHitAndSingleflight(t *testing.T) {
	cache := NewOCRCache(4, time.Minute)
	var calls atomic.Int32
	compute := func() (ocr.Result, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return ocr.Result{Text: "ok"}, nil
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, _, err := cache.Do(context.Background(), "same", compute)
			if err != nil || result.Text != "ok" {
				t.Errorf("unexpected result: %+v %v", result, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("compute calls = %d, want 1", calls.Load())
	}
	_, hit, err := cache.Do(context.Background(), "same", compute)
	if err != nil || !hit || calls.Load() != 1 {
		t.Fatalf("expected cache hit: hit=%v calls=%d err=%v", hit, calls.Load(), err)
	}
}
