package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Redacted is what stands in for a secret everywhere but the one call that
// needs the value.
const Redacted = "[redacted]"

// Secret holds a value that must never reach a log line, an error message, or
// the effective-config output.
//
// It is a struct rather than a string so that the only way to get the value
// out is to ask for it by name. A `type Secret string` would be printed by
// anything that prints a string, and "anything" includes %v on a struct that
// merely contains one, slog attributes, JSON encoding of a config dump, and
// the error text of a library that quotes what it was given. Each of those is
// a plausible route from a config file to a log file, and closing them one at
// a time as they are noticed is how a credential ends up in a log aggregator.
//
// So Secret implements the four interfaces that printing goes through —
// fmt.Stringer, fmt.GoStringer, json.Marshaler, slog.LogValuer — and every one
// of them returns Redacted. Value is the single exit.
type Secret struct {
	v string
}

// NewSecret wraps a value.
func NewSecret(v string) Secret { return Secret{v: v} }

// Value returns the secret itself. This is the only way to read it, and the
// only correct callers are the ones that hand it to the party it authenticates
// against.
func (s Secret) Value() string { return s.v }

// IsZero reports whether the secret is empty, without revealing anything else.
func (s Secret) IsZero() bool { return s.v == "" }

// String implements fmt.Stringer. It is the reason %v and %s are safe.
func (s Secret) String() string {
	if s.v == "" {
		return ""
	}
	return Redacted
}

// GoString implements fmt.GoStringer, which is what %#v uses. Without it, %#v
// on a struct holding a Secret would print the unexported field.
func (s Secret) GoString() string {
	if s.v == "" {
		return `config.Secret{}`
	}
	return `config.Secret{` + Redacted + `}`
}

// MarshalJSON keeps the secret out of any JSON rendering of the config,
// including the effective-config output.
func (s Secret) MarshalJSON() ([]byte, error) {
	if s.v == "" {
		return []byte(`""`), nil
	}
	return json.Marshal(Redacted)
}

// MarshalText covers encoders that prefer text, TOML among them.
func (s Secret) MarshalText() ([]byte, error) {
	if s.v == "" {
		return []byte(nil), nil
	}
	return []byte(Redacted), nil
}

// UnmarshalText lets a secret be decoded straight from the config file.
func (s *Secret) UnmarshalText(b []byte) error {
	s.v = string(b)
	return nil
}

// LogValue implements slog.LogValuer, so a Secret passed as a log attribute
// logs as redacted rather than as a struct.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// readSecretFile loads a secret from a file, which is how a secret reaches a
// container without being written into config.toml or into the environment of
// every process that can read /proc.
//
// A single trailing newline is removed. Every editor and every `echo > file`
// adds one, and a client secret with a newline on the end fails
// authentication in a way that looks like a wrong secret rather than a
// formatting mistake.
func readSecretFile(path string) (Secret, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Secret{}, err
	}
	v := strings.TrimRight(string(b), "\r\n")
	if v == "" {
		return Secret{}, fmt.Errorf("file is empty")
	}
	return Secret{v: v}, nil
}

// secretFileWarning reports a permission problem worth mentioning without
// refusing to start.
//
// Refusing would be wrong: bind-mounted secrets and Kubernetes projected
// volumes both land with modes an application does not control, and a forum
// that will not boot because a file is 0444 helps nobody. Saying nothing would
// also be wrong, because 0666 on a client secret is a real finding. So it is a
// warning, and `ketju config check` is where an operator will see it.
func secretFileWarning(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if mode := fi.Mode().Perm(); mode&0o022 != 0 {
		return fmt.Sprintf("%s is mode %04o: group or world writable, so anything that can write it can change who this server trusts", path, mode)
	}
	return ""
}
