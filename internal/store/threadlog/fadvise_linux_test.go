//go:build linux

package threadlog

import (
	"fmt"
	"os"
	"syscall"
)

// dropFromPageCache asks the kernel to forget a file's pages, which is how the
// cold-read measurement is made cold without root or a reboot.
//
// POSIX_FADV_DONTNEED is advisory and only evicts clean pages, so the file is
// synced first. It is not a guarantee — the kernel may keep pages another
// process has mapped — so the benchmark reports what it measured rather than
// claiming the cache was definitely empty.
func dropFromPageCache(f *os.File) error {
	if err := f.Sync(); err != nil {
		return err
	}
	const posixFadvDontNeed = 4
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FADVISE64, f.Fd(), 0, 0, posixFadvDontNeed, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func canDropPageCache() bool { return true }

// Filesystem magic numbers for the two filesystems where an fsync is a no-op.
const (
	tmpfsMagic = 0x01021994
	ramfsMagic = 0x858458f6
)

// checkDurableFS refuses a directory whose fsync cannot possibly reach a disk.
//
// This exists because the first version of these benchmarks wrote to
// b.TempDir(), TMPDIR was unset, /tmp is tmpfs, and every durability number
// was measured against RAM: SyncAlways came out at 4.8us against SyncNever at
// 4.0us, which is not a fast disk but an absent one. The numbers looked
// plausible enough to write down, which is what makes it worth a hard failure
// rather than a comment.
func checkDurableFS(dir string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return fmt.Errorf("statfs %s: %w", dir, err)
	}
	switch uint32(st.Type) {
	case tmpfsMagic:
		return fmt.Errorf("%s is tmpfs: fsync there is a no-op, so every durability "+
			"number would be a memory benchmark wearing a disk's name", dir)
	case ramfsMagic:
		return fmt.Errorf("%s is ramfs: fsync there is a no-op", dir)
	}
	return nil
}
