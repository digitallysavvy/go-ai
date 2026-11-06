package schema

import "testing"

// Typed nil and zero schema values are no schema: ParametersMap and
// ApplyDefaults do not call into them (gen CR r82 item 1).
func TestR82integNilSchemaValues(t *testing.T) {
	for name, s := range map[string]Schema{
		"typed nil SimpleJSONSchema":   (*SimpleJSONSchema)(nil),
		"zero SimpleJSONSchema":        &SimpleJSONSchema{},
		"typed nil SimpleStructSchema": (*SimpleStructSchema)(nil),
		"zero SimpleStructSchema":      &SimpleStructSchema{},
	} {
		t.Run(name, func(t *testing.T) {
			if m, ok := ParametersMap(s); ok || m != nil {
				t.Fatalf("ParametersMap = %v, %v", m, ok)
			}
			if v := s.Validator(); v != nil {
				t.Fatalf("Validator() = %#v, want nil", v)
			}
			in := map[string]interface{}{"a": 1}
			if got := ApplyDefaults(in, s); got.(map[string]interface{})["a"] != 1 {
				t.Fatalf("ApplyDefaults = %v", got)
			}
		})
	}
}

type r82integPtrSchema struct{ v Validator }

func (s *r82integPtrSchema) Validator() Validator { return s.v }

type r82integPtrValidator struct{ m map[string]interface{} }

func (v *r82integPtrValidator) Validate(interface{}) error         { return nil }
func (v *r82integPtrValidator) JSONSchema() map[string]interface{} { return v.m }

// A caller's own Schema / Validator types: a typed nil schema or a typed nil
// validator is no schema either.
func TestR82integNilCustomSchemaValues(t *testing.T) {
	for name, s := range map[string]Schema{
		"typed nil custom schema":            (*r82integPtrSchema)(nil),
		"custom schema, typed nil validator": &r82integPtrSchema{v: (*r82integPtrValidator)(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			if m, ok := ParametersMap(s); ok || m != nil {
				t.Fatalf("ParametersMap = %v, %v", m, ok)
			}
			in := map[string]interface{}{"a": 1}
			if got := ApplyDefaults(in, s); got.(map[string]interface{})["a"] != 1 {
				t.Fatalf("ApplyDefaults = %v", got)
			}
		})
	}
}
