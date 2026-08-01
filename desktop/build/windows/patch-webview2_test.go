package main

import (
	"strings"
	"testing"
)

func TestPatchWebView2InstallerScript(t *testing.T) {
	input := []byte("\t" + webview2BootstrapCommand + "\n")
	patched, err := patchWebView2InstallerScript(input)
	if err != nil {
		t.Fatalf("patchWebView2InstallerScript() error = %v", err)
	}
	text := string(patched)
	if !strings.Contains(text, webview2BootstrapCommand+" $1") {
		t.Fatalf("patched script does not capture the installer exit code: %q", text)
	}
	if !strings.Contains(text, "Abort") {
		t.Fatalf("patched script does not abort after a failed WebView2 install: %q", text)
	}
}

func TestPatchWebView2InstallerScriptIsIdempotent(t *testing.T) {
	input := []byte(webview2BootstrapCommand + webview2BootstrapFailureHandling)
	patched, err := patchWebView2InstallerScript(input)
	if err != nil {
		t.Fatalf("patchWebView2InstallerScript() error = %v", err)
	}
	if string(patched) != string(input) {
		t.Fatalf("second patch changed the installer script")
	}
}
