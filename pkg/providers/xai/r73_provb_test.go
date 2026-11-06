package xai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r73provbTSSchemas: TS input/output schemas of the xai tools at
// e64ed82f27 (zod 4.4.3 toJSONSchema, draft-7, io "input", reused "inline").
const r73provbTSSchemas = `{"xai.web_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"},"queries":{"type":"array","items":{"type":"string"}}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]},"sources":{"type":"array","items":{"type":"object","properties":{"type":{"type":"string","const":"url"},"url":{"type":"string"}},"required":["type","url"]}}}}},"xai.x_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"query":{"type":"string"},"posts":{"type":"array","items":{"type":"object","properties":{"author":{"type":"string"},"text":{"type":"string"},"url":{"type":"string"},"likes":{"type":"number"}},"required":["author","text","url","likes"]}}},"required":["query","posts"]}},"xai.code_execution":{"input":{"type":"object","properties":{},"description":"no input parameters"},"output":{"type":"object","properties":{"output":{"type":"string","description":"the output of the code execution"},"error":{"description":"any error that occurred","type":"string"}},"required":["output"]}},"xai.view_image":{"input":{"type":"object","properties":{},"description":"no input parameters"},"output":{"type":"object","properties":{"description":{"type":"string","description":"description of the image"},"objects":{"description":"objects detected in the image","type":"array","items":{"type":"string"}}},"required":["description"]}},"xai.view_x_video":{"input":{"type":"object","properties":{},"description":"no input parameters"},"output":{"type":"object","properties":{"transcript":{"description":"transcript of the video","type":"string"},"description":{"type":"string","description":"description of the video content"},"duration":{"description":"duration in seconds","type":"number"}},"required":["description"]}},"xai.image_generation":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"result":{"type":"string"},"prompt":{"type":"string"}},"required":["result"]}},"xai.file_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"queries":{"type":"array","items":{"type":"string"}},"results":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"fileId":{"type":"string"},"filename":{"type":"string"},"score":{"type":"number","minimum":0,"maximum":1},"text":{"type":"string"}},"required":["fileId","filename","score","text"]}},{"type":"null"}]}},"required":["queries","results"]}},"xai.mcp":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"string"},"result":{}},"required":["name","arguments","result"]}}}`

func r73provbJSON(t *testing.T, v interface{}) string {
	t.Helper()
	// r72provNormalize drops descriptions and sorts required lists.
	b, err := json.Marshal(r72provNormalize(t, v))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func r73provbRawJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(generic)
	return string(b)
}

func r73provbTools() []types.Tool {
	yes, no := true, false
	from, to := "2026-01-01", "2026-02-01"
	return []types.Tool{
		WebSearch(WebSearchConfig{AllowedDomains: []string{"a.com"}, EnableImageSearch: &yes, EnableImageUnderstanding: &no}),
		XSearch(XSearchConfig{AllowedXHandles: []string{"x"}, ExcludedXHandles: []string{"y"}, FromDate: &from, ToDate: &to, EnableImageUnderstanding: &yes, EnableVideoUnderstanding: &no}),
		CodeExecution(),
		ViewImage(),
		ViewXVideo(),
		ImageGeneration(ImageGenerationConfig{Action: "edit"}),
		FileSearch(FileSearchConfig{VectorStoreIDs: []string{"vs_1"}, MaxNumResults: 5}),
		MCPServer(MCPServerConfig{ServerURL: "https://mcp.example.com", ServerLabel: "l", ServerDescription: "d", AllowedTools: []string{"t"}, Headers: map[string]string{"X-A": "1"}, Authorization: "Bearer s"}),
	}
}

// TestR73ProvbXAIProviderToolIdentity (CR-3): every xai tool constructor is
// a provider tool with the TS id and the TS camelCase args (TS sends
// authorization in the mcp args, also to the AI Gateway).
func TestR73ProvbXAIProviderToolIdentity(t *testing.T) {
	want := map[string]string{
		"xai.web_search":       `{"allowedDomains":["a.com"],"enableImageSearch":true,"enableImageUnderstanding":false}`,
		"xai.x_search":         `{"allowedXHandles":["x"],"enableImageUnderstanding":true,"enableVideoUnderstanding":false,"excludedXHandles":["y"],"fromDate":"2026-01-01","toDate":"2026-02-01"}`,
		"xai.code_execution":   `{}`,
		"xai.view_image":       `{}`,
		"xai.view_x_video":     `{}`,
		"xai.image_generation": `{"action":"edit"}`,
		"xai.file_search":      `{"maxNumResults":5,"vectorStoreIds":["vs_1"]}`,
		"xai.mcp":              `{"allowedTools":["t"],"authorization":"Bearer s","headers":{"X-A":"1"},"serverDescription":"d","serverLabel":"l","serverUrl":"https://mcp.example.com"}`,
	}
	tools := r73provbTools()
	if len(tools) != len(want) {
		t.Fatalf("%d tools, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		w, ok := want[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != tool.Name || !tool.ProviderExecuted {
			t.Errorf("%s: Type=%q ProviderID=%q ProviderExecuted=%v", tool.Name, tool.Type, tool.ProviderID, tool.ProviderExecuted)
		}
		if got := r73provbRawJSON(t, tool.ProviderArgs); got != w {
			t.Errorf("%s ProviderArgs = %s, want %s", tool.Name, got, w)
		}
	}
	// Defaults: TS webSearch() / xSearch() / imageGeneration() send {} and
	// fileSearch omits an unset maxNumResults.
	for _, tool := range []types.Tool{WebSearch(WebSearchConfig{}), XSearch(XSearchConfig{}), ImageGeneration(ImageGenerationConfig{})} {
		if got := r73provbRawJSON(t, tool.ProviderArgs); got != "{}" {
			t.Errorf("%s default ProviderArgs = %s", tool.Name, got)
		}
	}
	if got := r73provbRawJSON(t, FileSearch(FileSearchConfig{VectorStoreIDs: []string{}}).ProviderArgs); got != `{"vectorStoreIds":[]}` {
		t.Errorf("file search default ProviderArgs = %s", got)
	}
	if got := r73provbRawJSON(t, MCPServerSimple("u").ProviderArgs); got != `{"serverUrl":"u"}` {
		t.Errorf("MCPServerSimple ProviderArgs = %s", got)
	}
}

func TestR73ProvbXAISchemasEqualTS(t *testing.T) {
	var want map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(r73provbTSSchemas), &want); err != nil {
		t.Fatal(err)
	}
	for _, tool := range r73provbTools() {
		if got, exp := r73provbJSON(t, tool.Parameters), r73provbJSON(t, want[tool.Name]["input"]); got != exp {
			t.Errorf("%s Parameters:\n got %s\nwant %s", tool.Name, got, exp)
		}
		if got, exp := r73provbJSON(t, tool.OutputSchema), r73provbJSON(t, want[tool.Name]["output"]); got != exp {
			t.Errorf("%s OutputSchema:\n got %s\nwant %s", tool.Name, got, exp)
		}
	}
}

// TestR73ProvbXAIOutputValidation: outputs TS accepts pass, rejects fail.
func TestR73ProvbXAIOutputValidation(t *testing.T) {
	byName := map[string]types.Tool{}
	for _, tool := range r73provbTools() {
		byName[tool.Name] = tool
	}
	cases := []struct {
		id, value string
		ok        bool
	}{
		{"xai.web_search", `{"action":{"type":"search","queries":["q"]},"sources":[{"type":"url","url":"https://a"}]}`, true},
		{"xai.web_search", `{"sources":[{"type":"api","name":"n"}]}`, false},
		{"xai.x_search", `{"query":"q","posts":[{"author":"a","text":"t","url":"u","likes":3}]}`, true},
		{"xai.x_search", `{"query":"q"}`, false},
		{"xai.code_execution", `{"output":"1"}`, true},
		{"xai.code_execution", `{"error":"e"}`, false},
		{"xai.view_image", `{"description":"d","objects":["o"]}`, true},
		{"xai.view_image", `{"objects":["o"]}`, false},
		{"xai.view_x_video", `{"description":"d","duration":3.5}`, true},
		{"xai.view_x_video", `{"description":"d","duration":"3"}`, false},
		{"xai.image_generation", `{"result":"b64","prompt":"p"}`, true},
		{"xai.image_generation", `{"prompt":"p"}`, false},
	}
	for _, c := range cases {
		err := r72provValidate(t, byName[c.id].OutputSchema, c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s output %s: err = %v, want ok=%v", c.id, c.value, err, c.ok)
		}
	}
	// The input of a server-side call (its arguments) passes the TS
	// z.object({}) input schema, extra keys included.
	if err := r72provValidate(t, byName["xai.web_search"].Parameters, `{"query":"q"}`); err != nil {
		t.Errorf("web search call input rejected: %v", err)
	}
}

// r73provbXAIWire is the xAI Responses `tools` array these constructors
// produced before CR-3 (the test passes with the c689e39f constructors).
const r73provbXAIWire = `[{"allowed_domains":["a.com"],"enable_image_search":true,"enable_image_understanding":false,"type":"web_search"},{"allowed_x_handles":["x"],"enable_image_understanding":true,"enable_video_understanding":false,"excluded_x_handles":["y"],"from_date":"2026-01-01","to_date":"2026-02-01","type":"x_search"},{"type":"code_interpreter"},{"type":"view_image"},{"type":"view_x_video"},{"action":"edit","type":"image_generation"},{"max_num_results":5,"type":"file_search","vector_store_ids":["vs_1"]},{"allowed_tools":["t"],"authorization":"Bearer s","headers":{"X-A":"1"},"server_description":"d","server_label":"l","server_url":"https://mcp.example.com","type":"mcp"}]`

// TestR73ProvbXAIToolsWireUnchanged: the direct xAI request for every xai
// tool constructor is unchanged by CR-3, and a TS-shaped copy (args only)
// sends the same tools.
func TestR73ProvbXAIToolsWireUnchanged(t *testing.T) {
	tools := r73provbTools()
	defs, warnings := prepareXAIResponsesToolsWithWarnings(tools)
	if got := r73provbRawJSON(t, defs); got != r73provbXAIWire {
		t.Fatalf("tools =\n%s\nwant\n%s", got, r73provbXAIWire)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if err := validateXAIResponsesToolArgs(tools); err != nil {
		t.Fatal(err)
	}
	tsShaped := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(r73provbRawJSON(t, tool.ProviderArgs)), &args); err != nil {
			t.Fatal(err)
		}
		tsShaped = append(tsShaped, types.Tool{Name: tool.Name, Type: types.ToolTypeProviderDefined, ProviderID: tool.ProviderID, ProviderArgs: args})
	}
	if err := validateXAIResponsesToolArgs(tsShaped); err != nil {
		t.Fatal(err)
	}
	defs, _ = prepareXAIResponsesToolsWithWarnings(tsShaped)
	if got := r73provbRawJSON(t, defs); got != r73provbXAIWire {
		t.Fatalf("TS-shaped tools =\n%s\nwant\n%s", got, r73provbXAIWire)
	}
}
