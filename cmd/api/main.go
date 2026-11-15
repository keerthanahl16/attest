package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/keerthanahl16/attest/internal/api"
	"github.com/keerthanahl16/attest/internal/auth"
	"github.com/keerthanahl16/attest/internal/config"
	"github.com/keerthanahl16/attest/internal/db"
	"github.com/keerthanahl16/attest/internal/seed"
	"github.com/keerthanahl16/attest/internal/store"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	st := store.New(pool)
	if cfg.SeedDemo {
		if _, err := seed.EnsureDemo(ctx, st, log); err != nil {
			log.Error("seed failed", "err", err)
			os.Exit(1)
		}
	}

	srv := &api.Server{
		Cfg:    cfg,
		Store:  st,
		Tokens: auth.NewTokenService(cfg.JWTSecret, cfg.AccessTokenTTL),
		Log:    log,
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("api stopped", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
