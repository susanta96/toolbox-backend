package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/susanta96/toolbox-backend/internal/model"
)

// ExchangeRateRepository provides data operations for currency rates.
type ExchangeRateRepository struct {
	pool *pgxpool.Pool
}

// NewExchangeRateRepository creates a new exchange rate repository.
func NewExchangeRateRepository(pool *pgxpool.Pool) *ExchangeRateRepository {
	return &ExchangeRateRepository{pool: pool}
}

// Upsert stores or updates a rate for a given base/target/date.
func (r *ExchangeRateRepository) Upsert(ctx context.Context, rec *model.ExchangeRate) error {
	query := `
		INSERT INTO exchange_rates (base, target, rate, rate_date, source, fetched_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (base, target, rate_date)
		DO UPDATE SET
			rate = EXCLUDED.rate,
			source = EXCLUDED.source,
			fetched_at = EXCLUDED.fetched_at,
			expires_at = EXCLUDED.expires_at`

	_, err := r.pool.Exec(ctx, query,
		rec.Base,
		rec.Target,
		rec.Rate,
		rec.RateDate,
		rec.Source,
		rec.FetchedAt,
		rec.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upsert exchange rate: %w", err)
	}

	return nil
}

// UpsertMany stores multiple rates in a single transaction.
func (r *ExchangeRateRepository) UpsertMany(ctx context.Context, rows []*model.ExchangeRate) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin exchange rate tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	query := `
		INSERT INTO exchange_rates (base, target, rate, rate_date, source, fetched_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (base, target, rate_date)
		DO UPDATE SET
			rate = EXCLUDED.rate,
			source = EXCLUDED.source,
			fetched_at = EXCLUDED.fetched_at,
			expires_at = EXCLUDED.expires_at`

	for _, rec := range rows {
		if _, err := tx.Exec(ctx, query,
			rec.Base,
			rec.Target,
			rec.Rate,
			rec.RateDate,
			rec.Source,
			rec.FetchedAt,
			rec.ExpiresAt,
		); err != nil {
			return fmt.Errorf("upsert exchange rate row: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit exchange rate tx: %w", err)
	}

	return nil
}

// GetLatest returns the newest known rate for a pair.
func (r *ExchangeRateRepository) GetLatest(ctx context.Context, base, target string) (*model.ExchangeRate, error) {
	query := `
		SELECT id, base, target, rate, rate_date, source, fetched_at, expires_at
		FROM exchange_rates
		WHERE base = $1 AND target = $2
		ORDER BY rate_date DESC, fetched_at DESC
		LIMIT 1`

	rec := &model.ExchangeRate{}
	if err := r.pool.QueryRow(ctx, query, base, target).Scan(
		&rec.ID,
		&rec.Base,
		&rec.Target,
		&rec.Rate,
		&rec.RateDate,
		&rec.Source,
		&rec.FetchedAt,
		&rec.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("get latest exchange rate: %w", err)
	}

	return rec, nil
}

// GetHistorical returns rates for a pair within the date window.
func (r *ExchangeRateRepository) GetHistorical(ctx context.Context, base, target string, startDate, endDate time.Time) ([]model.ExchangeRate, error) {
	query := `
		SELECT id, base, target, rate, rate_date, source, fetched_at, expires_at
		FROM exchange_rates
		WHERE base = $1 AND target = $2
			AND rate_date >= $3::date
			AND rate_date <= $4::date
		ORDER BY rate_date ASC`

	rows, err := r.pool.Query(ctx, query, base, target, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("query historical exchange rates: %w", err)
	}
	defer rows.Close()

	history := make([]model.ExchangeRate, 0)
	for rows.Next() {
		var rec model.ExchangeRate
		if err := rows.Scan(
			&rec.ID,
			&rec.Base,
			&rec.Target,
			&rec.Rate,
			&rec.RateDate,
			&rec.Source,
			&rec.FetchedAt,
			&rec.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan historical exchange rate: %w", err)
		}
		history = append(history, rec)
	}

	return history, nil
}

// PruneOlderThan deletes history older than the cutoff date.
func (r *ExchangeRateRepository) PruneOlderThan(ctx context.Context, cutoffDate time.Time) (int64, error) {
	query := `DELETE FROM exchange_rates WHERE rate_date < $1::date`
	result, err := r.pool.Exec(ctx, query, cutoffDate)
	if err != nil {
		return 0, fmt.Errorf("prune exchange rates: %w", err)
	}

	return result.RowsAffected(), nil
}
