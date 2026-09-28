package config

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
)

// Layer names where an effective value came from. `ketju config check`
// prints one beside every key, because "why is the server doing this" is
// usually answered by "because a variable in the compose file says so".
type Layer string

const (
	// LayerDefault is a key no layer set. Its value is the documented
	// default, or the zero value for an optional key with none.
	LayerDefault Layer = "default"

	// LayerFile is a key config.toml set.
	LayerFile Layer = "file"

	// LayerEnv is a key a KETJU_* variable set.
	LayerEnv Layer = "env"

	// LayerEnvFile is a key a KETJU_*_FILE variable set, from the file it
	// names.
	LayerEnvFile Layer = "env-file"

	// LayerSecretFile is a provider's client_secret, read from the file
	// client_secret_file names.
	LayerSecretFile Layer = "secret-file"
)

// Source is where one key's effective value came from.
type Source struct {
	Layer Layer

	// From names the variable for env and env-file, and the path for
	// secret-file. It is never a value.
	From string
}

func (s Source) String() string {
	if s.From == "" {
		return string(s.Layer)
	}
	return string(s.Layer) + " " + s.From
}

// Source reports where key's value came from, keyed as Entries keys it. A
// key no layer set is LayerDefault.
func (c *Config) Source(key string) Source {
	if s, ok := c.sources[key]; ok {
		return s
	}
	return Source{Layer: LayerDefault}
}

func (c *Config) setSource(key string, s Source) {
	if c.sources == nil {
		c.sources = map[string]Source{}
	}
	c.sources[key] = s
}

// Entry is one key of the effective configuration.
type Entry struct {
	// Key is the dotted path. A provider's fields are keyed by the
	// provider's key, oidc.providers.google.issuer, as problems and
	// environment overrides name them.
	Key string

	// Value is the field itself, typed. A secret is a [Secret], so every
	// printer that renders it -- fmt, TOML, JSON, slog -- renders
	// [Redacted]. Nothing here ever calls Secret.Value.
	Value any

	Source Source
}

// Entries lists every key this version understands with its effective value
// and source, in the order the struct declares them.
//
// It walks the struct rather than a list of names, so a key added to Config
// is listed without anyone remembering to list it: a key missing from
// `ketju config check` would be a key an operator cannot see is set.
func (c *Config) Entries() []Entry {
	var out []Entry
	c.walk("", reflect.ValueOf(c).Elem(), &out)
	return out
}

var textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()

func (c *Config) walk(prefix string, v reflect.Value, out *[]Entry) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		fv := v.Field(i)

		switch {
		case f.Type.Implements(textMarshaler):
			// A Secret, or anything else that renders itself. It is a leaf,
			// whatever its insides are, so its own marshalling decides what
			// is printed.
			*out = append(*out, Entry{Key: key, Value: fv.Interface(), Source: c.Source(key)})
		case f.Type.Kind() == reflect.Struct:
			c.walk(key, fv, out)
		case f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct:
			for j := 0; j < fv.Len(); j++ {
				c.walk(elementKey(key, j, fv.Index(j)), fv.Index(j), out)
			}
		default:
			val := fv.Interface()
			switch {
			case f.Type.Kind() == reflect.Slice && fv.IsNil():
				// An empty list prints as [] rather than as nothing, in
				// text and in JSON alike.
				val = reflect.MakeSlice(f.Type, 0, 0).Interface()
			case f.Type.Kind() == reflect.Map && fv.IsNil():
				val = reflect.MakeMap(f.Type).Interface()
			}
			*out = append(*out, Entry{Key: key, Value: val, Source: c.Source(key)})
		}
	}
}

// elementKey names one element of an array of tables by its "key" field when
// it has one, as providers do, and by its index otherwise.
func elementKey(prefix string, i int, v reflect.Value) string {
	t := v.Type()
	for j := 0; j < t.NumField(); j++ {
		name, _, _ := strings.Cut(t.Field(j).Tag.Get("toml"), ",")
		if name == "key" && t.Field(j).Type.Kind() == reflect.String {
			if k := v.Field(j).String(); k != "" {
				return prefix + "." + k
			}
		}
	}
	return fmt.Sprintf("%s[%d]", prefix, i)
}

// recordFileSources marks every key the file set. raw is the file decoded
// loosely; providers are keyed by their key field, as Entries keys them.
func recordFileSources(c *Config, raw map[string]any) {
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			switch v := v.(type) {
			case map[string]any:
				// A table the struct declares (storage) and a map-valued
				// key (group_map) look the same here; marking both keeps
				// the second right and costs the first nothing.
				c.setSource(path, Source{Layer: LayerFile})
				walk(path, v)
			case []any:
				tables := true
				for _, e := range v {
					if _, ok := e.(map[string]any); !ok {
						tables = false
					}
				}
				if !tables || len(v) == 0 {
					c.setSource(path, Source{Layer: LayerFile})
					continue
				}
				for i, e := range v {
					t := e.(map[string]any)
					elem := fmt.Sprintf("%s[%d]", path, i)
					if key, ok := t["key"].(string); ok && key != "" {
						elem = path + "." + key
					}
					walk(elem, t)
				}
			default:
				c.setSource(path, Source{Layer: LayerFile})
			}
		}
	}
	walk("", raw)
}
