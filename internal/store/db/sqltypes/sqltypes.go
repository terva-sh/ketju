// Package sqltypes holds the Go types sqlc maps particular columns to. It is
// separate from package db because the generated dbq package imports these
// types, and db imports dbq.
package sqltypes

import (
	"database/sql/driver"
	"fmt"
)

// SessionHash is the stored form of a session id: the 32-byte hash of the
// random value in the cookie (docs/05). It is a fixed-size array rather than
// a string or a slice so that no session query can be handed the plaintext
// id by mistake; the conversion from cookie value to hash lives in the
// session store, and only there.
type SessionHash [32]byte

// Value stores the hash as a 32-byte BLOB, which the column CHECK requires.
func (h SessionHash) Value() (driver.Value, error) {
	return h[:], nil
}

// Scan reads a hash back, refusing anything that is not exactly 32 bytes.
func (h *SessionHash) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("sqltypes: SessionHash from %T, want []byte", src)
	}
	if len(b) != len(h) {
		return fmt.Errorf("sqltypes: SessionHash of %d bytes, want %d", len(b), len(h))
	}
	copy(h[:], b)
	return nil
}
