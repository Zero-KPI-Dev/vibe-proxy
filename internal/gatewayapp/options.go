package gatewayapp

import (
	"net"
	"time"

	"github.com/a448582655/vibe-proxy/internal/runtime"
)

// Options configures a gateway application lifecycle.
type Options struct {
	ConfigPath             string
	Runtime                runtime.Options
	Listen                 func(network, address string) (net.Listener, error)
	RetentionSweepInterval time.Duration
}
