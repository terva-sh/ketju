package threadid

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The layout, from docs/03:
//
//	data/threads/3f/7a/3f7a9c21e0000001.log
//	data/threads/3f/7a/3f7a9c21e0000001.idx
//
// Two levels of shard, taken from the first two bytes of the hex form. With the
// millisecond in bits 63..16, the first byte is bits 63..56 and steps every
// 2^40 ms, about 35 years; the second is bits 55..48 and steps every 2^32 ms,
// about 50 days. So threads created near each other land in the same directory,
// and a directory fills at the rate the board is actually used rather than all
// at once.
const (
	// ThreadsDir is the subdirectory of the data root holding every thread.
	ThreadsDir = "threads"

	// LogExt and IndexExt name the two files a thread is made of.
	LogExt   = ".log"
	IndexExt = ".idx"
)

// Dir returns the directory a thread's files live in.
//
// Pure: the shard is computed from the id, never discovered by scanning. A
// lookup is therefore one open() rather than a walk, which is the whole reason
// the layout is sharded on the id instead of on an insertion counter.
func (id ID) Dir(root string) string {
	h := id.Hex()
	return filepath.Join(root, ThreadsDir, h[0:2], h[2:4])
}

// LogPath returns the append-only log's path.
func (id ID) LogPath(root string) string {
	return filepath.Join(id.Dir(root), id.Hex()+LogExt)
}

// IndexPath returns the sidecar index's path.
func (id ID) IndexPath(root string) string {
	return filepath.Join(id.Dir(root), id.Hex()+IndexExt)
}

// FromLogPath recovers the id from a path produced by LogPath or IndexPath.
//
// For tools that walk the threads directory: fsck and dump find files and need
// to know which thread they belong to. The shard directories are checked
// against the id rather than ignored, because a file sitting in the wrong shard
// is a real fault — something moved it, or wrote it by hand — and reporting it
// is the entire job of the tool that will call this.
func FromLogPath(path string) (ID, error) {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	switch ext {
	case LogExt, IndexExt:
	default:
		return 0, fmt.Errorf("%w: %q has no thread file extension", ErrMalformed, base)
	}

	id, err := ParseHex(strings.TrimSuffix(base, ext))
	if err != nil {
		return 0, err
	}

	dir := filepath.Dir(path)
	b1 := filepath.Base(dir)
	b0 := filepath.Base(filepath.Dir(dir))
	if h := id.Hex(); b0 != h[0:2] || b1 != h[2:4] {
		return 0, fmt.Errorf("%w: %s is in shard %s/%s but belongs in %s/%s",
			ErrMalformed, base, b0, b1, h[0:2], h[2:4])
	}
	return id, nil
}
