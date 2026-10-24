package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/storage"
)

type fakeRepo struct {
	created   []*model.Paste
	createErr error
	onCreate  func() // optional hook run at the start of Create
	getResult *model.Paste
	getErr    error
}

func (f *fakeRepo) Create(_ context.Context, p *model.Paste) error {
	if f.onCreate != nil {
		f.onCreate()
	}
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, p)
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _ string) (*model.Paste, error) {
	return f.getResult, f.getErr
}

func (f *fakeRepo) IncrementViewCount(_ context.Context, _ string) error { return nil }

func (f *fakeRepo) IDExists(_ context.Context, _ string) (bool, error) { return false, nil }

type putCall struct {
	folder string
	name   string
	body   []byte
}

type fakeStore struct {
	putCalls    []putCall
	putErr      error
	deleteCalls []string
	deleteErr   error
	// deleteCtxErrs records ctx.Err() as seen by each Delete call.
	deleteCtxErrs []error
}

func (f *fakeStore) Put(_ context.Context, folder, name string, r io.Reader) (storage.StoredObject, error) {
	if f.putErr != nil {
		return storage.StoredObject{}, f.putErr
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return storage.StoredObject{}, err
	}
	f.putCalls = append(f.putCalls, putCall{folder: folder, name: name, body: body})
	return storage.StoredObject{FileID: "file-123", URL: "https://cdn.example/" + name}, nil
}

// Delete honours ctx like a real network client: it fails if ctx is done.
func (f *fakeStore) Delete(ctx context.Context, fileID string) error {
	f.deleteCalls = append(f.deleteCalls, fileID)
	f.deleteCtxErrs = append(f.deleteCtxErrs, ctx.Err())
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.deleteErr
}

const testMaxBytes int64 = 1024

// zipBytes returns n bytes that start with a valid zip local file header.
func zipBytes(n int) []byte {
	return append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0x01}, n-4)...)
}

func newArchiveService(repo *fakeRepo, store storage.ObjectStore) *PasteService {
	return NewPasteService(repo, store, "/toolbox-pastes", testMaxBytes)
}

func TestCreateArchivePaste_StoresAndExpiresIn24h(t *testing.T) {
	repo, store := &fakeRepo{}, &fakeStore{}
	svc := newArchiveService(repo, store)
	data := zipBytes(600)

	res, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := time.Now().Add(24 * time.Hour)
	if res.ExpiresAt == nil || res.ExpiresAt.Sub(want).Abs() > 5*time.Second {
		t.Fatalf("ExpiresAt = %v, want about %v", res.ExpiresAt, want)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d pastes, want 1", len(repo.created))
	}
	p := repo.created[0]
	if p.ID != res.ID {
		t.Errorf("paste ID = %q, result ID = %q", p.ID, res.ID)
	}
	if p.Content != "" {
		t.Errorf("Content = %q, want empty", p.Content)
	}
	if p.Language != "text" {
		t.Errorf("Language = %q, want text", p.Language)
	}
	if p.StorageFileID == nil || *p.StorageFileID != "file-123" {
		t.Errorf("StorageFileID = %v", p.StorageFileID)
	}
	if p.StorageURL == nil || *p.StorageURL != "https://cdn.example/"+res.ID+"_a.zip" {
		t.Errorf("StorageURL = %v", p.StorageURL)
	}
	if p.FileSize == nil || *p.FileSize != int64(len(data)) {
		t.Errorf("FileSize = %v, want %d", p.FileSize, len(data))
	}
	if p.MimeType == nil || *p.MimeType != "application/zip" {
		t.Errorf("MimeType = %v", p.MimeType)
	}
	if p.FileName == nil || *p.FileName != "a.zip" {
		t.Errorf("FileName = %v", p.FileName)
	}
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(*res.ExpiresAt) {
		t.Errorf("paste ExpiresAt = %v, result = %v", p.ExpiresAt, res.ExpiresAt)
	}

	if len(store.putCalls) != 1 {
		t.Fatalf("Put called %d times, want 1", len(store.putCalls))
	}
	if got := store.putCalls[0]; got.folder != "/toolbox-pastes" || got.name != res.ID+"_a.zip" {
		t.Errorf("Put(folder=%q, name=%q), want folder /toolbox-pastes name %s_a.zip", got.folder, got.name, res.ID)
	}
}

func TestCreateArchivePaste_PassesFullBytesToStore(t *testing.T) {
	repo, store := &fakeRepo{}, &fakeStore{}
	svc := newArchiveService(repo, store)
	data := zipBytes(900) // larger than the 512 byte peek window

	if _, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.putCalls) != 1 {
		t.Fatalf("Put called %d times, want 1", len(store.putCalls))
	}
	if !bytes.Equal(store.putCalls[0].body, data) {
		t.Errorf("store received %d bytes, want the original %d bytes including the head", len(store.putCalls[0].body), len(data))
	}
}

func TestCreateArchivePaste_IgnoresNeverTTL(t *testing.T) {
	repo, store := &fakeRepo{}, &fakeStore{}
	svc := newArchiveService(repo, store)
	data := zipBytes(100)

	res, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExpiresAt == nil {
		t.Fatal("result ExpiresAt is nil, archives must always expire")
	}
	if repo.created[0].ExpiresAt == nil {
		t.Fatal("created paste ExpiresAt is nil, archives must always expire")
	}
}

func TestCreateArchivePaste_SizeBoundaries(t *testing.T) {
	t.Run("size equals max succeeds", func(t *testing.T) {
		repo, store := &fakeRepo{}, &fakeStore{}
		svc := newArchiveService(repo, store)
		data := zipBytes(int(testMaxBytes))

		if _, err := svc.CreateArchivePaste(context.Background(), "a.zip", testMaxBytes, bytes.NewReader(data)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(store.putCalls) != 1 {
			t.Errorf("Put called %d times, want 1", len(store.putCalls))
		}
	})

	t.Run("size max+1 is too large", func(t *testing.T) {
		repo, store := &fakeRepo{}, &fakeStore{}
		svc := newArchiveService(repo, store)
		data := zipBytes(int(testMaxBytes) + 1)

		_, err := svc.CreateArchivePaste(context.Background(), "a.zip", testMaxBytes+1, bytes.NewReader(data))
		if !errors.Is(err, ErrArchiveTooLarge) {
			t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
		}
		if len(store.putCalls) != 0 {
			t.Error("Put must not be called for an oversized archive")
		}
	})

	t.Run("size zero is invalid", func(t *testing.T) {
		repo, store := &fakeRepo{}, &fakeStore{}
		svc := newArchiveService(repo, store)

		_, err := svc.CreateArchivePaste(context.Background(), "a.zip", 0, bytes.NewReader(nil))
		if !errors.Is(err, ErrArchiveInvalid) {
			t.Fatalf("err = %v, want ErrArchiveInvalid", err)
		}
		if len(store.putCalls) != 0 {
			t.Error("Put must not be called for an empty archive")
		}
	})
}

func TestCreateArchivePaste_RejectsDisguisedBeforeUpload(t *testing.T) {
	repo, store := &fakeRepo{}, &fakeStore{}
	svc := newArchiveService(repo, store)
	data := []byte("MZ" + strings.Repeat("\x00", 98))

	_, err := svc.CreateArchivePaste(context.Background(), "x.zip", int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, ErrArchiveInvalid) {
		t.Fatalf("err = %v, want ErrArchiveInvalid", err)
	}
	if len(store.putCalls) != 0 {
		t.Error("Put must not be called for a disguised file")
	}
	if len(repo.created) != 0 {
		t.Error("no row must be created for a disguised file")
	}
}

func TestCreateArchivePaste_DBFailureDeletesUploadedFile(t *testing.T) {
	dbErr := errors.New("db down")
	repo, store := &fakeRepo{createErr: dbErr}, &fakeStore{}
	svc := newArchiveService(repo, store)
	data := zipBytes(100)

	_, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, dbErr) {
		t.Fatalf("err = %v, want the insert error", err)
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != "file-123" {
		t.Errorf("Delete calls = %v, want exactly [file-123]", store.deleteCalls)
	}
}

func TestCreateArchivePaste_DBFailureWithCancelledCtxStillDeletesFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The client disconnects while the insert is in flight.
	repo := &fakeRepo{createErr: context.Canceled, onCreate: cancel}
	store := &fakeStore{}
	svc := newArchiveService(repo, store)
	data := zipBytes(100)

	_, err := svc.CreateArchivePaste(ctx, "a.zip", int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the original insert error", err)
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != "file-123" {
		t.Fatalf("Delete calls = %v, want exactly [file-123]", store.deleteCalls)
	}
	if store.deleteCtxErrs[0] != nil {
		t.Errorf("Delete received a done context (%v); cleanup must use a detached context", store.deleteCtxErrs[0])
	}
}

func TestCreateArchivePaste_UploadFailureCreatesNoRow(t *testing.T) {
	putErr := errors.New("imagekit down")
	repo, store := &fakeRepo{}, &fakeStore{putErr: putErr}
	svc := newArchiveService(repo, store)
	data := zipBytes(100)

	_, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, putErr) {
		t.Fatalf("err = %v, want the upload error", err)
	}
	if len(repo.created) != 0 {
		t.Error("no row must be created when the upload fails")
	}
	if len(store.deleteCalls) != 0 {
		t.Error("nothing to delete when the upload fails")
	}
}

func TestCreateArchivePaste_DisabledWhenNoStore(t *testing.T) {
	repo := &fakeRepo{}
	svc := newArchiveService(repo, nil)
	data := zipBytes(100)

	_, err := svc.CreateArchivePaste(context.Background(), "a.zip", int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("err = %v, want ErrArchiveDisabled", err)
	}
	if len(repo.created) != 0 {
		t.Error("no row must be created when archives are disabled")
	}
}

func TestGetPaste_ExpiredArchiveReturnsExpiredError(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	url, fileID := "https://cdn.example/secret.zip", "file-123"
	repo := &fakeRepo{getResult: &model.Paste{
		ID:            "abc123",
		ExpiresAt:     &past,
		StorageURL:    &url,
		StorageFileID: &fileID,
	}}
	svc := newArchiveService(repo, &fakeStore{})

	paste, err := svc.GetPaste(context.Background(), "abc123")
	if err == nil || err.Error() != "paste has expired" {
		t.Fatalf("err = %v, want \"paste has expired\"", err)
	}
	if paste != nil {
		t.Error("paste must be nil so storage_url cannot leak")
	}
}
