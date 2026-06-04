package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/susanta96/toolbox-backend/internal/model"
)

type PasteRepository struct {
	pool *pgxpool.Pool
}

func NewPasteRepository(pool *pgxpool.Pool) *PasteRepository {
	return &PasteRepository{pool: pool}
}

func (r *PasteRepository) Create(ctx context.Context, p *model.Paste) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO pastes (id, content, language, expires_at) VALUES ($1, $2, $3, $4)`,
		p.ID, p.Content, p.Language, p.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create paste: %w", err)
	}
	return nil
}

func (r *PasteRepository) GetByID(ctx context.Context, id string) (*model.Paste, error) {
	p := &model.Paste{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, content, language, expires_at, created_at, view_count FROM pastes WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Content, &p.Language, &p.ExpiresAt, &p.CreatedAt, &p.ViewCount)
	if err != nil {
		return nil, fmt.Errorf("get paste: %w", err)
	}
	return p, nil
}

func (r *PasteRepository) IncrementViewCount(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE pastes SET view_count = view_count + 1 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("increment view count: %w", err)
	}
	return nil
}

func (r *PasteRepository) IDExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pastes WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check paste id: %w", err)
	}
	return exists, nil
}

func (r *PasteRepository) GetExpiredIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM pastes WHERE expires_at IS NOT NULL AND expires_at < NOW()`,
	)
	if err != nil {
		return nil, fmt.Errorf("query expired pastes: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired paste id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired paste rows: %w", err)
	}
	return ids, nil
}

func (r *PasteRepository) DeleteByID(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM pastes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete paste: %w", err)
	}
	return nil
}
