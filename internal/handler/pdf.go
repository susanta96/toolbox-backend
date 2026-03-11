package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/repository"
	"github.com/susanta96/toolbox-backend/internal/service"
	"github.com/susanta96/toolbox-backend/pkg/response"
)

// PDFHandler handles PDF-related HTTP requests.
type PDFHandler struct {
	pdfService    *service.PDFService
	repo          *repository.FileRecordRepository
	uploadDir     string
	retention     time.Duration
	maxMergeFiles int
}

// NewPDFHandler creates a new PDF handler.
func NewPDFHandler(pdfSvc *service.PDFService, repo *repository.FileRecordRepository, uploadDir string, retention time.Duration, maxMergeFiles int) *PDFHandler {
	return &PDFHandler{
		pdfService:    pdfSvc,
		repo:          repo,
		uploadDir:     uploadDir,
		retention:     retention,
		maxMergeFiles: maxMergeFiles,
	}
}

// LockPDF handles POST /api/v1/pdf/lock
// Expects multipart form: file (PDF), password (string), owner_password (optional string).
func (h *PDFHandler) LockPDF(w http.ResponseWriter, r *http.Request) {
	inputPath, originalName, err := h.saveUploadedFile(r, "file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	password := strings.TrimSpace(r.FormValue("password"))
	if password == "" {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "password is required")
		return
	}
	ownerPassword := strings.TrimSpace(r.FormValue("owner_password"))

	// Create DB record
	rec := &model.FileRecord{
		OriginalName: originalName,
		StoredPath:   inputPath,
		Operation:    model.OperationLockPDF,
		Status:       model.StatusPending,
		ExpiresAt:    time.Now().Add(h.retention),
	}

	recordID, err := h.repo.Create(r.Context(), rec)
	if err != nil {
		os.Remove(inputPath)
		response.Error(w, http.StatusInternalServerError, "failed to create file record")
		return
	}

	// Generate output filename (UUID-prefixed for disk, friendly name for download)
	outputFilename := fmt.Sprintf("%s_locked.pdf", strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	downloadName := friendlyDownloadName(originalName, model.OperationLockPDF)

	outputPath, err := h.pdfService.LockPDF(inputPath, outputFilename, password, ownerPassword)
	if err != nil {
		h.repo.UpdateStatus(r.Context(), recordID, model.StatusFailed, "", err.Error()) //nolint:errcheck
		response.Error(w, http.StatusUnprocessableEntity, "failed to lock PDF — the file may be corrupted or invalid")
		return
	}

	h.repo.UpdateStatus(r.Context(), recordID, model.StatusCompleted, outputPath, "") //nolint:errcheck

	response.Success(w, http.StatusOK, "PDF locked successfully", map[string]string{
		"id":        recordID,
		"download":  fmt.Sprintf("/api/v1/pdf/download/%s", recordID),
		"file_name": downloadName,
	})
}

// UnlockPDF handles POST /api/v1/pdf/unlock
// Expects multipart form: file (PDF), password (string).
func (h *PDFHandler) UnlockPDF(w http.ResponseWriter, r *http.Request) {
	inputPath, originalName, err := h.saveUploadedFile(r, "file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	password := strings.TrimSpace(r.FormValue("password"))
	if password == "" {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "password is required")
		return
	}

	rec := &model.FileRecord{
		OriginalName: originalName,
		StoredPath:   inputPath,
		Operation:    model.OperationUnlockPDF,
		Status:       model.StatusPending,
		ExpiresAt:    time.Now().Add(h.retention),
	}

	// Check if PDF is actually encrypted before attempting unlock
	encrypted, err := h.pdfService.IsEncrypted(inputPath)
	if err != nil {
		os.Remove(inputPath)
		response.Error(w, http.StatusUnprocessableEntity, "failed to read PDF file")
		return
	}
	if !encrypted {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "this PDF is not locked — no password protection found")
		return
	}

	recordID, err := h.repo.Create(r.Context(), rec)
	if err != nil {
		os.Remove(inputPath)
		response.Error(w, http.StatusInternalServerError, "failed to create file record")
		return
	}

	outputFilename := fmt.Sprintf("%s_unlocked.pdf", strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	downloadName := friendlyDownloadName(originalName, model.OperationUnlockPDF)

	outputPath, err := h.pdfService.UnlockPDF(inputPath, outputFilename, password)
	if err != nil {
		errMsg := err.Error()
		h.repo.UpdateStatus(r.Context(), recordID, model.StatusFailed, "", errMsg) //nolint:errcheck
		if errors.Is(err, service.ErrInvalidPassword) {
			response.Error(w, http.StatusBadRequest, "incorrect password — please check and try again")
			return
		}
		response.Error(w, http.StatusUnprocessableEntity, "failed to unlock PDF — the file may be corrupted")
		return
	}

	h.repo.UpdateStatus(r.Context(), recordID, model.StatusCompleted, outputPath, "") //nolint:errcheck

	response.Success(w, http.StatusOK, "PDF unlocked successfully", map[string]string{
		"id":        recordID,
		"download":  fmt.Sprintf("/api/v1/pdf/download/%s", recordID),
		"file_name": downloadName,
	})
}

// validCompressionLevels defines accepted compression quality levels.
var validCompressionLevels = map[string]bool{
	"low":     true,
	"medium":  true,
	"high":    true,
	"maximum": true,
}

// CompressPDF handles POST /api/v1/pdf/compress
// Expects multipart form: file (PDF), level (string: low|medium|high|maximum).
func (h *PDFHandler) CompressPDF(w http.ResponseWriter, r *http.Request) {
	inputPath, originalName, err := h.saveUploadedFile(r, "file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	level := strings.TrimSpace(r.FormValue("level"))
	if level == "" {
		level = "medium"
	}
	if !validCompressionLevels[level] {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "invalid compression level — use low, medium, high, or maximum")
		return
	}

	rec := &model.FileRecord{
		OriginalName: originalName,
		StoredPath:   inputPath,
		Operation:    model.OperationCompressPDF,
		Status:       model.StatusPending,
		ExpiresAt:    time.Now().Add(h.retention),
	}

	recordID, err := h.repo.Create(r.Context(), rec)
	if err != nil {
		os.Remove(inputPath)
		response.Error(w, http.StatusInternalServerError, "failed to create file record")
		return
	}

	outputFilename := fmt.Sprintf("%s_compressed.pdf", strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	downloadName := friendlyDownloadName(originalName, model.OperationCompressPDF)

	result, err := h.pdfService.CompressPDF(inputPath, outputFilename, level)
	if err != nil {
		h.repo.UpdateStatus(r.Context(), recordID, model.StatusFailed, "", err.Error()) //nolint:errcheck
		response.Error(w, http.StatusUnprocessableEntity, "failed to compress PDF — the file may be corrupted or invalid")
		return
	}

	h.repo.UpdateStatus(r.Context(), recordID, model.StatusCompleted, result.OutputPath, "") //nolint:errcheck

	savedBytes := result.OriginalSize - result.CompressedSize
	var compressionPercent float64
	if result.OriginalSize > 0 {
		compressionPercent = float64(savedBytes) / float64(result.OriginalSize) * 100
	}

	response.Success(w, http.StatusOK, "PDF compressed successfully", map[string]any{
		"id":                  recordID,
		"download":            fmt.Sprintf("/api/v1/pdf/download/%s", recordID),
		"file_name":           downloadName,
		"original_size":       result.OriginalSize,
		"compressed_size":     result.CompressedSize,
		"saved_bytes":         savedBytes,
		"compression_percent": fmt.Sprintf("%.1f", compressionPercent),
	})
}

// Download handles GET /api/v1/pdf/download/{id}
func (h *PDFHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, http.StatusBadRequest, "file id is required")
		return
	}

	rec, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "file not found")
		return
	}

	if rec.Status != model.StatusCompleted || rec.OutputPath == "" {
		response.Error(w, http.StatusNotFound, "file not ready or processing failed")
		return
	}

	if time.Now().After(rec.ExpiresAt) {
		response.Error(w, http.StatusGone, "file has expired and been removed")
		return
	}

	downloadName := friendlyDownloadName(rec.OriginalName, rec.Operation)

	// Detect content type from output file extension
	contentType := "application/pdf"
	if strings.HasSuffix(strings.ToLower(rec.OutputPath), ".zip") {
		contentType = "application/zip"
		// Override download name to use .zip extension for split operations
		ext := filepath.Ext(downloadName)
		downloadName = strings.TrimSuffix(downloadName, ext) + ".zip"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadName))
	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, rec.OutputPath)
}

// saveUploadedFile parses the multipart form and saves the uploaded file to disk.
func (h *PDFHandler) saveUploadedFile(r *http.Request, fieldName string) (string, string, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", "", fmt.Errorf("missing or invalid file field '%s': %w", fieldName, err)
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		return "", "", fmt.Errorf("only PDF files are accepted")
	}

	// Sanitize filename
	safeBase := sanitizeFilename(header.Filename)
	storedName := fmt.Sprintf("%s_%s", uuid.New().String(), safeBase)
	storedPath := filepath.Join(h.uploadDir, storedName)

	dst, err := os.Create(storedPath)
	if err != nil {
		return "", "", fmt.Errorf("create upload file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(storedPath)
		return "", "", fmt.Errorf("save upload file: %w", err)
	}

	return storedPath, header.Filename, nil
}

// sanitizeFilename removes path separators, null bytes, and replaces spaces for safe storage.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

// friendlyDownloadName builds a user-friendly download filename from the original name and operation.
// e.g. "MyStatement 7.pdf" + "lock_pdf" → "MyStatement 7 (Locked).pdf"
func friendlyDownloadName(originalName, operation string) string {
	ext := filepath.Ext(originalName)
	base := strings.TrimSuffix(originalName, ext)

	var suffix string
	switch operation {
	case model.OperationLockPDF:
		suffix = "Locked"
	case model.OperationUnlockPDF:
		suffix = "Unlocked"
	case model.OperationCompressPDF:
		suffix = "Compressed"
	case model.OperationMergePDF:
		suffix = "Merged"
	case model.OperationSplitPDF:
		suffix = "Split"
		ext = ".zip"
	default:
		suffix = "Processed"
	}

	return fmt.Sprintf("%s (%s)%s", base, suffix, ext)
}

// MergePDF handles POST /api/v1/pdf/merge
// Expects multipart form: files (multiple PDF files, 2 to maxMergeFiles).
func (h *PDFHandler) MergePDF(w http.ResponseWriter, r *http.Request) {
	inputPaths, originalNames, err := h.saveMultipleUploadedFiles(r, "files")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(inputPaths) < 2 {
		cleanupUploadedFiles(inputPaths)
		response.Error(w, http.StatusBadRequest, "at least 2 PDF files are required for merging")
		return
	}
	if len(inputPaths) > h.maxMergeFiles {
		cleanupUploadedFiles(inputPaths)
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("too many files — maximum %d files allowed for merging", h.maxMergeFiles))
		return
	}

	rec := &model.FileRecord{
		OriginalName: originalNames[0],
		StoredPath:   inputPaths[0],
		Operation:    model.OperationMergePDF,
		Status:       model.StatusPending,
		ExpiresAt:    time.Now().Add(h.retention),
	}

	recordID, err := h.repo.Create(r.Context(), rec)
	if err != nil {
		cleanupUploadedFiles(inputPaths)
		response.Error(w, http.StatusInternalServerError, "failed to create file record")
		return
	}

	outputFilename := fmt.Sprintf("%s_merged.pdf", uuid.New().String())
	downloadName := friendlyDownloadName(originalNames[0], model.OperationMergePDF)

	outputPath, err := h.pdfService.MergePDFs(inputPaths, outputFilename)
	if err != nil {
		h.repo.UpdateStatus(r.Context(), recordID, model.StatusFailed, "", err.Error()) //nolint:errcheck
		response.Error(w, http.StatusUnprocessableEntity, "failed to merge PDFs — one or more files may be corrupted or invalid")
		return
	}

	h.repo.UpdateStatus(r.Context(), recordID, model.StatusCompleted, outputPath, "") //nolint:errcheck

	response.Success(w, http.StatusOK, "PDFs merged successfully", map[string]any{
		"id":         recordID,
		"download":   fmt.Sprintf("/api/v1/pdf/download/%s", recordID),
		"file_name":  downloadName,
		"file_count": len(inputPaths),
	})
}

// validSplitModes defines accepted split modes.
var validSplitModes = map[string]bool{
	"all":    true,
	"ranges": true,
}

// SplitPDF handles POST /api/v1/pdf/split
// Expects multipart form: file (PDF), mode (string: all|ranges), pages (string, required if mode=ranges).
func (h *PDFHandler) SplitPDF(w http.ResponseWriter, r *http.Request) {
	inputPath, originalName, err := h.saveUploadedFile(r, "file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode == "" {
		mode = "all"
	}
	if !validSplitModes[mode] {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "invalid split mode — use \"all\" or \"ranges\"")
		return
	}

	pages := strings.TrimSpace(r.FormValue("pages"))
	if mode == "ranges" && pages == "" {
		os.Remove(inputPath)
		response.Error(w, http.StatusBadRequest, "pages parameter is required when mode is \"ranges\" (e.g. \"1-3,4-6\")")
		return
	}

	rec := &model.FileRecord{
		OriginalName: originalName,
		StoredPath:   inputPath,
		Operation:    model.OperationSplitPDF,
		Status:       model.StatusPending,
		ExpiresAt:    time.Now().Add(h.retention),
	}

	recordID, err := h.repo.Create(r.Context(), rec)
	if err != nil {
		os.Remove(inputPath)
		response.Error(w, http.StatusInternalServerError, "failed to create file record")
		return
	}

	outputPrefix := uuid.New().String()
	downloadName := friendlyDownloadName(originalName, model.OperationSplitPDF)

	result, err := h.pdfService.SplitPDF(inputPath, outputPrefix, mode, pages)
	if err != nil {
		h.repo.UpdateStatus(r.Context(), recordID, model.StatusFailed, "", err.Error()) //nolint:errcheck
		response.Error(w, http.StatusUnprocessableEntity, fmt.Sprintf("failed to split PDF: %s", err.Error()))
		return
	}

	h.repo.UpdateStatus(r.Context(), recordID, model.StatusCompleted, result.ZipPath, "") //nolint:errcheck

	response.Success(w, http.StatusOK, "PDF split successfully", map[string]any{
		"id":          recordID,
		"download":    fmt.Sprintf("/api/v1/pdf/download/%s", recordID),
		"file_name":   downloadName,
		"page_count":  result.PageCount,
		"split_count": result.SplitCount,
	})
}

func (h *PDFHandler) saveMultipleUploadedFiles(r *http.Request, fieldName string) ([]string, []string, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}

	if r.MultipartForm == nil || len(r.MultipartForm.File) == 0 {
		return nil, nil, fmt.Errorf("no files provided — send PDF files in field '%s'", fieldName)
	}

	// Collect file headers from all keys that match the field name.
	// Clients may use: "files", "files[]", "files[0]", "files[1]", etc.
	var allHeaders []*multipart.FileHeader
	for key, headers := range r.MultipartForm.File {
		if key == fieldName || strings.HasPrefix(key, fieldName+"[") || key == fieldName+"[]" {
			allHeaders = append(allHeaders, headers...)
		}
	}

	// Log for debugging multipart form structure
	var keys []string
	for k, v := range r.MultipartForm.File {
		keys = append(keys, fmt.Sprintf("%s(%d)", k, len(v)))
	}
	slog.Info("merge: multipart form parsed",
		"field", fieldName,
		"matched_files", len(allHeaders),
		"all_file_keys", keys,
	)

	if len(allHeaders) == 0 {
		return nil, nil, fmt.Errorf("no files provided in field '%s' — found keys: %v", fieldName, keys)
	}

	var paths []string
	var names []string

	for _, header := range allHeaders {
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
			cleanupUploadedFiles(paths)
			return nil, nil, fmt.Errorf("only PDF files are accepted, got: %s", header.Filename)
		}

		file, err := header.Open()
		if err != nil {
			cleanupUploadedFiles(paths)
			return nil, nil, fmt.Errorf("open uploaded file %s: %w", header.Filename, err)
		}

		safeBase := sanitizeFilename(header.Filename)
		storedName := fmt.Sprintf("%s_%s", uuid.New().String(), safeBase)
		storedPath := filepath.Join(h.uploadDir, storedName)

		dst, err := os.Create(storedPath)
		if err != nil {
			file.Close()
			cleanupUploadedFiles(paths)
			return nil, nil, fmt.Errorf("create upload file: %w", err)
		}

		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			file.Close()
			os.Remove(storedPath)
			cleanupUploadedFiles(paths)
			return nil, nil, fmt.Errorf("save upload file: %w", err)
		}

		dst.Close()
		file.Close()
		paths = append(paths, storedPath)
		names = append(names, header.Filename)
	}

	return paths, names, nil
}

// cleanupUploadedFiles removes a list of uploaded files from disk.
func cleanupUploadedFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}
