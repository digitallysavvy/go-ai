package schema

import (
	"strings"
	"testing"
)

// validateCase is a table-driven test case shared by the keyword tests
// below: a schema, a value, and whether Validate should accept it. When
// wantErr is true, errSubstr (if non-empty) must appear in the returned
// error, so failures name the right path/keyword.
type validateCase struct {
	name      string
	schema    map[string]interface{}
	value     interface{}
	wantErr   bool
	errSubstr string
}

func runValidateCases(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := NewJSONSchema(tc.schema).Validate(tc.value)
			if tc.wantErr && err == nil {
				t.Fatalf("Validate(%v) = nil, want error", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate(%v) = %v, want nil", tc.value, err)
			}
			if tc.wantErr && tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("Validate(%v) error = %q, want substring %q", tc.value, err.Error(), tc.errSubstr)
			}
		})
	}
}

func TestValidateConst(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{name: "matches", schema: map[string]interface{}{"const": "fixed"}, value: "fixed"},
		{name: "mismatch", schema: map[string]interface{}{"const": "fixed"}, value: "other", wantErr: true, errSubstr: "const"},
		{name: "numeric cross-type match", schema: map[string]interface{}{"const": 1}, value: float64(1)},
		{name: "numeric cross-type mismatch", schema: map[string]interface{}{"const": 1}, value: float64(2), wantErr: true},
	})
}

func TestValidateAnyOf(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{
		"anyOf": []interface{}{
			map[string]interface{}{"type": "string"},
			map[string]interface{}{"type": "integer"},
		},
	}
	runValidateCases(t, []validateCase{
		{name: "matches first", schema: sch, value: "hello"},
		{name: "matches second", schema: sch, value: 5},
		{name: "matches neither", schema: sch, value: true, wantErr: true, errSubstr: "anyOf"},
	})
}

func TestValidateOneOf(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{
		"oneOf": []interface{}{
			map[string]interface{}{"type": "number", "minimum": 0},
			map[string]interface{}{"type": "number", "maximum": 10},
		},
	}
	runValidateCases(t, []validateCase{
		{name: "matches exactly one (negative)", schema: sch, value: -5},
		{name: "matches exactly one (large positive)", schema: sch, value: 100},
		{name: "matches both branches", schema: sch, value: 5, wantErr: true, errSubstr: "oneOf"},
	})
}

func TestValidateNot(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{"not": map[string]interface{}{"type": "string"}}
	runValidateCases(t, []validateCase{
		{name: "value avoids schema", schema: sch, value: 5},
		{name: "value matches forbidden schema", schema: sch, value: "nope", wantErr: true, errSubstr: "not"},
	})
}

func TestValidateStringLength(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{"type": "string", "minLength": 2, "maxLength": 4}
	runValidateCases(t, []validateCase{
		{name: "within bounds", schema: sch, value: "abc"},
		{name: "too short", schema: sch, value: "a", wantErr: true, errSubstr: "minLength"},
		{name: "too long", schema: sch, value: "abcde", wantErr: true, errSubstr: "maxLength"},
		{name: "unicode code points not bytes", schema: map[string]interface{}{"minLength": 3, "maxLength": 3}, value: "日本語"},
	})
}

func TestValidatePattern(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{"type": "string", "pattern": "^[a-z]+$"}
	runValidateCases(t, []validateCase{
		{name: "matches", schema: sch, value: "abc"},
		{name: "does not match", schema: sch, value: "ABC123", wantErr: true, errSubstr: "pattern"},
		{
			name:      "invalid regexp surfaces as validation error, not panic",
			schema:    map[string]interface{}{"pattern": "("},
			value:     "x",
			wantErr:   true,
			errSubstr: "pattern",
		},
	})
}

func TestValidateFormat(t *testing.T) {
	t.Parallel()
	fmtSchema := func(format string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "format": format}
	}
	runValidateCases(t, []validateCase{
		{name: "date-time valid", schema: fmtSchema("date-time"), value: "2024-01-02T15:04:05Z"},
		{name: "date-time invalid", schema: fmtSchema("date-time"), value: "not-a-date", wantErr: true, errSubstr: "format"},
		{name: "date valid", schema: fmtSchema("date"), value: "2024-01-02"},
		{name: "date invalid calendar", schema: fmtSchema("date"), value: "2024-02-30", wantErr: true, errSubstr: "format"},
		{name: "time valid", schema: fmtSchema("time"), value: "15:04:05Z"},
		{name: "time invalid", schema: fmtSchema("time"), value: "25:99:99", wantErr: true, errSubstr: "format"},
		{name: "email valid", schema: fmtSchema("email"), value: "user@example.com"},
		{name: "email invalid", schema: fmtSchema("email"), value: "not-an-email", wantErr: true, errSubstr: "format"},
		{name: "uri valid", schema: fmtSchema("uri"), value: "https://example.com/path"},
		{name: "uri invalid", schema: fmtSchema("uri"), value: "not a uri", wantErr: true, errSubstr: "format"},
		{name: "uuid valid", schema: fmtSchema("uuid"), value: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "uuid invalid", schema: fmtSchema("uuid"), value: "not-a-uuid", wantErr: true, errSubstr: "format"},
		{name: "ipv4 valid", schema: fmtSchema("ipv4"), value: "192.168.1.1"},
		{name: "ipv4 invalid", schema: fmtSchema("ipv4"), value: "::1", wantErr: true, errSubstr: "format"},
		{name: "ipv6 valid", schema: fmtSchema("ipv6"), value: "2001:db8::1"},
		{name: "ipv6 invalid", schema: fmtSchema("ipv6"), value: "192.168.1.1", wantErr: true, errSubstr: "format"},
		{name: "unrecognized format is a non-validating annotation", schema: fmtSchema("made-up-format"), value: "anything"},
	})
}

func TestValidateNumericBounds(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{name: "minimum ok", schema: map[string]interface{}{"minimum": 0}, value: 0},
		{name: "minimum violated", schema: map[string]interface{}{"minimum": 0}, value: -1, wantErr: true, errSubstr: "minimum"},
		{name: "maximum ok", schema: map[string]interface{}{"maximum": 10}, value: 10},
		{name: "maximum violated", schema: map[string]interface{}{"maximum": 10}, value: 11, wantErr: true, errSubstr: "maximum"},
		{
			name:      "exclusiveMinimum draft-07 boolean form",
			schema:    map[string]interface{}{"minimum": 0, "exclusiveMinimum": true},
			value:     0,
			wantErr:   true,
			errSubstr: "exclusiveMinimum",
		},
		{name: "exclusiveMinimum draft-07 boolean form passes above bound", schema: map[string]interface{}{"minimum": 0, "exclusiveMinimum": true}, value: 1},
		{
			name:      "exclusiveMinimum 2020-12 numeric form",
			schema:    map[string]interface{}{"exclusiveMinimum": 0},
			value:     0,
			wantErr:   true,
			errSubstr: "exclusiveMinimum",
		},
		{
			name:      "exclusiveMaximum 2020-12 numeric form",
			schema:    map[string]interface{}{"exclusiveMaximum": 10},
			value:     10,
			wantErr:   true,
			errSubstr: "exclusiveMaximum",
		},
		{name: "multipleOf satisfied", schema: map[string]interface{}{"multipleOf": 5}, value: 15},
		{name: "multipleOf violated", schema: map[string]interface{}{"multipleOf": 5}, value: 12, wantErr: true, errSubstr: "multipleOf"},
		{name: "multipleOf float tolerance", schema: map[string]interface{}{"multipleOf": 0.1}, value: 0.3},
	})
}

func TestValidateArrayKeywords(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{name: "minItems ok", schema: map[string]interface{}{"minItems": 2}, value: []interface{}{1, 2}},
		{name: "minItems violated", schema: map[string]interface{}{"minItems": 2}, value: []interface{}{1}, wantErr: true, errSubstr: "minItems"},
		{name: "maxItems ok", schema: map[string]interface{}{"maxItems": 2}, value: []interface{}{1, 2}},
		{name: "maxItems violated", schema: map[string]interface{}{"maxItems": 2}, value: []interface{}{1, 2, 3}, wantErr: true, errSubstr: "maxItems"},
		{name: "uniqueItems ok", schema: map[string]interface{}{"uniqueItems": true}, value: []interface{}{1, 2, 3}},
		{name: "uniqueItems violated", schema: map[string]interface{}{"uniqueItems": true}, value: []interface{}{1, 2, 1}, wantErr: true, errSubstr: "uniqueItems"},
		{
			name: "prefixItems positional",
			schema: map[string]interface{}{
				"prefixItems": []interface{}{
					map[string]interface{}{"type": "string"},
					map[string]interface{}{"type": "integer"},
				},
			},
			value: []interface{}{"a", 1},
		},
		{
			name: "prefixItems positional mismatch",
			schema: map[string]interface{}{
				"prefixItems": []interface{}{
					map[string]interface{}{"type": "string"},
					map[string]interface{}{"type": "integer"},
				},
			},
			value:     []interface{}{1, "a"},
			wantErr:   true,
			errSubstr: "[0]",
		},
		{
			name: "prefixItems with items schema for the rest",
			schema: map[string]interface{}{
				"prefixItems": []interface{}{map[string]interface{}{"type": "string"}},
				"items":       map[string]interface{}{"type": "boolean"},
			},
			value: []interface{}{"a", true, false},
		},
		{
			name: "prefixItems with items schema rejects wrong-typed rest",
			schema: map[string]interface{}{
				"prefixItems": []interface{}{map[string]interface{}{"type": "string"}},
				"items":       map[string]interface{}{"type": "boolean"},
			},
			value:   []interface{}{"a", "not-bool"},
			wantErr: true,
		},
		{
			name: "draft-07 tuple-form items",
			schema: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"type": "string"},
					map[string]interface{}{"type": "integer"},
				},
			},
			value: []interface{}{"a", 1},
		},
		{
			name: "draft-07 tuple-form items with additionalItems=false forbids extras",
			schema: map[string]interface{}{
				"items":           []interface{}{map[string]interface{}{"type": "string"}},
				"additionalItems": false,
			},
			value:     []interface{}{"a", "b"},
			wantErr:   true,
			errSubstr: "items",
		},
		{
			name: "draft-07 tuple-form items with schema-valued additionalItems",
			schema: map[string]interface{}{
				"items":           []interface{}{map[string]interface{}{"type": "string"}},
				"additionalItems": map[string]interface{}{"type": "integer"},
			},
			value: []interface{}{"a", 1, 2},
		},
		{
			name: "single-schema items still applies to every element",
			schema: map[string]interface{}{
				"items": map[string]interface{}{"type": "integer"},
			},
			value:     []interface{}{1, "not-an-int"},
			wantErr:   true,
			errSubstr: "[1]",
		},
	})
}

func TestValidateObjectKeywords(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{name: "minProperties ok", schema: map[string]interface{}{"minProperties": 1}, value: map[string]interface{}{"a": 1}},
		{name: "minProperties violated", schema: map[string]interface{}{"minProperties": 1}, value: map[string]interface{}{}, wantErr: true, errSubstr: "minProperties"},
		{name: "maxProperties ok", schema: map[string]interface{}{"maxProperties": 1}, value: map[string]interface{}{"a": 1}},
		{name: "maxProperties violated", schema: map[string]interface{}{"maxProperties": 1}, value: map[string]interface{}{"a": 1, "b": 2}, wantErr: true, errSubstr: "maxProperties"},
		{
			name: "patternProperties applies schema to matching keys",
			schema: map[string]interface{}{
				"patternProperties": map[string]interface{}{
					"^x-": map[string]interface{}{"type": "string"},
				},
			},
			value: map[string]interface{}{"x-foo": "bar"},
		},
		{
			name: "patternProperties rejects mismatched value type",
			schema: map[string]interface{}{
				"patternProperties": map[string]interface{}{
					"^x-": map[string]interface{}{"type": "string"},
				},
			},
			value:   map[string]interface{}{"x-foo": 1},
			wantErr: true,
		},
		{
			name: "propertyNames constrains keys",
			schema: map[string]interface{}{
				"propertyNames": map[string]interface{}{"pattern": "^[a-z]+$"},
			},
			value: map[string]interface{}{"abc": 1},
		},
		{
			name: "propertyNames rejects a bad key",
			schema: map[string]interface{}{
				"propertyNames": map[string]interface{}{"pattern": "^[a-z]+$"},
			},
			value:   map[string]interface{}{"ABC": 1},
			wantErr: true,
		},
		{
			name: "additionalProperties=false still allows patternProperties-covered keys",
			schema: map[string]interface{}{
				"properties":           map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
				"patternProperties":    map[string]interface{}{"^x-": map[string]interface{}{"type": "string"}},
				"additionalProperties": false,
			},
			value: map[string]interface{}{"name": "a", "x-custom": "b"},
		},
		{
			name: "additionalProperties schema validates unknown properties",
			schema: map[string]interface{}{
				"properties":           map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
				"additionalProperties": map[string]interface{}{"type": "integer"},
			},
			value: map[string]interface{}{"name": "a", "extra": 5},
		},
		{
			name: "additionalProperties schema rejects wrong-typed unknown property",
			schema: map[string]interface{}{
				"properties":           map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
				"additionalProperties": map[string]interface{}{"type": "integer"},
			},
			value:   map[string]interface{}{"name": "a", "extra": "not-an-int"},
			wantErr: true,
		},
	})
}

func TestValidateRefAndDefs(t *testing.T) {
	t.Parallel()
	sch := map[string]interface{}{
		"type": "object",
		"$defs": map[string]interface{}{
			"Region": map[string]interface{}{
				"type": "string",
				"enum": []interface{}{"us-east-1", "us-west-2"},
			},
		},
		"properties": map[string]interface{}{
			"region": map[string]interface{}{"$ref": "#/$defs/Region"},
		},
	}
	runValidateCases(t, []validateCase{
		{name: "$ref resolves and validates", schema: sch, value: map[string]interface{}{"region": "us-east-1"}},
		{name: "$ref resolves and rejects", schema: sch, value: map[string]interface{}{"region": "eu-west-1"}, wantErr: true},
	})

	definitionsSchema := map[string]interface{}{
		"type": "object",
		"definitions": map[string]interface{}{
			"Region": map[string]interface{}{"type": "string"},
		},
		"properties": map[string]interface{}{
			"region": map[string]interface{}{"$ref": "#/definitions/Region"},
		},
	}
	runValidateCases(t, []validateCase{
		{name: "draft-07 definitions $ref resolves", schema: definitionsSchema, value: map[string]interface{}{"region": "anywhere"}},
		{name: "draft-07 definitions $ref rejects wrong type", schema: definitionsSchema, value: map[string]interface{}{"region": 5}, wantErr: true},
	})

	nestedRefArray := map[string]interface{}{
		"$defs": map[string]interface{}{
			"Item": map[string]interface{}{"type": "string"},
		},
		"type":  "array",
		"items": map[string]interface{}{"$ref": "#/$defs/Item"},
	}
	runValidateCases(t, []validateCase{
		{name: "$ref inside array items", schema: nestedRefArray, value: []interface{}{"a", "b"}},
		{name: "$ref inside array items rejects", schema: nestedRefArray, value: []interface{}{"a", 1}, wantErr: true},
	})

	t.Run("unresolvable local ref is reported", func(t *testing.T) {
		t.Parallel()
		sch := map[string]interface{}{"$ref": "#/$defs/Missing"}
		if err := NewJSONSchema(sch).Validate("x"); err == nil {
			t.Fatal("expected error for unresolvable $ref")
		}
	})

	t.Run("external ref is treated as unconstrained, not an error", func(t *testing.T) {
		t.Parallel()
		sch := map[string]interface{}{"$ref": "https://example.com/schema.json"}
		if err := NewJSONSchema(sch).Validate("anything"); err != nil {
			t.Fatalf("external $ref should not fail validation: %v", err)
		}
	})

	t.Run("circular $ref chain is reported, not infinite recursion", func(t *testing.T) {
		t.Parallel()
		sch := map[string]interface{}{
			"$defs": map[string]interface{}{
				"A": map[string]interface{}{"$ref": "#/$defs/B"},
				"B": map[string]interface{}{"$ref": "#/$defs/A"},
			},
			"$ref": "#/$defs/A",
		}
		if err := NewJSONSchema(sch).Validate("x"); err == nil {
			t.Fatal("expected error for circular $ref chain")
		}
	})
}

func TestApplyDefaultsWithRefAndTuples(t *testing.T) {
	t.Parallel()

	t.Run("default declared on a $ref target is applied", func(t *testing.T) {
		t.Parallel()
		s := NewSimpleJSONSchema(map[string]interface{}{
			"type": "object",
			"$defs": map[string]interface{}{
				"Region": map[string]interface{}{"type": "string", "default": "us-east-1"},
			},
			"properties": map[string]interface{}{
				"region": map[string]interface{}{"$ref": "#/$defs/Region"},
			},
		})
		got := ApplyDefaults(map[string]interface{}{}, s)
		obj, ok := got.(map[string]interface{})
		if !ok || obj["region"] != "us-east-1" {
			t.Fatalf("ApplyDefaults() = %+v, want region=us-east-1", got)
		}
	})

	t.Run("prefixItems positional defaults are applied", func(t *testing.T) {
		t.Parallel()
		s := NewSimpleJSONSchema(map[string]interface{}{
			"prefixItems": []interface{}{
				map[string]interface{}{"type": "object", "properties": map[string]interface{}{
					"enabled": map[string]interface{}{"type": "boolean", "default": true},
				}},
			},
		})
		got := ApplyDefaults([]interface{}{map[string]interface{}{}}, s)
		arr, ok := got.([]interface{})
		if !ok || len(arr) != 1 {
			t.Fatalf("ApplyDefaults() = %+v, want 1-element array", got)
		}
		elem, ok := arr[0].(map[string]interface{})
		if !ok || elem["enabled"] != true {
			t.Fatalf("ApplyDefaults()[0] = %+v, want enabled=true", arr[0])
		}
	})
}
