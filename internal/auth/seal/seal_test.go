package seal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type flow struct {
	State string `json:"state"`
	N     int    `json:"n"`
}

func sealer(t *testing.T, key byte, now *time.Time) *Sealer {
	t.Helper()
	s, err := New(bytes.Repeat([]byte{key}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return *now }
	return s
}

func TestRoundTrip(t *testing.T) {
	now := time.Unix(1_757_500_000, 0)
	s := sealer(t, 1, &now)
	v, err := s.Seal("login", flow{"abc", 7})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(v, "abc") {
		t.Errorf("the sealed value shows its contents: %s", v)
	}
	var got flow
	if err := s.Open("login", v, time.Minute, &got); err != nil || got != (flow{"abc", 7}) {
		t.Fatalf("Open = %+v, %v", got, err)
	}
	again, _ := s.Seal("login", flow{"abc", 7})
	if again == v {
		t.Error("two seals of one value are identical: the nonce is not fresh")
	}
}

// Anything but this key's seal for this purpose is ErrInvalid, whichever
// part was wrong.
func TestOnlyThisKeysSealForThisPurposeOpens(t *testing.T) {
	now := time.Unix(1_757_500_000, 0)
	s := sealer(t, 1, &now)
	v, _ := s.Seal("login", flow{"abc", 7})
	raw, _ := base64.RawURLEncoding.DecodeString(v)

	flip := func(i int) string {
		b := bytes.Clone(raw)
		b[i] ^= 1
		return base64.RawURLEncoding.EncodeToString(b)
	}
	cases := map[string]struct{ purpose, value string }{
		"a byte of the nonce altered":      {"login", flip(0)},
		"a byte of the ciphertext altered": {"login", flip(len(raw) / 2)},
		"a byte of the tag altered":        {"login", flip(len(raw) - 1)},
		"truncated":                        {"login", base64.RawURLEncoding.EncodeToString(raw[:len(raw)-1])},
		"shorter than a nonce and tag":     {"login", base64.RawURLEncoding.EncodeToString(raw[:20])},
		"not base64":                       {"login", v + "!"},
		"padded base64":                    {"login", v + "="},
		"empty":                            {"login", ""},
		"sealed for another purpose":       {"registration", v},
	}
	for name, c := range cases {
		var got flow
		if err := s.Open(c.purpose, c.value, time.Minute, &got); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	other := sealer(t, 2, &now)
	var got flow
	if err := other.Open("login", v, time.Minute, &got); !errors.Is(err, ErrInvalid) {
		t.Errorf("another key: err = %v, want ErrInvalid", err)
	}
	// Sealed properly, but not the shape asked for.
	odd, _ := s.Seal("login", []int{1})
	if err := s.Open("login", odd, time.Minute, &got); !errors.Is(err, ErrInvalid) {
		t.Errorf("the wrong shape: err = %v, want ErrInvalid", err)
	}
}

func TestAValueExpires(t *testing.T) {
	now := time.Unix(1_757_500_000, 0)
	s := sealer(t, 1, &now)
	v, _ := s.Seal("login", flow{"abc", 7})
	var got flow
	now = now.Add(10 * time.Minute)
	if err := s.Open("login", v, 10*time.Minute, &got); err != nil {
		t.Errorf("at exactly maxAge: %v", err)
	}
	now = now.Add(time.Second)
	if err := s.Open("login", v, 10*time.Minute, &got); !errors.Is(err, ErrExpired) {
		t.Errorf("a second past maxAge: err = %v, want ErrExpired", err)
	}
	// A value from the future is a clock the server does not have.
	now = time.Unix(1_757_500_000, 0).Add(-time.Minute)
	if err := s.Open("login", v, 10*time.Minute, &got); err != nil {
		t.Errorf("a minute of skew: %v", err)
	}
	now = now.Add(-time.Second)
	if err := s.Open("login", v, 10*time.Minute, &got); !errors.Is(err, ErrExpired) {
		t.Errorf("past the skew: err = %v, want ErrExpired", err)
	}
}

func TestNewWantsAnAES256Key(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("a %d-byte key was accepted", n)
		}
	}
}

func TestLoadOrCreateKeyCreatesOnceAndKeeps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "cookie.key")
	key, warn, err := LoadOrCreateKey(path)
	if err != nil || warn != "" {
		t.Fatalf("create: %v %q", err, warn)
	}
	if len(key) != KeySize || bytes.Equal(key, make([]byte, KeySize)) {
		t.Fatalf("created key %x", key)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("the key is mode %04o, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("its directory is mode %04o, want 0700", di.Mode().Perm())
	}
	again, _, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(again, key) {
		t.Fatalf("a second start read %x (%v), want the key it made", again, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".cookie-key-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// Two processes starting together agree on one key.
func TestLoadOrCreateKeyRaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookie.key")
	keys := make([][]byte, 8)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, _, err := LoadOrCreateKey(path)
			if err != nil {
				t.Error(err)
			}
			keys[i] = k
		}()
	}
	wg.Wait()
	for _, k := range keys[1:] {
		if !bytes.Equal(k, keys[0]) {
			t.Fatal("racing starts ended with different keys")
		}
	}
}

func TestLoadOrCreateKeyRefusesAWrongKeyAndWarnsOnAnOpenOne(t *testing.T) {
	dir := t.TempDir()
	short := filepath.Join(dir, "short.key")
	os.WriteFile(short, []byte("too short"), 0o600)
	if _, _, err := LoadOrCreateKey(short); err == nil || !strings.Contains(err.Error(), "9 bytes") {
		t.Errorf("a short key: err = %v", err)
	}
	long := filepath.Join(dir, "long.key")
	os.WriteFile(long, make([]byte, 64), 0o600)
	if _, _, err := LoadOrCreateKey(long); err == nil || !strings.Contains(err.Error(), "33 bytes") {
		t.Errorf("a long key: err = %v", err)
	}
	open := filepath.Join(dir, "open.key")
	os.WriteFile(open, bytes.Repeat([]byte{3}, KeySize), 0o644)
	os.Chmod(open, 0o644)
	if _, warn, err := LoadOrCreateKey(open); err != nil || !strings.Contains(warn, "0644") {
		t.Errorf("an open key: warn %q, err %v", warn, err)
	}
	if _, _, err := LoadOrCreateKey(filepath.Join(short, "under-a-file")); err == nil {
		t.Error("a key under a file was created")
	}
}
