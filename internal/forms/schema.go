// Package forms is the declarative form system of docs/05 "Forms": the
// schema a form version stores, the rules a schema must meet, and the
// server-side validation of an applicant's answers against it.
//
// The form is data rather than code because three things read it: the
// Preact client, the future html/template client, and the validator here.
// Per-field validation written by hand would be written three times and
// drift twice. So nothing in this package names a field key. Every rule,
// for the schema and for the answers, is reached through one table of field
// types (types, below), and a schema with keys nobody has seen before
// validates through the same code as the seeded registration form.
//
// The package is pure: no database, no HTTP, and no import of
// internal/problem. internal/service/forms stores and loads versions, and
// internal/problem maps this package's errors to API codes.
//
// # Errors carry paths and codes, never request text
//
// A refusal is a *Invalid listing FieldErrors. A schema error's Field is a
// path of property names and indices, such as "fields.2.options", and never
// a key the request supplied. An answer error's Field is the key from the
// stored schema. Code comes from a closed list (Codes). So an error body
// built from these echoes nothing a caller sent, as docs/07's error
// catalogue requires.
package forms

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Schema is one form version's schema_json (docs/05 "Forms").
type Schema struct {
	Title         string  `json:"title"`
	IntroMarkdown string  `json:"intro_markdown,omitempty"`
	Fields        []Field `json:"fields"`
}

// Field is one entry in a schema's field list. Which properties a field may
// carry depends on its type; the types table says which.
type Field struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
	// Required means an answer must be given. For a checkbox it means the
	// box must be ticked.
	Required bool `json:"required,omitempty"`
	// Max is the most characters an answer may have. 0 means the type's
	// cap.
	Max int `json:"max,omitempty"`
	// Options and Multiple are a select's.
	Options  []string `json:"options,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
	// FromClaim names the IdP claim that prefills the field.
	FromClaim string `json:"from_claim,omitempty"`
	// Locked means the claim is the answer: the server ignores whatever
	// was submitted for the field. It requires FromClaim.
	Locked bool `json:"locked,omitempty"`
	// Markdown is a markdown_notice's body, which docs/05 does not name.
	Markdown string `json:"markdown,omitempty"`
}

// The limits a schema is held to. docs/05 "Forms" lists them.
const (
	MaxSchemaBytes = 64 << 10
	MaxTitle       = 200
	MaxMarkdown    = 10000
	MaxFields      = 50
	MaxLabel       = 500
	MaxOptions     = 100
	MaxOption      = 200
)

// Claims are the IdP claims a field may name in from_claim.
var Claims = []string{"name", "email", "preferred_username"}

// The errors. ErrInvalidSchema and ErrInvalidAnswers arrive as the Kind of a
// *Invalid, which lists what was wrong. ErrUnknownAnswer is a client bug:
// the client drew its inputs from the schema, so an answer for a key the
// schema lacks means it did not.
var (
	ErrInvalidSchema  = errors.New("forms: the schema breaks the rules")
	ErrInvalidAnswers = errors.New("forms: the answers do not validate")
	ErrUnknownAnswer  = errors.New("forms: an answer names no field")
)

// The error codes, a closed list: they are API surface, carried in a
// validation-failed body's errors (docs/07 "Error catalogue").
const (
	CodeRequired        = "required"
	CodeTooLong         = "too_long"
	CodeInvalidEmail    = "invalid_email"
	CodeInvalidText     = "invalid_text"
	CodeNotAnOption     = "not_an_option"
	CodeDuplicateOption = "duplicate_option"
	CodeWrongType       = "wrong_type"
	CodeNotAllowed      = "not_allowed"
	CodeInvalidKey      = "invalid_key"
	CodeDuplicateKey    = "duplicate_key"
	CodeUnknownType     = "unknown_type"
	CodeUnknownClaim    = "unknown_claim"
	CodeOutOfRange      = "out_of_range"
	CodeTooMany         = "too_many"
	CodeUnknownProperty = "unknown_property"
	CodeMalformed       = "malformed"
)

// Codes lists every code, for the documentation test.
var Codes = []string{
	CodeRequired, CodeTooLong, CodeInvalidEmail, CodeInvalidText, CodeNotAnOption, CodeDuplicateOption,
	CodeWrongType, CodeNotAllowed, CodeInvalidKey, CodeDuplicateKey, CodeUnknownType, CodeUnknownClaim,
	CodeOutOfRange, CodeTooMany, CodeUnknownProperty, CodeMalformed,
}

// FieldError is one thing wrong: where, and which code.
type FieldError struct {
	// Field is an answer's key from the stored schema, or a path into a
	// schema such as "fields.2.options". The schema itself is "".
	Field string
	Code  string
}

// Invalid is a refusal that lists what was wrong. Kind is ErrInvalidSchema
// or ErrInvalidAnswers, and errors.Is finds it.
type Invalid struct {
	Kind   error
	Fields []FieldError
}

func (e *Invalid) Error() string {
	var b strings.Builder
	b.WriteString(e.Kind.Error())
	for i, f := range e.Fields {
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %s", f.Field, f.Code)
	}
	return b.String()
}

func (e *Invalid) Unwrap() error { return e.Kind }

// keyPattern is what a field key must match: it names the answer in
// answers_json and in the HTML client's form post.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// The properties every field has, and those any type may add. A type's
// entry in the table lists which of the optional ones it allows.
var (
	baseProps     = map[string]bool{"key": true, "type": true, "label": true}
	optionalProps = map[string]bool{"required": true, "max": true, "options": true, "multiple": true,
		"from_claim": true, "locked": true, "markdown": true}
	schemaProps = map[string]bool{"title": true, "intro_markdown": true, "fields": true}
)

// Parse reads a schema and checks it against the rules. On a refusal it
// returns a *Invalid whose Kind is ErrInvalidSchema, listing every rule the
// schema broke.
//
// It decodes one raw object at a time rather than through one strict
// decoder, so an unknown property or a value of the wrong JSON type is
// reported at its path rather than as one message about the whole
// document. An unknown property is refused, not ignored: a schema written
// by a newer build then fails loudly in an older one rather than losing a
// rule.
func Parse(b []byte) (Schema, error) {
	var s Schema
	p := &parser{}
	if len(b) > MaxSchemaBytes {
		p.add("", CodeTooLong)
		return s, p.err()
	}
	top, ok := p.object(b, "")
	if !ok {
		return s, p.err()
	}
	for _, k := range sortedKeys(top) {
		if !schemaProps[k] {
			p.add("", CodeUnknownProperty)
			break
		}
	}
	p.text(top, "title", "", &s.Title, true, MaxTitle, true)
	p.text(top, "intro_markdown", "", &s.IntroMarkdown, false, MaxMarkdown, false)

	raw := top["fields"]
	var fields []json.RawMessage
	switch {
	case absent(top, "fields"):
		p.add("fields", CodeRequired)
	case json.Unmarshal(raw, &fields) != nil:
		p.add("fields", CodeWrongType)
	case len(fields) == 0:
		p.add("fields", CodeRequired)
	case len(fields) > MaxFields:
		p.add("fields", CodeTooMany)
	default:
		seen := map[string]bool{}
		for i, rf := range fields {
			f, ok := p.field(rf, "fields."+strconv.Itoa(i))
			if !ok {
				continue
			}
			if f.Key != "" && keyPattern.MatchString(f.Key) {
				if seen[f.Key] {
					p.add("fields."+strconv.Itoa(i)+".key", CodeDuplicateKey)
				}
				seen[f.Key] = true
			}
			s.Fields = append(s.Fields, f)
		}
	}
	if err := p.err(); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// Canonical is s as it is stored: the JSON Parse reads back to s. A saved
// version is stored in this form, so two saves of the same schema store the
// same bytes whatever spacing or property order the editor sent.
func Canonical(s Schema) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Markdown and labels are text, not HTML; escaping < and & would make
	// the stored form harder to read and change nothing about its meaning.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// field reads and checks one field at path.
func (p *parser) field(b json.RawMessage, path string) (Field, bool) {
	var f Field
	props, ok := p.object(b, path)
	if !ok {
		return f, false
	}
	before := len(p.errs)
	at := func(prop string) string { return path + "." + prop }

	for _, k := range sortedKeys(props) {
		if !baseProps[k] && !optionalProps[k] {
			p.add(path, CodeUnknownProperty)
			break
		}
	}

	if p.decode(props, "key", path, &f.Key) {
		switch {
		case f.Key == "":
			p.add(at("key"), CodeRequired)
		case !keyPattern.MatchString(f.Key):
			p.add(at("key"), CodeInvalidKey)
		}
	} else if absent(props, "key") {
		p.add(at("key"), CodeRequired)
	}

	var t fieldType
	if p.decode(props, "type", path, &f.Type) {
		var known bool
		if t, known = types[f.Type]; !known {
			p.add(at("type"), CodeUnknownType)
		}
	} else if absent(props, "type") {
		p.add(at("type"), CodeRequired)
	}
	if t.answer == nil {
		// Without a known type nothing else about the field can be
		// judged; what was found so far stands.
		return f, len(p.errs) == before
	}

	for _, k := range sortedKeys(props) {
		if optionalProps[k] && !t.allows[k] {
			p.add(at(k), CodeNotAllowed)
		}
	}

	p.text(props, "label", path, &f.Label, !t.display, MaxLabel, true)
	if t.allows["markdown"] {
		p.text(props, "markdown", path, &f.Markdown, true, MaxMarkdown, false)
	}
	if t.allows["required"] {
		p.decode(props, "required", path, &f.Required)
	}
	if t.allows["multiple"] {
		p.decode(props, "multiple", path, &f.Multiple)
	}
	if t.allows["max"] && p.decode(props, "max", path, &f.Max) {
		if f.Max < 1 || f.Max > t.cap {
			p.add(at("max"), CodeOutOfRange)
		}
	}
	if t.allows["options"] {
		p.options(props, at("options"), &f.Options)
	}
	if t.allows["from_claim"] && p.decode(props, "from_claim", path, &f.FromClaim) {
		if !slices.Contains(Claims, f.FromClaim) {
			p.add(at("from_claim"), CodeUnknownClaim)
		}
	}
	if t.allows["locked"] && p.decode(props, "locked", path, &f.Locked) {
		if f.Locked && f.FromClaim == "" {
			p.add(at("from_claim"), CodeRequired)
		}
	}
	return f, len(p.errs) == before
}

// options reads and checks a select's options.
func (p *parser) options(props map[string]json.RawMessage, path string, out *[]string) {
	raw, ok := props["options"]
	if !ok || isNull(raw) {
		p.add(path, CodeRequired)
		return
	}
	var opts []string
	if err := json.Unmarshal(raw, &opts); err != nil {
		p.add(path, CodeWrongType)
		return
	}
	switch {
	case len(opts) == 0:
		p.add(path, CodeRequired)
		return
	case len(opts) > MaxOptions:
		p.add(path, CodeTooMany)
		return
	}
	seen := map[string]bool{}
	for i, o := range opts {
		at := path + "." + strconv.Itoa(i)
		switch {
		case o == "":
			p.add(at, CodeRequired)
		case utf8.RuneCountInString(o) > MaxOption:
			p.add(at, CodeTooLong)
		case hasControl(o, ""):
			p.add(at, CodeInvalidText)
		case seen[o]:
			p.add(at, CodeDuplicateOption)
		}
		seen[o] = true
	}
	*out = opts
}

// parser collects the errors of one Parse.
type parser struct {
	errs []FieldError
}

func (p *parser) add(field, code string) {
	p.errs = append(p.errs, FieldError{Field: field, Code: code})
}

func (p *parser) err() error {
	if len(p.errs) == 0 {
		return nil
	}
	return &Invalid{Kind: ErrInvalidSchema, Fields: p.errs}
}

// object reads b as exactly one JSON object.
func (p *parser) object(b []byte, path string) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			p.add(path, CodeWrongType)
		} else {
			p.add(path, CodeMalformed)
		}
		return nil, false
	}
	if m == nil {
		p.add(path, CodeWrongType)
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		p.add(path, CodeMalformed)
		return nil, false
	}
	return m, true
}

// decode reads the property name into v, reporting a wrong type. It reports
// whether the property was there and decoded. A null is absent.
func (p *parser) decode(props map[string]json.RawMessage, name, path string, v any) bool {
	raw, ok := props[name]
	if !ok || isNull(raw) {
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		p.add(join(path, name), CodeWrongType)
		return false
	}
	return true
}

// absent reports a property that is missing or null.
func absent(props map[string]json.RawMessage, name string) bool {
	raw, ok := props[name]
	return !ok || isNull(raw)
}

// text reads a string property and checks its length: required, at most max
// characters, and free of control characters when line is set.
func (p *parser) text(props map[string]json.RawMessage, name, path string, v *string, required bool, max int, line bool) {
	at := join(path, name)
	if !p.decode(props, name, path, v) {
		if required && absent(props, name) {
			p.add(at, CodeRequired)
		}
		return
	}
	switch {
	case strings.TrimSpace(*v) == "":
		if required {
			p.add(at, CodeRequired)
		}
	case utf8.RuneCountInString(*v) > max:
		p.add(at, CodeTooLong)
	case line && hasControl(*v, ""):
		p.add(at, CodeInvalidText)
	case !line && hasControl(*v, "\n\t\r"):
		p.add(at, CodeInvalidText)
	}
}

// join is the path of property name under path.
func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// hasControl reports a control character in s other than those in allowed.
func hasControl(s, allowed string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && !strings.ContainsRune(allowed, r) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
