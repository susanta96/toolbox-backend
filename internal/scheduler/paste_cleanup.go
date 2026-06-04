package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/susanta96/toolbox-backend/internal/repository"
)

type PasteCleanup struct {
	cron *cron.Cron
	repo *repository.PasteRepository
}

func NewPasteCleanup(repo *repository.PasteRepository) *PasteCleanup {
	return &PasteCleanup{
		cron: cron.New(),
		repo: repo,
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ids, err := c.repo.GetExpiredIDs(ctx)
	if err != nil {
		slog.Error("paste cleanup: failed to query expired pastes", "error", err)
		return
	}

	removed := 0
	for _, id := range ids {
		if err := c.repo.DeleteByID(ctx, id); err != nil {
			slog.Warn("paste cleanup: failed to delete paste", "id", id, "error", err)
		} else {
			removed++
		}
	}

	slog.Info("paste cleanup: done", "pastes_deleted", removed)
}
