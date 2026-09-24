//go:build unix

package lock

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var update = flag.Bool("update", false,
	"rewrite testdata/held.golden from the current refusal message")

const heldGolden = "testdata/held.golden"

// The helper process. A test that needs a second process re-runs this test
// binary with KETJU_LOCK_HELPER set; TestMain sees it and acts as that
// process instead of running tests. flock is per open file description, so
// an in-process second Acquire is refused too, but only a real second
// process shows what an operator sees: a pid that is not ours, and a lock
// the kernel drops when that process is killed.
func TestMain(m *testing.M) {
	if dir := os.Getenv("KETJU_LOCK_HELPER"); dir != "" {
		os.Exit(helper(dir, os.Getenv("KETJU_LOCK_HELPER_MODE")))
	}
	os.Exit(m.Run())
}

// helper acquires the lock on dir. In "hold" mode it says so on stdout and
// waits to be killed; otherwise it reports the refusal on stderr and exits
// non-zero, as serve will.
func helper(dir, mode string) int {
	l, err := Acquire(dir, "serve")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if mode == "hold" {
		fmt.Println("held")
		select {}
	}
	l.Release()
	return 0
}

func helperCmd(t *testing.T, dir, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "KETJU_LOCK_HELPER="+dir, "KETJU_LOCK_HELPER_MODE="+mode)
	return cmd
}

// startHolder runs a helper that takes the lock and holds it, and returns
// once it has.
func startHolder(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	cmd := helperCmd(t, dir, "hold")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "held" {
		t.Fatalf("the holder did not take the lock: %q, %v", line, err)
	}
	return cmd
}

func TestSecondAcquireRefusedNamingHolder(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir, "serve")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer l.Release()

	// Another process: refused, non-zero, and told whose lock it is.
	cmd := helperCmd(t, dir, "")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("a second process acquired the lock: err %v, output %s", err, out)
	}
	if want := fmt.Sprintf("pid %d on host", os.Getpid()); !strings.Contains(string(out), want) {
		t.Errorf("the refusal does not name the holder (%q):\n%s", want, out)
	}

	// This process, through a second open: refused too, as a HeldError
	// carrying the record the first Acquire wrote.
	_, err = Acquire(dir, "reindex")
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("a second in-process acquire: got %v, want a *HeldError", err)
	}
	host, _ := os.Hostname()
	if h := held.Holder; h == nil || h.PID != os.Getpid() || h.Host != host || h.Purpose != "serve" {
		t.Errorf("holder = %+v, want this process's serve", held.Holder)
	}
}

// A lock left by a process that was killed is not held: the kernel dropped
// it with the process. Nothing is deleted to get it back.
func TestKilledHolderIsNotStale(t *testing.T) {
	dir := t.TempDir()
	cmd := startHolder(t, dir)
	if _, err := Acquire(dir, "serve"); !errors.As(err, new(*HeldError)) {
		t.Fatalf("while the holder lives: got %v, want a *HeldError", err)
	}

	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("the lock file should still be there: %v", err)
	}

	l, err := Acquire(dir, "serve")
	if err != nil {
		t.Fatalf("after the holder was killed: %v", err)
	}
	defer l.Release()
	h, err := readHolder(filepath.Join(dir, FileName))
	if err != nil || h.PID != os.Getpid() {
		t.Errorf("the record was not rewritten for the new holder: %+v, %v", h, err)
	}
}

func TestReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir, "serve")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("a second release should do nothing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("release removed the lock file, which races a process about to lock it: %v", err)
	}

	// Released, the lock is free to another process as well as this one.
	if out, err := helperCmd(t, dir, "").CombinedOutput(); err != nil {
		t.Fatalf("another process after release: %v\n%s", err, out)
	}
	l2, err := Acquire(dir, "serve")
	if err != nil {
		t.Fatalf("this process after release: %v", err)
	}
	l2.Release()
}

// The refusal says what to check, not only that it failed. The golden holds
// the whole text, so a change to it is a reviewed change.
func TestHeldMessageSaysWhatToCheck(t *testing.T) {
	e := &HeldError{Dir: "/srv/ketju/data", Holder: &Holder{
		PID: 1234, Host: "board-1", Purpose: "serve",
		StartedAt: time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC), Version: "v0.1.0",
	}}
	unread := &HeldError{Dir: "/srv/ketju/data", ReadErr: errors.New("unexpected end of JSON input")}
	got := e.Error() + "\n" + unread.Error() + "\n"

	if *update {
		if err := os.WriteFile(heldGolden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(heldGolden)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/lock -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("the refusal message changed. If that was intended, regenerate with -update and say so in the ticket.\n\ngot:\n%s\nwant:\n%s", got, want)
	}

	for _, part := range []string{"/srv/ketju/data", "pid 1234", "host board-1", "ps", "systemctl status", "docker ps", "network filesystem"} {
		if !strings.Contains(e.Error(), part) {
			t.Errorf("the message does not mention %q", part)
		}
	}
	if !strings.Contains(unread.Error(), "another process") || !strings.Contains(unread.Error(), "could not be read") {
		t.Errorf("an unreadable record should still refuse and say why the holder is unnamed: %s", unread.Error())
	}
}

// A record that cannot be read does not unlock anything: the flock is what
// is held.
func TestUnreadableRecordStillRefuses(t *testing.T) {
	dir := t.TempDir()
	startHolder(t, dir)
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Acquire(dir, "serve")
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("got %v, want a *HeldError", err)
	}
	if held.Holder != nil || held.ReadErr == nil {
		t.Errorf("holder %+v, read error %v: want no holder and the reason", held.Holder, held.ReadErr)
	}
}

func TestRecordIsTheHolder(t *testing.T) {
	dir := t.TempDir()
	// A longer record left by an earlier holder must not leave its tail
	// behind the new one.
	old, _ := json.Marshal(Holder{PID: 1, Host: strings.Repeat("h", 200), Purpose: "serve"})
	if err := os.WriteFile(filepath.Join(dir, FileName), old, 0o644); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Truncate(time.Second)
	l, err := Acquire(dir, "reindex")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	var h Holder
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("the record is not one JSON object: %v\n%s", err, b)
	}
	host, _ := os.Hostname()
	if h.PID != os.Getpid() || h.Host != host || h.Purpose != "reindex" || h.Version == "" ||
		h.StartedAt.Before(before) || h.StartedAt.After(time.Now()) {
		t.Errorf("record = %+v", h)
	}
}

func TestMissingDirectoryIsNotCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "typo")
	if _, err := Acquire(dir, "serve"); err == nil {
		t.Fatal("acquired a lock in a directory that does not exist")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Acquire created %s: %v", dir, err)
	}
}
