package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/storage"
)

type expiredStore interface {
	GetExpired(ctx context.Context) ([]model.ExpiredPaste, error)
	DeleteByID(ctx context.Context, id string) error
}

type PasteCleanup struct {
	cron  *cron.Cron
	repo  expiredStore
	store storage.ObjectStore // nil when archive uploads are disabled
}

func NewPasteCleanup(repo expiredStore, store storage.ObjectStore) *PasteCleanup {
	return &PasteCleanup{
		cron:  cron.New(),
		repo:  repo,
		store: store,
	}
}

func (c *PasteCleanup) Start(interval time.Duration) error {
	spec := "@every " + interval.String()
	if _, err := c.cron.AddFunc(spec, c.run); err != nil {
		return err
	}
	c.cron.Start()
	slog.Info("paste cleanup scheduler started", "interval", interval.String())
	go c.run()
	return nil
}

func (c *PasteCleanup) Stop() {
	ctx := c.cron.Stop()
	<-ctx.Done()
	slog.Info("paste cleanup scheduler stopped")
}

func (c *PasteCleanup) run() {
	slog.Info("paste cleanup: running")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	deleted, failed := c.runOnce(ctx)
	slog.Info("paste cleanup: done", "pastes_deleted", deleted, "pastes_failed", failed)
}

// runOnce removes expired pastes. For archive pastes the stored object is
// deleted first so a failure never orphans a file whose row is already gone;
// on any storage failure the row is kept and retried on the next run.
func (c *PasteCleanup) runOnce(ctx context.Context) (deleted, failed int) {
	expired, err := c.repo.GetExpired(ctx)
	if err != nil {
		slog.Error("paste cleanup: failed to query expired pastes", "error", err)
		return 0, 0
	}

	for _, p := range expired {
		if p.StorageFileID != nil {
			if c.store == nil {
				slog.Warn("paste cleanup: archive storage disabled, keeping expired paste", "id", p.ID)
				failed++
				continue
			}
			if err := c.store.Delete(ctx, *p.StorageFileID); err != nil && !errors.Is(err, storage.ErrNotFound) {
				slog.Warn("paste cleanup: failed to delete stored file, keeping paste", "id", p.ID, "error", err)
				failed++
				continue
			}
		}
		if err := c.repo.DeleteByID(ctx, p.ID); err != nil {
			slog.Warn("paste cleanup: failed to delete paste", "id", p.ID, "error", err)
			failed++
			continue
		}
		deleted++
	}
	return deleted, failed
}
