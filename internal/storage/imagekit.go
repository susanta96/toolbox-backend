package storage

// SDK notes (github.com/imagekit-developer/imagekit-go/v2 v2.12.0), verified from source:
//   - Upload:  client.Files.Upload(ctx, imagekit.FileUploadParams{File io.Reader,
//     FileName string, Folder/UseUniqueFileName/IsPrivateFile param.Opt[...]}).
//     Optional fields use param.Opt, built with imagekit.String / imagekit.Bool.
//     Response is *imagekit.FileUploadResponse with FileID and URL.
//   - Delete:  client.Files.Delete(ctx, fileID) returns only error.
//   - The service is client.Files (plural), not client.File.
//   - Errors:  non-2xx responses are *imagekit.Error (alias of apierror.Error) with
//     StatusCode; use errors.As.
//   - Base URL: Upload hard-codes https://upload.imagekit.io/ but prepends it BEFORE the
//     client-level options, so a client-level option.WithBaseURL overrides it. One
//     WithBaseURL therefore redirects both upload and delete to an httptest server.
//   - NewClient also reads IMAGEKIT_PRIVATE_KEY from the environment; the explicit
//     option.WithPrivateKey passed here is applied afterwards and wins.
//   - The SDK retries 5xx/429 by default (2 retries); tests disable it via WithMaxRetries(0).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	imagekit "github.com/imagekit-developer/imagekit-go/v2"
	"github.com/imagekit-developer/imagekit-go/v2/option"
)

// ImageKit is an ObjectStore backed by ImageKit.io.
type ImageKit struct {
	client imagekit.Client
}

var _ ObjectStore = (*ImageKit)(nil)

// NewImageKit builds a store authenticated with the given private key.
func NewImageKit(privateKey string) *ImageKit {
	return newImageKit(privateKey)
}

func newImageKit(privateKey string, opts ...option.RequestOption) *ImageKit {
	all := append([]option.RequestOption{option.WithPrivateKey(privateKey)}, opts...)
	return &ImageKit{client: imagekit.NewClient(all...)}
}

// Put uploads r as a public file named name in folder, without a unique suffix.
func (s *ImageKit) Put(ctx context.Context, folder, name string, r io.Reader) (StoredObject, error) {
	res, err := s.client.Files.Upload(ctx, imagekit.FileUploadParams{
		File:              r,
		FileName:          name,
		Folder:            imagekit.String(folder),
		UseUniqueFileName: imagekit.Bool(false),
		IsPrivateFile:     imagekit.Bool(false),
	})
	if err != nil {
		return StoredObject{}, fmt.Errorf("storage: imagekit upload: %w", err)
	}
	return StoredObject{FileID: res.FileID, URL: res.URL}, nil
}

// Delete removes the file; an already-missing file yields ErrNotFound.
func (s *ImageKit) Delete(ctx context.Context, fileID string) error {
	return mapDeleteErr(fileID, s.client.Files.Delete(ctx, fileID))
}

func mapDeleteErr(fileID string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *imagekit.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, fileID)
	}
	return fmt.Errorf("storage: imagekit delete %s: %w", fileID, err)
}
