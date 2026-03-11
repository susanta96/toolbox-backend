package scheduler

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/susanta96/toolbox-backend/internal/repository"
)

// Cleanup manages periodic removal of expired files from disk and their DB records.
type Cleanup struct {
	cron      *cron.Cron
	repo      *repository.FileRecordRepository
	dirs      []string
	retention time.Duration
}

// NewCleanup creates a new cleanup scheduler.
func NewCleanup(repo *repository.FileRecordRepository, dirs []string, retention time.Duration) *Cleanup {
	return &Cleanup{
		cron:      cron.New(),
		repo:      repo,
		dirs:      dirs,
		retention: retention,
	}
}

// Start begins the cleanup cron job.
func (c *Cleanup) Start(interval time.Duration) error {
	spec := "@every " + interval.String()

	_, err := c.cron.AddFunc(spec, c.run)
	if err != nil {
		return err
	}

	c.cron.Start()
	slog.Info("cleanup scheduler started", "interval", interval.String(), "retention", c.retention.String(), "dirs", c.dirs)

	// Run once immediately at startup
	go c.run()

	return nil
}

// Stop gracefully stops the scheduler.
func (c *Cleanup) Stop() {
	ctx := c.cron.Stop()
	<-ctx.Done()
	slog.Info("cleanup scheduler stopped")
}

func (c *Cleanup) run() {
	slog.Info("cleanup: running")

	// 1. Filesystem cleanup — remove old files from upload + generated dirs
	cutoff := time.Now().Add(-c.retention)
	filesRemoved := 0
	for _, dir := range c.dirs {
		removed, err := cleanDir(dir, cutoff)
		if err != nil {
			slog.Error("cleanup: error scanning directory", "dir", dir, "error", err)
			continue
		}
		filesRemoved += removed
	}

	// 2. DB cleanup — archive expired records (soft-delete for analytics)
	dbArchived := c.cleanDB()

	slog.Info("cleanup: done", "files_removed", filesRemoved, "db_records_archived", dbArchived)
}

// cleanDB removes expired file records from the database.
func (c *Cleanup) cleanDB() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	records, err := c.repo.GetExpired(ctx)
	if err != nil {
		slog.Error("cleanup: failed to query expired DB records", "error", err)
		return 0
	}

	removed := 0
	for _, rec := range records {
		if err := c.repo.ArchiveByID(ctx, rec.ID); err != nil {
			slog.Warn("cleanup: failed to archive DB record", "id", rec.ID, "error", err)
		} else {
			removed++
		}
	}
	return removed
}

// cleanDir removes files in dir that were modified before the cutoff time.
func cleanDir(dir string, cutoff time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			slog.Warn("cleanup: failed to get file info", "file", entry.Name(), "error", err)
			continue
		}

		if info.ModTime().Before(cutoff) {
			fullPath := filepath.Join(dir, entry.Name())
			if err := os.Remove(fullPath); err != nil {
				slog.Warn("cleanup: failed to remove file", "path", fullPath, "error", err)
			} else {
				slog.Info("cleanup: removed expired file", "path", fullPath, "age", time.Since(info.ModTime()).Round(time.Second))
				removed++
			}
		}
	}

	return removed, nil
}
