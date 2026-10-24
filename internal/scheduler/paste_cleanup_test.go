package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/storage"
)

// calls is a shared, ordered record of fake interactions.
type calls struct{ log []string }

func (c *calls) add(s string) { c.log = append(c.log, s) }

type fakeExpiredStore struct {
	rec     *calls
	expired []model.ExpiredPaste
}

func (f *fakeExpiredStore) GetExpired(ctx context.Context) ([]model.ExpiredPaste, error) {
	return f.expired, nil
}

func (f *fakeExpiredStore) DeleteByID(ctx context.Context, id string) error {
	f.rec.add("row:" + id)
	return nil
}

type fakeObjectStore struct {
	rec    *calls
	errFor map[string]error
}

func (f *fakeObjectStore) Put(ctx context.Context, folder, name string, r io.Reader) (storage.StoredObject, error) {
	return storage.StoredObject{}, errors.New("unexpected Put")
}

func (f *fakeObjectStore) Delete(ctx context.Context, fileID string) error {
	f.rec.add("file:" + fileID)
	return f.errFor[fileID]
}

func strPtr(s string) *string { return &s }

func TestRunOnce_DeletesStorageThenRow(t *testing.T) {
	rec := &calls{}
	repo := &fakeExpiredStore{rec: rec, expired: []model.ExpiredPaste{{ID: "p1", StorageFileID: strPtr("f1")}}}
	store := &fakeObjectStore{rec: rec}

	deleted, failed := NewPasteCleanup(repo, store).runOnce(context.Background())

	if deleted != 1 || failed != 0 {
		t.Fatalf("deleted=%d failed=%d, want 1, 0", deleted, failed)
	}
	want := []string{"file:f1", "row:p1"}
	if !reflect.DeepEqual(rec.log, want) {
		t.Fatalf("call order = %v, want %v", rec.log, want)
	}
}

func TestRunOnce_TextPasteSkipsStorage(t *testing.T) {
	rec := &calls{}
	repo := &fakeExpiredStore{rec: rec, expired: []model.ExpiredPaste{{ID: "p1"}}}
	store := &fakeObjectStore{rec: rec}

	deleted, failed := NewPasteCleanup(repo, store).runOnce(context.Background())

	if deleted != 1 || failed != 0 {
		t.Fatalf("deleted=%d failed=%d, want 1, 0", deleted, failed)
	}
	want := []string{"row:p1"}
	if !reflect.DeepEqual(rec.log, want) {
		t.Fatalf("calls = %v, want %v", rec.log, want)
	}
}

func TestRunOnce_StorageErrorKeepsRow(t *testing.T) {
	rec := &calls{}
	repo := &fakeExpiredStore{rec: rec, expired: []model.ExpiredPaste{
		{ID: "bad", StorageFileID: strPtr("fbad")},
		{ID: "good", StorageFileID: strPtr("fgood")},
	}}
	store := &fakeObjectStore{rec: rec, errFor: map[string]error{"fbad": errors.New("boom")}}

	deleted, failed := NewPasteCleanup(repo, store).runOnce(context.Background())

	if deleted != 1 || failed != 1 {
		t.Fatalf("deleted=%d failed=%d, want 1, 1", deleted, failed)
	}
	want := []string{"file:fbad", "file:fgood", "row:good"}
	if !reflect.DeepEqual(rec.log, want) {
		t.Fatalf("calls = %v, want %v", rec.log, want)
	}
}

func TestRunOnce_StorageNotFoundStillDeletesRow(t *testing.T) {
	rec := &calls{}
	repo := &fakeExpiredStore{rec: rec, expired: []model.ExpiredPaste{{ID: "p1", StorageFileID: strPtr("f1")}}}
	store := &fakeObjectStore{rec: rec, errFor: map[string]error{"f1": fmt.Errorf("%w: x", storage.ErrNotFound)}}

	deleted, failed := NewPasteCleanup(repo, store).runOnce(context.Background())

	if deleted != 1 || failed != 0 {
		t.Fatalf("deleted=%d failed=%d, want 1, 0", deleted, failed)
	}
	want := []string{"file:f1", "row:p1"}
	if !reflect.DeepEqual(rec.log, want) {
		t.Fatalf("calls = %v, want %v", rec.log, want)
	}
}

func TestRunOnce_NilStoreKeepsArchiveRows(t *testing.T) {
	rec := &calls{}
	repo := &fakeExpiredStore{rec: rec, expired: []model.ExpiredPaste{
		{ID: "arch", StorageFileID: strPtr("f1")},
		{ID: "text"},
	}}

	deleted, failed := NewPasteCleanup(repo, nil).runOnce(context.Background())

	if deleted != 1 || failed != 1 {
		t.Fatalf("deleted=%d failed=%d, want 1, 1", deleted, failed)
	}
	want := []string{"row:text"}
	if !reflect.DeepEqual(rec.log, want) {
		t.Fatalf("calls = %v, want %v", rec.log, want)
	}
}
