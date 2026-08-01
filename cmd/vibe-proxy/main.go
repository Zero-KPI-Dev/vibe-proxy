package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a448582655/vibe-proxy/internal/buildinfo"
	"github.com/a448582655/vibe-proxy/internal/gatewayapp"
	"github.com/a448582655/vibe-proxy/internal/ocr"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == ocr.BuiltinWorkerArgument {
		if err := ocr.RunBuiltinWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	cfgPath := flag.String("config", "configs/config.yaml", "configuration file path")
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *version {
		fmt.Fprintln(os.Stdout, buildinfo.String())
		return
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := gatewayapp.Start(signalCtx, gatewayapp.Options{ConfigPath: *cfgPath})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("vibe-proxy listening on %s", app.Address())

	select {
	case <-signalCtx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	case <-app.Done():
		if err := app.Wait(); err != nil {
			log.Fatal(err)
		}
	}
}
