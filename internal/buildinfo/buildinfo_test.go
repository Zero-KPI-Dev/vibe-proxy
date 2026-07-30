package buildinfo

import "testing"

func TestStringUsesDevelopmentDefaults(t *testing.T) {
	got := String()
	want := "vibe-proxy dev (commit unknown, built unknown)"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringUsesInjectedValues(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = oldVersion, oldCommit, oldDate
	})

	Version = "v0.1.0-rc.1"
	Commit = "9a4caf7"
	BuildDate = "2026-07-30T12:00:00Z"

	got := String()
	want := "vibe-proxy v0.1.0-rc.1 (commit 9a4caf7, built 2026-07-30T12:00:00Z)"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
