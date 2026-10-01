package config

import (
	"fmt"
	"reflect"
	"strings"
)

// Section is one top-level part of config.toml, as the admin settings
// screen names it (TKT-01M29CXK2H): its name, why it lives in the file
// rather than in the settings table, and the keys under it. It carries no
// values. `ketju config check` shows those, with secrets redacted, to an
// operator on the machine; a browser gains nothing from paths and issuer
// URLs.
type Section struct {
	Name   string
	Reason string
	// Keys are the dotted keys under the section, as Entries keys them. A
	// list of tables is keyed with [] where Entries puts each element's key:
	// oidc.providers[].issuer.
	Keys []string
}

// The reasons, one per top-level key of Config. TestEverySectionHasAReason
// fails on a key added to Config without one, so a new section is explained
// on purpose.
const readBeforeDB = "Read before the database opens, so it cannot be kept in the database."

var sectionReasons = map[string]string{
	"listen":          readBeforeDB + " The server binds it at startup.",
	"data_dir":        readBeforeDB + " It says where the database is.",
	"site_url":        readBeforeDB + " Sign-in redirects are built from it.",
	"cookie_key_file": readBeforeDB + " It seals sign-ins in progress, and holds a secret.",
	"storage":         readBeforeDB + " The thread logs and caches are opened with it.",
	"log":             readBeforeDB + " The logger is set up before anything else runs.",
	"bootstrap":       "It decides who is admin before any admin exists, so no admin can be asked.",
	"oidc":            "It holds provider secrets, and is needed before anyone can sign in.",
	"limits":          readBeforeDB + " The upload cap is sized to the machine's disk and the stream limits to its connections, which are the operator's to judge.",
}

// Sections lists every top-level key of Config as a Section, in the order
// the struct declares them. It walks the type, as Entries walks the value,
// so a key added to Config is listed without anyone remembering to.
func Sections() []Section {
	t := reflect.TypeFor[Config]()
	var out []Section
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := tomlName(f)
		if name == "" {
			continue
		}
		reason, ok := sectionReasons[name]
		if !ok {
			panic(fmt.Sprintf("config: the section %s has no reason in sectionReasons", name))
		}
		out = append(out, Section{Name: name, Reason: reason, Keys: typeKeys(name, f.Type)})
	}
	return out
}

// tomlName is a field's key in the file, or "" for a field that has none.
func tomlName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	if name == "-" {
		return ""
	}
	return name
}

// typeKeys is every leaf key under key, for a field of type t, flattened as
// walk flattens a value: a struct is descended into, a list of tables is
// descended into with [] for its element, and anything that renders itself
// is a leaf.
func typeKeys(key string, t reflect.Type) []string {
	switch {
	case t.Implements(textMarshaler):
		return []string{key}
	case t.Kind() == reflect.Struct:
		var out []string
		for i := 0; i < t.NumField(); i++ {
			if name := tomlName(t.Field(i)); name != "" {
				out = append(out, typeKeys(key+"."+name, t.Field(i).Type)...)
			}
		}
		return out
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		return typeKeys(key+"[]", t.Elem())
	default:
		return []string{key}
	}
}
