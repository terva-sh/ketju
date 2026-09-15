//go:build !linux

package threadlog

import (
	"errors"
	"os"
)

func dropFromPageCache(*os.File) error { return errors.New("page cache eviction needs Linux") }

func canDropPageCache() bool { return false }

// checkDurableFS cannot identify the filesystem portably. The fsync floor
// benchmark reports what fsync actually costs, which is the check that
// matters; this one only catches the obvious case early.
func checkDurableFS(string) error { return nil }
