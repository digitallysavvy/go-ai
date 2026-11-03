package tool

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Port of sanitize-json-schema.test.ts 'keeps definitions next to a
// top-level $ref' (TS f4c5f1e959, sanitize-json-schema.ts 57-60, 149-170).
// The expected values are the TS sanitizeJsonSchema output (ai@7.0.162,
// node) for the raw-schema path. Compared as values: Go keeps the input's
// key order (jsonorder.Hinted), TS the sanitizer's insertion order.
func TestR85genSanitizeAnthropicKeepsDefinitionsNextToRef(t *testing.T) {
	for in, want := range map[string]string{
		`{"$ref":"#/definitions/Answer","definitions":{"Answer":{"type":"object","properties":{"answer":{"type":"string","minLength":1}},"required":["answer"]}},"$defs":{"Unused":{"type":"string"}}}`: `{"$ref":"#/definitions/Answer","definitions":{"Answer":{"type":"object","properties":{"answer":{"type":"string","description":"min length: 1."}},"additionalProperties":false,"required":["answer"]}},"$defs":{"Unused":{"type":"string"}}}`,
		`{"type":"object","properties":{"a":{"$ref":"#/$defs/X","$defs":{"X":{"type":"string","format":"regex"}},"description":"dropped"}}}`:                                                            `{"type":"object","properties":{"a":{"$ref":"#/$defs/X","$defs":{"X":{"type":"string","description":"format: regex."}}}},"additionalProperties":false}`,
		`{"$defs":{"N":{"type":"number","minimum":0}},"$ref":"#/$defs/N","title":"t"}`:                                                                                                                  `{"$ref":"#/$defs/N","$defs":{"N":{"type":"number","description":"minimum: 0."}}}`,
	} {
		got, err := json.Marshal(sanitizeAnthropicRawSchema([]byte(in)))
		if err != nil {
			t.Fatal(err)
		}
		var gotV, wantV interface{}
		if err := json.Unmarshal(got, &gotV); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(want), &wantV); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotV, wantV) {
			t.Errorf("sanitize(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}
