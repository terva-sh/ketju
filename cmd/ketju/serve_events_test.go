package main

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
)

// TestServeShutdownClosesEventStreams: a live-update stream never finishes by
// itself, so serve's shutdown has to end it. With one stream open,
// cancelling serve ends the stream inside the drain, and serve returns
// without cutting requests. With the hub not closed at shutdown, the drain
// would run out and log that it did (TKT-01M29CRJ3X).
func TestServeShutdownClosesEventStreams(t *testing.T) {
	path, data := writeConfig(t, "")
	seedPeople(t, data, "https://idp.example", seeded{subject: "alice", handle: "alice", role: domain.RoleMember})
	ids := seedThreads(t, data, 1)
	r := start(t, path, serveOpts{drain: 3 * time.Second})

	resp, err := http.Get("http://" + r.addr + "/events?threads=" + ids[0].String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("GET /events = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := make(chan string, 64)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	waitLine := func(want string) {
		t.Helper()
		for {
			select {
			case l := <-lines:
				if strings.Contains(l, want) {
					return
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the stream never sent %q", want)
			}
		}
	}
	waitLine("event: ready")
	waitLine(`"thread_id":"` + ids[0].String() + `"`) // the snapshot

	began := time.Now()
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was still open after serve returned")
	}
	if took := time.Since(began); took >= 3*time.Second {
		t.Errorf("shutdown took %s: the drain waited for the stream", took)
	}
	if strings.Contains(r.log.String(), "requests still running after the drain") {
		t.Errorf("the drain ran out with a stream open:\n%s", r.log)
	}
	if !strings.Contains(r.log.String(), "requests drained") {
		t.Errorf("the log does not say the requests drained:\n%s", r.log)
	}
}
