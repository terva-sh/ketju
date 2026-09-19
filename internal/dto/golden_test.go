package dto

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false,
	"rewrite testdata/golden from the current DTOs and fixtures")

const goldenDir = "testdata/golden"

// encode is the one place a DTO becomes JSON in this package.
//
// Indented, because these files are read by people: a client developer opens
// one instead of asking what a response looks like, and a reviewer has to see
// a field move in a diff. The cost is that whitespace is part of the contract
// here, which is fine -- it is not part of it on the wire, where the handler
// marshals compactly.
func encode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Struct field values are already HTML-safe -- rendered post HTML is
	// sanitized by internal/render -- and escaping < and & here would make the
	// goldens unreadable for the one field most likely to contain them.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	return buf.Bytes()
}

// TestGoldenJSON is AC2: a field change fails with a readable diff.
func TestGoldenJSON(t *testing.T) {
	if len(corpus) == 0 {
		t.Fatal("the corpus is empty, so every other test in this file passes vacuously")
	}

	for _, name := range corpusNames() {
		t.Run(name, func(t *testing.T) {
			got := encode(t, corpus[name])
			path := filepath.Join(goldenDir, name+".json")

			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("writing %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the golden file: %v\nrun: go test ./internal/dto -update", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s no longer matches its golden file.\n%s\n\n"+
					"If this change is intended, run: go test ./internal/dto -update",
					name, diff(string(want), string(got)))
			}
		})
	}
}

// TestNoStaleGoldens catches the other direction: a golden whose fixture was
// deleted keeps being read as the contract by anyone browsing the directory,
// and -update never touches it because nothing generates it any more.
func TestNoStaleGoldens(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(goldenDir, "*.json"))
	if err != nil {
		t.Fatalf("globbing goldens: %v", err)
	}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if _, ok := corpus[name]; !ok {
			t.Errorf("%s has no fixture; delete it or add the case back", p)
		}
	}
}

// TestEveryFieldIsPopulatedSomewhere is what makes the corpus worth having.
//
// A golden catches a renamed field only if some fixture gives that field a
// value. A field left at its zero value serializes to 0, "" or false in every
// case, so renaming it changes a key nobody is looking at and the diff reads
// as noise. Without this test the harness would report full coverage of a
// contract it is not actually holding.
func TestEveryFieldIsPopulatedSomewhere(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range corpusNames() {
		walk(reflect.ValueOf(corpus[name]), func(owner reflect.Type, f reflect.StructField, v reflect.Value) {
			if !v.IsZero() {
				seen[owner.Name()+"."+f.Name] = true
			}
		})
	}

	for _, missing := range missingFields(t, seen) {
		t.Errorf("%s is never given a value by any fixture, so no golden would catch its rename", missing)
	}
}

// TestEveryPointerFieldIsSeenBothWays. A pointer here means the null is
// meaningful and a client is expected to branch on it, so a corpus that only
// ever shows one side documents half a contract -- and the half it leaves out
// is the one a client gets wrong.
func TestEveryPointerFieldIsSeenBothWays(t *testing.T) {
	null := map[string]bool{}
	set := map[string]bool{}

	for _, name := range corpusNames() {
		walk(reflect.ValueOf(corpus[name]), func(owner reflect.Type, f reflect.StructField, v reflect.Value) {
			if f.Type.Kind() != reflect.Pointer {
				return
			}
			key := owner.Name() + "." + f.Name
			if v.IsNil() {
				null[key] = true
			} else {
				set[key] = true
			}
		})
	}

	for _, key := range sortedKeys(mergeKeys(null, set)) {
		switch {
		case !null[key]:
			t.Errorf("%s is never null in the corpus; a client would not learn that it can be", key)
		case !set[key]:
			t.Errorf("%s is never set in the corpus; nothing pins the shape it takes when it is", key)
		}
	}
}

// TestEveryDeclaredTypeIsReachableFromAResponseRoot.
//
// The list of types is read out of the source rather than written here,
// because a hand-written list would be updated by the same person who forgot
// to wire the type into a response, which is exactly the failure this catches.
// A type in this package that no response reaches is either dead or a shape
// somebody is about to serialize without a golden.
func TestEveryDeclaredTypeIsReachableFromAResponseRoot(t *testing.T) {
	declared := declaredTypes(t)
	if len(declared) == 0 {
		t.Fatal("no types parsed out of dto.go, so this test is asserting nothing")
	}

	reached := map[string]bool{}
	for _, name := range corpusNames() {
		reachable(reflect.TypeOf(corpus[name]), reached)
	}

	for _, name := range declared {
		if !reached[name] {
			t.Errorf("type %s is not reachable from any corpus case, so nothing pins its JSON", name)
		}
	}
}

// TestViewerClassesDiffer is AC4.
//
// Two identical goldens for the same endpoint mean one of two things: the
// class distinction is not real, or the fixture does not exercise it. Both are
// worth failing over, and neither announces itself -- a duplicated golden
// looks like thorough coverage.
func TestViewerClassesDiffer(t *testing.T) {
	byEndpoint := map[string]map[string]string{}
	for _, name := range corpusNames() {
		endpoint, class, ok := strings.Cut(name, ".")
		if !ok {
			t.Fatalf("corpus case %q is not named <endpoint>.<class>", name)
		}
		if byEndpoint[endpoint] == nil {
			byEndpoint[endpoint] = map[string]string{}
		}
		byEndpoint[endpoint][class] = string(encode(t, corpus[name]))
	}

	for _, endpoint := range sortedKeys(byEndpoint) {
		classes := byEndpoint[endpoint]
		if len(classes) < 2 {
			continue
		}
		names := sortedKeys(classes)
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				if classes[names[i]] == classes[names[j]] {
					t.Errorf("%s.%s and %s.%s serialize identically; either the class distinction is not real or the fixture does not exercise it",
						endpoint, names[i], endpoint, names[j])
				}
			}
		}
	}
}

// The three tests below are the docs/09 json/v2 risk, held down today.
//
// encoding/json/v2 ships in go1.27.1 with no GOEXPERIMENT, but it is gated on
// the module's go directive, which is 1.26.2, so importing it here would be a
// module-wide language bump and belongs to whoever makes that call. Rather
// than wait, the three strictnesses that would bite were measured against the
// real toolchain and each is asserted directly. When the bump happens these
// should already pass, and if one does not, it fails here rather than at a
// client.

// TestNoTwoFieldsShareATag is the collision that can actually happen here, and
// it is worse than it sounds.
//
// Given two fields with the same tag at the same depth, encoding/json drops
// *both* and returns no error: a struct with a colliding pair marshals to {}.
// TestGoldenJSON would catch that today, but only until somebody runs -update
// to "fix" the failure, at which point two fields disappear from the API and
// every test in this package passes. Checking the tags directly is the only
// version of this that survives a regeneration.
func TestNoTwoFieldsShareATag(t *testing.T) {
	for _, typ := range corpusTypes() {
		byTag := map[string][]string{}
		for i := range typ.NumField() {
			tag := jsonName(typ.Field(i))
			if tag == "" {
				continue
			}
			byTag[tag] = append(byTag[tag], typ.Field(i).Name)
		}
		for _, tag := range sortedKeys(byTag) {
			if len(byTag[tag]) > 1 {
				t.Errorf("%s: fields %v all carry json:%q; encoding/json drops every one of them and reports no error",
					typ.Name(), byTag[tag], tag)
			}
		}
	}
}

// TestGoldensHaveNoDuplicateKeys reads the committed files rather than
// re-marshalling, because the file is what a client developer opens and what a
// hand-edit would change.
//
// Today's structs cannot produce a duplicate member -- Go resolves a tag
// collision by dropping the fields, which is what the test above is for -- so
// this guards two other things: a golden edited by hand, and the day a field
// arrives that carries pre-built JSON (a json.RawMessage for prefs_json is the
// obvious candidate) or a custom MarshalJSON, either of which can emit one.
// json/v2 rejects such a document outright; v1 keeps the last member and says
// nothing.
func TestGoldensHaveNoDuplicateKeys(t *testing.T) {
	for _, name := range corpusNames() {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(goldenDir, name+".json"))
			if err != nil {
				t.Fatalf("reading the golden file: %v\nrun: go test ./internal/dto -update", err)
			}
			checkNoDuplicates(t, json.NewDecoder(bytes.NewReader(b)), "")
		})
	}
}

// TestGoldensAreValidUTF8. v1 replaces invalid bytes with U+FFFD on the way
// out, so a corrupt string survives as mojibake; v2 refuses. Rendered post
// HTML is the realistic source of one.
func TestGoldensAreValidUTF8(t *testing.T) {
	for _, name := range corpusNames() {
		b, err := os.ReadFile(filepath.Join(goldenDir, name+".json"))
		if err != nil {
			t.Fatalf("reading the golden file: %v\nrun: go test ./internal/dto -update", err)
		}
		if !utf8.Valid(b) {
			t.Errorf("%s is not valid UTF-8", name)
		}
		if bytes.ContainsRune(b, utf8.RuneError) {
			t.Errorf("%s contains U+FFFD, which is what v1 leaves behind when it repairs invalid input", name)
		}
	}
}

// TestNoTwoFieldsDifferOnlyByCase. v1 matches field names case-insensitively
// on the way in, so two tags differing only by case resolve to whichever v1
// picks; v2 is case-sensitive and would route them differently. Today no
// handler unmarshals these, which is precisely why the collision would be
// introduced without anything noticing.
func TestNoTwoFieldsDifferOnlyByCase(t *testing.T) {
	for _, typ := range corpusTypes() {
		lower := map[string][]string{}
		for i := range typ.NumField() {
			tag := jsonName(typ.Field(i))
			if tag == "" {
				continue
			}
			key := strings.ToLower(tag)
			lower[key] = append(lower[key], tag)
		}
		for _, key := range sortedKeys(lower) {
			if len(lower[key]) > 1 {
				t.Errorf("%s has fields %v, which differ only by case", typ.Name(), lower[key])
			}
		}
	}
}

// --- helpers -------------------------------------------------------------

func corpusNames() []string {
	names := make([]string, 0, len(corpus))
	for n := range corpus {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func jsonName(f reflect.StructField) string {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	return name
}

// walk visits every struct field in v, descending through pointers, slices and
// nested structs, and calls fn with the struct type that owns the field.
func walk(v reflect.Value, fn func(owner reflect.Type, f reflect.StructField, v reflect.Value)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walk(v.Index(i), fn)
		}
	case reflect.Struct:
		typ := v.Type()
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			fn(typ, f, v.Field(i))
			walk(v.Field(i), fn)
		}
	}
}

// reachable records every named struct type reachable from typ.
func reachable(typ reflect.Type, out map[string]bool) {
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		reachable(typ.Elem(), out)
	case reflect.Struct:
		if typ.Name() != "" {
			if out[typ.Name()] {
				return // already walked; also stops a self-referential type
			}
			out[typ.Name()] = true
		}
		for i := range typ.NumField() {
			reachable(typ.Field(i).Type, out)
		}
	default:
		if typ.Name() != "" && typ.PkgPath() != "" {
			out[typ.Name()] = true // a defined scalar such as Millis
		}
	}
}

// corpusTypes returns every named struct type any corpus case reaches.
func corpusTypes() []reflect.Type {
	byName := map[string]reflect.Type{}
	var collect func(reflect.Type)
	collect = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			collect(typ.Elem())
		case reflect.Struct:
			if typ.Name() != "" {
				if _, ok := byName[typ.Name()]; ok {
					return
				}
				byName[typ.Name()] = typ
			}
			for i := range typ.NumField() {
				collect(typ.Field(i).Type)
			}
		}
	}
	for _, v := range corpus {
		collect(reflect.TypeOf(v))
	}
	out := make([]reflect.Type, 0, len(byName))
	for _, n := range sortedKeys(byName) {
		out = append(out, byName[n])
	}
	return out
}

// missingFields lists every exported field of every reachable struct type that
// no fixture gave a non-zero value.
func missingFields(t *testing.T, seen map[string]bool) []string {
	t.Helper()
	var out []string
	for _, typ := range corpusTypes() {
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			key := typ.Name() + "." + f.Name
			if !seen[key] {
				out = append(out, key)
			}
		}
	}
	sort.Strings(out)
	return out
}

// declaredTypes parses dto.go for the type names it declares.
func declaredTypes(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "dto.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing dto.go: %v", err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if ok && ts.Name.IsExported() {
			out = append(out, ts.Name.Name)
		}
		return true
	})
	sort.Strings(out)
	return out
}

// checkNoDuplicates walks a decoded document reporting any object that names
// the same member twice.
func checkNoDuplicates(t *testing.T, dec *json.Decoder, path string) {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			key := keyTok.(string)
			if seen[key] {
				t.Errorf("%s/%s appears twice; json/v2 rejects the document and v1 silently keeps the last one", path, key)
			}
			seen[key] = true
			checkNoDuplicates(t, dec, path+"/"+key)
		}
	case '[':
		for i := 0; dec.More(); i++ {
			checkNoDuplicates(t, dec, fmt.Sprintf("%s/%d", path, i))
		}
	}
	if _, err := dec.Token(); err != nil { // the closing delimiter
		t.Fatalf("%s: %v", path, err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mergeKeys(ms ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		for k := range m {
			out[k] = true
		}
	}
	return out
}

// diff prints the changed lines with a little context, rather than two whole
// documents. A thread golden is long enough that a full dump buries the one
// line that moved, and the point of the failure message is to name it.
func diff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")

	var b strings.Builder
	b.WriteString("--- golden\n+++ current\n")
	n := max(len(w), len(g))
	for i := range n {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		fmt.Fprintf(&b, "@@ line %d @@\n", i+1)
		if i < len(w) {
			fmt.Fprintf(&b, "-%s\n", wl)
		}
		if i < len(g) {
			fmt.Fprintf(&b, "+%s\n", gl)
		}
	}
	return b.String()
}
