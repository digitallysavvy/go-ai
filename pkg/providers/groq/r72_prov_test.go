package groq

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72provTSSchemas: TS zod schemas of groq.browser_search at e64ed82f27.
const r72provTSSchemas = `{"groq.browser_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{}}}}`

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

func TestR72ProvBrowserSearchSchemasEqualTS(t *testing.T) {
	r72provCheckTS(t, r72provTSSchemas, map[string]types.Tool{"groq.browser_search": BrowserSearchTool()})
}

// TS outputSchema z.object({}) is not strict: an output with keys passes
// ValidateUIMessages (Go had additionalProperties false and rejected it).
func TestR72ProvBrowserSearchOutputNotStrict(t *testing.T) {
	if err := r72provValidate(t, BrowserSearchTool().OutputSchema, `{"results":[{"url":"https://example.com"}]}`); err != nil {
		t.Errorf("output with keys rejected: %v", err)
	}
	if err := r72provValidate(t, BrowserSearchTool().OutputSchema, `"text"`); err == nil {
		t.Error("a non-object output passed")
	}
}
