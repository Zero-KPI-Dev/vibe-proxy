package registry

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
)

type Snapshot struct {
	LoadedAt     time.Time
	ClientKeys   []config.ClientKeyConfig
	Channels     map[string]config.ChannelConfig
	Routes       []CompiledRoute
	AdminToken   string
	MasterKeyEnv string
}

type CompiledRoute struct {
	Config config.ModelRoute
	Regex  *regexp.Regexp
}

type Store struct{ value atomic.Value }

func NewStore(s *Snapshot) *Store {
	st := &Store{}
	st.value.Store(s)
	return st
}

func (s *Store) Current() *Snapshot  { return s.value.Load().(*Snapshot) }
func (s *Store) Swap(next *Snapshot) { s.value.Store(next) }

func BuildSnapshot(cfg *config.Config, adminToken string) (*Snapshot, error) {
	channels := make(map[string]config.ChannelConfig, len(cfg.Channels))
	for _, ch := range cfg.Channels {
		if ch.Weight <= 0 {
			ch.Weight = 1
		}
		if ch.MaxConcurrency <= 0 {
			ch.MaxConcurrency = 32
		}
		if ch.Timeout.Duration == 0 {
			ch.Timeout.Duration = 120 * time.Second
		}
		channels[ch.ID] = ch
	}
	routes := make([]CompiledRoute, 0, len(cfg.ModelRoutes))
	for _, r := range cfg.ModelRoutes {
		pattern := wildcardToRegex(r.Match)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		routes = append(routes, CompiledRoute{Config: r, Regex: re})
	}
	return &Snapshot{LoadedAt: time.Now(), ClientKeys: cfg.ClientKeys, Channels: channels, Routes: routes, AdminToken: adminToken, MasterKeyEnv: cfg.Security.MasterKeyEnv}, nil
}

func wildcardToRegex(pattern string) string {
	if strings.HasPrefix(pattern, "regex:") {
		return strings.TrimPrefix(pattern, "regex:")
	}
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, "\\*", ".*")
	return "^" + quoted + "$"
}

func (s *Snapshot) MatchRoute(model string) (config.ModelRoute, bool) {
	for _, r := range s.Routes {
		if r.Regex.MatchString(model) || okGlob(r.Config.Match, model) {
			return r.Config, true
		}
	}
	return config.ModelRoute{}, false
}

func okGlob(pattern, model string) bool {
	ok, err := filepath.Match(pattern, model)
	return err == nil && ok
}
