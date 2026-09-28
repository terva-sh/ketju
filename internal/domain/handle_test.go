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

// A grandfathered row is one the schema stores and a new handle may not be,
// so it is never also Valid, and it is refused for the trailing '-' and not
// for something else.
func TestGrandfatheredHandlesEndInADash(t *testing.T) {
	for _, c := range HandleCases {
		if !c.Grandfathered {
			continue
		}
		if c.Valid {
			t.Errorf("%q is marked both Valid and Grandfathered (%s)", c.Handle, c.Why)
		}
		if err := ValidateHandle(c.Handle[:len(c.Handle)-1] + "x"); err != nil {
			t.Errorf("%q with its last '-' replaced is still refused, so the row tests more than the trailing '-': %v", c.Handle, err)
		}
	}
}
