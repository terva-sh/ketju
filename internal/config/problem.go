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
}

// decodeError turns a TOML decoding failure into something that names a key.
//
// go-toml reports unknown fields as a StrictMissingError carrying one entry
// per offending key, and type mismatches as a DecodeError with a source
// excerpt. Both are better than what a caller would print by default, and
// neither is useful unless it is unwrapped.
func decodeError(file string, err error) error {
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
		if key == "" {
			key = "(file)"
		}
		row, col := dec.Position()
		p.addf(key, "%s (line %d, column %d)", dec.Error(), row, col)
		return p.err()
	}

	return fmt.Errorf("config: parsing %s: %w", file, err)
}

func topLevel(key string) string {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i]
	}
	return key
}
