package model

import "time"

type Paste struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	Language  string     `json:"language"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	ViewCount int        `json:"view_count"`
	FileName  *string    `json:"file_name"`
	MimeType  *string    `json:"mime_type"`

	StorageFileID *string `json:"-"`
	StorageURL    *string `json:"storage_url"`
	FileSize      *int64  `json:"file_size"`
}

// ExpiredPaste identifies an expired paste and the stored object (if any) that
// must be removed along with its row.
type ExpiredPaste struct {
	ID            string
	StorageFileID *string
}
