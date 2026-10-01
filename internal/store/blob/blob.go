// Package blob is the on-disk side of attachments (docs/03 "Attachments"):
// where a blob's bytes live, named by their SHA-256, and how an upload gets
// them there (staging.go). The rules about who may upload, attach and read
// a blob are internal/service/blobs's; this package knows only files.
package blob

import (
	"errors"
	"path/filepath"
)

// The layout, from docs/03:
//
//	data/blobs/sha256/ab/abcd…ef
//
// One level of shard on the first byte of the hash. Hashes are uniform, so
// the 256 directories fill evenly from the first upload.
const (
	// Dir is the subdirectory of the data root holding every blob.
	Dir = "blobs"

	// Algorithm is the directory naming the hash a blob is stored under.
	Algorithm = "sha256"

	// HashLen is the length of a hash in lowercase hex.
	HashLen = 64

	// Tmp is the subdirectory of Dir where an upload is staged until its
	// hash is known (docs/03, decided by the run 2026-10-01). It is on the
	// same filesystem as the blobs, so the rename into place is atomic, and
	// it holds nothing that outlives a request: serve empties it at startup
	// and the collector removes anything in it over an hour old.
	Tmp = "tmp"
)

// ErrBadHash means a string is not a lowercase hex SHA-256.
var ErrBadHash = errors.New("blob: not a lowercase hex SHA-256")

// ValidHash reports whether h is a hash as the blobs table stores it:
// sixty-four lowercase hex digits, the same rule as the table's CHECK.
func ValidHash(h string) bool {
	if len(h) != HashLen {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Path returns where the blob named by hash is stored under root. A hash
// that is not valid is refused rather than joined, so no caller can build a
// path that climbs out of the blobs directory.
func Path(root, hash string) (string, error) {
	if !ValidHash(hash) {
		return "", ErrBadHash
	}
	return filepath.Join(root, Dir, Algorithm, hash[0:2], hash), nil
}
