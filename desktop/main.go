//go:build windows || darwin

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/a448582655/vibe-proxy/desktop/wailsapp"
	"github.com/a448582655/vibe-proxy/internal/buildinfo"
	"github.com/a448582655/vibe-proxy/internal/ocr"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == ocr.BuiltinWorkerArgument {
		if err := ocr.RunBuiltinWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if slices.Contains(os.Args[1:], "--version") {
		fmt.Fprintln(os.Stdout, buildinfo.String())
		return
	}
	if err := wailsapp.Run(context.Background()); err != nil {
		log.Print(err)
	}
}
