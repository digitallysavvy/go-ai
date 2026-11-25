package tool

import (
	"encoding/json"
	"testing"
)

// V061 item 19: the raw-schema path (json.RawMessage, []byte, and through
// them *schema.OrderedSchema and struct schemas) writes the keys in the TS
// sanitizer's insertion order (sanitize-json-schema.ts sanitizeSchema:
// $ref, definitions, $defs; or $schema, $id, title, description, default,
// const, enum, type, anyOf, allOf, definitions, $defs, properties,
// additionalProperties, required, items, format, then a constraint-only
// description), with properties and definitions in the input's order.
// Expected values: TS sanitizeJsonSchema at 30548ee7f9 run under node 24 on
// the same input text, JSON.stringify output compared byte for byte.
func TestR86genSanitizeAnthropicTSKeyOrder(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"$ref":"#/definitions/Answer","definitions":{"Answer":{"type":"object","properties":{"answer":{"type":"string","minLength":1}},"required":["answer"]}},"$defs":{"Unused":{"type":"string"}}}`,
			`{"$ref":"#/definitions/Answer","definitions":{"Answer":{"type":"object","properties":{"answer":{"type":"string","description":"min length: 1."}},"additionalProperties":false,"required":["answer"]}},"$defs":{"Unused":{"type":"string"}}}`},
		{`{"$defs":{"N":{"type":"number","minimum":0}},"$ref":"#/$defs/N","title":"t"}`,
			`{"$ref":"#/$defs/N","$defs":{"N":{"type":"number","description":"minimum: 0."}}}`},
		{`{"required":["b","a"],"additionalProperties":true,"properties":{"b":{"type":"string"},"a":{"maxLength":3,"type":"string","description":"A"}},"type":"object","description":"root","title":"T","$schema":"http://json-schema.org/draft-07/schema#"}`,
			`{"$schema":"http://json-schema.org/draft-07/schema#","title":"T","description":"root","type":"object","properties":{"b":{"type":"string"},"a":{"description":"A\nmax length: 3.","type":"string"}},"additionalProperties":false,"required":["b","a"]}`},
		{`{"type":"object","properties":{"z":{"type":"array","items":{"type":"integer","minimum":1,"maximum":9}},"y":{"oneOf":[{"type":"string"},{"type":"null"}]},"x":{"enum":["b","a"],"default":"a","type":"string"}},"required":["z"]}`,
			`{"type":"object","properties":{"z":{"type":"array","items":{"type":"integer","description":"minimum: 1; maximum: 9."}},"y":{"anyOf":[{"type":"string"},{"type":"null"}]},"x":{"default":"a","enum":["b","a"],"type":"string"}},"additionalProperties":false,"required":["z"]}`},
		{`{"format":"regex","type":"string","pattern":"^a","description":"d"}`,
			`{"description":"d\npattern: ^a; format: regex.","type":"string"}`},
		{`{"type":"string","format":"email","minLength":2}`,
			`{"type":"string","format":"email","description":"min length: 2."}`},
		{`{"allOf":[{"type":"object","properties":{"q":{"type":"boolean"}}}],"anyOf":[{"type":"number","exclusiveMinimum":0}],"type":"object"}`,
			`{"type":"object","anyOf":[{"type":"number","description":"exclusive minimum: 0."}],"allOf":[{"type":"object","properties":{"q":{"type":"boolean"}},"additionalProperties":false}],"additionalProperties":false}`},
		{`{"properties":{"2":{"type":"string"},"b":{"type":"string"},"1":{"type":"string"}},"type":"object"}`,
			`{"type":"object","properties":{"1":{"type":"string"},"2":{"type":"string"},"b":{"type":"string"}},"additionalProperties":false}`},
		{`{"type":"object","properties":{"p":{"type":"object","default":{"z":1,"a":2},"const":{"y":1,"b":2},"properties":{"k":{"type":"string"}}}},"$defs":{"B":{"uniqueItems":true,"type":"array","items":[{"type":"string"},{"type":"number","multipleOf":2}]}},"definitions":{"A":{"not":{"type":"null"},"type":"string"}}}`,
			`{"type":"object","definitions":{"A":{"type":"string","description":"not: {\"type\":\"null\"}."}},"$defs":{"B":{"type":"array","items":[{"type":"string"},{"type":"number","description":"multiple of: 2."}],"description":"unique items: true."}},"properties":{"p":{"default":{"z":1,"a":2},"const":{"y":1,"b":2},"type":"object","properties":{"k":{"type":"string"}},"additionalProperties":false}},"additionalProperties":false}`},
		{`{"$id":"x","minProperties":1,"maxProperties":4,"type":"object","properties":{}}`,
			`{"$id":"x","type":"object","properties":{},"additionalProperties":false,"description":"min properties: 1; max properties: 4."}`},
	} {
		got, err := json.Marshal(sanitizeAnthropicRawSchema([]byte(tc.in)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("sanitize(%s)\n got %s\nwant %s", tc.in, got, tc.want)
		}
		// The json.RawMessage entry point is the same path.
		got2, err := json.Marshal(SanitizeAnthropicSchema(json.RawMessage(tc.in)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got2) != tc.want {
			t.Errorf("SanitizeAnthropicSchema(RawMessage %s)\n got %s\nwant %s", tc.in, got2, tc.want)
		}
	}
}
