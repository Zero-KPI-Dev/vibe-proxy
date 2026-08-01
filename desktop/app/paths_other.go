//go:build !windows && !darwin

package app

// platformPaths preserves Linux test compilation while ResolvePaths returns the
// unsupported-platform error for the current runtime OS.
func platformPaths(goos string, getenv func(string) string, userHome func() (string, error)) (Paths, error) {
	return ResolvePaths(goos, getenv, userHome)
}
