package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Plain's goldens: every case of the renderer's corpus in testdata/golden,
// and the cases only Plain needs in testdata/plain, each with its output
// beside it as NAME.txt in testdata/plain. -update rewrites them, as it does
// the HTML goldens.
const (
	plainDir     = "testdata/plain"
	plainSumPath = "testdata/plain.sum"
)

// plainCases maps a case name to its markdown's path.
func plainCases(t *testing.T) map[string]string {
	t.Helper()
	cases := map[string]string{}
	for _, dir := range []string{goldenDir, plainDir} {
		paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			name := strings.TrimSuffix(filepath.Base(p), ".md")
			if prev, ok := cases[name]; ok {
				t.Fatalf("%s and %s are both case %q", prev, p, name)
			}
			cases[name] = p
		}
	}
	return cases
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestPlainGolden is criterion 3's TestPlainGolden: the plain text of every
// case matches its committed .txt. The corpus covers headings, emphasis,
// links (whose destinations are absent), code, quotations, mentions, raw
// HTML (dropped) and nested lists.
func TestPlainGolden(t *testing.T) {
	cases := plainCases(t)
	if len(cases) == 0 {
		t.Fatal("no plain cases")
	}
	for _, name := range sortedKeys(cases) {
		t.Run(name, func(t *testing.T) {
			got := Plain(readFile(t, cases[name]))
			path := filepath.Join(plainDir, name+".txt")
			if *update {
				writeFile(t, path, got)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the golden file: %v\nrun: go test ./internal/render -update", err)
			}
			if got != string(want) {
				t.Errorf("output changed.\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}

// TestPlainVersionTracksOutput holds Plain to the rule in PlainVersion's
// comment, as TestRendererVersionTracksOutput holds Render to Version: -update
// refuses to rewrite the hash of a case whose output changed unless
// PlainVersion was bumped first. An index built by an older extraction is
// rebuilt only when the version says it is older.
func TestPlainVersionTracksOutput(t *testing.T) {
	cases := plainCases(t)
	current := map[string]string{}
	for name, path := range cases {
		sum := sha256.Sum256([]byte(Plain(readFile(t, path))))
		current[name] = hex.EncodeToString(sum[:])
	}
	recordedVersion, recorded := readPlainSum(t)
	var changed []string
	for name, h := range current {
		if old, ok := recorded[name]; ok && old != h {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	if *update {
		if len(changed) > 0 && recordedVersion == PlainVersion {
			t.Fatalf("the plain text of %s changed while PlainVersion is still %d.\n"+
				"Bump PlainVersion in plain.go first, then rerun with -update. The search index "+
				"records it, and leaving it alone keeps every post indexed by the old extraction.",
				strings.Join(changed, ", "), PlainVersion)
		}
		var b strings.Builder
		b.WriteString("# Per-case sha256 of Plain's output, with the PlainVersion that produced it.\n")
		b.WriteString("# A case whose hash changes here without the version changing leaves the\n")
		b.WriteString("# search index holding the old extraction. See plain.go.\n")
		fmt.Fprintf(&b, "version %d\n", PlainVersion)
		for _, n := range sortedKeys(current) {
			fmt.Fprintf(&b, "%s %s\n", n, current[n])
		}
		writeFile(t, plainSumPath, b.String())
		return
	}
	if recordedVersion != PlainVersion {
		t.Errorf("PlainVersion is %d but %s records %d; rerun with -update", PlainVersion, plainSumPath, recordedVersion)
	}
	for name, h := range current {
		if old, ok := recorded[name]; !ok {
			t.Errorf("%s has no recorded hash; rerun with -update", name)
		} else if old != h {
			t.Errorf("%s's plain text differs from its recorded hash", name)
		}
	}
	for name := range recorded {
		if _, ok := current[name]; !ok {
			t.Errorf("%s records %s, which no longer exists; rerun with -update", plainSumPath, name)
		}
	}
}

func readPlainSum(t *testing.T) (int, map[string]string) {
	t.Helper()
	b, err := os.ReadFile(plainSumPath)
	if os.IsNotExist(err) {
		return 0, map[string]string{}
	}
	if err != nil {
		t.Fatal(err)
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
			t.Fatalf("%s: cannot parse %q", plainSumPath, line)
		}
		if name == "version" {
			if version, err = strconv.Atoi(hash); err != nil {
				t.Fatalf("%s: version %q is not a number", plainSumPath, hash)
			}
			continue
		}
		out[name] = hash
	}
	return version, out
}

// TestPlainDropsSentinels: the snippet markers cannot be forged. A source
// carrying them, in text, in code and in a link's text, loses them, and the
// text around them stays.
func TestPlainDropsSentinels(t *testing.T) {
	open, closing := string(SnippetOpen), string(SnippetClose)
	src := "a " + open + "forged" + closing + " match, `" + open + "code" + closing + "`, [" + open + "link" + closing + "](x)\n\n" +
		"```\n" + open + "block" + closing + "\n```"
	got := Plain(src)
	if strings.ContainsAny(got, open+closing) {
		t.Fatalf("Plain kept a sentinel: %q", got)
	}
	for _, w := range []string{"forged", "code", "link", "block"} {
		if !strings.Contains(got, w) {
			t.Errorf("Plain lost %q around the sentinels: %q", w, got)
		}
	}
}

// TestPlainKeepsNoDestination: no URL a link, an image or an autolink
// points at reaches the index, while the text a reader sees does.
func TestPlainKeepsNoDestination(t *testing.T) {
	got := Plain("[shown text](https://hidden.example/a) ![alt shown](/blobs/hiddenhash) " +
		"<https://hidden.example/b> www.hidden.example and [ref text][r]\n\n[r]: https://hidden.example/c \"title hidden\"")
	if strings.Contains(got, "hidden") {
		t.Errorf("a destination reached the plain text: %q", got)
	}
	for _, w := range []string{"shown text", "alt shown", "ref text"} {
		if !strings.Contains(got, w) {
			t.Errorf("Plain lost %q: %q", w, got)
		}
	}
}

// TestPlainDropsAttributedQuotations: a quotation of another post is not
// indexed under the post that quotes it, while a plain blockquote is.
func TestPlainDropsAttributedQuotations(t *testing.T) {
	got := Plain("> [quote=#3]\n> copied words\n\nmy reply\n\n> set apart")
	if strings.Contains(got, "copied") {
		t.Errorf("an attributed quotation reached the plain text: %q", got)
	}
	if !strings.Contains(got, "my reply") || !strings.Contains(got, "set apart") {
		t.Errorf("Plain lost the reply or the plain blockquote: %q", got)
	}
}
