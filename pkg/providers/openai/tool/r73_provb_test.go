package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r73provbTSSchemas: the TS input/output schemas of the openai tools at
// e64ed82f27 (zod 4.4.3 toJSONSchema, target draft-7, io "input", reused
// "inline"; produced by running packages/openai/src/tool/*.ts).
const r73provbTSSchemas = `{"openai.web_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"},"queries":{"type":"array","items":{"type":"string"}}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]},"sources":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"url"},"url":{"type":"string"}},"required":["type","url"]},{"type":"object","properties":{"type":{"type":"string","const":"api"},"name":{"type":"string"}},"required":["type","name"]}]}}}}},"openai.web_search_preview":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]}}}},"openai.code_interpreter":{"input":{"type":"object","properties":{"code":{"anyOf":[{"type":"string"},{"type":"null"}]},"containerId":{"type":"string"}},"required":["containerId"]},"output":{"type":"object","properties":{"outputs":{"anyOf":[{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"logs"},"logs":{"type":"string"}},"required":["type","logs"]},{"type":"object","properties":{"type":{"type":"string","const":"image"},"url":{"type":"string"}},"required":["type","url"]}]}},{"type":"null"}]}}}},"openai.file_search":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"queries":{"type":"array","items":{"type":"string"}},"results":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"attributes":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}},"fileId":{"type":"string"},"filename":{"type":"string"},"score":{"type":"number"},"text":{"type":"string"}},"required":["attributes","fileId","filename","score","text"]}},{"type":"null"}]}},"required":["queries","results"]}},"openai.image_generation":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"result":{"type":"string"}},"required":["result"]}},"openai.mcp":{"input":{"type":"object","properties":{}},"output":{"type":"object","properties":{"type":{"type":"string","const":"call"},"serverLabel":{"type":"string"},"name":{"type":"string"},"arguments":{"type":"string"},"output":{"anyOf":[{"type":"string"},{"type":"null"}]},"error":{"anyOf":[{"type":"string"},{"$ref":"#/definitions/__schema0"}]}},"required":["type","serverLabel","name","arguments"],"definitions":{"__schema0":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"},{"type":"array","items":{"$ref":"#/definitions/__schema0"}},{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}]}}}},"openai.tool_search":{"input":{"type":"object","properties":{"arguments":{},"call_id":{"anyOf":[{"type":"string"},{"type":"null"}]}}},"output":{"type":"object","properties":{"tools":{"type":"array","items":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}}}},"required":["tools"]}}}`

func r73provbNormalize(t *testing.T, v interface{}) interface{} {
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

func r73provbJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(r73provbNormalize(t, v))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func r73provbTools() map[string]types.Tool {
	maxResults := 4
	compression := 80
	external := false
	readOnly := true
	return map[string]types.Tool{
		"openai.web_search": WebSearch(WebSearchConfig{
			ExternalWebAccess: &external,
			Filters:           &WebSearchFilters{AllowedDomains: []string{"a.com"}, BlockedDomains: []string{"b.com"}},
			SearchContextSize: "high",
			UserLocation:      &WebSearchLocation{City: "Paris", Country: "FR"},
		}),
		"openai.web_search_preview": WebSearchPreview(WebSearchPreviewConfig{SearchContextSize: "low", UserLocation: &WebSearchLocation{Type: "approximate", Timezone: "Europe/Paris"}}),
		"openai.code_interpreter":   CodeInterpreter(CodeInterpreterConfig{Container: CodeInterpreterContainer{FileIDs: []string{"f1"}}}),
		"openai.file_search": FileSearch(FileSearchConfig{
			VectorStoreIDs: []string{"vs_1"},
			MaxNumResults:  &maxResults,
			Ranking:        &FileSearchRanking{Ranker: "auto"},
			Filters:        map[string]interface{}{"key": "k", "type": "eq", "value": "v"},
		}),
		"openai.image_generation": ImageGeneration(ImageGenerationConfig{OutputFormat: "webp", OutputCompression: &compression, InputImageMask: &ImageGenerationMask{FileID: "m1"}}),
		"openai.mcp": MCP(MCPConfig{
			ServerLabel:     "docs",
			ServerURL:       "https://mcp.example.com",
			Authorization:   "Bearer x",
			Headers:         map[string]string{"X-A": "1"},
			AllowedTools:    MCPAllowedTools{ReadOnly: &readOnly},
			RequireApproval: "always",
		}),
		"openai.tool_search": ToolSearch(ToolSearchArgs{
			Execution:   "client",
			Description: "Find tools",
			Parameters:  map[string]interface{}{"type": "object"},
		}),
	}
}

// TestR73ProvbProviderToolIdentity (CR-3): each openai/tool constructor is a
// provider tool with the TS id and the TS camelCase args, as TS
// createProvider*ToolFactory builds it, so the AI Gateway sends it as
// {type: "provider", id, name, args}. ProviderOptions keeps the typed config
// for the direct OpenAI request.
func TestR73ProvbProviderToolIdentity(t *testing.T) {
	want := map[string]string{
		"openai.web_search":         `{"externalWebAccess":false,"filters":{"allowedDomains":["a.com"],"blockedDomains":["b.com"]},"searchContextSize":"high","userLocation":{"city":"Paris","country":"FR","type":"approximate"}}`,
		"openai.web_search_preview": `{"searchContextSize":"low","userLocation":{"timezone":"Europe/Paris","type":"approximate"}}`,
		"openai.code_interpreter":   `{"container":{"fileIds":["f1"]}}`,
		"openai.file_search":        `{"filters":{"key":"k","type":"eq","value":"v"},"maxNumResults":4,"ranking":{"ranker":"auto"},"vectorStoreIds":["vs_1"]}`,
		"openai.image_generation":   `{"inputImageMask":{"fileId":"m1"},"outputCompression":80,"outputFormat":"webp"}`,
		"openai.mcp":                `{"allowedTools":{"readOnly":true},"authorization":"Bearer x","headers":{"X-A":"1"},"requireApproval":"always","serverLabel":"docs","serverUrl":"https://mcp.example.com"}`,
		"openai.tool_search":        `{"description":"Find tools","execution":"client","parameters":{"type":"object"}}`,
	}
	for id, tool := range r73provbTools() {
		if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != id || tool.Name != id {
			t.Errorf("%s: Type=%q ProviderID=%q Name=%q, want provider/%s/%s", id, tool.Type, tool.ProviderID, tool.Name, id, id)
		}
		if got := r73provbJSON(t, tool.ProviderArgs); got != want[id] {
			t.Errorf("%s ProviderArgs = %s, want %s", id, got, want[id])
		}
		if tool.ProviderOptions == nil {
			t.Errorf("%s: ProviderOptions dropped", id)
		}
	}
	// Defaults: TS webSearch() / codeInterpreter() / imageGeneration() get {} args.
	for _, tool := range []types.Tool{WebSearch(WebSearchConfig{}), CodeInterpreter(CodeInterpreterConfig{}), ImageGeneration(ImageGenerationConfig{}), ToolSearch(ToolSearchArgs{})} {
		if got := r73provbJSON(t, tool.ProviderArgs); got != "{}" {
			t.Errorf("%s default ProviderArgs = %s, want {}", tool.Name, got)
		}
	}
	// A string container id, as TS codeInterpreter({container: "cntr_1"}).
	if got := r73provbJSON(t, CodeInterpreter(CodeInterpreterConfig{Container: "cntr_1"}).ProviderArgs); got != `{"container":"cntr_1"}` {
		t.Errorf("string container ProviderArgs = %s", got)
	}
	// MCP with a custom SDK name keeps the TS id.
	named := MCP(MCPConfig{Name: "docsServer", ServerLabel: "docs", ServerURL: "u"})
	if named.Name != "docsServer" || named.ProviderID != "openai.mcp" || named.Type != types.ToolTypeProviderDefined {
		t.Errorf("named MCP = %q/%q/%q", named.Name, named.ProviderID, named.Type)
	}
	if _, ok := named.ProviderArgs["Name"]; ok {
		t.Errorf("MCP args carry the Go-only Name: %v", named.ProviderArgs)
	}
}

func TestR73ProvbCustomToolIdentity(t *testing.T) {
	syntax, definition := "lark", "start: \"a\""
	async := true
	tool := NewCustomTool(WithDescription("d"), WithFormat(CustomToolFormat{Type: "grammar", Syntax: &syntax, Definition: &definition}), WithAsync(async)).ToTool("grammarTool")
	if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != "openai.custom" || tool.Name != "grammarTool" || tool.ProviderExecuted {
		t.Fatalf("custom tool = Type %q ProviderID %q Name %q ProviderExecuted %v", tool.Type, tool.ProviderID, tool.Name, tool.ProviderExecuted)
	}
	if got, want := r73provbJSON(t, tool.ProviderArgs), `{"async":true,"description":"d","format":{"definition":"start: \"a\"","syntax":"lark","type":"grammar"}}`; got != want {
		t.Fatalf("custom ProviderArgs = %s, want %s", got, want)
	}
	if got := r73provbJSON(t, NewCustomTool().ToTool("plain").ProviderArgs); got != "{}" {
		t.Fatalf("plain custom ProviderArgs = %s, want {}", got)
	}
}

// TestR73ProvbSchemasEqualTS: Parameters and OutputSchema of each openai
// tool equal the TS zod schemas (provider tool calls are validated against
// Parameters; ValidateUIMessages checks outputs against OutputSchema).
func TestR73ProvbSchemasEqualTS(t *testing.T) {
	var want map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(r73provbTSSchemas), &want); err != nil {
		t.Fatal(err)
	}
	tools := r73provbTools()
	if len(tools) != len(want) {
		t.Fatalf("%d tools, %d TS schemas", len(tools), len(want))
	}
	for id, tool := range tools {
		for _, c := range []struct {
			field    string
			got, exp interface{}
		}{{"Parameters", tool.Parameters, want[id]["input"]}, {"OutputSchema", tool.OutputSchema, want[id]["output"]}} {
			if g, e := r73provbNormalize(t, c.got), r73provbNormalize(t, c.exp); !reflect.DeepEqual(g, e) {
				t.Errorf("%s %s:\n got %s\nwant %s", id, c.field, r73provbJSON(t, c.got), r73provbJSON(t, c.exp))
			}
		}
	}
	// Each constructor call gets its own schema maps.
	a, b := WebSearch(WebSearchConfig{}), WebSearch(WebSearchConfig{})
	a.OutputSchema.(map[string]interface{})["x"] = 1
	if _, shared := b.OutputSchema.(map[string]interface{})["x"]; shared {
		t.Fatal("OutputSchema map shared between tools")
	}
}

func r73provbValidate(t *testing.T, sch interface{}, value string) error {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		t.Fatal(err)
	}
	return schema.NewSimpleJSONSchema(sch.(map[string]interface{})).Validator().Validate(v)
}

// TestR73ProvbOutputSchemaValidation: outputs TS accepts pass, outputs TS
// rejects fail.
func TestR73ProvbOutputSchemaValidation(t *testing.T) {
	tools := r73provbTools()
	cases := []struct {
		id, value string
		ok        bool
	}{
		{"openai.web_search", `{"action":{"type":"search","query":"q"},"sources":[{"type":"url","url":"https://a"},{"type":"api","name":"oai-weather"}]}`, true},
		{"openai.web_search", `{"action":{"type":"openPage","url":null}}`, true},
		{"openai.web_search", `{"action":{"type":"click"}}`, false},
		{"openai.web_search_preview", `{"action":{"type":"findInPage","url":"u","pattern":null}}`, true},
		{"openai.code_interpreter", `{"outputs":null}`, true},
		{"openai.code_interpreter", `{"outputs":[{"type":"logs","logs":"1"},{"type":"image","url":"u"}]}`, true},
		{"openai.code_interpreter", `{"outputs":[{"type":"logs"}]}`, false},
		{"openai.file_search", `{"queries":["q"],"results":null}`, true},
		{"openai.file_search", `{"queries":["q"],"results":[{"attributes":{},"fileId":"f","filename":"a","score":0.3,"text":"t"}]}`, true},
		{"openai.file_search", `{"queries":["q"]}`, false},
		{"openai.image_generation", `{"result":"b64"}`, true},
		{"openai.image_generation", `{}`, false},
		{"openai.mcp", `{"type":"call","serverLabel":"s","name":"n","arguments":"{}","output":null,"error":{"code":1,"details":[true,null]}}`, true},
		{"openai.mcp", `{"type":"call","serverLabel":"s","name":"n"}`, false},
		{"openai.tool_search", `{"tools":[{"type":"function","name":"f"}]}`, true},
		{"openai.tool_search", `{"tools":"f"}`, false},
	}
	for _, c := range cases {
		err := r73provbValidate(t, tools[c.id].OutputSchema, c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s output %s: err = %v, want ok=%v", c.id, c.value, err, c.ok)
		}
	}
	// tool_search input: {arguments?, call_id?: string | null}, as the
	// provider maps a tool_search_call.
	in := tools["openai.tool_search"].Parameters
	if err := r73provbValidate(t, in, `{"arguments":{"query":"x"},"call_id":null}`); err != nil {
		t.Errorf("tool_search input rejected: %v", err)
	}
	if err := r73provbValidate(t, in, `{"call_id":5}`); err == nil {
		t.Error("tool_search input with numeric call_id accepted")
	}
}

// TestR73ProvbToolSearchParametersAreConfig: the client-mode search schema
// is tool config (TS args.parameters), not the call input schema.
func TestR73ProvbToolSearchParametersAreConfig(t *testing.T) {
	params := map[string]interface{}{"type": "object", "required": []interface{}{"query"}}
	tool := ToolSearch(ToolSearchArgs{Execution: "client", Parameters: params, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return nil, nil
	}})
	opts, ok := tool.ProviderOptions.(ToolSearchOptions)
	if !ok || !reflect.DeepEqual(opts.Parameters, params) {
		t.Fatalf("ProviderOptions = %#v, want Parameters %v", tool.ProviderOptions, params)
	}
	// A client tool_search call input (TS shape) passes the tool's
	// Parameters even though the search schema requires "query".
	if err := r73provbValidate(t, tool.Parameters, `{"arguments":{"paths":["a"]},"call_id":"c1"}`); err != nil {
		t.Fatalf("client tool_search call input rejected: %v", err)
	}
}
