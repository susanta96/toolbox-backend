package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/service"
	"github.com/susanta96/toolbox-backend/pkg/response"
)

// archiveUploadDeadline extends the server-wide read/write timeouts for archive
// uploads, which are larger and trigger a remote upload before responding.
const archiveUploadDeadline = 2 * time.Minute

// archiveBodySlack is the allowance for multipart framing on top of the
// archive size limit.
const archiveBodySlack = 1 << 20

// archiveMemoryLimit is how much of a multipart form is held in memory; the
// rest spills to temporary files.
const archiveMemoryLimit = 8 << 20

// pasteService is satisfied by *service.PasteService.
type pasteService interface {
	CreatePaste(ctx context.Context, req service.CreatePasteRequest) (*service.CreatePasteResult, error)
	GetPaste(ctx context.Context, id string) (*model.Paste, error)
	CreateArchivePaste(ctx context.Context, fileName string, size int64, r io.Reader) (*service.CreatePasteResult, error)
}

type PasteHandler struct {
	svc             pasteService
	archiveMaxBytes int64
}

func NewPasteHandler(svc pasteService, archiveMaxBytes int64) *PasteHandler {
	return &PasteHandler{svc: svc, archiveMaxBytes: archiveMaxBytes}
}

func (h *PasteHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req service.CreatePasteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.svc.CreatePaste(r.Context(), req)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(w, http.StatusCreated, "Paste created successfully", result)
}

func (h *PasteHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, http.StatusBadRequest, "paste id is required")
		return
	}

	paste, err := h.svc.GetPaste(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "paste not found")
			return
		}
		if err.Error() == "paste has expired" {
			response.Error(w, http.StatusGone, "paste has expired")
			return
		}
		response.Error(w, http.StatusNotFound, "paste not found")
		return
	}

	response.Success(w, http.StatusOK, "OK", paste)
}

// CreateArchive accepts a multipart upload with a single "file" part holding a
// zip/tar archive and stores it as a paste.
func (h *PasteHandler) CreateArchive(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(archiveUploadDeadline)
	if err := rc.SetReadDeadline(deadline); err != nil {
		slog.Debug("archive upload: cannot extend read deadline", "error", err)
	}
	if err := rc.SetWriteDeadline(deadline); err != nil {
		slog.Debug("archive upload: cannot extend write deadline", "error", err)
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.archiveMaxBytes+archiveBodySlack)

	if err := r.ParseMultipartForm(archiveMemoryLimit); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Error(w, http.StatusRequestEntityTooLarge, "archive exceeds size limit")
			return
		}
		response.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll() //nolint:errcheck
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	result, err := h.svc.CreateArchivePaste(r.Context(), header.Filename, header.Size, file)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrArchiveInvalid):
			response.Error(w, http.StatusBadRequest, "unsupported or corrupt archive")
		case errors.Is(err, service.ErrArchiveTooLarge):
			response.Error(w, http.StatusRequestEntityTooLarge, "archive exceeds size limit")
		case errors.Is(err, service.ErrArchiveDisabled):
			response.Error(w, http.StatusServiceUnavailable, "archive uploads are not available")
		default:
			slog.Error("create archive paste failed", "error", err)
			response.Error(w, http.StatusInternalServerError, "failed to upload archive")
		}
		return
	}

	response.Success(w, http.StatusCreated, "Archive uploaded successfully", result)
}
