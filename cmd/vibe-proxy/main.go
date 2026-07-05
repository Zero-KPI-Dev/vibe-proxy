package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/runtime"
	"github.com/a448582655/vibe-proxy/internal/store"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "configuration file path")
	flag.Parse()
	cfg, err := config.LoadRuntime(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_ = db.Retain(cfg.Storage.RetentionDays)
	prom := metrics.New()
	recent := telemetry.NewRecentStore(200)
	sink := metrics.MultiSink{db, prom, recent}
	app := runtime.New(*cfgPath, cfg, sink, prom)
	srv := &http.Server{Addr: cfg.Server.Listen, Handler: app.Routes(), ReadTimeout: cfg.Server.ReadTimeout.Duration, WriteTimeout: cfg.Server.WriteTimeout.Duration, IdleTimeout: cfg.Server.IdleTimeout.Duration}
	if srv.ReadTimeout == 0 {
		srv.ReadTimeout = 30 * time.Second
	}
	if srv.IdleTimeout == 0 {
		srv.IdleTimeout = 120 * time.Second
	}
	log.Printf("vibe-proxy listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
