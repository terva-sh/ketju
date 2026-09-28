package db

import (
	"runtime/debug"
	"slices"
	"testing"
)

// ncrucesDirSyncBug lists the ncruces/go-sqlite3 versions read and found to
// skip the directory fsync SQLite asks for after creating a WAL or journal
// file. In vfs/file.go, (*vfsFile).Sync opens the parent directory and then
// syncs the file a second time, osSync(f.File, ...), instead of the directory
// handle d (TKT-01M392DP19). Open's syncDir call after ensureWAL, and
// FCNTL_PERSIST_WAL in openPool, exist because of it.
var ncrucesDirSyncBug = []string{"v0.35.6"}

// TestNcrucesDirSyncWorkaroundIsPinned fails whenever the build uses an
// ncruces version nobody has checked for the directory-fsync bug. Upgrading
// is then a decision, not an accident: read (*vfsFile).Sync in the new
// version's vfs/file.go.
//
//   - Still osSync(f.File, ...) in the _FLAG_SYNC_DIR branch: add the version
//     to ncrucesDirSyncBug. The workaround stays.
//   - Fixed to sync the directory handle: TKT-01M392DP19 says what may then be
//     removed and what docs/04 must say. Keep a test that the data directory
//     is still fsynced once the WAL exists, whoever does it.
func TestNcrucesDirSyncWorkaroundIsPinned(t *testing.T) {
	const mod = "github.com/ncruces/go-sqlite3"
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("no build info in the test binary, so the driver version cannot be checked")
	}
	for _, d := range bi.Deps {
		if d.Path != mod {
			continue
		}
		if d.Replace != nil {
			t.Fatalf("%s is replaced by %s %s; check its (*vfsFile).Sync for the directory-fsync bug (TKT-01M392DP19) and extend this test",
				mod, d.Replace.Path, d.Replace.Version)
		}
		if !slices.Contains(ncrucesDirSyncBug, d.Version) {
			t.Fatalf("%s is at %s, which nobody has checked for the directory-fsync bug (TKT-01M392DP19). Read (*vfsFile).Sync in its vfs/file.go and follow this test's doc comment",
				mod, d.Version)
		}
		return
	}
	t.Fatalf("%s is not in the build; if the driver changed, re-check what Open's directory fsync is working around", mod)
}
