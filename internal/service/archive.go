package service

import (
	"bytes"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ArchiveKind describes a supported archive format.
type ArchiveKind struct {
	Name string
	Mime string
}

// ErrArchiveInvalid is returned when a file is not a supported archive, or its
// contents do not match what its extension claims.
var ErrArchiveInvalid = errors.New("unsupported or corrupt archive")

const (
	maxFileNameLen   = 120
	maxFileNameBytes = 200
	maxExtensionLen  = 16
	fallbackFileName = "archive"
	tarMagicOffset   = 257
)

var (
	kindZip   = ArchiveKind{Name: "zip", Mime: "application/zip"}
	kindTar   = ArchiveKind{Name: "tar", Mime: "application/x-tar"}
	kindGzip  = ArchiveKind{Name: "gzip", Mime: "application/gzip"}
	kindBzip2 = ArchiveKind{Name: "bzip2", Mime: "application/x-bzip2"}
	kindXz    = ArchiveKind{Name: "xz", Mime: "application/x-xz"}
	kind7z    = ArchiveKind{Name: "7z", Mime: "application/x-7z-compressed"}
	kindRar   = ArchiveKind{Name: "rar", Mime: "application/vnd.rar"}
)

// archiveExtensions maps a lower-case file name suffix to its archive kind.
// Compound suffixes such as .tar.gz map to the outer compression format.
var archiveExtensions = []struct {
	suffix string
	kind   ArchiveKind
}{
	{".tar.gz", kindGzip},
	{".tgz", kindGzip},
	{".tar.bz2", kindBzip2},
	{".tbz2", kindBzip2},
	{".tar.xz", kindXz},
	{".txz", kindXz},
	{".zip", kindZip},
	{".tar", kindTar},
	{".gz", kindGzip},
	{".bz2", kindBzip2},
	{".xz", kindXz},
	{".7z", kind7z},
	{".rar", kindRar},
}

// DetectArchive determines the archive kind from the file name extension and
// verifies that the leading bytes (head: the first >=512 bytes of the file, or
// the whole file if shorter) match that kind's magic number. The extension and
// content must agree, otherwise ErrArchiveInvalid is returned.
func DetectArchive(fileName string, head []byte) (ArchiveKind, error) {
	base := strings.ToLower(lastPathSegment(fileName))

	for _, e := range archiveExtensions {
		// Require a non-empty stem so that a bare ".zip" is not accepted.
		if len(base) > len(e.suffix) && strings.HasSuffix(base, e.suffix) {
			if hasMagic(e.kind, head) {
				return e.kind, nil
			}
			return ArchiveKind{}, ErrArchiveInvalid
		}
	}
	return ArchiveKind{}, ErrArchiveInvalid
}

func hasMagic(kind ArchiveKind, head []byte) bool {
	switch kind {
	case kindZip:
		// Local file header (PK\x03\x04), empty archive (PK\x05\x06) or
		// spanned archive marker (PK\x07\x08).
		return bytes.HasPrefix(head, []byte("PK\x03\x04")) ||
			bytes.HasPrefix(head, []byte("PK\x05\x06")) ||
			bytes.HasPrefix(head, []byte("PK\x07\x08"))
	case kindTar:
		// POSIX/GNU tars carry "ustar" at offset 257. Legacy v7 tars have no
		// such marker and are deliberately rejected as unverifiable.
		return len(head) >= tarMagicOffset+5 &&
			bytes.Equal(head[tarMagicOffset:tarMagicOffset+5], []byte("ustar"))
	case kindGzip:
		return bytes.HasPrefix(head, []byte{0x1f, 0x8b})
	case kindBzip2:
		return bytes.HasPrefix(head, []byte("BZh"))
	case kindXz:
		return bytes.HasPrefix(head, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00})
	case kind7z:
		return bytes.HasPrefix(head, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c})
	case kindRar:
		return bytes.HasPrefix(head, []byte("Rar!\x1a\x07"))
	}
	return false
}

// lastPathSegment returns the part of name after the last / or \.
func lastPathSegment(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

// SanitizeFileName returns a safe display/storage name: at most 120 characters
// and 200 bytes,
// no path separators or control characters, extension preserved, and never
// empty (falls back to "archive").
func SanitizeFileName(name string) string {
	name = strings.TrimSpace(lastPathSegment(name))
	if strings.Trim(name, ".") == "" {
		return fallbackFileName
	}

	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*`, r) || r == unicode.ReplacementChar {
			return '_'
		}
		return r
	}, name)
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", "_")
	}

	if utf8.RuneCountInString(name) > maxFileNameLen || len(name) > maxFileNameBytes {
		name = truncateFileName(name)
	}

	out := strings.TrimSpace(name)
	if out == "" {
		return fallbackFileName
	}
	return out
}

// truncateFileName shortens name to at most maxFileNameLen runes and
// maxFileNameBytes bytes, cutting only on rune boundaries and keeping a short
// extension. Dots and spaces left dangling at the cut are dropped so that the
// stem joined with the extension never contains "..".
func truncateFileName(name string) string {
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		if e := name[i:]; utf8.RuneCountInString(e) <= maxExtensionLen && len(e) <= maxExtensionLen*utf8.UTFMax {
			ext = e
		}
	}
	stem := strings.TrimSuffix(name, ext)

	runeBudget := maxFileNameLen - utf8.RuneCountInString(ext)
	byteBudget := maxFileNameBytes - len(ext)

	cut, runes := 0, 0
	for i, r := range stem {
		size := utf8.RuneLen(r)
		if runes+1 > runeBudget || i+size > byteBudget {
			break
		}
		runes++
		cut = i + size
	}
	stem = strings.TrimRight(stem[:cut], ". ")
	if stem == "" {
		stem = fallbackFileName
	}
	return stem + ext
}
