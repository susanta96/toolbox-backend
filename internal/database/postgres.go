package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a connection pool to PostgreSQL (Neon-compatible).
func New(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	// Neon-friendly pool settings
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	slog.Info("database connected successfully")
	return pool, nil
}

// RunMigrations executes the schema migrations.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	CREATE TABLE IF NOT EXISTS file_records (
		id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		original_name TEXT    NOT NULL,
		stored_path   TEXT    NOT NULL,
		output_path   TEXT,
		operation     TEXT    NOT NULL,
		status        TEXT    NOT NULL DEFAULT 'pending',
		error_message TEXT,
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		expires_at    TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_file_records_expires_at ON file_records (expires_at);
	CREATE INDEX IF NOT EXISTS idx_file_records_status     ON file_records (status);
	`

	if _, err := pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	// Migration: add archived_at column for soft-delete (analytics retention)
	alterQuery := `
	ALTER TABLE file_records ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
	CREATE INDEX IF NOT EXISTS idx_file_records_archived_at ON file_records (archived_at) WHERE archived_at IS NULL;
	`

	if _, err := pool.Exec(ctx, alterQuery); err != nil {
		return fmt.Errorf("run migration (archived_at): %w", err)
	}

	slog.Info("database migrations completed")
	return nil
}
