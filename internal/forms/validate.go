package forms

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"
)

// fieldType is one entry in the table: what a field of the type may carry,
// and how an answer to it is checked.
type fieldType struct {
	// allows is the optional properties the type takes. Any other is
	// refused as not_allowed.
	allows map[string]bool
	// cap is the most max may be, and the limit when max is unset. 0 for a
	// type without one.
	cap int
	// display is a type that shows something and takes no answer. Its
	// label is optional.
	display bool
	// answer checks one submitted value. max is the field's limit (Limit),
	// and present is false for a value that is absent or null. It returns
	// the value to store, whether that value is empty (for required), and a
	// code when the value is refused.
	answer func(f Field, max int, raw json.RawMessage, present bool) (value any, empty bool, code string)
}

func props(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// types is the table every rule goes through: Parse reads it to judge a
// field's properties, and Validate to judge its answer. Adding a type is
// adding an entry here and nothing else.
var types = map[string]fieldType{
	"text": {
		allows: props("required", "max", "from_claim", "locked"),
		cap:    200,
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			return line(max, raw, present, nil)
		},
	},
	"email": {
		allows: props("required", "max", "from_claim", "locked"),
		cap:    254,
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			return line(max, raw, present, func(v string) string {
				if a, err := mail.ParseAddress(v); err != nil || a.Address != v {
					return CodeInvalidEmail
				}
				return ""
			})
		},
	},
	"textarea": {
		allows: props("required", "max"),
		cap:    5000,
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			if !present {
				return "", true, ""
			}
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, false, CodeWrongType
			}
			v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
			switch {
			case hasControl(v, "\n\t"):
				return nil, false, CodeInvalidText
			case utf8.RuneCountInString(v) > max:
				return nil, false, CodeTooLong
			}
			return v, v == "", ""
		},
	},
	"checkbox": {
		allows: props("required"),
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			if !present {
				return false, true, ""
			}
			var v bool
			if json.Unmarshal(raw, &v) != nil {
				return nil, false, CodeWrongType
			}
			// An unticked box is no answer, so a required box must be
			// ticked.
			return v, !v, ""
		},
	},
	"select": {
		allows: props("required", "options", "multiple"),
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			if !f.Multiple {
				if !present {
					return "", true, ""
				}
				var v string
				if json.Unmarshal(raw, &v) != nil {
					return nil, false, CodeWrongType
				}
				if v != "" && !slices.Contains(f.Options, v) {
					return nil, false, CodeNotAnOption
				}
				return v, v == "", ""
			}
			vs := []string{}
			if !present {
				return vs, true, ""
			}
			if json.Unmarshal(raw, &vs) != nil || vs == nil {
				return nil, false, CodeWrongType
			}
			seen := map[string]bool{}
			for _, v := range vs {
				switch {
				case !slices.Contains(f.Options, v):
					return nil, false, CodeNotAnOption
				case seen[v]:
					return nil, false, CodeDuplicateOption
				}
				seen[v] = true
			}
			return vs, len(vs) == 0, ""
		},
	},
	"markdown_notice": {
		allows:  props("markdown"),
		display: true,
		answer: func(f Field, max int, raw json.RawMessage, present bool) (any, bool, string) {
			if present {
				return nil, false, CodeNotAllowed
			}
			return nil, true, ""
		},
	},
}

// Types lists the field types, sorted, for the documentation test.
func Types() []string {
	out := make([]string, 0, len(types))
	for t := range types {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Limit is the most characters an answer to f may have: its max, or its
// type's cap. It is 0 for a type that takes no text.
func Limit(f Field) int {
	t, ok := types[f.Type]
	switch {
	case !ok || t.cap == 0:
		return 0
	case f.Max > 0:
		return f.Max
	}
	return t.cap
}

// line checks a one-line string answer: trimmed, no control characters, at
// most max characters, and then whatever more asks.
func line(max int, raw json.RawMessage, present bool, more func(string) string) (any, bool, string) {
	if !present {
		return "", true, ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return nil, false, CodeWrongType
	}
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "", true, ""
	case hasControl(v, ""):
		return nil, false, CodeInvalidText
	case utf8.RuneCountInString(v) > max:
		return nil, false, CodeTooLong
	}
	if more != nil {
		if code := more(v); code != "" {
			return nil, false, code
		}
	}
	return v, false, ""
}

// Answers are validated answers, normalized, keyed by field key: what an
// application stores as answers_json. A markdown_notice has no entry.
type Answers map[string]any

// Validate checks answers against s and returns them normalized: strings
// trimmed, and every locked field replaced by its claim.
//
// A locked field ignores what was submitted and takes claims[from_claim],
// because the form disabling the input is the client's promise, not the
// server's. When the claim is absent it stores "" and skips required: the
// applicant cannot supply a value the IdP withheld.
//
// An answer for a key no field has is ErrUnknownAnswer, before anything
// else is judged. Field failures return a *Invalid with Kind
// ErrInvalidAnswers, one entry per field that failed, in schema order.
//
// s must have come from Parse, so every field's type is in the table.
func Validate(s Schema, answers map[string]json.RawMessage, claims map[string]string) (Answers, error) {
	known := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		known[f.Key] = true
	}
	for k := range answers {
		if !known[k] {
			return nil, fmt.Errorf("%w: %d answers, and one names no field", ErrUnknownAnswer, len(answers))
		}
	}

	out := Answers{}
	var errs []FieldError
	for _, f := range s.Fields {
		t, ok := types[f.Type]
		if !ok {
			return nil, fmt.Errorf("forms: field type %q is not in the table; the schema did not come from Parse", f.Type)
		}
		if f.Locked {
			out[f.Key] = strings.TrimSpace(claims[f.FromClaim])
			continue
		}
		raw, present := answers[f.Key]
		present = present && !isNull(raw)
		v, empty, code := t.answer(f, Limit(f), raw, present)
		switch {
		case code != "":
			errs = append(errs, FieldError{Field: f.Key, Code: code})
			continue
		case empty && f.Required:
			errs = append(errs, FieldError{Field: f.Key, Code: CodeRequired})
			continue
		}
		if !t.display {
			out[f.Key] = v
		}
	}
	if len(errs) > 0 {
		return nil, &Invalid{Kind: ErrInvalidAnswers, Fields: errs}
	}
	return out, nil
}
