package gatewayapp

import (
	"fmt"
	"time"
)

// State describes the gateway lifecycle state.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// Status is an immutable snapshot of gateway lifecycle state.
type Status struct {
	State     State
	Address   string
	StartedAt time.Time
	LastError string
}

// StartupError identifies the startup stage that failed.
type StartupError struct {
	Stage   string
	Address string
	Err     error
}

func (e *StartupError) Error() string {
	switch e.Stage {
	case "config":
		return fmt.Sprintf("load config: %v", e.Err)
	case "validate":
		return fmt.Sprintf("invalid config: %v", e.Err)
	case "database":
		return fmt.Sprintf("open sqlite: %v", e.Err)
	case "listen":
		return fmt.Sprintf("listen %s: %v", e.Address, e.Err)
	default:
		return fmt.Sprintf("gateway startup %s: %v", e.Stage, e.Err)
	}
}

func (e *StartupError) Unwrap() error { return e.Err }
