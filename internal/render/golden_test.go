package render

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false,
	"rewrite the golden HTML and testdata/pipeline.sum from the current pipeline")

const (
	goldenDir = "testdata/golden"
	sumPath   = "testdata/pipeline.sum"
)

// goldenContext is what the corpus renders against.
//
// Every ordinal resolves and a fixed handful of handles do, so the corpus locks
// the markup for a resolved quote and a resolved mention rather than only the
// degraded forms. The unresolved shapes are covered by the unit tests, which can
// state what they are testing; a golden file cannot.
func goldenContext() Context {
	return Context{
		Posts: ResolverFunc(func(uint32) (bool, bool) { return true, false }),
		Handles: HandleFunc(func(typed string) (int64, string, bool) {
			switch strings.ToLower(typed) {
			case "alice":
				return 1, "alice", true
			case "bob":
				return 2, "bob", true
			}
			return 0, "", false
		}),
	}
}

// TestGoldenOutput renders every case in testdata/golden and compares the
// result to the committed HTML.
//
// The corpus is the markdown this forum claims to support. A feature that is
// not in here is a feature nobody would notice losing, which is the real test
// of whether it is in scope.
func TestGoldenOutput(t *testing.T) {
	cases := goldenCases(t)
	if len(cases) == 0 {
		t.Fatal("no golden cases; the corpus is the specification and an empty one asserts nothing")
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			src := readFile(t, filepath.Join(goldenDir, name+".md"))
			got, err := RenderWith(src, goldenContext())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			htmlPath := filepath.Join(goldenDir, name+".html")

			if *update {
				writeFile(t, htmlPath, got.HTML)
				return
			}
			want, err := os.ReadFile(htmlPath)
			if err != nil {
				t.Fatalf("reading the golden file: %v\nrun: go test ./internal/render -update", err)
			}
			if got.HTML != string(want) {
				t.Errorf("output changed.\n--- want ---\n%s\n--- got ---\n%s", want, got.HTML)
			}
		})
	}
}

// TestRendererVersionTracksOutput enforces the rule in the package comment.
//
// Writing "bump Version when output changes" in a doc comment does not make it
// happen; the person who changes the output is the person who is not thinking
// about caches. So the hash of each case's output is committed alongside it,
// and -update refuses to rewrite a case whose output changed unless Version
// was bumped first.
//
// Adding or removing a case is free. Only a case whose rendering changed
// forces a bump, which is exactly the set of changes that leave stale HTML in
// somebody's cache.
func TestRendererVersionTracksOutput(t *testing.T) {
	cases := goldenCases(t)
	current := map[string]string{}
	for _, name := range cases {
		src := readFile(t, filepath.Join(goldenDir, name+".md"))
		r, err := RenderWith(src, goldenContext())
		if err != nil {
			t.Fatalf("%s: Render: %v", name, err)
		}
		sum := sha256.Sum256([]byte(r.HTML))
		current[name] = hex.EncodeToString(sum[:])
	}

	recordedVersion, recorded := readSum(t)

	var changed []string
	for name, h := range current {
		if old, ok := recorded[name]; ok && old != h {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)

	if *update {
		if len(changed) > 0 && recordedVersion == Version {
			t.Fatalf("the output of %s changed while Version is still %d.\n"+
				"Bump Version in render.go first, then rerun with -update. It is part of every "+
				"cache key and every ETag, so leaving it alone means readers keep being served "+
				"the old HTML for as long as their caches hold it.",
				strings.Join(changed, ", "), Version)
		}
		writeSum(t, current)
		return
	}

	if recordedVersion != Version {
		t.Errorf("Version is %d but testdata/pipeline.sum records %d; rerun with -update",
			Version, recordedVersion)
	}
	for name, h := range current {
		old, ok := recorded[name]
		if !ok {
			t.Errorf("%s has no recorded hash; rerun with -update", name)
			continue
		}
		if old != h {
			t.Errorf("%s renders differently from its recorded hash", name)
		}
	}
	for name := range recorded {
		if _, ok := current[name]; !ok {
			t.Errorf("pipeline.sum records %s, which no longer exists; rerun with -update", name)
		}
	}
}

func goldenCases(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(goldenDir, "*.md"))
	if err != nil {
		t.Fatalf("globbing golden inputs: %v", err)
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".md"))
	}
	sort.Strings(names)
	return names
}

func readSum(t *testing.T) (int, map[string]string) {
	t.Helper()
	b, err := os.ReadFile(sumPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, map[string]string{}
		}
		t.Fatalf("reading %s: %v", sumPath, err)
	}
	out := map[string]string{}
	version := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("%s: cannot parse %q", sumPath, line)
		}
		hash = strings.TrimSpace(hash)
		if name == "version" {
			n, err := strconv.Atoi(hash)
			if err != nil {
				t.Fatalf("%s: version %q is not a number", sumPath, hash)
			}
			version = n
			continue
		}
		out[name] = hash
	}
	return version, out
}

func writeSum(t *testing.T, sums map[string]string) {
	t.Helper()
	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Per-case sha256 of the rendered output, with the renderer version that\n")
	b.WriteString("# produced it. A case whose hash changes here without the version changing\n")
	b.WriteString("# is stale HTML in every cache in the system. See render.go.\n")
	fmt.Fprintf(&b, "version %d\n", Version)
	for _, n := range names {
		fmt.Fprintf(&b, "%s %s\n", n, sums[n])
	}
	writeFile(t, sumPath, b.String())
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
