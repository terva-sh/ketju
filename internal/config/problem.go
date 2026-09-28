package config

import (
	"errors"
	"fmt"
	"reflect"
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
// per offending key, and syntax errors and type mismatches as a DecodeError
// with a position. Neither is useful unless it is unwrapped.
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

	// go-toml's own message is never passed on. It can quote the file -- a
	// character of an invalid escape, the text of a number that did not
	// parse -- and the file can hold a client_secret, on the very line that
	// failed. So the message is built here from the key, the position and
	// the type the key expects, none of which is the file's content, and a
	// secret on a broken line cannot reach a log or `ketju config check`.
	var dec *toml.DecodeError
	if errors.As(err, &dec) {
		row, col := dec.Position()
		if key := strings.Join(dec.Key(), "."); key != "" {
			if want := expects(key); want != "" {
				p.addf(key, "the value is not %s (line %d, column %d)", want, row, col)
			} else {
				p.addf(key, "the value is not one this key accepts (line %d, column %d)", row, col)
			}
			return p.err()
		}
		// A syntax error carries no key, but the line it is on usually
		// names one, and "the key" is what the operator is looking for.
		p.addf(keyAtLine(src, row), "not valid TOML at line %d, column %d", row, col)
		return p.err()
	}

	// Anything else go-toml reports is still the file's fault rather than a
	// failure to read it, so it is a problem like the others, and
	// `ketju config check` exits with the same code for it. Its text is left
	// out for the reason above.
	p.add("(file)", "not valid TOML, or not in the shape this version reads")
	return p.err()
}

// expects names the kind of value key takes, found by following its toml
// tags through Config. A segment naming no field -- an index, or a
// provider's key -- is skipped, which is how an array of tables is entered.
func expects(key string) string {
	t := reflect.TypeFor[Config]()
	for _, seg := range strings.Split(key, ".") {
		for t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return ""
		}
		f, ok := fieldByTag(t, seg)
		if !ok {
			continue
		}
		t = f.Type
	}
	switch {
	case t == reflect.TypeFor[Secret](), t.Kind() == reflect.String:
		return "a string"
	case t.Kind() == reflect.Int:
		return "a whole number"
	case t.Kind() == reflect.Bool:
		return "true or false"
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String:
		return "a list of strings"
	case t.Kind() == reflect.Map:
		return "a table of strings"
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		return "an array of tables"
	case t.Kind() == reflect.Struct:
		return "a table"
	}
	return ""
}

func fieldByTag(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
		if tag == name {
			return t.Field(i), true
		}
	}
	return reflect.StructField{}, false
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
