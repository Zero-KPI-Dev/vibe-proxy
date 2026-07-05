package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/a448582655/vibe-proxy/internal/config"
	"github.com/a448582655/vibe-proxy/internal/metrics"
	"github.com/a448582655/vibe-proxy/internal/proxy"
	"github.com/a448582655/vibe-proxy/internal/registry"
	"github.com/a448582655/vibe-proxy/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "configuration file path")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	adminToken := os.Getenv(cfg.Security.AdminBearerTokenEnv)
	snap, err := registry.BuildSnapshot(cfg, adminToken)
	if err != nil {
		log.Fatalf("build runtime snapshot: %v", err)
	}
	db, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_ = db.Retain(cfg.Storage.RetentionDays)
	prom := metrics.New()
	sink := metrics.MultiSink{db, prom}
	app := proxy.New(*cfgPath, snap, sink, prom.Handler())
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
