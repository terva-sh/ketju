package main

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func init() { moduleRoot = filepath.Join("..", "..") }

func settings(kv ...string) *buildinfo.BuildInfo {
	bi := &buildinfo.BuildInfo{}
	for i := 0; i+1 < len(kv); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return bi
}

// TestCheckBuildInfo: build info recording cgo, a build tag, the race
// detector, a missing -trimpath or another platform is refused.
func TestCheckBuildInfo(t *testing.T) {
	p := Platform{"linux", "arm64"}
	good := []string{"CGO_ENABLED", "0", "-trimpath", "true", "GOOS", "linux", "GOARCH", "arm64"}
	if err := CheckBuildInfo(settings(good...), p); err != nil {
		t.Fatalf("a good build was refused: %v", err)
	}
	for name, kv := range map[string][]string{
		"cgo":        {"CGO_ENABLED", "1", "-trimpath", "true", "GOOS", "linux", "GOARCH", "arm64"},
		"no cgo key": {"-trimpath", "true", "GOOS", "linux", "GOARCH", "arm64"},
		"e2e tag":    append(slicesClone(good), "-tags", "e2e"),
		"race":       append(slicesClone(good), "-race", "true"),
		"no trim":    {"CGO_ENABLED", "0", "GOOS", "linux", "GOARCH", "arm64"},
		"amd64":      {"CGO_ENABLED", "0", "-trimpath", "true", "GOOS", "linux", "GOARCH", "amd64"},
	} {
		if err := CheckBuildInfo(settings(kv...), p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

// TestOutputDirIsAbsolute: --out . becomes the working directory's absolute
// path, so the host binary is run from DIR and not looked up in PATH
// (Terva review 1999).
func TestOutputDirIsAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{".": wd, "out": filepath.Join(wd, "out"), "/tmp/x/": "/tmp/x"} {
		got, err := outputDir(in)
		if err != nil || got != want || !filepath.IsAbs(got) {
			t.Errorf("outputDir(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// buildProgram builds a one-file program in its own module and returns the
// binary's path.
func buildProgram(t *testing.T, src string, cgo bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "bin")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	env := "CGO_ENABLED=0"
	if cgo {
		env = "CGO_ENABLED=1"
	}
	cmd.Env = append(os.Environ(), env, "GOFLAGS=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, b)
	}
	return out
}

// TestCheckStaticELF: a binary built without cgo passes; one built with cgo
// and linked against libc is refused.
func TestCheckStaticELF(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the release is built for linux, and this checks a linux ELF")
	}
	static := buildProgram(t, "package main\n\nfunc main() {}\n", false)
	if err := CheckStaticELF(static); err != nil {
		t.Errorf("a CGO_ENABLED=0 binary was refused: %v", err)
	}
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("no C compiler to build the cgo binary with; CI has gcc")
	}
	dynamic := buildProgram(t, "package main\n\n// #include <stdlib.h>\nimport \"C\"\n\nfunc main() { C.free(nil) }\n", true)
	if err := CheckStaticELF(dynamic); err == nil {
		t.Error("a cgo binary linked against libc was accepted")
	}
}

// TestChecksums: SHA256SUMS is written in sha256sum's format, verifies, and
// fails to verify once one byte of one file changes.
func TestChecksums(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"ketju_v1_linux_arm64", "ketju_v1_linux_amd64"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("binary "+n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteSums(dir, []string{"ketju_v1_linux_arm64", "ketju_v1_linux_amd64"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, SumsName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "  ketju_v1_linux_amd64") || len(strings.Fields(lines[0])[0]) != 64 {
		t.Errorf("SHA256SUMS is not sorted sha256sum lines:\n%s", b)
	}
	if err := VerifySums(dir); err != nil {
		t.Fatalf("a fresh SHA256SUMS does not verify: %v", err)
	}
	if path, err := exec.LookPath("sha256sum"); err == nil {
		cmd := exec.Command(path, "-c", SumsName)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("sha256sum -c refuses the file: %v\n%s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ketju_v1_linux_arm64"), []byte("binary ketju_v1_linux_arm6X"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifySums(dir); err == nil {
		t.Error("a changed binary still verified")
	}
}

// TestNoDependencyUsesCgo: no package outside the standard library that
// cmd/ketju imports has cgo files, for any release platform, with cgo
// enabled.
func TestNoDependencyUsesCgo(t *testing.T) {
	for _, p := range Platforms {
		if err := CheckNoCgoDeps(p); err != nil {
			t.Error(err)
		}
	}
}
