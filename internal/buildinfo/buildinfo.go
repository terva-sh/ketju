// Package buildinfo says which build of ketju is running: the version and
// commit a release stamps in, or what the Go toolchain recorded when nothing
// was stamped (TKT-01M29CXK3S).
//
// A release build sets the two variables below with
//
//	-ldflags "-X github.com/terva-sh/ketju/internal/buildinfo.version=v1.0.0
//	          -X github.com/terva-sh/ketju/internal/buildinfo.commit=<sha>"
//
// which tools/release does. A development build leaves them empty, and Get
// falls back to debug.ReadBuildInfo, so `go build` from a checkout still
// reports the commit it was built from.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set by -ldflags -X in a release build.
var (
	version string
	commit  string
)

// Info is one build's identity.
type Info struct {
	// Version is the release version, such as v1.0.0, or "(devel)".
	Version string
	// Commit is the full commit hash, or "" when nothing recorded one.
	Commit string
	// Modified is true when the build's working tree had uncommitted
	// changes, as the toolchain recorded it.
	Modified  bool
	GoVersion string
	OS, Arch  string
}

// Get is the running binary's Info.
func Get() Info {
	bi, _ := debug.ReadBuildInfo()
	return from(version, commit, bi)
}

// from builds an Info from the stamped values and the toolchain's record,
// which may be nil. The stamped values win.
func from(v, c string, bi *debug.BuildInfo) Info {
	in := Info{Version: v, Commit: c, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if bi != nil {
		if in.Version == "" && bi.Main.Version != "" {
			in.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if in.Commit == "" {
					in.Commit = s.Value
				}
			case "vcs.modified":
				// A stamped release is built from a clean tree: tools/release
				// refuses a dirty one unless told otherwise, and says so then.
				if c == "" {
					in.Modified = s.Value == "true"
				}
			}
		}
		if bi.GoVersion != "" {
			in.GoVersion = bi.GoVersion
		}
	}
	if in.Version == "" {
		in.Version = "(devel)"
	}
	return in
}

// ShortCommit is the commit's first 12 characters, or "unknown".
func (i Info) ShortCommit() string {
	switch {
	case i.Commit == "":
		return "unknown"
	case len(i.Commit) > 12:
		return i.Commit[:12]
	}
	return i.Commit
}

// String is the line `ketju version` prints:
// ketju v1.0.0 (commit 0123456789ab, go1.27.1, linux/amd64).
func (i Info) String() string {
	c := i.ShortCommit()
	if i.Modified {
		c += "-dirty"
	}
	return fmt.Sprintf("ketju %s (commit %s, %s, %s/%s)", i.Version, c, i.GoVersion, i.OS, i.Arch)
}
