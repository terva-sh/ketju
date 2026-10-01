package blobs

import "context"

// The seams blobs_test reaches through.

// SetAfterCommit makes the upload's transaction run f once its file is in
// place, and fail with f's error.
func SetAfterCommit(s *Service, f func() error) { s.afterCommit = f }

// SetCleanup replaces the write that removes the file of an upload whose
// transaction failed.
func SetCleanup(s *Service, f func(ctx context.Context, hash string) error) { s.cleanup = f }

// CleanName is cleanName.
var CleanName = cleanName

// MakeThumb is makeThumb: the thumbnail for hash made, waited for, or
// refused as failed before, without Authorize.
func MakeThumb(s *Service, ctx context.Context, hash, mime string) error {
	return s.makeThumb(ctx, hash, mime)
}

// FailedSet is the failure list, for its own test.
type FailedSet = lru

// NewFailedSet is newLRU.
var NewFailedSet = newLRU

func (l *lru) Has(k string) bool { return l.has(k) }
func (l *lru) Add(k string)      { l.add(k) }
