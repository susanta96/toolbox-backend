package storage

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imagekit-developer/imagekit-go/v2/option"
)

// newTestStore points the SDK at srv for both upload and management calls and
// disables retries so error-path tests stay fast.
func newTestStore(srv *httptest.Server) *ImageKit {
	return newImageKit("fake_private_key", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

func TestImageKit_PutReturnsFileIDAndURL(t *testing.T) {
	fields := map[string]string{}
	var gotPath, gotMethod, gotFileContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FormName() == "file" {
				gotFileContent = string(b)
			} else {
				fields[p.FormName()] = string(b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fileId":"abc123","url":"https://ik.example/toolbox-pastes/x_a.zip"}`))
	}))
	defer srv.Close()

	got, err := newTestStore(srv).Put(context.Background(), "/toolbox-pastes", "x_a.zip", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := StoredObject{FileID: "abc123", URL: "https://ik.example/toolbox-pastes/x_a.zip"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "/api/v1/files/upload") {
		t.Errorf("unexpected request %s %s", gotMethod, gotPath)
	}
	if fields["folder"] != "/toolbox-pastes" {
		t.Errorf("folder = %q", fields["folder"])
	}
	if fields["fileName"] != "x_a.zip" {
		t.Errorf("fileName = %q", fields["fileName"])
	}
	if fields["useUniqueFileName"] != "false" {
		t.Errorf("useUniqueFileName = %q", fields["useUniqueFileName"])
	}
	if gotFileContent != "data" {
		t.Errorf("file content = %q", gotFileContent)
	}
}

func TestImageKit_DeleteMapsHTTP404ToErrNotFound(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"The requested file does not exist."}`))
	}))
	defer srv.Close()

	err := newTestStore(srv).Delete(context.Background(), "abc123")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if gotMethod != http.MethodDelete || !strings.HasSuffix(gotPath, "/v1/files/abc123") {
		t.Errorf("unexpected request %s %s", gotMethod, gotPath)
	}
}

func TestImageKit_DeleteSurfacesOtherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	err := newTestStore(srv).Delete(context.Background(), "abc123")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("500 must not map to ErrNotFound: %v", err)
	}
}

func TestImageKit_DeleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestStore(srv).Delete(context.Background(), "abc123"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
