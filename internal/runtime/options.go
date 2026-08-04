package runtime

import (
	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

// Options configures optional runtime behavior.
type Options struct {
	AdminTokenOverride string
	DesktopSessions    *auth.DesktopSessionStore
	PasswordAuth       *auth.PasswordAuth
	DesktopController  desktopbridge.Controller
	OnConfigApplied    func(*config.RuntimeConfig)
}
