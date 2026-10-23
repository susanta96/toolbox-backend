package service

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func padTo(b []byte, n int) []byte {
	out := make([]byte, n)
	copy(out, b)
	return out
}

func tarHead() []byte {
	h := make([]byte, 512)
	copy(h[257:], "ustar")
	return h
}

func TestDetectArchive_AcceptsValid(t *testing.T) {
	tests := []struct {
		name string
		file string
		head []byte
		mime string
	}{
		{"zip", "a.zip", padTo([]byte("PK\x03\x04"), 512), "application/zip"},
		{"zip uppercase ext", "A.ZIP", padTo([]byte("PK\x03\x04"), 512), "application/zip"},
		{"empty zip", "a.zip", []byte("PK\x05\x06"), "application/zip"},
		{"tar", "a.tar", tarHead(), "application/x-tar"},
		{"tar.gz", "a.tar.gz", padTo([]byte{0x1f, 0x8b, 0x08}, 512), "application/gzip"},
		{"tgz", "a.tgz", padTo([]byte{0x1f, 0x8b, 0x08}, 512), "application/gzip"},
		{"tar.gz with dirs", "some/dir/A.TAR.GZ", padTo([]byte{0x1f, 0x8b, 0x08}, 512), "application/gzip"},
		{"bz2", "a.bz2", padTo([]byte("BZh9"), 512), "application/x-bzip2"},
		{"tar.bz2", "a.tar.bz2", padTo([]byte("BZh9"), 512), "application/x-bzip2"},
		{"tar.xz", "a.tar.xz", padTo([]byte{0xfd, '7', 'z', 'X', 'Z', 0x00}, 512), "application/x-xz"},
		{"7z", "a.7z", padTo([]byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, 512), "application/x-7z-compressed"},
		{"rar", "a.rar", padTo([]byte("Rar!\x1a\x07"), 512), "application/vnd.rar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, err := DetectArchive(tt.file, tt.head)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind.Mime != tt.mime {
				t.Errorf("Mime = %q, want %q", kind.Mime, tt.mime)
			}
			if kind.Name == "" {
				t.Errorf("Name is empty")
			}
		})
	}
}

func TestDetectArchive_RejectsDisguised(t *testing.T) {
	tests := []struct {
		name string
		file string
		head []byte
	}{
		{"exe as zip", "evil.zip", padTo([]byte("MZ\x90\x00"), 512)},
		{"html as zip", "page.zip", padTo([]byte("<html><body>"), 512)},
		{"gzip magic as zip", "a.zip", padTo([]byte{0x1f, 0x8b, 0x08}, 512)},
		{"empty head", "a.zip", nil},
		{"tar without ustar", "a.tar", make([]byte, 512)},
		{"short tar", "a.tar", []byte("ustar")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DetectArchive(tt.file, tt.head); !errors.Is(err, ErrArchiveInvalid) {
				t.Fatalf("err = %v, want ErrArchiveInvalid", err)
			}
		})
	}
}

func TestDetectArchive_RejectsUnknownExtension(t *testing.T) {
	for _, file := range []string{"a.exe", "noext", "a.zip.exe", ".zip"} {
		t.Run(file, func(t *testing.T) {
			if _, err := DetectArchive(file, padTo([]byte("PK\x03\x04"), 512)); !errors.Is(err, ErrArchiveInvalid) {
				t.Fatalf("err = %v, want ErrArchiveInvalid", err)
			}
		})
	}
}

func TestSanitizeFileName(t *testing.T) {
	t.Run("path traversal", func(t *testing.T) {
		got := SanitizeFileName("../../etc/passwd.zip")
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("unsafe result %q", got)
		}
		if !strings.HasSuffix(got, ".zip") {
			t.Errorf("extension lost: %q", got)
		}
	})
	t.Run("backslash traversal", func(t *testing.T) {
		got := SanitizeFileName(`..\..\evil.zip`)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") || !strings.HasSuffix(got, ".zip") {
			t.Errorf("unsafe result %q", got)
		}
	})
	t.Run("long name", func(t *testing.T) {
		got := SanitizeFileName(strings.Repeat("a", 300) + ".zip")
		if utf8.RuneCountInString(got) > 120 {
			t.Errorf("length %d > 120", utf8.RuneCountInString(got))
		}
		if !strings.HasSuffix(got, ".zip") {
			t.Errorf("extension lost: %q", got)
		}
	})
	t.Run("unicode kept, unsafe replaced", func(t *testing.T) {
		if got := SanitizeFileName("résumé final.zip"); got != "résumé final.zip" {
			t.Errorf("got %q", got)
		}
		got := SanitizeFileName("a\x00b\nc:d.zip")
		if strings.ContainsAny(got, "\x00\n:") {
			t.Errorf("unsafe chars remain: %q", got)
		}
		if !strings.HasSuffix(got, ".zip") {
			t.Errorf("extension lost: %q", got)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		for _, in := range []string{"", "///", ".."} {
			if got := SanitizeFileName(in); got != "archive" {
				t.Errorf("SanitizeFileName(%q) = %q, want archive", in, got)
			}
		}
		if got := SanitizeFileName("\x00"); got == "" || strings.ContainsRune(got, 0) {
			t.Errorf("bad result for NUL: %q", got)
		}
	})
}
