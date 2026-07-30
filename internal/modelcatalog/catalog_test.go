package modelcatalog

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const catalogFixture = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-vision": {
        "id": "gpt-vision",
        "name": "GPT Vision",
        "attachment": true,
        "reasoning": true,
        "tool_call": true,
        "structured_output": true,
        "temperature": true,
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 128000, "input": 100000, "output": 16000}
      },
      "shared-text": {
        "id": "shared-text",
        "name": "Shared Text",
        "attachment": false,
        "reasoning": false,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 32000, "output": 4000}
      },
      "conflict": {
        "id": "conflict",
        "name": "Conflict Vision",
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 32000, "output": 4000}
      }
    }
  },
  "wrapper": {
    "id": "wrapper",
    "name": "Wrapper",
    "api": "https://wrapper.example/v1",
    "models": {
      "shared-text": {
        "id": "shared-text",
        "name": "Shared Text Wrapper",
        "attachment": false,
        "reasoning": false,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 64000, "output": 4000}
      },
      "conflict": {
        "id": "conflict",
        "name": "Conflict Text",
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 32000, "output": 4000}
      },
      "wrapper-only": {
        "id": "wrapper-only",
        "name": "Wrapper Only",
        "reasoning": true,
        "tool_call": false,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 16000, "output": 2000}
      }
    }
  }
}`

func fixtureSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	snapshot, err := parseSnapshot([]byte(catalogFixture), State{SourceURL: DefaultSourceURL, FetchedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestLookupProviderPrefixedUniqueConsensusAndAmbiguous(t *testing.T) {
	snapshot := fixtureSnapshot(t)

	tests := []struct {
		name        string
		provider    string
		vibeID      string
		baseURL     string
		model       string
		status      MatchStatus
		imageInput  SupportState
		providerOut string
	}{
		{name: "explicit provider", provider: "openai", model: "gpt-vision", status: MatchExactProvider, imageInput: SupportSupported, providerOut: "openai"},
		{name: "prefixed model", model: "openai/gpt-vision", status: MatchExactPrefixed, imageInput: SupportSupported, providerOut: "openai"},
		{name: "vibe provider id", vibeID: "wrapper", model: "shared-text", status: MatchExactProvider, imageInput: SupportUnsupported, providerOut: "wrapper"},
		{name: "base url inference", baseURL: "https://wrapper.example/v1/", model: "wrapper-only", status: MatchExactProvider, imageInput: SupportUnsupported, providerOut: "wrapper"},
		{name: "unique global", vibeID: "newapi", model: "wrapper-only", status: MatchExactUnique, imageInput: SupportUnsupported, providerOut: "wrapper"},
		{name: "consensus global", vibeID: "newapi", model: "shared-text", status: MatchConsensus, imageInput: SupportUnsupported},
		{name: "ambiguous global", vibeID: "newapi", model: "conflict", status: MatchAmbiguous, imageInput: SupportUnknown},
		{name: "missing", model: "private-model", status: MatchNotFound, imageInput: SupportUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			match := snapshot.Lookup(tc.provider, tc.vibeID, tc.baseURL, tc.model)
			if match.Status != tc.status || match.ImageInput != tc.imageInput {
				t.Fatalf("got status=%s image=%s; want status=%s image=%s", match.Status, match.ImageInput, tc.status, tc.imageInput)
			}
			if tc.providerOut != "" {
				if match.Model == nil || match.Model.ProviderID != tc.providerOut {
					t.Fatalf("unexpected model: %#v", match.Model)
				}
			}
		})
	}
}

func TestConsensusDropsConflictingNonRoutingMetadata(t *testing.T) {
	match := fixtureSnapshot(t).Lookup("", "newapi", "", "shared-text")
	if match.Status != MatchConsensus || match.Model == nil {
		t.Fatalf("unexpected match: %#v", match)
	}
	if match.Model.ContextLimit != nil {
		t.Fatalf("conflicting context limit should be omitted: %#v", match.Model.ContextLimit)
	}
	if match.Model.ToolCall == nil || !*match.Model.ToolCall {
		t.Fatalf("matching tool capability should be retained: %#v", match.Model.ToolCall)
	}
}

func TestServiceRefreshUsesETagAndDiskCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "models-dev.json")
	requests := 0
	client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if got := req.Header.Get("If-None-Match"); got != "" {
				t.Fatalf("first request sent etag %q", got)
			}
			return response(http.StatusOK, catalogFixture, map[string]string{"ETag": `"fixture-v1"`}), nil
		}
		if got := req.Header.Get("If-None-Match"); got != `"fixture-v1"` {
			t.Fatalf("second request etag=%q", got)
		}
		return response(http.StatusNotModified, "", nil), nil
	})}

	service := NewService(Options{SourceURL: "https://models.dev/api.json", CachePath: cachePath, Client: client})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstFetchedAt := service.State().FetchedAt
	if service.State().Models != 6 || service.State().ETag != `"fixture-v1"` {
		t.Fatalf("unexpected state: %#v", service.State())
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !service.State().FetchedAt.After(firstFetchedAt) || service.State().Stale {
		t.Fatalf("304 did not refresh state: %#v", service.State())
	}

	offline := NewService(Options{SourceURL: "https://models.dev/api.json", CachePath: cachePath, Client: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}})
	if offline.State().Models != 6 || !offline.State().Stale {
		t.Fatalf("disk cache not loaded as stale: %#v", offline.State())
	}
	match := offline.Lookup("openai", "", "", "gpt-vision")
	if match.ImageInput != SupportSupported {
		t.Fatalf("offline lookup failed: %#v", match)
	}
}

func TestServiceRefreshKeepsStaleCatalogOnFailure(t *testing.T) {
	responses := 0
	service := NewService(Options{Client: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		responses++
		if responses == 1 {
			return response(http.StatusOK, catalogFixture, nil), nil
		}
		return nil, context.DeadlineExceeded
	})}})
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("expected refresh failure")
	}
	if service.State().Models != 6 || !service.State().Stale || service.State().Error == "" {
		t.Fatalf("stale state lost catalog: %#v", service.State())
	}
}

func TestServiceImportsOfflineCatalogAndPersistsOrigin(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "models-dev.json")
	service := NewService(Options{CachePath: cachePath})
	if err := service.Import(strings.NewReader(catalogFixture)); err != nil {
		t.Fatal(err)
	}
	if state := service.State(); state.Models != 6 || state.Providers != 2 || state.Origin != "upload" || state.Stale {
		t.Fatalf("unexpected imported state: %#v", state)
	}
	if match := service.Lookup("openai", "", "", "gpt-vision"); match.ImageInput != SupportSupported {
		t.Fatalf("imported catalog lookup failed: %#v", match)
	}

	reloaded := NewService(Options{CachePath: cachePath})
	if state := reloaded.State(); state.Models != 6 || state.Origin != "upload" || !state.Stale {
		t.Fatalf("import origin was not preserved through disk cache: %#v", state)
	}
}

func TestServiceRejectsInvalidImportWithoutReplacingCatalog(t *testing.T) {
	service := NewService(Options{})
	if err := service.Import(strings.NewReader(catalogFixture)); err != nil {
		t.Fatal(err)
	}
	if err := service.Import(strings.NewReader(`{"broken":`)); err == nil {
		t.Fatal("expected invalid import error")
	}
	if state := service.State(); state.Models != 6 || state.Origin != "upload" {
		t.Fatalf("invalid import replaced the usable catalog: %#v", state)
	}
}

func TestServiceRejectsOversizedCatalog(t *testing.T) {
	service := NewService(Options{Client: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		body := io.NopCloser(io.MultiReader(bytes.NewReader([]byte(`{"provider":{"models":{}}}`)), strings.NewReader(strings.Repeat(" ", MaxCatalogBytes))))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, ContentLength: -1}, nil
	})}})
	if err := service.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func response(status int, body string, headers map[string]string) *http.Response {
	header := make(http.Header)
	for key, value := range headers {
		header.Set(key, value)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
