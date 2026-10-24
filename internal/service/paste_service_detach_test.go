package service

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/susanta96/toolbox-backend/internal/model"
	"github.com/susanta96/toolbox-backend/internal/storage"
)

// ctxStore fails Put when handed a done context, like a real network client,
// and records whether a deadline was set.
type ctxStore struct {
	fakeStore
	putHadDeadline bool
}

func (c *ctxStore) Put(ctx context.Context, folder, name string, r io.Reader) (storage.StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return storage.StoredObject{}, err
	}
	_, c.putHadDeadline = ctx.Deadline()
	return c.fakeStore.Put(ctx, folder, name, r)
}

type ctxRepo struct {
	fakeRepo
	createHadDeadline bool
}

func (c *ctxRepo) Create(ctx context.Context, p *model.Paste) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, c.createHadDeadline = ctx.Deadline()
	return c.fakeRepo.Create(ctx, p)
}

func TestCreateArchivePaste_CancelledRequestStillCompletes(t *testing.T) {
	repo, store := &ctxRepo{}, &ctxStore{}
	svc := NewPasteService(repo, store, "/toolbox-pastes", testMaxBytes)
	data := zipBytes(600)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client already gone by the time the upload starts

	res, err := svc.CreateArchivePaste(ctx, "a.zip", int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.putCalls) != 1 {
		t.Fatalf("put calls = %d, want 1", len(store.putCalls))
	}
	if len(repo.created) != 1 || repo.created[0].ID != res.ID {
		t.Fatalf("paste row not created for %q: %+v", res.ID, repo.created)
	}
	if !store.putHadDeadline || !repo.createHadDeadline {
		t.Errorf("Put/Create contexts must be bounded by archiveUploadTimeout (put=%v create=%v)",
			store.putHadDeadline, repo.createHadDeadline)
	}
	if archiveUploadTimeout != 90*time.Second {
		t.Errorf("archiveUploadTimeout = %v, want 90s", archiveUploadTimeout)
	}
}
