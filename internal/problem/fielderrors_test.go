package problem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/terva-sh/ketju/internal/forms"
)

const fieldErrorsPath = "testdata/field-errors.json"

// TestFieldErrorsGolden is AC3: a missing required answer is 422
// validation-failed carrying errors: [{field, code}], pinned by a golden
// body. The title is the catalogue's, whatever the error says.
func TestFieldErrorsGolden(t *testing.T) {
	s, err := forms.Parse([]byte(`{"title":"T","fields":[
		{"key":"who","type":"textarea","label":"Who invited you?","required":true},
		{"key":"rules","type":"checkbox","label":"Rules","required":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = forms.Validate(s, map[string]json.RawMessage{"rules": json.RawMessage(`"yes"`)}, nil)
	if err == nil {
		t.Fatal("the answers validated")
	}

	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, httptest.NewRequest(http.MethodPost, "/api/v1/apply", nil), discardLogger(),
		fmt.Errorf("apply: %w", err))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", rec.Code)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if title, _ := Title(CodeValidationFailed); p.Title != title || p.Type != CodeValidationFailed {
		t.Errorf("type %q title %q, want the catalogue's validation-failed", p.Type, p.Title)
	}

	var got bytes.Buffer
	if err := json.Indent(&got, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(fieldErrorsPath, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(fieldErrorsPath)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/problem -update): %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("the field-error body changed.\n got:\n%s\nwant:\n%s", got.Bytes(), want)
	}
}

// TestOnlyValidationFailedCarriesErrors: field errors wrapped in an error
// another code claims are not listed, so a forbidden or internal body never
// says what a form held.
func TestOnlyValidationFailedCarriesErrors(t *testing.T) {
	inv := &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: []forms.FieldError{{Field: "who", Code: forms.CodeRequired}}}
	for _, c := range []struct {
		err    error
		errors bool
	}{
		{inv, true},
		{&forms.Invalid{Kind: forms.ErrInvalidSchema, Fields: []forms.FieldError{{Field: "fields.0.key", Code: forms.CodeInvalidKey}}}, true},
		{Coded(CodeForbidden, inv), false},
		{Coded(CodeInternal, inv), false},
		{forms.ErrInvalidAnswers, false},
	} {
		rec := httptest.NewRecorder()
		WriteError(context.Background(), rec, httptest.NewRequest(http.MethodPost, "/", nil), discardLogger(), c.err)
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if got := len(p.Errors) > 0; got != c.errors {
			t.Errorf("%v (%s): errors listed %t, want %t: %s", c.err, p.Type, got, c.errors, rec.Body)
		}
		if !c.errors && bytes.Contains(rec.Body.Bytes(), []byte(`"errors"`)) {
			t.Errorf("%s body names errors: %s", p.Type, rec.Body)
		}
	}
}

// TestFormErrorsMapToTheirCode covers the forms entries in domain.go.
func TestFormErrorsMapToTheirCode(t *testing.T) {
	for _, c := range []struct {
		err  error
		want Code
	}{
		{forms.ErrInvalidAnswers, CodeValidationFailed},
		{forms.ErrInvalidSchema, CodeValidationFailed},
		{forms.ErrUnknownAnswer, CodeBadRequest},
		{&forms.Invalid{Kind: forms.ErrInvalidSchema}, CodeValidationFailed},
	} {
		if got := FromError(fmt.Errorf("x: %w", c.err)); got != c.want {
			t.Errorf("%v: %s, want %s", c.err, got, c.want)
		}
	}
	if errors.Is(forms.ErrUnknownAnswer, forms.ErrInvalidAnswers) {
		t.Error("ErrUnknownAnswer is ErrInvalidAnswers, which maps first")
	}
}
