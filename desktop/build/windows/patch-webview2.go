package main

import (
	"fmt"
	"os"
	"strings"
)

const webview2BootstrapCommand = `ExecWait '"$pluginsdir\webview2bootstrapper\MicrosoftEdgeWebview2Setup.exe" /silent /install'`

const webview2BootstrapFailureHandling = ` $1
    ${If} $1 != 0
        MessageBox MB_OK|MB_ICONSTOP "Microsoft Edge WebView2 Runtime could not be installed. Connect to the internet or install the Evergreen Standalone Runtime manually, then run this installer again."
        Abort
    ${EndIf}`

func patchWebView2InstallerScript(contents []byte) ([]byte, error) {
	text := string(contents)
	if strings.Contains(text, webview2BootstrapCommand+" $1") {
		return contents, nil
	}
	if !strings.Contains(text, webview2BootstrapCommand) {
		return nil, fmt.Errorf("WebView2 bootstrapper command not found")
	}
	return []byte(strings.Replace(text, webview2BootstrapCommand, webview2BootstrapCommand+webview2BootstrapFailureHandling, 1)), nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: patch-webview2.go path/to/wails_tools.nsh")
		os.Exit(2)
	}
	path := os.Args[1]
	contents, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	patched, err := patchWebView2InstallerScript(contents)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, patched, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
