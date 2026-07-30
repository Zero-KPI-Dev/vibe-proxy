//go:build !windows

package app

import "os"

// replacePreferenceFile atomically replaces a same-filesystem target on Unix.
func replacePreferenceFile(temporaryPath, path string) error {
	return os.Rename(temporaryPath, path)
}
