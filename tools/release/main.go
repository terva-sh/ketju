// Command release builds ketju's release artifacts and checks them, and
// publishes nothing (TKT-01M29CXK3S). Publishing from a tag is
// TKT-01M3YSVEDJ's, and stays with the maintainer (docs/15, stop class 3).
//
//	go run ./tools/release --version v1.0.0 --out DIR [--allow-dirty]
//
// For each platform it builds ./cmd/ketju with CGO_ENABLED=0 and -trimpath,
// stamping the version and commit into internal/buildinfo, and then fails
// unless:
//   - the binary records CGO_ENABLED=0, -trimpath, the target platform, and
//     no build tags and no -race (so an e2e build cannot be released);
//   - the ELF has no interpreter and needs no shared library;
//   - no package outside the standard library in cmd/ketju's dependency
//     graph has cgo files, even with cgo enabled.
//
// It writes DIR/SHA256SUMS in sha256sum's format, reads it back and verifies
// every file, and runs the host's binary to see it report the version and
// commit. CI runs it on every change, as a dry run.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Platforms are what a release is built for: a VPS and a 64-bit home
// server (docs/02, decided by the run 2026-10-02, pending maintainer
// review).
var Platforms = []Platform{{"linux", "amd64"}, {"linux", "arm64"}}

// Platform is one GOOS/GOARCH pair.
type Platform struct{ OS, Arch string }

func (p Platform) String() string { return p.OS + "/" + p.Arch }

const buildinfoPkg = "github.com/terva-sh/ketju/internal/buildinfo"

// moduleRoot is where go and the VCS run: the module's root, which is where
// `go run ./tools/release` is run from. Tests point it up from their
// package.
var moduleRoot = "."

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	version := fs.String("version", "", "the version to stamp, such as v1.0.0 (required)")
	dir := fs.String("out", "", "the directory to write the binaries and SHA256SUMS to (required)")
	allowDirty := fs.Bool("allow-dirty", false, "build from a working tree with uncommitted changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" || *dir == "" || fs.NArg() != 0 {
		return errors.New("usage: go run ./tools/release --version V --out DIR [--allow-dirty]")
	}
	if strings.ContainsAny(*version, " \t\n/'\"") {
		return fmt.Errorf("version %q has a character a file name or -ldflags cannot carry", *version)
	}
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if dirty, err := git("status", "--porcelain"); err != nil {
		return err
	} else if dirty != "" && !*allowDirty {
		return fmt.Errorf("the working tree has uncommitted changes, so the binary would not be commit %s:\n%s", commit, dirty)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}

	var names []string
	for _, p := range Platforms {
		if err := CheckNoCgoDeps(p); err != nil {
			return err
		}
		name := fmt.Sprintf("ketju_%s_%s_%s", *version, p.OS, p.Arch)
		path := filepath.Join(*dir, name)
		if err := build(p, *version, commit, path); err != nil {
			return err
		}
		bi, err := buildinfo.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: reading its build info: %w", name, err)
		}
		if err := CheckBuildInfo(bi, p); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := CheckStaticELF(path); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "built %s (%s, %d bytes): static, CGO_ENABLED=0, no tags\n", name, p, fi.Size())
		names = append(names, name)
	}
	if err := WriteSums(*dir, names); err != nil {
		return err
	}
	if err := VerifySums(*dir); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote and verified %s\n", filepath.Join(*dir, SumsName))

	host := Platform{runtime.GOOS, runtime.GOARCH}
	if slices.Contains(Platforms, host) {
		path := filepath.Join(*dir, fmt.Sprintf("ketju_%s_%s_%s", *version, host.OS, host.Arch))
		got, err := exec.Command(path, "version").Output()
		if err != nil {
			return fmt.Errorf("running %s version: %w", path, err)
		}
		line := strings.TrimSpace(string(got))
		if !strings.Contains(line, " "+*version+" ") || !strings.Contains(line, "commit "+commit[:12]+",") {
			return fmt.Errorf("%s version printed %q, which does not name %s and commit %s", path, line, *version, commit[:12])
		}
		fmt.Fprintf(out, "%s\n", line)
	}
	fmt.Fprintln(out, "published nothing")
	return nil
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// build runs go build for p, with an environment that says everything the
// checks then verify, and GOFLAGS emptied so no tag can come in from it.
func build(p Platform, version, commit, path string) error {
	ldflags := fmt.Sprintf("-s -w -X %s.version=%s -X %s.commit=%s", buildinfoPkg, version, buildinfoPkg, commit)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", path, "./cmd/ketju")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+p.OS, "GOARCH="+p.Arch, "GOFLAGS=")
	cmd.Dir = moduleRoot
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building for %s: %w", p, err)
	}
	return nil
}

// CheckBuildInfo fails unless bi records a build for p with CGO_ENABLED=0,
// -trimpath, and no build tags and no race detector.
func CheckBuildInfo(bi *buildinfo.BuildInfo, p Platform) error {
	set := map[string]string{}
	for _, s := range bi.Settings {
		set[s.Key] = s.Value
	}
	var bad []string
	if set["CGO_ENABLED"] != "0" {
		bad = append(bad, fmt.Sprintf("CGO_ENABLED=%q, want 0", set["CGO_ENABLED"]))
	}
	if set["-trimpath"] != "true" {
		bad = append(bad, "built without -trimpath")
	}
	if set["GOOS"] != p.OS || set["GOARCH"] != p.Arch {
		bad = append(bad, fmt.Sprintf("built for %s/%s, want %s", set["GOOS"], set["GOARCH"], p))
	}
	if t := set["-tags"]; t != "" {
		bad = append(bad, fmt.Sprintf("built with tags %q; a release has none", t))
	}
	if set["-race"] == "true" {
		bad = append(bad, "built with the race detector")
	}
	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "; "))
	}
	return nil
}

// CheckStaticELF fails unless path is an ELF with no interpreter and no
// needed shared library: a binary that runs on glibc and musl alike.
func CheckStaticELF(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("not an ELF: %w", err)
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return errors.New("the ELF names an interpreter, so it is dynamically linked")
		}
	}
	if f.Section(".dynamic") != nil {
		libs, err := f.ImportedLibraries()
		if err != nil {
			return fmt.Errorf("reading its needed libraries: %w", err)
		}
		if len(libs) > 0 {
			return fmt.Errorf("the ELF needs %v", libs)
		}
	}
	return nil
}

// CheckNoCgoDeps fails, naming each package, if any package outside the
// standard library that cmd/ketju imports for p has cgo files when cgo is
// enabled. CGO_ENABLED=0 would hide such a package's cgo half, and the
// first to find it would be a user who builds with cgo on musl.
func CheckNoCgoDeps(p Platform) error {
	cmd := exec.Command("go", "list", "-deps",
		"-f", "{{if not .Standard}}{{if .CgoFiles}}{{.ImportPath}}{{end}}{{end}}", "./cmd/ketju")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOOS="+p.OS, "GOARCH="+p.Arch, "GOFLAGS=")
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("listing cmd/ketju's dependencies for %s: %w: %s", p, err, strings.TrimSpace(stderr.String()))
	}
	if pkgs := strings.Fields(string(out)); len(pkgs) > 0 {
		return fmt.Errorf("for %s these dependencies have cgo files: %s", p, strings.Join(pkgs, ", "))
	}
	return nil
}

// SumsName is the checksums file's name.
const SumsName = "SHA256SUMS"

// WriteSums writes dir/SHA256SUMS for names, in sha256sum's format: the
// lowercase hex digest, two spaces, the name, sorted by name.
func WriteSums(dir string, names []string) error {
	names = slices.Sorted(slices.Values(names))
	var b strings.Builder
	for _, n := range names {
		sum, err := fileSHA256(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, n)
	}
	return os.WriteFile(filepath.Join(dir, SumsName), []byte(b.String()), 0o644)
}

// VerifySums reads dir/SHA256SUMS and fails on any line that does not parse
// or any file whose digest differs, as sha256sum -c does.
func VerifySums(dir string) error {
	f, err := os.Open(filepath.Join(dir, SumsName))
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		want, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok || len(want) != 64 || name == "" || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("%s: line %d is not a sha256sum line: %q", SumsName, n+1, sc.Text())
		}
		got, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s: %s has digest %s, the file lists %s", SumsName, name, got, want)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s lists nothing", SumsName)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
