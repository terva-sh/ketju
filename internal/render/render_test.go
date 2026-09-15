package render

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// TestTheSanitizerAloneIsSufficient is the test behind the claim that both
// halves of the defence are load-bearing.
//
// The pipeline drops raw HTML twice: goldmark refuses to emit it, and the
// sanitizer would drop it if goldmark did. The first is a renderer option,
// which means it is one line away from being changed by somebody adding an
// extension that needs raw HTML for something else. So this test builds the
// pipeline the dangerous way -- goldmark with WithUnsafe, passing every raw
// tag straight through -- and asserts the sanitizer catches all of it anyway.
//
// If this passes, the sanitizer is a real backstop rather than a second copy
// of a check that already happened.
func TestTheSanitizerAloneIsSufficient(t *testing.T) {
	t.Parallel()

	unsafeRenderer := html.New(html.WithUnsafe(), html.WithExtensions(
		extension.GFMHTMLRenderer,
		extension.FootnoteHTMLRenderer,
	))

	sources := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror="alert(1)">`,
		`<a href="javascript:alert(1)">x</a>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<form action="/x"><input name="p" type="password"></form>`,
		`<svg onload="alert(1)"><use href="#x"/></svg>`,
		`<div style="position:fixed;top:0;left:0;width:100vw;height:100vh">cover</div>`,
		`<base href="https://evil.example/">`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		`<style>body{display:none}</style>`,
		`<a href="#x" id="main-content">anchor hijack</a>`,
		`<input type="hidden" name="csrf" value="stolen">`,
	}
	for _, src := range sources {
		b := []byte(src)
		var buf bytes.Buffer
		doc := mdParser.Parse(b)
		if err := unsafeRenderer.Render(&buf, b, doc); err != nil {
			t.Fatalf("rendering %q: %v", src, err)
		}
		if !strings.Contains(buf.String(), "<") {
			t.Fatalf("the unsafe renderer dropped %q anyway, so this case proves nothing", src)
		}
		assertSafe(t, src, sanitize(buf.String()))
	}
}

// TestRenderIsSafeForConcurrentUse. The parser and renderer are package-level
// singletons, which is only correct if they hold no per-document state. Run
// this with -race; without it, it asserts nothing.
func TestRenderIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	const goroutines = 16
	src := "# H\n\nA [link](https://x.example) with a note[^1] and `code`.\n\n" +
		"| a | b |\n|--:|:--|\n| 1 | 2 |\n\n- [x] done\n\n[^1]: the note\n"

	want, err := Render(src)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				got, err := Render(src)
				if err != nil {
					t.Errorf("Render: %v", err)
					return
				}
				if got.HTML != want.HTML {
					t.Errorf("concurrent renders disagree:\n%s\n---\n%s", want.HTML, got.HTML)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestEmptyAndWhitespaceSourcesRenderToNothing(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"", "\n", "   \n\t\n", "\r\n"} {
		got, err := Render(src)
		if err != nil {
			t.Fatalf("Render(%q): %v", src, err)
		}
		if strings.TrimSpace(got.HTML) != "" {
			t.Errorf("Render(%q) = %q, want nothing", src, got.HTML)
		}
	}
}

func TestOversizedSourceIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := Render(strings.Repeat("a", MaxSource)); err != nil {
		t.Errorf("a source of exactly MaxSource was refused: %v", err)
	}
	_, err := Render(strings.Repeat("a", MaxSource+1))
	if err == nil {
		t.Fatal("a source over MaxSource was accepted")
	}
	if err != ErrTooLarge {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

// TestVersionIsPositive guards the one way the constant can be meaningless.
// Zero would make every cache key look like "no renderer version".
func TestVersionIsPositive(t *testing.T) {
	t.Parallel()

	if Version < 1 {
		t.Errorf("Version = %d; it is part of every cache key and every ETag", Version)
	}
}

// TestNothingElseRendersMarkdown is the first acceptance criterion, enforced
// rather than asserted.
//
// "Every caller uses this function" cannot be checked by looking at the
// callers, because the failure is a caller that does not exist yet. What can
// be checked is that nothing outside this package imports goldmark or
// bluemonday, which is the only way a second pipeline could come into being.
// A second pipeline is how the composer preview stops matching the post.
func TestNothingElseRendersMarkdown(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	watched := []string{"github.com/yuin/goldmark", "github.com/microcosm-cc/bluemonday"}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// spikes/ is exploratory code that is not part of the binary and
			// is allowed to import anything; testdata is not compiled.
			case ".git", "node_modules", "testdata", "spikes":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, filepath.Join("internal", "render")+string(filepath.Separator)) {
			return nil
		}

		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Errorf("parsing %s: %v", rel, perr)
			return nil
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, w := range watched {
				if strings.HasPrefix(p, w) {
					t.Errorf("%s imports %s directly. Markdown goes through render.Render, "+
						"or the composer preview stops matching the post and the renderer "+
						"version stops meaning anything.", rel, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
}

// repoRoot walks up from the test's directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
