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
