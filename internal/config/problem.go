package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Problem is one thing wrong with the configuration, named by its key.
//
// The key is the point. "invalid value" sends an operator to read the whole
// file; "storage.fsync: unknown policy \"gruop\"" sends them to one line.
type Problem struct {
	// Key is the dotted path, as it appears in the file.
	Key string

	// Msg says what is wrong and, where there is one, what to do instead.
	Msg string
}

func (p Problem) Error() string { return p.Key + ": " + p.Msg }

// Error is every problem found in one load, so that a broken config is fixed
// in one pass rather than one restart per mistake.
type Error struct {
	File     string
	Problems []Problem
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "config: %s has %d problem", e.File, len(e.Problems))
	if len(e.Problems) != 1 {
		b.WriteByte('s')
	}
	b.WriteByte(':')
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.Error())
	}
	return b.String()
}

// Keys reports the offending keys, which is what a test asserts on.
func (e *Error) Keys() []string {
	out := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = p.Key
	}
	return out
}

type problemList struct {
	file string
	list []Problem
}

func (p *problemList) add(key, msg string) {
	p.list = append(p.list, Problem{Key: key, Msg: msg})
}

func (p *problemList) addf(key, format string, args ...any) {
	p.add(key, fmt.Sprintf(format, args...))
}

func (p *problemList) err() error {
	if len(p.list) == 0 {
		return nil
	}
	sort.SliceStable(p.list, func(i, j int) bool { return p.list[i].Key < p.list[j].Key })
	return &Error{File: p.file, Problems: p.list}
}

// movedKeys are keys a reader could reasonably expect to find here and which
// live somewhere else, mapped to where.
//
// They get a pointed message rather than "unknown key", because "unknown key"
// invites the reader to conclude the key was renamed and go looking for the
// new spelling. These were not renamed; they moved to the settings table,
// where an admin changes them without a restart and the change leaves an audit
// row. docs/05 shows some of them in a config.toml example, which is the
// document that is wrong.
var movedKeys = map[string]string{
	"registration":                       "registration policy is a runtime setting: it lives in the settings table and is changed in the admin UI, not by editing this file and restarting",
	"registration.mode":                  "registration mode is a runtime setting: it lives in the settings table and is changed in the admin UI",
	"registration.allow_local_passwords": "local passwords are a runtime setting: they live in the settings table and are changed in the admin UI",
	"site_name":                          "the site name is a runtime setting: it lives in the settings table and is changed in the admin UI",
	"slow_mode":                          "slow mode is a runtime setting: it is off by default and a moderator sets it per thread (docs/08), in the settings table rather than this file",
	"posts":                              "the edit and delete windows are runtime settings: they live in the settings table and are changed in the admin UI",
	"flags":                              "the flag threshold is a runtime setting: it lives in the settings table and is changed in the admin UI",
	"public_edit_history":                "whether members may read edit history is a runtime setting: it lives in the settings table and is changed in the admin UI",
}

// decodeError turns a TOML decoding failure into something that names a key.
//
// go-toml reports unknown fields as a StrictMissingError carrying one entry
// per offending key, and type mismatches as a DecodeError with a source
// excerpt. Both are better than what a caller would print by default, and
// neither is useful unless it is unwrapped.
func decodeError(file string, src []byte, err error) error {
	p := &problemList{file: file}

	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		for _, e := range strict.Errors {
			key := strings.Join(e.Key(), ".")
			if where, ok := movedKeys[key]; ok {
				p.add(key, where)
				continue
			}
			if where, ok := movedKeys[topLevel(key)]; ok {
				p.add(key, where)
				continue
			}
			p.add(key, "unknown key; `ketju config check` prints every key this version understands")
		}
		return p.err()
	}

	var dec *toml.DecodeError
	if errors.As(err, &dec) {
		key := strings.Join(dec.Key(), ".")
		row, col := dec.Position()
		if key == "" {
			// A syntax error carries no key, but the line it is on usually
			// names one, and "the key" is what the operator is looking for.
			key = keyAtLine(src, row)
		}
		p.addf(key, "%s (line %d, column %d)", dec.Error(), row, col)
		return p.err()
	}

	// Anything else go-toml reports is still the file's fault rather than a
	// failure to read it, so it is a problem like the others, and
	// `ketju config check` exits with the same code for it.
	p.add("(file)", err.Error())
	return p.err()
}

// keyAtLine names the key written on line row (1-based) of src, under the
// table header above it, or "(file)" when the line does not start with a
// plain key. It reads the line as text, because the file failed to parse.
func keyAtLine(src []byte, row int) string {
	lines := strings.Split(string(src), "\n")
	if row < 1 || row > len(lines) {
		return "(file)"
	}
	name, _, ok := strings.Cut(lines[row-1], "=")
	name = strings.TrimSpace(name)
	if !ok || !isPlainKey(name) {
		return "(file)"
	}
	for i := row - 2; i >= 0; i-- {
		h := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(h, "[") {
			continue
		}
		h = strings.Trim(h, "[] \t")
		if isPlainKey(h) {
			return h + "." + name
		}
		return "(file)"
	}
	return name
}

// isPlainKey reports whether s is a bare or dotted TOML key with no
// quoting, which is every key this package understands.
func isPlainKey(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		for _, c := range part {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

func topLevel(key string) string {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i]
	}
	return key
}
