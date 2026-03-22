package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/susanta96/toolbox-backend/internal/service"
)

// CurrencyWarmup periodically warms top requested currency pairs and prunes old history.
type CurrencyWarmup struct {
	cron  *cron.Cron
	svc   *service.CurrencyService
	limit int
}

// NewCurrencyWarmup creates a warmup scheduler.
func NewCurrencyWarmup(svc *service.CurrencyService, limit int) *CurrencyWarmup {
	return &CurrencyWarmup{
		cron:  cron.New(),
		svc:   svc,
		limit: limit,
	}
}

// Start starts scheduler.
func (c *CurrencyWarmup) Start(interval time.Duration) error {
	spec := "@every " + interval.String()
	_, err := c.cron.AddFunc(spec, c.run)
	if err != nil {
		return err
	}

	c.cron.Start()
	slog.Info("currency warmup scheduler started", "interval", interval.String(), "pair_limit", c.limit)
	go c.run()
	return nil
}

// Stop stops scheduler.
func (c *CurrencyWarmup) Stop() {
	ctx := c.cron.Stop()
	<-ctx.Done()
	slog.Info("currency warmup scheduler stopped")
}

func (c *CurrencyWarmup) run() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := c.svc.WarmMostRequested(ctx, c.limit); err != nil {
		slog.Warn("currency warmup failed", "error", err)
	}

	pruned, err := c.svc.PruneHistoricalData(ctx)
	if err != nil {
		slog.Warn("currency history prune failed", "error", err)
		return
	}

	slog.Info("currency warmup cycle complete", "pruned_rows", pruned)
}
