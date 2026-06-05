package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/repository"
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

type PasteService struct {
	repo *repository.PasteRepository
}

func NewPasteService(repo *repository.PasteRepository) *PasteService {
	return &PasteService{repo: repo}
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
