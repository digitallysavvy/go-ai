package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72provTSSchemas: TS zod schemas of anthropic.tool_search_regex_20251119 at e64ed82f27.
const r72provTSSchemas = `{"anthropic.tool_search_regex_20251119":{"input":{"type":"object","properties":{"pattern":{"type":"string"},"limit":{"type":"number"}},"required":["pattern"]},"output":{"type":"array","items":{"type":"object","properties":{"type":{"type":"string","const":"tool_reference"},"toolName":{"type":"string"}},"required":["type","toolName"]}}}}`

// r72provNormalize round-trips v through JSON, drops "description" (the
// provider tool descriptions are not part of validation) and sorts
// "required" lists so Go literals and zod output compare equal.
func r72provNormalize(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	var walk func(interface{})
	walk = func(n interface{}) {
		switch x := n.(type) {
		case map[string]interface{}:
			delete(x, "description")
			for k, child := range x {
				if k == "required" {
					if list, ok := child.([]interface{}); ok {
						sort.Slice(list, func(i, j int) bool { return list[i].(string) < list[j].(string) })
					}
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(out)
	return out
}

// r72provCheckTS compares each tool's Parameters and OutputSchema with the
// zod toJSONSchema output (draft-07, io "input", "$schema" removed) of the
// TS tool at e64ed82f27. Provider tool calls are validated against
// Parameters and ValidateUIMessages checks outputs against OutputSchema.
func r72provCheckTS(t *testing.T, tsJSON string, tools map[string]types.Tool) {
	t.Helper()
	var want map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(tsJSON), &want); err != nil {
		t.Fatal(err)
	}
	for id, tool := range tools {
		w := want[id]
		for _, c := range []struct {
			field    string
			got, exp interface{}
		}{{"Parameters", tool.Parameters, w["input"]}, {"OutputSchema", tool.OutputSchema, w["output"]}} {
			g, e := r72provNormalize(t, c.got), r72provNormalize(t, c.exp)
			if !reflect.DeepEqual(g, e) {
				gb, _ := json.Marshal(g)
				eb, _ := json.Marshal(e)
				t.Errorf("%s %s:\n got %s\nwant %s", id, c.field, gb, eb)
			}
		}
	}
}

// r72provValidate reports whether value (JSON text) passes the schema.
func r72provValidate(t *testing.T, sch interface{}, value string) error {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		t.Fatal(err)
	}
	return schema.NewSimpleJSONSchema(sch.(map[string]interface{})).Validator().Validate(v)
}

func TestR72ProvToolSearchRegexSchemasEqualTS(t *testing.T) {
	r72provCheckTS(t, r72provTSSchemas, map[string]types.Tool{"anthropic.tool_search_regex_20251119": ToolSearchRegex20251119()})
}

// The output schema is TS's array of tool references; a pattern longer than
// 200 characters still passes input validation (TS has no max, the API
// enforces it).
func TestR72ProvToolSearchRegexValidation(t *testing.T) {
	tool := ToolSearchRegex20251119()
	if err := r72provValidate(t, tool.OutputSchema, `[{"type":"tool_reference","toolName":"get_weather"}]`); err != nil {
		t.Errorf("tool references rejected: %v", err)
	}
	for _, bad := range []string{`[{"type":"tool","toolName":"x"}]`, `[{"type":"tool_reference"}]`, `{"type":"tool_reference","toolName":"x"}`} {
		if err := r72provValidate(t, tool.OutputSchema, bad); err == nil {
			t.Errorf("%s accepted, want invalid", bad)
		}
	}
	long := `{"pattern":"` + strings.Repeat("a", 250) + `","limit":2.5}`
	if err := r72provValidate(t, tool.Parameters, long); err != nil {
		t.Errorf("TS-valid input rejected: %v", err)
	}
}
