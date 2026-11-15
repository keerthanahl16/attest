package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/keerthanahl16/attest/internal/config"
	"github.com/keerthanahl16/attest/internal/db"
	"github.com/keerthanahl16/attest/internal/store"
	"github.com/keerthanahl16/attest/internal/worker"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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

	w := &worker.Worker{
		Store:  store.New(pool),
		Lease:  cfg.WorkerLease,
		Poll:   cfg.WorkerPollInterval,
		Logger: log,
	}
	w.Run(ctx)
}
