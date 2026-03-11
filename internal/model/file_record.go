package model

import "time"

// FileRecord tracks uploaded and generated files for lifecycle management.
type FileRecord struct {
	ID           string     `json:"id"`
	OriginalName string     `json:"original_name"`
	StoredPath   string     `json:"-"`
	OutputPath   string     `json:"-"`
	Operation    string     `json:"operation"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
}

// Operation types.
const (
	OperationLockPDF     = "lock_pdf"
	OperationUnlockPDF   = "unlock_pdf"
	OperationCompressPDF = "compress_pdf"
	OperationMergePDF    = "merge_pdf"
	OperationSplitPDF    = "split_pdf"
)

// Status values.
const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)
