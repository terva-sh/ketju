package domain

import (
	"errors"
	"testing"
)

// HandleCases is the shared table for the handle rule. It is exported to the
// db tests through handle_cases.go so the schema CHECK is tested against
// exactly the same inputs.
func TestValidateHandle(t *testing.T) {
	for _, c := range HandleCases {
		err := ValidateHandle(c.Handle)
		if (err == nil) != c.Valid {
			t.Errorf("ValidateHandle(%q) = %v, want valid=%v (%s)", c.Handle, err, c.Valid, c.Why)
		}
		if err != nil && !errors.Is(err, ErrInvalidHandle) {
			t.Errorf("ValidateHandle(%q) error does not wrap ErrInvalidHandle: %v", c.Handle, err)
		}
	}
}
