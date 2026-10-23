// Package storage abstracts object storage for archive uploads.
package storage

import (
	"context"
	"errors"
	"io"
)

// StoredObject identifies an object held by the backing store.
type StoredObject struct {
	FileID string
	URL    string
}

// ErrNotFound is returned (wrapped) when the object no longer exists.
var ErrNotFound = errors.New("storage: object not found")

// ObjectStore stores and removes opaque binary objects.
type ObjectStore interface {
	Put(ctx context.Context, folder, name string, r io.Reader) (StoredObject, error)
	// Delete returns ErrNotFound (wrapped) when the object is already gone.
	Delete(ctx context.Context, fileID string) error
}
