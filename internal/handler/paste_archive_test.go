package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/service"
)

const testArchiveMax = 1024

type fakePasteService struct {
	called   bool
	fileName string
	size     int64
	body     []byte
	result   *service.CreatePasteResult
	err      error
}

func (f *fakePasteService) CreatePaste(context.Context, service.CreatePasteRequest) (*service.CreatePasteResult, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePasteService) GetPaste(context.Context, string) (*model.Paste, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePasteService) CreateArchivePaste(_ context.Context, fileName string, size int64, r io.Reader) (*service.CreatePasteResult, error) {
	f.called = true
	f.fileName = fileName
	f.size = size
	f.body, _ = io.ReadAll(r)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func multipartBody(t *testing.T, field, name string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func doArchive(h *PasteHandler, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/paste/archive", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.CreateArchive(rec, req)
	return rec
}

func TestCreateArchive_Success(t *testing.T) {
	fake := &fakePasteService{result: &service.CreatePasteResult{ID: "abc12345"}}
	h := NewPasteHandler(fake, testArchiveMax)
	content := []byte("PK\x03\x04hello")

	body, ct := multipartBody(t, "file", "a.zip", content)
	rec := doArchive(h, body, ct)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		Data    struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != "abc12345" {
		t.Errorf("data.id = %q, want abc12345", resp.Data.ID)
	}
	if resp.Message != "Archive uploaded successfully" {
		t.Errorf("message = %q", resp.Message)
	}
	if fake.fileName != "a.zip" {
		t.Errorf("fileName = %q, want a.zip", fake.fileName)
	}
	if fake.size != int64(len(content)) {
		t.Errorf("size = %d, want %d", fake.size, len(content))
	}
	if !bytes.Equal(fake.body, content) {
		t.Errorf("service received body %q, want %q", fake.body, content)
	}
}

func TestCreateArchive_MissingFile(t *testing.T) {
	fake := &fakePasteService{}
	h := NewPasteHandler(fake, testArchiveMax)

	body, ct := multipartBody(t, "other", "a.zip", []byte("PK"))
	rec := doArchive(h, body, ct)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if fake.called {
		t.Error("service must not be called")
	}
}

func TestCreateArchive_NotMultipart(t *testing.T) {
	fake := &fakePasteService{}
	h := NewPasteHandler(fake, testArchiveMax)

	rec := doArchive(h, strings.NewReader("{}"), "application/json")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if fake.called {
		t.Error("service must not be called")
	}
}

func TestCreateArchive_BodyOverLimit(t *testing.T) {
	fake := &fakePasteService{}
	h := NewPasteHandler(fake, testArchiveMax)

	// Larger than archiveMaxBytes + 1 MiB of slack.
	big := make([]byte, testArchiveMax+(1<<20)+4096)
	body, ct := multipartBody(t, "file", "a.zip", big)
	rec := doArchive(h, body, ct)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if fake.called {
		t.Error("service must not be called")
	}
}

func TestCreateArchive_MapsServiceErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid", service.ErrArchiveInvalid, http.StatusBadRequest},
		{"too large", service.ErrArchiveTooLarge, http.StatusRequestEntityTooLarge},
		{"disabled", service.ErrArchiveDisabled, http.StatusServiceUnavailable},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePasteService{err: tc.err}
			h := NewPasteHandler(fake, testArchiveMax)

			body, ct := multipartBody(t, "file", "a.zip", []byte("PK\x03\x04"))
			rec := doArchive(h, body, ct)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("response leaks internal error: %s", rec.Body.String())
			}
		})
	}
}
