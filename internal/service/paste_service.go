package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/storage"
)

const (
	maxPasteSizeBytes = 1 * 1024 * 1024  // 1 MB for text pastes
	maxFileSizeBytes  = 7 * 1024 * 1024  // 7 MB for base64-encoded file pastes (~5 MB actual file)
	idAlphabet        = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

var validLanguages = map[string]bool{
	"auto": true, "text": true, "javascript": true, "typescript": true,
	"python": true, "go": true, "java": true, "csharp": true,
	"cpp": true, "rust": true, "sql": true, "json": true,
	"yaml": true, "html": true, "css": true, "bash": true, "markdown": true,
}

type CreatePasteRequest struct {
	Content  string  `json:"content"`
	Language string  `json:"language"`
	TTL      string  `json:"ttl"` // "1h" | "24h" | "7d" | "30d" | "never"
	FileName *string `json:"file_name"`
	MimeType *string `json:"mime_type"`
}

type CreatePasteResult struct {
	ID        string     `json:"id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// pasteStore is the persistence contract of PasteService; it is satisfied by
// *repository.PasteRepository.
type pasteStore interface {
	Create(ctx context.Context, p *model.Paste) error
	GetByID(ctx context.Context, id string) (*model.Paste, error)
	IncrementViewCount(ctx context.Context, id string) error
	IDExists(ctx context.Context, id string) (bool, error)
}

var (
	ErrArchiveTooLarge = errors.New("archive exceeds size limit")
	ErrArchiveDisabled = errors.New("archive uploads are not configured")
)

// archiveTTL is the fixed lifetime of archive pastes; they never live forever.
const archiveTTL = 24 * time.Hour

// archiveCleanupTimeout bounds the compensating delete of an orphaned upload.
const archiveCleanupTimeout = 10 * time.Second

// archiveUploadTimeout bounds the store upload and row insert, which run on a
// context detached from the request so a client disconnect cannot orphan a
// file that was already sent to storage.
const archiveUploadTimeout = 90 * time.Second

// archiveSniffBytes is how much of an upload is inspected to verify its type.
const archiveSniffBytes = 512

type PasteService struct {
	repo            pasteStore
	store           storage.ObjectStore // nil when archive uploads are disabled
	archiveFolder   string
	archiveMaxBytes int64
}

func NewPasteService(repo pasteStore, store storage.ObjectStore, archiveFolder string, archiveMaxBytes int64) *PasteService {
	return &PasteService{
		repo:            repo,
		store:           store,
		archiveFolder:   archiveFolder,
		archiveMaxBytes: archiveMaxBytes,
	}
}

// CreateArchivePaste verifies an archive upload, streams it to object storage
// and records a paste that expires after archiveTTL. size is the declared
// upload size in bytes; r is passed through to the store without being
// buffered beyond the type-sniffing window. size is trusted: the caller (the
// HTTP handler) must cap the reader so it cannot deliver more than size bytes.
func (s *PasteService) CreateArchivePaste(ctx context.Context, fileName string, size int64, r io.Reader) (*CreatePasteResult, error) {
	if s.store == nil {
		return nil, ErrArchiveDisabled
	}
	if size <= 0 {
		return nil, ErrArchiveInvalid
	}
	if size > s.archiveMaxBytes {
		return nil, ErrArchiveTooLarge
	}

	// Peek the head without consuming it, then hand the same bufio.Reader to
	// the store so the sniffed bytes are still uploaded.
	br := bufio.NewReaderSize(r, archiveSniffBytes)
	head, err := br.Peek(archiveSniffBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read archive head: %w", err)
	}
	kind, err := DetectArchive(fileName, head)
	if err != nil {
		return nil, err
	}

	id, err := s.generateUniqueID(ctx)
	if err != nil {
		return nil, err
	}

	// From here on the work must not die with the request: once storage has
	// the bytes, we need the row (or a compensating delete) to follow.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), archiveUploadTimeout)
	defer cancel()

	safeName := SanitizeFileName(fileName)
	obj, err := s.store.Put(ctx, s.archiveFolder, id+"_"+safeName, br)
	if err != nil {
		return nil, fmt.Errorf("upload archive: %w", err)
	}

	expiresAt := time.Now().Add(archiveTTL)
	mime := kind.Mime
	paste := &model.Paste{
		ID:            id,
		Content:       "",
		Language:      "text",
		ExpiresAt:     &expiresAt,
		FileName:      &safeName,
		MimeType:      &mime,
		StorageFileID: &obj.FileID,
		StorageURL:    &obj.URL,
		FileSize:      &size,
	}
	if err := s.repo.Create(ctx, paste); err != nil {
		// Best effort: do not leave an orphaned object behind. The upload
		// context may be the very reason the insert failed (deadline hit), so
		// clean up on a fresh, detached, bounded context.
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), archiveCleanupTimeout)
		defer dcancel()
		if delErr := s.store.Delete(dctx, obj.FileID); delErr != nil {
			slog.Warn("failed to delete uploaded archive after insert failure",
				"id", id, "file_id", obj.FileID, "error", delErr)
		}
		return nil, err
	}

	return &CreatePasteResult{ID: id, ExpiresAt: &expiresAt}, nil
}

func (s *PasteService) CreatePaste(ctx context.Context, req CreatePasteRequest) (*CreatePasteResult, error) {
	if len(req.Content) == 0 {
		return nil, errors.New("content cannot be empty")
	}
	isFilePaste := req.FileName != nil && *req.FileName != ""
	if isFilePaste {
		if len(req.Content) > maxFileSizeBytes {
			return nil, errors.New("file exceeds 5 MB limit")
		}
		req.Language = "text"
	} else {
		if len(req.Content) > maxPasteSizeBytes {
			return nil, errors.New("content exceeds 1 MB limit")
		}
		if !validLanguages[req.Language] {
			req.Language = "auto"
		}
	}

	expiresAt, err := parseTTL(req.TTL)
	if err != nil {
		return nil, err
	}

	id, err := s.generateUniqueID(ctx)
	if err != nil {
		return nil, err
	}

	paste := &model.Paste{
		ID:        id,
		Content:   req.Content,
		Language:  req.Language,
		ExpiresAt: expiresAt,
		FileName:  req.FileName,
		MimeType:  req.MimeType,
	}
	if err := s.repo.Create(ctx, paste); err != nil {
		return nil, err
	}

	return &CreatePasteResult{ID: id, ExpiresAt: expiresAt}, nil
}

func (s *PasteService) GetPaste(ctx context.Context, id string) (*model.Paste, error) {
	paste, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if paste.ExpiresAt != nil && paste.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("paste has expired")
	}
	go func() {
		if err := s.repo.IncrementViewCount(context.Background(), id); err != nil {
			slog.Warn("failed to increment view count", "id", id, "error", err)
		}
	}()
	return paste, nil
}

func (s *PasteService) generateUniqueID(ctx context.Context) (string, error) {
	for i := 0; i < 5; i++ {
		id, err := generatePasteID()
		if err != nil {
			return "", fmt.Errorf("generate id: %w", err)
		}
		exists, err := s.repo.IDExists(ctx, id)
		if err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
	}
	return "", errors.New("failed to generate unique paste id after 5 attempts")
}

func parseTTL(ttl string) (*time.Time, error) {
	var d time.Duration
	switch ttl {
	case "1h":
		d = time.Hour
	case "24h":
		d = 24 * time.Hour
	case "7d", "":
		d = 7 * 24 * time.Hour
	case "30d":
		d = 30 * 24 * time.Hour
	case "never":
		return nil, nil
	default:
		d = 7 * 24 * time.Hour
	}
	t := time.Now().Add(d)
	return &t, nil
}

func generatePasteID() (string, error) {
	const alphabetLen = byte(len(idAlphabet)) // 62
	id := make([]byte, 0, 6)
	buf := make([]byte, 12)
	for len(id) < 6 {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 248 { // 248 = 4*62; reject [248,255] to eliminate bias
				id = append(id, idAlphabet[b%alphabetLen])
				if len(id) == 6 {
					break
				}
			}
		}
	}
	return string(id), nil
}
