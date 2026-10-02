package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/buildinfo"
	"github.com/terva-sh/ketju/internal/lock"
)

// TestVersionCommand: `ketju version` prints one line naming the version,
// the short commit, the Go version and the platform (TKT-01M29CXK3S).
func TestVersionCommand(t *testing.T) {
	orig := buildInfo
	t.Cleanup(func() { buildInfo = orig })
	buildInfo = func() buildinfo.Info {
		return buildinfo.Info{Version: "v1.2.3", Commit: "0123456789abcdef0123456789abcdef01234567",
			GoVersion: "go1.27.1", OS: "linux", Arch: "arm64"}
	}
	var out bytes.Buffer
	if err := versionMain(&out, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "ketju v1.2.3 (commit 0123456789ab, go1.27.1, linux/arm64)\n"; got != want {
		t.Errorf("ketju version = %q, want %q", got, want)
	}
	if err := versionMain(&out, []string{"x"}); exitCodeFor(err) != exitUsage {
		t.Errorf("ketju version x = %v, want a usage error", err)
	}

	// The real one, unstamped: this test binary is a development build.
	buildInfo = orig
	out.Reset()
	if err := versionMain(&out, nil); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^ketju \S+ \(commit \S+, go\S+, \w+/\w+\)\n$`).MatchString(out.String()) {
		t.Errorf("ketju version = %q", out.String())
	}
}

// TestLockRecordsVersion: the lock record and serve's start line carry the
// version buildinfo reports.
func TestLockRecordsVersion(t *testing.T) {
	path, data := writeConfig(t, "")
	r := start(t, path, serveOpts{})
	b, err := os.ReadFile(filepath.Join(data, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var h lock.Holder
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("the lock record: %v\n%s", err, b)
	}
	want := buildinfo.Get()
	if h.Version != want.Version {
		t.Errorf("the lock records version %q, want %q", h.Version, want.Version)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.log.String(), `version=`+want.Version) && !strings.Contains(r.log.String(), `"version":"`+want.Version) {
		t.Errorf("the start line does not carry the version %q:\n%s", want.Version, r.log)
	}
}
