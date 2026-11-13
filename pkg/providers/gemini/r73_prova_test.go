package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func r73provaValidate(t *testing.T, tl types.Tool, output interface{}) error {
	t.Helper()
	m, ok := tl.OutputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("%s: no output schema", tl.ProviderID)
	}
	return schema.NewSimpleJSONSchema(m).Validator().Validate(output)
}

// For reviewers item 1: TS declares an outputSchema for every google tool
// (z.object({}) for all but code execution), and the tools record key names
// each tool. The constructors set both.
func TestR73prova_GoogleToolsNameAndOutputSchema(t *testing.T) {
	cases := []struct {
		tool types.Tool
		name string
	}{
		{GoogleSearchTool(), "google_search"},
		{EnterpriseWebSearchTool(), "enterprise_web_search"},
		{GoogleMapsTool(), "google_maps"},
		{UrlContextTool(), "url_context"},
		{FileSearchTool(map[string]interface{}{"fileSearchStoreNames": []interface{}{"s"}}), "file_search"},
		{CodeExecutionTool(), "code_execution"},
		{VertexRagStoreTool("corpus"), "vertex_rag_store"},
	}
	for _, c := range cases {
		if c.tool.Name != c.name || c.tool.ProviderID != "google."+c.name || c.tool.Type != types.ToolTypeProviderDefined {
			t.Errorf("%s: Name=%q ProviderID=%q Type=%q", c.name, c.tool.Name, c.tool.ProviderID, c.tool.Type)
		}
		if err := r73provaValidate(t, c.tool, map[string]interface{}{"outcome": "OUTCOME_OK", "output": "1", "extra": true}); err != nil {
			t.Errorf("%s rejected an object output: %v", c.name, err)
		}
		if err := r73provaValidate(t, c.tool, "text"); err == nil {
			t.Errorf("%s accepted a string output (zod z.object rejects it)", c.name)
		}
	}
	if err := r73provaValidate(t, CodeExecutionTool(), map[string]interface{}{"outcome": "OUTCOME_OK"}); err == nil {
		t.Error("code execution accepted an output without output (required in TS)")
	}
}

// The code execution result the provider produces passes the output schema,
// and is reported under the constructor's name.
func TestR73prova_CodeExecutionResultMatchesOutputSchema(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"executableCode":{"language":"PYTHON","code":"print(1)"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1\n"}}]},"finishReason":"STOP"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	tool := CodeExecutionTool()
	result := m.convertResponse(resp, newToolNameMapping([]types.Tool{tool}))
	found := false
	for _, part := range result.Content {
		tr, ok := part.(types.ToolResultContent)
		if !ok {
			continue
		}
		found = true
		if tr.ToolName != tool.Name {
			t.Errorf("tool name = %q", tr.ToolName)
		}
		raw, _ := json.Marshal(tr.Result)
		var output interface{}
		_ = json.Unmarshal(raw, &output)
		if err := r73provaValidate(t, tool, output); err != nil {
			t.Errorf("output %s: %v", raw, err)
		}
	}
	if !found {
		t.Fatal("no tool result")
	}
}

// TS google tools come from createProviderExecutedToolFactory with
// z.object({}) inputs (code execution: {language, code}).
func TestR73prova_GoogleToolsProviderExecutedWithInputSchema(t *testing.T) {
	for _, tl := range []types.Tool{GoogleSearchTool(), EnterpriseWebSearchTool(), GoogleMapsTool(), UrlContextTool(), FileSearchTool(), CodeExecutionTool(), VertexRagStoreTool("c")} {
		if !tl.ProviderExecuted {
			t.Errorf("%s: not provider-executed", tl.Name)
		}
		p, ok := tl.Parameters.(map[string]interface{})
		if !ok || p["type"] != "object" {
			t.Errorf("%s: Parameters = %v", tl.Name, tl.Parameters)
		}
	}
}
