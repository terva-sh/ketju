// Package seal keeps short-lived state in a cookie rather than a table: the
// pre-login state of an OIDC sign-in (docs/05 "Login flow"), and the verified
// claims a registration carries from the callback to the handle screen
// (TKT-01M29C8005).
//
// A sealed value is AES-256-GCM over its JSON, so the browser can neither
// read it nor change it: the cookie is both secret and tamper-evident, as
// docs/05 and TKT-01M29BSPEF decided in place of an anonymous session row,
// which would cost a write for every abandoned login. Each value is sealed
// for a purpose, bound in as additional data, so a value sealed for one
// cookie cannot be replayed as another. And each carries the time it was
// sealed, so a value outlives its purpose only as long as Open allows.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// KeySize is the key length: 32 bytes, AES-256.
const KeySize = 32

var (
	// ErrInvalid is a value that was not sealed by this key for this
	// purpose: altered, truncated, sealed for another cookie, or garbage.
	ErrInvalid = errors.New("seal: not a value this server sealed for this purpose")
	// ErrExpired is a genuine value older than Open allows.
	ErrExpired = errors.New("seal: the value has expired")
)

// Sealer seals and opens values with one key.
type Sealer struct {
	aead cipher.AEAD
	// Now is the clock; nil means time.Now. Tests move it.
	Now func() time.Time
}

// New returns a Sealer for a KeySize-byte key.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("seal: the key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type envelope struct {
	// At is when the value was sealed, in Unix seconds.
	At int64           `json:"at"`
	V  json.RawMessage `json:"v"`
}

// Seal returns v sealed for purpose, as URL-safe base64 fit for a cookie.
func (s *Sealer) Seal(purpose string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(envelope{At: s.now().Unix(), V: raw})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plain)+s.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, []byte(purpose))), nil
}

// Open reads a value Seal produced for purpose into v. It returns ErrInvalid
// for anything else, and ErrExpired for a value sealed more than maxAge ago,
// or dated in the future beyond a minute's clock skew.
func (s *Sealer) Open(purpose, sealed string, maxAge time.Duration, v any) error {
	b, err := base64.RawURLEncoding.Strict().DecodeString(sealed)
	if err != nil || len(b) < s.aead.NonceSize()+s.aead.Overhead() {
		return ErrInvalid
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, b[:n], b[n:], []byte(purpose))
	if err != nil {
		return ErrInvalid
	}
	var e envelope
	if err := json.Unmarshal(plain, &e); err != nil {
		return ErrInvalid
	}
	at, now := time.Unix(e.At, 0), s.now()
	if now.Sub(at) > maxAge || at.Sub(now) > time.Minute {
		return ErrExpired
	}
	if err := json.Unmarshal(e.V, v); err != nil {
		return ErrInvalid
	}
	return nil
}

// LoadOrCreateKey reads the KeySize-byte key at path, creating it with a
// fresh random key, mode 0600 in a 0700 directory, when there is nothing
// there. warning is set when the file is group or world accessible, which is
// worth saying without refusing to start (config's secretFileWarning
// explains why a mode is a warning).
//
// The key must persist: one made up at each start would break every login
// in flight across a restart. Nothing here logs it.
func LoadOrCreateKey(path string) (key []byte, warning string, err error) {
	key, err = readKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := create(path); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, "", fmt.Errorf("seal: creating the cookie key %s: %w", path, err)
		}
		// Read back what is on disk, which is ours or, had two processes
		// raced to create it, the winner's.
		key, err = readKey(path)
	}
	if err != nil {
		return nil, "", err
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		warning = fmt.Sprintf("the cookie key %s is mode %04o: anyone who can read it can forge a sign-in's state",
			path, fi.Mode().Perm())
	}
	return key, warning, nil
}

func readKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	key, err := io.ReadAll(io.LimitReader(f, KeySize+1))
	if err != nil {
		return nil, fmt.Errorf("seal: reading the cookie key %s: %w", path, err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("seal: the cookie key %s is %d bytes, want exactly %d; "+
			"delete it to have a new one made, which signs out nobody but ends sign-ins in progress",
			path, len(key), KeySize)
	}
	return key, nil
}

func create(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	// Written under a temporary name and linked into place, so no reader
	// ever sees a partial key, and O_EXCL-like: link fails if one exists.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cookie-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmp.Name(), path)
}
