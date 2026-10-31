package responses

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r73provbTSSchemas: TS input/output schemas of the openai local_shell,
// shell, apply_patch, computer and programmatic_tool_calling tools at
// e64ed82f27 (zod 4.4.3 toJSONSchema, draft-7, io "input", reused "inline").
const r73provbTSSchemas = `{"openai.local_shell":{"input":{"type":"object","properties":{"action":{"type":"object","properties":{"type":{"type":"string","const":"exec"},"command":{"type":"array","items":{"type":"string"}},"timeoutMs":{"type":"number"},"user":{"type":"string"},"workingDirectory":{"type":"string"},"env":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"string"}}},"required":["type","command"]}},"required":["action"]},"output":{"type":"object","properties":{"output":{"type":"string"}},"required":["output"]}},"openai.shell":{"input":{"type":"object","properties":{"action":{"type":"object","properties":{"commands":{"type":"array","items":{"type":"string"}},"timeoutMs":{"type":"number"},"maxOutputLength":{"type":"number"}},"required":["commands"]}},"required":["action"]},"output":{"type":"object","properties":{"maxOutputLength":{"type":"number"},"output":{"type":"array","items":{"type":"object","properties":{"stdout":{"type":"string"},"stderr":{"type":"string"},"outcome":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"timeout"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"exit"},"exitCode":{"type":"number"}},"required":["type","exitCode"]}]}},"required":["stdout","stderr","outcome"]}}},"required":["output"]}},"openai.apply_patch":{"input":{"type":"object","properties":{"callId":{"type":"string"},"operation":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"create_file"},"path":{"type":"string"},"diff":{"type":"string"}},"required":["type","path","diff"]},{"type":"object","properties":{"type":{"type":"string","const":"delete_file"},"path":{"type":"string"}},"required":["type","path"]},{"type":"object","properties":{"type":{"type":"string","const":"update_file"},"path":{"type":"string"},"diff":{"type":"string"}},"required":["type","path","diff"]}]}},"required":["callId","operation"]},"output":{"type":"object","properties":{"status":{"type":"string","enum":["completed","failed"]},"output":{"type":"string"}},"required":["status"]}},"openai.computer":{"input":{"type":"object","properties":{"actions":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"click"},"button":{"type":"string","enum":["left","right","wheel","back","forward"]},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","button","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"double_click"},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"drag"},"path":{"type":"array","items":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","path"]},{"type":"object","properties":{"type":{"type":"string","const":"keypress"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","keys"]},{"type":"object","properties":{"type":{"type":"string","const":"move"},"x":{"type":"number"},"y":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y"]},{"type":"object","properties":{"type":{"type":"string","const":"screenshot"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"scroll"},"x":{"type":"number"},"y":{"type":"number"},"scrollX":{"type":"number"},"scrollY":{"type":"number"},"keys":{"type":"array","items":{"type":"string"}}},"required":["type","x","y","scrollX","scrollY"]},{"type":"object","properties":{"type":{"type":"string","const":"type"},"text":{"type":"string"}},"required":["type","text"]},{"type":"object","properties":{"type":{"type":"string","const":"wait"}},"required":["type"]}]}},"pendingSafetyChecks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"code":{"type":"string"},"message":{"type":"string"}},"required":["id"]}},"status":{"type":"string","enum":["in_progress","completed","incomplete"]}},"required":["actions","pendingSafetyChecks","status"]},"output":{"type":"object","properties":{"output":{"anyOf":[{"type":"object","properties":{"type":{"type":"string","const":"computer_screenshot"},"imageUrl":{"type":"string"},"fileId":{"type":"string"},"detail":{"type":"string","enum":["auto","low","high","original"]}},"required":["type","imageUrl"]},{"type":"object","properties":{"type":{"type":"string","const":"computer_screenshot"},"fileId":{"type":"string"},"imageUrl":{"type":"string"},"detail":{"type":"string","enum":["auto","low","high","original"]}},"required":["type","fileId"]}]},"acknowledgedSafetyChecks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"code":{"type":"string"},"message":{"type":"string"}},"required":["id"]}}},"required":["output"]}},"openai.programmatic_tool_calling":{"input":{"type":"object","properties":{"code":{"type":"string"},"fingerprint":{"type":"string"}},"required":["code","fingerprint"]},"output":{"type":"object","properties":{"result":{"type":"string"},"status":{"type":"string","enum":["completed","incomplete"]}},"required":["result","status"]}}}`

func r73provbJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	var walk func(interface{})
	walk = func(n interface{}) {
		switch x := n.(type) {
		case map[string]interface{}:
			for k, child := range x {
				if list, ok := child.([]interface{}); ok && k == "required" {
					sort.Slice(list, func(i, j int) bool { return list[i].(string) < list[j].(string) })
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(generic)
	b, err = json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func r73provbClientTools() map[string]types.Tool {
	return map[string]types.Tool{
		"openai.local_shell":               NewLocalShellTool(),
		"openai.shell":                     NewShellTool(),
		"openai.apply_patch":               NewApplyPatchTool(),
		"openai.computer":                  NewComputerTool(),
		"openai.programmatic_tool_calling": NewProgrammaticToolCallingTool(),
	}
}

// TestR73ProvbResponsesToolIdentity (CR-3): the openai/responses tool
// constructors are provider tools with the TS id and TS args.
func TestR73ProvbResponsesToolIdentity(t *testing.T) {
	for id, tool := range r73provbClientTools() {
		if tool.Type != types.ToolTypeProviderDefined || tool.ProviderID != id || tool.Name != id {
			t.Errorf("%s: Type=%q ProviderID=%q Name=%q", id, tool.Type, tool.ProviderID, tool.Name)
		}
		if got := r73provbJSON(t, tool.ProviderArgs); got != "{}" {
			t.Errorf("%s ProviderArgs = %s, want {}", id, got)
		}
		// Only programmatic tool calling is provider-executed (TS
		// createProviderExecutedToolFactory); the others are client tools.
		if tool.ProviderExecuted != (id == "openai.programmatic_tool_calling") {
			t.Errorf("%s ProviderExecuted = %v", id, tool.ProviderExecuted)
		}
	}
}

// TestR73ProvbShellEnvironmentArgs: NewShellTool's ProviderArgs are the TS
// openai.shell args (camelCase, TS environment type names) and map back to
// the same wire environment.
func TestR73ProvbShellEnvironmentArgs(t *testing.T) {
	memory := MemoryLimit16G
	skillRef, inline := "skill_reference", "inline"
	skillID, version, name, desc, path := "sk_1", "2", "n", "d", "/s"
	containerID := "cntr_9"
	cases := []struct {
		env  ShellEnvironment
		args string
	}{
		{ShellEnvironment{Type: "container_auto", FileIDs: []string{"f"}, MemoryLimit: &memory,
			NetworkPolicy: &ShellNetworkPolicy{Type: "allowlist", AllowedDomains: []string{"a.com"}, DomainSecrets: []ShellDomainSecret{{Domain: "a.com", Name: "K", Value: "v"}}},
			Skills:        []ShellSkill{{Type: &skillRef, SkillID: &skillID, Version: &version}, {Type: &inline, Name: &name, Description: &desc, Source: &ShellSkillSource{Type: "base64", MediaType: "application/zip", Data: "UEs="}}}},
			`{"environment":{"fileIds":["f"],"memoryLimit":"16g","networkPolicy":{"allowedDomains":["a.com"],"domainSecrets":[{"domain":"a.com","name":"K","value":"v"}],"type":"allowlist"},"skills":[{"providerReference":{"openai":"sk_1"},"type":"skillReference","version":"2"},{"description":"d","name":"n","source":{"data":"UEs=","mediaType":"application/zip","type":"base64"},"type":"inline"}],"type":"containerAuto"}}`},
		{ShellEnvironment{Type: "container_reference", ContainerID: &containerID}, `{"environment":{"containerId":"cntr_9","type":"containerReference"}}`},
		{ShellEnvironment{Type: "local", Skills: []ShellSkill{{Name: &name, Description: &desc, Path: &path}}}, `{"environment":{"skills":[{"description":"d","name":"n","path":"/s"}],"type":"local"}}`},
	}
	for _, c := range cases {
		tool := NewShellTool(WithShellEnvironment(c.env))
		if got := r73provbJSON(t, tool.ProviderArgs); got != c.args {
			t.Errorf("%s env ProviderArgs =\n%s\nwant\n%s", c.env.Type, got, c.args)
		}
		// A TS-shaped shell tool (args only) sends the same wire tool.
		tsShaped := types.Tool{Name: "shell", Type: types.ToolTypeProviderDefined, ProviderID: "openai.shell", ProviderArgs: tool.ProviderArgs}
		if got, want := r73provbJSON(t, convertTool(tsShaped)), r73provbJSON(t, convertTool(tool)); got != want {
			t.Errorf("%s env: TS-shaped wire =\n%s\nwant\n%s", c.env.Type, got, want)
		}
	}
}

func TestR73ProvbResponsesSchemasEqualTS(t *testing.T) {
	var want map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(r73provbTSSchemas), &want); err != nil {
		t.Fatal(err)
	}
	for id, tool := range r73provbClientTools() {
		if got, exp := r73provbJSON(t, tool.Parameters), r73provbJSON(t, want[id]["input"]); got != exp {
			t.Errorf("%s Parameters:\n got %s\nwant %s", id, got, exp)
		}
		if got, exp := r73provbJSON(t, tool.OutputSchema), r73provbJSON(t, want[id]["output"]); got != exp {
			t.Errorf("%s OutputSchema:\n got %s\nwant %s", id, got, exp)
		}
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

// TestR73ProvbClientToolInputValidation: the inputs the provider maps from
// shell_call / local_shell_call / apply_patch_call / computer_call / program
// items pass the TS input schemas; malformed ones fail as in TS.
func TestR73ProvbClientToolInputValidation(t *testing.T) {
	tools := r73provbClientTools()
	cases := []struct {
		id, value string
		ok        bool
	}{
		{"openai.shell", `{"action":{"commands":["ls"],"timeoutMs":1000}}`, true},
		{"openai.shell", `{"action":{}}`, false},
		{"openai.local_shell", `{"action":{"type":"exec","command":["ls"],"timeoutMs":5}}`, true},
		{"openai.local_shell", `{"action":{"type":"exec","command":["ls"],"timeout_ms":5,"working_directory":"/"}}`, true},
		{"openai.local_shell", `{"action":{"type":"exec","command":null}}`, false},
		{"openai.apply_patch", `{"callId":"c","operation":{"type":"delete_file","path":"a"}}`, true},
		{"openai.apply_patch", `{"callId":"c","operation":{"type":"update_file","path":"a"}}`, false},
		{"openai.computer", `{"actions":[{"type":"click","button":"left","x":1,"y":2},{"type":"scroll","x":0,"y":0,"scrollX":1,"scrollY":2},{"type":"screenshot"}],"pendingSafetyChecks":[],"status":"completed"}`, true},
		{"openai.computer", `{"actions":[{"type":"click","x":1,"y":2}],"pendingSafetyChecks":[],"status":"completed"}`, false},
		{"openai.programmatic_tool_calling", `{"code":"1","fingerprint":"fp"}`, true},
		{"openai.programmatic_tool_calling", `{"code":"1"}`, false},
	}
	for _, c := range cases {
		err := r73provbValidate(t, tools[c.id].Parameters, c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s input %s: err = %v, want ok=%v", c.id, c.value, err, c.ok)
		}
	}
	outputs := []struct {
		id, value string
		ok        bool
	}{
		{"openai.shell", `{"output":[{"stdout":"","stderr":"","outcome":{"type":"exit","exitCode":0}}]}`, true},
		{"openai.shell", `{"output":[{"stdout":"","stderr":"","outcome":{"type":"exit"}}]}`, false},
		{"openai.local_shell", `{"output":"ok"}`, true},
		{"openai.apply_patch", `{"status":"failed","output":"x"}`, true},
		{"openai.apply_patch", `{"status":"done"}`, false},
		{"openai.computer", `{"output":{"type":"computer_screenshot","fileId":"f"}}`, true},
		{"openai.computer", `{"output":{"type":"computer_screenshot"}}`, false},
		{"openai.programmatic_tool_calling", `{"result":"r","status":"incomplete"}`, true},
	}
	for _, c := range outputs {
		err := r73provbValidate(t, tools[c.id].OutputSchema, c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s output %s: err = %v, want ok=%v", c.id, c.value, err, c.ok)
		}
	}
}

// TestR73ProvbTSShapedToolsWire: an openai provider tool given in TS shape
// (Type "provider", ProviderID, camelCase ProviderArgs; e.g. restored from
// a workflow, where ProviderOptions is no longer the typed config) sends
// the same Responses tool as the typed constructor. v0.5 dropped the args.
func TestR73ProvbTSShapedToolsWire(t *testing.T) {
	maxResults := 2
	readOnly := false
	syntax, definition := "regex", "a+"
	typed := []types.Tool{
		openaitool.WebSearch(openaitool.WebSearchConfig{SearchContextSize: "low", UserLocation: &openaitool.WebSearchLocation{City: "Oslo"}, Filters: &openaitool.WebSearchFilters{BlockedDomains: []string{"b.com"}}}),
		openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{SearchContextSize: "medium"}), //nolint:staticcheck // the deprecated tool is still sent
		openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: openaitool.CodeInterpreterContainer{FileIDs: []string{"f"}}}),
		openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: "cntr_1"}),
		openaitool.FileSearch(openaitool.FileSearchConfig{VectorStoreIDs: []string{"vs"}, MaxNumResults: &maxResults, Ranking: &openaitool.FileSearchRanking{Ranker: "auto"}}),
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{Quality: "high", Size: "1024x1024"}),
		openaitool.MCP(openaitool.MCPConfig{ServerLabel: "s", ServerURL: "u", AllowedTools: openaitool.MCPAllowedTools{ReadOnly: &readOnly, ToolNames: []string{"a"}}, RequireApproval: openaitool.MCPRequireApproval{Never: &openaitool.MCPApprovalFilter{ToolNames: []string{"a"}}}}),
		openaitool.MCP(openaitool.MCPConfig{ServerLabel: "s2", ConnectorID: "c", AllowedTools: []string{"x"}, RequireApproval: "always"}),
		openaitool.ToolSearch(openaitool.ToolSearchArgs{Execution: "server", Description: "d", Parameters: map[string]interface{}{"type": "object"}}),
		openaitool.NewCustomTool(openaitool.WithFormat(openaitool.CustomToolFormat{Type: "grammar", Syntax: &syntax, Definition: &definition})).ToTool("re"),
	}
	for _, tool := range typed {
		// Through JSON, as a workflow round trip does.
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(r73provbJSON(t, tool.ProviderArgs)), &args); err != nil {
			t.Fatal(err)
		}
		tsShaped := types.Tool{Name: tool.Name, Type: types.ToolTypeProviderDefined, ProviderID: tool.ProviderID, ProviderArgs: args,
			ProviderOptions: map[string]interface{}{"not": "typed"}}
		gotDefs, err := PrepareToolsWithError([]types.Tool{tsShaped})
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		wantDefs, err := PrepareToolsWithError([]types.Tool{tool})
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		if got, want := r73provbJSON(t, gotDefs), r73provbJSON(t, wantDefs); got != want {
			t.Errorf("%s TS-shaped wire =\n%s\nwant\n%s", tool.Name, got, want)
		}
	}
	// A TS-shaped MCP tool without serverUrl/connectorId fails as TS does.
	_, err := PrepareToolsWithError([]types.Tool{{Name: "openai.mcp", Type: types.ToolTypeProviderDefined, ProviderID: "openai.mcp", ProviderArgs: map[string]interface{}{"serverLabel": "s"}}})
	if err == nil {
		t.Fatal("TS-shaped MCP tool without serverUrl/connectorId accepted")
	}
	// A TS-shaped custom tool is allow-listed as a custom tool.
	custom := types.Tool{Name: "re", Type: types.ToolTypeProviderDefined, ProviderID: "openai.custom", ProviderArgs: map[string]interface{}{}}
	if res := resolveAllowedToolForTool(custom); !res.supported || res.entry.Type != "custom" || res.entry.Name != "re" {
		t.Fatalf("TS-shaped custom allowed tool = %#v", res)
	}
}

// TestR73ProvbAllowedToolsToolSearch: with Type "provider" a tool_search
// tool is not allow-listed (TS toAllowedToolResolution default case); v0.5
// sent it as a function entry named "openai.tool_search".
func TestR73ProvbAllowedToolsToolSearch(t *testing.T) {
	res := resolveAllowedToolForTool(openaitool.ToolSearch(openaitool.ToolSearchArgs{}))
	if res.supported || res.reason != "OpenAI does not support tool_search tools in tool_choice.allowed_tools" {
		t.Fatalf("tool_search resolution = %#v", res)
	}
}

// TestR73ProvbToolArgsValidated: TS prepareResponsesTools validates every
// provider tool's args (validateTypes) before the request, so invalid
// config fails locally with a type validation error. v0.5 sent it to the
// API.
func TestR73ProvbToolArgsValidated(t *testing.T) {
	bad := 200
	partial := 5
	syntax := "regex"
	memory := "2g"
	cases := map[string]types.Tool{
		"searchContextSize enum": openaitool.WebSearch(openaitool.WebSearchConfig{SearchContextSize: "huge"}),
		"preview enum":           openaitool.WebSearchPreview(openaitool.WebSearchPreviewConfig{SearchContextSize: "max"}), //nolint:staticcheck // the deprecated tool is still sent
		"outputCompression max":  openaitool.ImageGeneration(openaitool.ImageGenerationConfig{OutputCompression: &bad}),
		"partialImages max":      openaitool.ImageGeneration(openaitool.ImageGenerationConfig{PartialImages: &partial}),
		"size pattern":           openaitool.ImageGeneration(openaitool.ImageGenerationConfig{Size: "big"}),
		"quality enum":           openaitool.ImageGeneration(openaitool.ImageGenerationConfig{Quality: "ultra"}),
		"vectorStoreIds missing": openaitool.FileSearch(openaitool.FileSearchConfig{}),
		"grammar definition":     openaitool.NewCustomTool(openaitool.WithFormat(openaitool.CustomToolFormat{Type: "grammar", Syntax: &syntax})).ToTool("g"),
		"tool_search execution":  openaitool.ToolSearch(openaitool.ToolSearchArgs{Execution: "both"}),
		"shell memoryLimit":      NewShellTool(WithShellEnvironment(ShellEnvironment{Type: "container_auto", MemoryLimit: &memory})),
		"mcp serverLabel":        {Name: "openai.mcp", Type: types.ToolTypeProviderDefined, ProviderID: "openai.mcp", ProviderArgs: map[string]interface{}{"serverUrl": "u"}},
		"code_interpreter type":  {Name: "openai.code_interpreter", Type: types.ToolTypeProviderDefined, ProviderID: "openai.code_interpreter", ProviderArgs: map[string]interface{}{"container": 5}},
	}
	for name, tool := range cases {
		_, err := PrepareToolsWithError([]types.Tool{tool})
		var ve *providererrors.ValidationError
		if !errors.As(err, &ve) || !strings.HasPrefix(ve.Message, "Type validation failed for "+tool.Name+": ") {
			t.Errorf("%s: err = %v, want a type validation error for %s", name, err, tool.Name)
		}
	}
	// Valid edge values pass: TS bounds are inclusive, size may be any WxH.
	zero, hundred, three := 0, 100, 3
	for _, tool := range []types.Tool{
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{OutputCompression: &zero, PartialImages: &three, Size: "640x480", Quality: "max"}),
		openaitool.ImageGeneration(openaitool.ImageGenerationConfig{OutputCompression: &hundred, Size: "auto"}),
		openaitool.FileSearch(openaitool.FileSearchConfig{VectorStoreIDs: []string{}, Filters: map[string]interface{}{"type": "and", "filters": []interface{}{map[string]interface{}{"key": "a", "type": "in", "value": []interface{}{"x"}}}}}),
		openaitool.NewCustomTool(openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"})).ToTool("txt"),
		// Hand-built tools without TS args are not checked (v0.5 shape).
		{Name: "openai.web_search", ProviderOptions: openaitool.WebSearchConfig{SearchContextSize: "huge"}},
	} {
		if _, err := PrepareToolsWithError([]types.Tool{tool}); err != nil {
			t.Errorf("%s: unexpected error %v", tool.Name, err)
		}
	}
}
