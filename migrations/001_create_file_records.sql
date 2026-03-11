-- 001_create_file_records.sql
-- Creates the file_records table for tracking uploaded/generated files.

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
