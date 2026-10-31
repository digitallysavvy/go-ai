package xai

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r72provTSSchemas: TS zod schemas of xai.mcp, xai.file_search at e64ed82f27.
const r72provTSSchemas = `{"xai.mcp":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"},"result":{}},"required":["name","arguments","result"]}},"xai.file_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"queries":{"type":"array","items":{"type":"string"}},"results":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"fileId":{"type":"string"},"filename":{"type":"string"},"score":{"type":"number","minimum":0,"maximum":1},"text":{"type":"string"}},"required":["fileId","filename","score","text"]}},{"type":"null"}]}},"required":["queries","results"]}}}`

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

func TestR72ProvToolSchemasEqualTS(t *testing.T) {
	r72provCheckTS(t, r72provTSSchemas, map[string]types.Tool{
		"xai.mcp":         MCPServer(MCPServerConfig{ServerURL: "https://mcp.example.com", Authorization: "Bearer r72-secret"}),
		"xai.file_search": FileSearch(FileSearchConfig{VectorStoreIDs: []string{"vs_1"}, MaxNumResults: 3}),
	})
}

// No config value (the MCP authorization token, headers, vector store IDs)
// is in the input or output schema, so none can be merged into call input
// as a default or reach anything that serializes the schemas.
func TestR72ProvConfigNotInSchemas(t *testing.T) {
	mcp := MCPServer(MCPServerConfig{ServerURL: "https://r72-mcp.example.com", ServerLabel: "r72-label", AllowedTools: []string{"r72-tool"},
		Headers: map[string]string{"X-R72": "r72-header"}, Authorization: "Bearer r72-secret"})
	fs := FileSearch(FileSearchConfig{VectorStoreIDs: []string{"r72-vs"}, MaxNumResults: 7})
	for _, tool := range []types.Tool{mcp, fs} {
		b, _ := json.Marshal([]interface{}{tool.Parameters, tool.OutputSchema})
		for _, secret := range []string{"r72-secret", "r72-header", "r72-mcp", "r72-label", "r72-tool", "r72-vs", "default"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("%s schemas contain %q: %s", tool.Name, secret, b)
			}
		}
	}
}

// The output schemas validate like TS zod: file search results may be null,
// scores are 0..1 and the MCP output needs name, arguments and result.
func TestR72ProvOutputSchemas(t *testing.T) {
	fs := FileSearch(FileSearchConfig{}).OutputSchema
	mcp := MCPServer(MCPServerConfig{}).OutputSchema
	cases := []struct {
		sch   interface{}
		value string
		valid bool
	}{
		{fs, `{"queries":["q"],"results":null}`, true},
		{fs, `{"queries":[],"results":[{"fileId":"f","filename":"a.txt","score":0.5,"text":"t"}]}`, true},
		{fs, `{"queries":[],"results":[{"fileId":"f","filename":"a.txt","score":1.5,"text":"t"}]}`, false},
		{fs, `{"queries":[],"results":[{"fileId":"f","score":0.5,"text":"t"}]}`, false},
		{fs, `{"queries":["q"]}`, false},
		{mcp, `{"name":"search","arguments":"{}","result":{"any":"thing"}}`, true},
		{mcp, `{"name":"search","arguments":"{}"}`, false},
	}
	for _, c := range cases {
		err := r72provValidate(t, c.sch, c.value)
		if c.valid != (err == nil) {
			t.Errorf("%s: valid=%v, err=%v", c.value, c.valid, err)
		}
	}
}
