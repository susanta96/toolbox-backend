package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/susanta96/toolbox-backend/internal/model"
)

// FileRecordRepository provides CRUD operations for file tracking records.
type FileRecordRepository struct {
	pool *pgxpool.Pool
}

// NewFileRecordRepository creates a new repository instance.
func NewFileRecordRepository(pool *pgxpool.Pool) *FileRecordRepository {
	return &FileRecordRepository{pool: pool}
}

// Create inserts a new file record and returns its ID.
func (r *FileRecordRepository) Create(ctx context.Context, rec *model.FileRecord) (string, error) {
	query := `
		INSERT INTO file_records (original_name, stored_path, output_path, operation, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	var id string
	err := r.pool.QueryRow(ctx, query,
		rec.OriginalName,
		rec.StoredPath,
		rec.OutputPath,
		rec.Operation,
		rec.Status,
		rec.ExpiresAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create file record: %w", err)
	}
	return id, nil
}

// GetByID retrieves a file record by its ID.
func (r *FileRecordRepository) GetByID(ctx context.Context, id string) (*model.FileRecord, error) {
	query := `
		SELECT id, original_name, stored_path, COALESCE(output_path, ''), operation, status, COALESCE(error_message, ''), created_at, expires_at
		FROM file_records WHERE id = $1`

	rec := &model.FileRecord{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&rec.ID, &rec.OriginalName, &rec.StoredPath, &rec.OutputPath,
		&rec.Operation, &rec.Status, &rec.ErrorMessage,
		&rec.CreatedAt, &rec.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get file record: %w", err)
	}
	return rec, nil
}

// UpdateStatus updates the status and optional error message of a record.
func (r *FileRecordRepository) UpdateStatus(ctx context.Context, id, status, outputPath, errMsg string) error {
	query := `
		UPDATE file_records
		SET status = $2, output_path = $3, error_message = $4
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id, status, outputPath, errMsg)
	if err != nil {
		return fmt.Errorf("update file record status: %w", err)
	}
	return nil
}

// GetExpired returns all records that have expired and are not yet archived.
func (r *FileRecordRepository) GetExpired(ctx context.Context) ([]*model.FileRecord, error) {
	query := `
		SELECT id, original_name, stored_path, COALESCE(output_path, ''), operation, status, COALESCE(error_message, ''), created_at, expires_at
		FROM file_records
		WHERE expires_at < NOW() AND archived_at IS NULL`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query expired records: %w", err)
	}
	defer rows.Close()

	var records []*model.FileRecord
	for rows.Next() {
		rec := &model.FileRecord{}
		if err := rows.Scan(
			&rec.ID, &rec.OriginalName, &rec.StoredPath, &rec.OutputPath,
			&rec.Operation, &rec.Status, &rec.ErrorMessage,
			&rec.CreatedAt, &rec.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan expired record: %w", err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// ArchiveByID soft-deletes a file record by setting archived_at timestamp.
// The record is retained for analytics; only disk files are removed.
func (r *FileRecordRepository) ArchiveByID(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE file_records SET archived_at = NOW(), stored_path = '', output_path = '' WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("archive file record: %w", err)
	}
	return nil
}
