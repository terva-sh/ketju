package api

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestSessionListIsUnchangedByTheMove (TKT-01M3YME9DM): the three session
// routes answer exactly as they did when the handlers called the session
// store themselves. Every exchange is written out, status, every header in
// order and the body, with each listed session id replaced by the name of
// the browser it belongs to and the CSP nonce by a placeholder, and
// compared with
// testdata/golden/settings.sessions.txt. That file was written by this test
// against main at 30834b8e, before the move, and the moved handlers pass it
// unedited.
func TestSessionListIsUnchangedByTheMove(t *testing.T) {
	w := newSettingsWorld(t)
	phone := w.signIn("alice", "club", iphone)
	w.now = w.now.Add(time.Minute)
	laptop := w.signIn("alice", "github", firefox)
	bob := w.signIn("bob", "github", firefox)
	names := map[string]string{w.idOf(laptop): "<laptop>", w.idOf(phone): "<phone>", w.idOf(bob): "<bob>"}

	var out bytes.Buffer
	exchange := func(token, method, path string) {
		t.Helper()
		rec := w.do(token, method, path, "")
		for id, name := range names {
			path = strings.ReplaceAll(path, id, name)
		}
		fmt.Fprintf(&out, "%s %s\n%d\n", method, path, rec.Code)
		hd := rec.Result().Header
		keys := make([]string, 0, len(hd))
		for k := range hd {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			for _, v := range hd[k] {
				fmt.Fprintf(&out, "%s: %s\n", k, v)
			}
		}
		body := rec.Body.String()
		for id, name := range names {
			body = strings.ReplaceAll(body, id, name)
		}
		fmt.Fprintf(&out, "\n%s\n\n", body)
	}

	exchange(laptop, http.MethodGet, "/api/v1/settings/sessions")
	exchange(laptop, http.MethodDelete, "/api/v1/settings/sessions/"+w.idOf(bob))
	exchange(laptop, http.MethodDelete, "/api/v1/settings/sessions/"+w.idOf(phone))
	exchange(laptop, http.MethodGet, "/api/v1/settings/sessions")
	exchange(laptop, http.MethodDelete, "/api/v1/settings/sessions/"+w.idOf(laptop))
	again := w.signIn("alice", "github", firefox)
	names[w.idOf(again)] = "<again>"
	exchange(again, http.MethodDelete, "/api/v1/settings/sessions")
	exchange(again, http.MethodGet, "/api/v1/settings/sessions")

	// The CSP nonce is fresh on every response.
	got := regexp.MustCompile(`'nonce-[^']*'`).ReplaceAll(out.Bytes(), []byte("'nonce-<nonce>'"))
	path := filepath.Join("testdata", "golden", "settings.sessions.txt")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the session routes serve differently from %s:\n%s", path, got)
	}
}
