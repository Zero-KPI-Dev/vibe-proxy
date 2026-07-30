package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("vibe-proxy %s (commit %s, built %s)", Version, Commit, BuildDate)
}
