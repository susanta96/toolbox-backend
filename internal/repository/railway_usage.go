package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RailwayUsageRepository tracks the shared monthly budget for paid RailKit calls.
type RailwayUsageRepository struct {
	pool *pgxpool.Pool
}

func NewRailwayUsageRepository(pool *pgxpool.Pool) *RailwayUsageRepository {
	return &RailwayUsageRepository{pool: pool}
}

// ReserveMonthlyRequest atomically reserves one request for the current UTC month.
// It returns false once the configured cap is reached.
func (r *RailwayUsageRepository) ReserveMonthlyRequest(ctx context.Context, limit int) (bool, error) {
	if limit < 1 {
		return false, nil
	}
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	var count int
	err := r.pool.QueryRow(ctx, `
		INSERT INTO railway_monthly_usage (month_start, request_count)
		VALUES ($1::date, 1)
		ON CONFLICT (month_start) DO UPDATE
		SET request_count = railway_monthly_usage.request_count + 1
		WHERE railway_monthly_usage.request_count < $2
		RETURNING request_count
	`, monthStart, limit).Scan(&count)
	if err != nil {
		// No row is returned when the conditional update hits the cap.
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
