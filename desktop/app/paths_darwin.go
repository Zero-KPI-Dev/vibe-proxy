//go:build darwin

package app

func platformPaths(goos string, getenv func(string) string, userHome func() (string, error)) (Paths, error) {
	return ResolvePaths(goos, getenv, userHome)
}
