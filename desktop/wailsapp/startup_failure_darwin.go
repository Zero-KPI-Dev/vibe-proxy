//go:build darwin

package wailsapp

import (
	"fmt"
	"os"
)

func reportNativeStartupFailure(failure nativeStartupFailure) {
	_, _ = fmt.Fprintf(os.Stderr, "Vibe Proxy could not open its desktop window: %s\n", displayStartupError(failure.cause))
	if failure.logPath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Diagnostics: %s\n", failure.logPath)
	}
	if failure.logWriteErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Diagnostic write error: %s\n", displayStartupError(failure.logWriteErr))
	}
}
