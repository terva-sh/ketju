package problem

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false,
	"rewrite testdata/catalogue.json from the current catalogue")

const cataloguePath = "testdata/catalogue.json"

// goldenEntry is one code's whole observable contract: what a client sees.
type goldenEntry struct {
	Status      int             `json:"status"`
	ContentType string          `json:"content_type"`
	Body        json.RawMessage `json:"body"`
}

// TestCatalogueGolden renders a real response for every code and compares it
// to the committed file.
//
// This is AC4, and the golden file is the point rather than the test: these
// codes are API surface. A client switches on `type`, and the future HTML
// client turns it into a flash message, so renaming one is a breaking change.
// A unit test asserting "not-found maps to 404" would pass just as happily
// after somebody renamed it to "not_found". A committed file that has to be
// regenerated makes the break show up in the diff, where a reviewer sees it.
//
// Regenerate deliberately with:
//
//	go test ./internal/problem -update
//
// and expect to justify the diff.
func TestCatalogueGolden(t *testing.T) {
	codes := Codes()
	if len(codes) == 0 {
		t.Fatal("the catalogue is empty; this test would assert nothing")
	}

	got := make(map[string]goldenEntry, len(codes))
	for _, c := range codes {
		rec := httptest.NewRecorder()
		Write(rec, httptest.NewRequest(http.MethodGet, "/api/v1/example", nil), c)

		var body json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: response body is not JSON: %v", c, err)
		}
		got[string(c)] = goldenEntry{
			Status:      rec.Code,
			ContentType: rec.Header().Get("Content-Type"),
			Body:        body,
		}
	}

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encode catalogue: %v", err)
	}
	encoded = append(encoded, '\n')

	if *update {
		if err := os.MkdirAll(filepath.Dir(cataloguePath), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(cataloguePath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", cataloguePath)
		return
	}

	want, err := os.ReadFile(cataloguePath)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/problem -update): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the catalogue changed.\n\nThese codes are API surface: renaming one, or moving\n"+
			"it to a different status, breaks every client that switched on it.\nIf the change is "+
			"intended, regenerate with -update and say so in the ticket.\n\ngot:\n%s\nwant:\n%s",
			encoded, want)
	}
}

// TestTheDocumentedCatalogueMatchesTheCode parses the table in
// docs/07-api-and-clients.md and checks it against the catalogue.
//
// AC4 asks for the catalogue to be documented, and documentation that is only
// asserted once is documentation that is wrong within a release. A table a
// client author reads and trusts has to be checked against the thing it
// describes, or the honest move would be not to write it.
func TestTheDocumentedCatalogueMatchesTheCode(t *testing.T) {
	const docPath = "../../docs/07-api-and-clients.md"

	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	// Rows look like: | `bad-request` | 400 | Unparseable JSON, ... |
	row := regexp.MustCompile("^\\|\\s*`([a-z-]+)`\\s*\\|\\s*(\\d{3})\\s*\\|")
	documented := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		status, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		documented[m[1]] = status
	}

	if len(documented) == 0 {
		t.Fatalf("found no catalogue rows in %s; this test is not measuring anything", docPath)
	}

	for _, c := range Codes() {
		status, ok := documented[string(c)]
		if !ok {
			t.Errorf("code %q is in the catalogue but not documented in %s", c, docPath)
			continue
		}
		if want, _ := Status(c); status != want {
			t.Errorf("%s: documented as %d, code says %d", c, status, want)
		}
	}
	for name := range documented {
		if _, ok := catalogue[Code(name)]; !ok {
			t.Errorf("%s documents code %q, which does not exist", docPath, name)
		}
	}
}
