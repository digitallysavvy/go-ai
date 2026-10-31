package workflow

import (
	"encoding/json"
	"testing"
)

// Messages and ordering of Ajv's uniqueItems and contains keywords, as run
// through the TS formatAjvError (allErrors, validateFormats false). The
// uniqueItems fast path (scalar item types) reports the later index first.
func TestValidateSerializableToolInputAjvUniqueItemsAndContains(t *testing.T) {
	d2020 := `"$schema":"https://json-schema.org/draft/2020-12/schema",`
	cases := []struct{ schema, data, want string }{
		{`{"type":"array","uniqueItems":true}`, `[1,2,1]`, `data must NOT have duplicate items (items ## 0 and 2 are identical)`},
		{`{"type":"array","items":{"type":"number"},"uniqueItems":true}`, `[1,2,1,2]`, `data must NOT have duplicate items (items ## 3 and 1 are identical)`},
		{`{"type":"array","uniqueItems":true}`, `[{"a":1,"b":2},{"b":2,"a":1}]`, `data must NOT have duplicate items (items ## 0 and 1 are identical)`},
		{`{"type":"array","uniqueItems":true,"items":{"type":"number"}}`, `[1,1,"x"]`, `data/2 must be number, data must NOT have duplicate items (items ## 1 and 0 are identical)`},
		{`{"type":"array","contains":{"type":"string"}}`, `[1]`, `data/0 must be string, data must contain at least 1 valid item(s)`},
		{`{"type":"array","contains":{"type":"string"}}`, `[]`, `data must contain at least 1 valid item(s)`},
		{`{` + d2020 + `"type":"array","contains":{"type":"string"},"minContains":2,"maxContains":3}`, `["a",1]`, `data/1 must be string, data must contain at least 2 and no more than 3 valid item(s)`},
		{`{` + d2020 + `"type":"array","contains":{"type":"string"},"maxContains":1}`, `["a","b"]`, `data must contain at least 1 and no more than 1 valid item(s)`},
		{`{` + d2020 + `"type":"array","contains":{"type":"string"},"minContains":2}`, `["a"]`, `data must contain at least 2 valid item(s)`},
	}
	for _, c := range cases {
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
			t.Errorf("schema %s data %s:\n got %v\nwant %s", c.schema, c.data, err, want)
		}
	}
	// Draft-07 Ajv has no minContains, so one match is enough; contains with
	// minContains 0 and no maxContains is ignored.
	for _, c := range []struct{ schema, data string }{
		{`{"type":"array","contains":{"type":"string"},"minContains":2}`, `["a"]`},
		{`{` + d2020 + `"type":"array","contains":{"type":"string"},"minContains":0}`, `[1]`},
		{`{"type":"array","uniqueItems":true}`, `[1,"1",true,null,{"a":1},[1]]`},
	} {
		var def SerializableToolDef
		if err := json.Unmarshal([]byte(`{"name":"w","inputSchema":`+c.schema+`}`), &def); err != nil {
			t.Fatal(err)
		}
		var data interface{}
		_ = json.Unmarshal([]byte(c.data), &data)
		if _, err := ValidateSerializableToolInput(def, data); err != nil {
			t.Errorf("schema %s data %s: unexpected %v", c.schema, c.data, err)
		}
	}
}
