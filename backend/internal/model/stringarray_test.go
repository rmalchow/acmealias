package model

import "testing"

// A nil StringArray must encode as an empty array, never SQL NULL: every text[]
// column in the schema is `not null default '{}'`.
func TestStringArrayNilEncodesAsEmptyArray(t *testing.T) {
	v, err := StringArray(nil).Value()
	if err != nil {
		t.Fatalf("Value() on nil: %v", err)
	}
	if v == nil {
		t.Fatal("nil StringArray encoded as SQL NULL; would violate a NOT NULL column")
	}
	if s, ok := v.(string); !ok || s != "{}" {
		t.Fatalf("nil StringArray encoded as %#v, want \"{}\"", v)
	}
}
