package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func vcs(rev, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{GoVersion: "go1.99.0", Main: debug.Module{Path: "github.com/terva-sh/ketju", Version: "(devel)"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}}
}

// TestGetPrefersStamped: a stamped version and commit win over what the
// toolchain recorded.
func TestGetPrefersStamped(t *testing.T) {
	const c = "0123456789abcdef0123456789abcdef01234567"
	in := from("v1.0.0", c, "", vcs("ffffffffffffffffffffffffffffffffffffffff", "true"))
	if in.Version != "v1.0.0" || in.Commit != c || in.Modified {
		t.Errorf("from = %+v", in)
	}
	if got, want := in.String(), "ketju v1.0.0 (commit 0123456789ab, go1.99.0, "; !strings.HasPrefix(got, want) {
		t.Errorf("String = %q, want a prefix %q", got, want)
	}
}

// TestStampedKeepsADirtyTree: a stamped build of a dirty tree says so,
// whether the stamp or the toolchain recorded it (Terva review 1999).
func TestStampedKeepsADirtyTree(t *testing.T) {
	const c = "0123456789abcdef0123456789abcdef01234567"
	for name, in := range map[string]Info{
		"stamped modified":     from("v1.0.0", c, "true", nil),
		"the toolchain's dirt": from("v1.0.0", c, "", vcs(c, "true")),
	} {
		if !in.Modified || !strings.Contains(in.String(), "commit 0123456789ab-dirty,") {
			t.Errorf("%s: %+v says %q", name, in, in.String())
		}
	}
	if in := from("v1.0.0", c, "", vcs(c, "false")); in.Modified {
		t.Errorf("a clean tree: %+v", in)
	}
}

// TestGetFallsBackToVCS: an unstamped build reports the revision and dirt
// the toolchain recorded, and "(devel)".
func TestGetFallsBackToVCS(t *testing.T) {
	in := from("", "", "", vcs("abcdefabcdefabcdefabcdefabcdefabcdefabcd", "true"))
	if in.Version != "(devel)" || in.Commit != "abcdefabcdefabcdefabcdefabcdefabcdefabcd" || !in.Modified {
		t.Errorf("from = %+v", in)
	}
	if got := in.String(); !strings.Contains(got, "commit abcdefabcdef-dirty") {
		t.Errorf("String = %q", got)
	}
	if in := from("", "", "", nil); in.Version != "(devel)" || in.ShortCommit() != "unknown" {
		t.Errorf("with no build info: %+v", in)
	}
}
