package modelcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const MaxCatalogBytes = 10 << 20

type Options struct {
	SourceURL string
	CachePath string
	Client    *http.Client
}

type Service struct {
	sourceURL string
	cachePath string
	client    *http.Client
	value     atomic.Pointer[Snapshot]
	refreshMu sync.Mutex
}

type cacheEnvelope struct {
	ETag      string          `json:"etag,omitempty"`
	FetchedAt time.Time       `json:"fetched_at"`
	Origin    string          `json:"origin,omitempty"`
	Data      json.RawMessage `json:"data"`
}

func NewService(opts Options) *Service {
	if opts.SourceURL == "" {
		opts.SourceURL = DefaultSourceURL
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 15 * time.Second}
	}
	s := &Service{sourceURL: opts.SourceURL, cachePath: opts.CachePath, client: opts.Client}
	s.value.Store(emptySnapshot(opts.SourceURL))
	_ = s.loadCache()
	return s
}

func (s *Service) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.client = client
	}
}

func (s *Service) Snapshot() *Snapshot { return s.value.Load() }

func (s *Service) State() State { return s.Snapshot().State() }

func (s *Service) Lookup(providerHint, vibeProviderID, baseURL, modelID string) Match {
	return s.Snapshot().Lookup(providerHint, vibeProviderID, baseURL, modelID)
}

func (s *Service) Ensure(ctx context.Context, maxAge time.Duration) error {
	state := s.State()
	if state.Models > 0 && maxAge > 0 && time.Since(state.FetchedAt) < maxAge {
		return nil
	}
	return s.Refresh(ctx)
}

func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	current := s.Snapshot()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sourceURL, nil)
	if err != nil {
		return s.markStale(err)
	}
	if current.state.ETag != "" {
		req.Header.Set("If-None-Match", current.state.ETag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return s.markStale(fmt.Errorf("fetch models.dev catalog: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		if len(current.raw) == 0 {
			return s.markStale(fmt.Errorf("models.dev returned 304 without a local cache"))
		}
		state := current.state
		state.FetchedAt = time.Now().UTC()
		state.Origin = "remote"
		state.Stale = false
		state.Error = ""
		next, err := parseSnapshot(current.raw, state)
		if err != nil {
			return s.markStale(err)
		}
		s.value.Store(next)
		return s.saveCache(next)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return s.markStale(fmt.Errorf("fetch models.dev catalog: HTTP %d", resp.StatusCode))
	}
	if resp.ContentLength > MaxCatalogBytes {
		return s.markStale(fmt.Errorf("fetch models.dev catalog: response too large"))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxCatalogBytes+1))
	if err != nil {
		return s.markStale(fmt.Errorf("fetch models.dev catalog: %w", err))
	}
	if len(raw) > MaxCatalogBytes {
		return s.markStale(fmt.Errorf("fetch models.dev catalog: response too large"))
	}
	state := State{
		SourceURL: s.sourceURL,
		ETag:      resp.Header.Get("ETag"),
		FetchedAt: time.Now().UTC(),
		Origin:    "remote",
	}
	next, err := parseSnapshot(raw, state)
	if err != nil {
		return s.markStale(err)
	}
	s.value.Store(next)
	return s.saveCache(next)
}

// Import replaces the in-memory and on-disk catalog with a validated models.dev
// api.json payload. It supports fully offline environments without changing the
// source URL used by future online refreshes.
func (s *Service) Import(reader io.Reader) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	raw, err := io.ReadAll(io.LimitReader(reader, MaxCatalogBytes+1))
	if err != nil {
		return fmt.Errorf("read imported models.dev catalog: %w", err)
	}
	if len(raw) > MaxCatalogBytes {
		return fmt.Errorf("import models.dev catalog: file too large")
	}
	state := State{
		SourceURL: s.sourceURL,
		FetchedAt: time.Now().UTC(),
		Origin:    "upload",
	}
	next, err := parseSnapshot(raw, state)
	if err != nil {
		return err
	}
	if err := s.saveCache(next); err != nil {
		return fmt.Errorf("save imported models.dev catalog: %w", err)
	}
	s.value.Store(next)
	return nil
}

func (s *Service) markStale(err error) error {
	current := s.Snapshot()
	state := current.state
	state.SourceURL = s.sourceURL
	state.Stale = true
	state.Error = err.Error()
	next := &Snapshot{Providers: current.Providers, byModel: current.byModel, state: state, raw: current.raw}
	s.value.Store(next)
	return err
}

func (s *Service) loadCache() error {
	if s.cachePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return s.markStale(fmt.Errorf("read model catalog cache: %w", err))
	}
	if len(raw) > MaxCatalogBytes+1<<20 {
		return s.markStale(fmt.Errorf("read model catalog cache: file too large"))
	}
	var envelope cacheEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return s.markStale(fmt.Errorf("read model catalog cache: %w", err))
	}
	origin := envelope.Origin
	if origin == "" {
		origin = "cache"
	}
	state := State{SourceURL: s.sourceURL, ETag: envelope.ETag, FetchedAt: envelope.FetchedAt, Origin: origin, Stale: true}
	next, err := parseSnapshot(envelope.Data, state)
	if err != nil {
		return s.markStale(err)
	}
	next.state.Stale = true
	s.value.Store(next)
	return nil
}

func (s *Service) saveCache(snapshot *Snapshot) error {
	if s.cachePath == "" || len(snapshot.raw) == 0 {
		return nil
	}
	envelope := cacheEnvelope{ETag: snapshot.state.ETag, FetchedAt: snapshot.state.FetchedAt, Origin: snapshot.state.Origin, Data: json.RawMessage(snapshot.raw)}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	if err := encoder.Encode(envelope); err != nil {
		return fmt.Errorf("encode model catalog cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0700); err != nil {
		return fmt.Errorf("create model catalog cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.cachePath), ".models-dev-*.tmp")
	if err != nil {
		return fmt.Errorf("create model catalog cache: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("write model catalog cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync model catalog cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.cachePath); err != nil {
		return fmt.Errorf("replace model catalog cache: %w", err)
	}
	return nil
}
