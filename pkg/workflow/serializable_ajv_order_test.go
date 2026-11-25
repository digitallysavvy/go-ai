package workflow

import (
	"encoding/json"
	"testing"
)

// Expected texts come from Ajv 8.11 (allErrors, strict false, validateFormats false)
// run through the TS formatAjvError, one case per row.
func TestValidateSerializableToolInputMatchesAjvErrorOrder(t *testing.T) {
	cases := []struct{ schema, data, want string }{
		{`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"number"}},"required":["z","a"]}`, `{"z":1,"a":"x"}`, `data/z must be string, data/a must be number`},
		{`{"anyOf":[{"type":"string"},{"type":"number","minimum":3}]}`, `true`, `data must be string, data must be number, data must match a schema in anyOf`},
		{`{"anyOf":[{"type":"string"},{"type":"number","minimum":3}]}`, `1`, `data must be string, data must be >= 3, data must match a schema in anyOf`},
		{`{"oneOf":[{"type":"string"},{"type":"number","minimum":3}]}`, `true`, `data must be string, data must be number, data must match exactly one schema in oneOf`},
		{`{"oneOf":[{"type":"number"},{"type":"number","minimum":3}]}`, `5`, `data must match exactly one schema in oneOf`},
		{`{"not":{"type":"string"}}`, `"s"`, `data must NOT be valid`},
		{`{"if":{"properties":{"k":{"const":1}},"required":["k"]},"then":{"required":["x"]},"else":{"required":["y"]}}`, `{"k":1}`, `data is missing required property "x", data must match "then" schema`},
		{`{"if":{"properties":{"k":{"const":1}},"required":["k"]},"then":{"required":["x"]},"else":{"required":["y"]}}`, `{"k":2}`, `data is missing required property "y", data must match "else" schema`},
		{`{"type":"string","enum":["a"],"minLength":3}`, `5`, `data must be one of "a", data must be string`},
		{`{"type":"integer","minimum":5,"enum":[7],"const":8}`, `2.5`, `data must be integer, data must equal 8, data must be one of 7, data must be >= 5`},
		{`{"type":"object","enum":[1]}`, `"s"`, `data must be object, data must be one of 1`},
		{`{"type":"number","maximum":3,"minimum":5,"multipleOf":2,"exclusiveMaximum":3,"exclusiveMinimum":5}`, `4.5`, `data must be <= 3, data must be >= 5, data must be < 3, data must be > 5, data must be multiple of 2`},
		{`{"type":"object","properties":{"a":{"$ref":"#/definitions/n"}},"definitions":{"n":{"type":"number","minimum":10}}}`, `{"a":1}`, `data/a must be >= 10`},
		{`{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":{"type":"number"}}`, `{"a":"s","b":"x","c":true}`, `data/b must be number, data/c must be number`},
		{`{"type":"object","patternProperties":{"^x":{"type":"number"}},"additionalProperties":false}`, `{"xa":"s","y":1}`, `data has unexpected property "y", data/xa must be number`},
		{`{"type":"array","items":[{"type":"string"},{"type":"number"}],"additionalItems":false}`, `[1,"s",3]`, `data must NOT have more than 2 items, data/0 must be string, data/1 must be number`},
		{`{"type":"array","items":{"type":"number"},"minItems":3,"maxItems":1}`, `["a"]`, `data must NOT have fewer than 3 items, data/0 must be number`},
		{`{"type":"object","minProperties":2,"required":["a"],"properties":{"a":{"type":"string"}}}`, `{"a":1}`, `data must NOT have fewer than 2 properties, data/a must be string`},
		{`{"type":["string","number"],"minLength":2}`, `true`, `data must be string,number`},
		{`{"type":"string","pattern":"^a"}`, `"b"`, `data must match pattern "^a"`},
		{`{"allOf":[{"type":"object","required":["a"]},{"type":"object","required":["b"]}]}`, `{}`, `data is missing required property "a", data is missing required property "b"`},
		{`{"type":"object","properties":{"o":{"$ref":"#/$defs/x","type":"string"}},"$defs":{"x":{"type":"number"}}}`, `{"o":true}`, `data/o must be string, data/o must be number`},
		{`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"array","prefixItems":[{"type":"string"},{"type":"number"}],"items":{"type":"boolean"}}`, `[1,"s",3]`, `data/0 must be string, data/1 must be number, data/2 must be boolean`},
		{`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"o":{"$ref":"#/$defs/x","type":"string"}},"$defs":{"x":{"type":"number"}}}`, `{"o":true}`, `data/o must be string, data/o must be number`},
		{`{"$schema":"https://json-schema.org/draft/2020-12/schema#","type":"string","enum":["a"],"minLength":3}`, `5`, `data must be one of "a", data must be string`},
	}
	for _, c := range cases {
		// Decode through the definition so the schema keeps its key order.
		var def SerializableToolDef
		if err := json.Unmarshal([]byte(`{"name":"w","inputSchema":`+c.schema+`}`), &def); err != nil {
			t.Fatal(err)
		}
		var data interface{}
		if err := json.Unmarshal([]byte(c.data), &data); err != nil {
			t.Fatal(err)
		}
		_, err := ValidateSerializableToolInput(def, data)
		want := "workflow: invalid input for w: " + c.want
		if err == nil || err.Error() != want {
			t.Errorf("schema %s data %s\n got  %v\n want %s", c.schema, c.data, err, want)
		}
	}
}
