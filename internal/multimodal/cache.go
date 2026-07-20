package multimodal

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/a448582655/vibe-proxy/internal/ocr"
)

type cacheEntry struct {
	key       string
	result    ocr.Result
	expiresAt time.Time
}

type cacheCall struct {
	done   chan struct{}
	result ocr.Result
	err    error
}

type OCRCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	entries    map[string]*list.Element
	lru        *list.List
	inflight   map[string]*cacheCall
}

func NewOCRCache(maxEntries int, ttl time.Duration) *OCRCache {
	return &OCRCache{maxEntries: maxEntries, ttl: ttl, entries: map[string]*list.Element{}, lru: list.New(), inflight: map[string]*cacheCall{}}
}

func (c *OCRCache) Do(ctx context.Context, key string, compute func() (ocr.Result, error)) (ocr.Result, bool, error) {
	if c == nil || c.maxEntries <= 0 || c.ttl <= 0 {
		result, err := compute()
		return result, false, err
	}
	c.mu.Lock()
	if element := c.entries[key]; element != nil {
		entry := element.Value.(*cacheEntry)
		if time.Now().Before(entry.expiresAt) {
			c.lru.MoveToFront(element)
			result := entry.result
			c.mu.Unlock()
			return result, true, nil
		}
		c.removeElement(element)
	}
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ocr.Result{}, false, ctx.Err()
		case <-call.done:
			return call.result, call.err == nil, call.err
		}
	}
	call := &cacheCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	result, err := compute()
	c.mu.Lock()
	call.result, call.err = result, err
	delete(c.inflight, key)
	if err == nil {
		entry := &cacheEntry{key: key, result: result, expiresAt: time.Now().Add(c.ttl)}
		c.entries[key] = c.lru.PushFront(entry)
		for c.lru.Len() > c.maxEntries {
			c.removeElement(c.lru.Back())
		}
	}
	close(call.done)
	c.mu.Unlock()
	return result, false, err
}

func (c *OCRCache) removeElement(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*cacheEntry)
	delete(c.entries, entry.key)
	c.lru.Remove(element)
}
