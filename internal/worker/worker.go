// Package worker claims Postgres jobs and executes tool steps with retry/backoff.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/keerthanahl16/attest/internal/store"
	"github.com/keerthanahl16/attest/internal/tools"
)

// Worker polls for claimed jobs and executes mock tools.
type Worker struct {
	Store  *store.Store
	ID     string
	Lease  time.Duration
	Poll   time.Duration
	Logger *slog.Logger
}

// Run blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w.ID == "" {
		w.ID = "worker-" + uuid.NewString()[:8]
	}
	if w.Logger == nil {
		w.Logger = slog.Default()
	}
	ticker := time.NewTicker(w.Poll)
	defer ticker.Stop()

	w.Logger.Info("worker started", "worker_id", w.ID)
	for {
		if err := w.tick(ctx); err != nil {
			w.Logger.Error("worker tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			w.Logger.Info("worker stopping")
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) tick(ctx context.Context) error {
	job, err := w.Store.ClaimJob(ctx, w.ID, w.Lease)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}
	return w.execute(ctx, job)
}

func (w *Worker) execute(ctx context.Context, job *store.Job) error {
	c, err := w.Store.LoadStepForExecution(ctx, job.StepID)
	if err != nil {
		return err
	}
	w.Logger.Info("executing step",
		"job_id", job.ID,
		"step_id", job.StepID,
		"tool", c.Tool.Name,
		"attempt", job.Attempts,
	)

	output, execErr := tools.Execute(c.Tool.MockHandler, c.Step.InputJSON)
	if execErr != nil {
		backoff := time.Duration(job.Attempts*job.Attempts) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		return w.Store.MarkStepFailed(ctx, job.ID, job.StepID, c.RunID, c.WorkspaceID, job.Attempts, 5, execErr.Error(), backoff)
	}
	return w.Store.MarkStepSucceeded(ctx, job.ID, job.StepID, c.RunID, c.WorkspaceID, job.Attempts, output)
}
