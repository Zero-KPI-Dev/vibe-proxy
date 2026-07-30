package runtime

import (
	"github.com/a448582655/vibe-proxy/internal/auth"
	"github.com/a448582655/vibe-proxy/internal/desktopbridge"
)

// Options configures optional runtime behavior.
type Options struct {
	AdminTokenOverride string
	DesktopSessions    *auth.DesktopSessionStore
	DesktopController  desktopbridge.Controller
}
