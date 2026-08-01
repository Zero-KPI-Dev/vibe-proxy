//go:build !windows

package app

import "os"

// replaceFile atomically replaces a same-filesystem target on Unix.
func replaceFile(temporaryPath, path string) error {
	return os.Rename(temporaryPath, path)
}
