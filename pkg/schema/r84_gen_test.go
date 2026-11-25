package schema

import "testing"

// A nil or zero OrderedSchema is no schema (r83 made JSONSchema and
// MarshalJSON nil-safe): Validate accepts any value and Keys is empty
// instead of panicking.
func TestR84genNilOrderedSchemaMethods(t *testing.T) {
	for _, s := range []*OrderedSchema{nil, {}} {
		if err := s.Validate(map[string]interface{}{"a": 1}); err != nil {
			t.Fatalf("Validate = %v", err)
		}
		if k := s.Keys(); k != nil {
			t.Fatalf("Keys = %v", k)
		}
		if err := s.Validator().Validate("x"); err != nil {
			t.Fatalf("Validator().Validate = %v", err)
		}
	}
}
