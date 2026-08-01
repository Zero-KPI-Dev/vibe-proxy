//go:build darwin

package wailsapp

import (
	"fmt"
	"os"
)

func reportNativeStartupFailure(logPath string) {
	if logPath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Vibe Proxy could not open its desktop window. Diagnostics: %s\n", logPath)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "Vibe Proxy could not open its desktop window.")
}
