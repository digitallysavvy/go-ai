package openai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// tsProviderTool maps a TS provider tool (id/name/args) to the typed Go tool.
func tsProviderTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	raw, _ := json.Marshal(args)
	decode := func(v interface{}) bool { return len(args) == 0 || json.Unmarshal(raw, v) == nil }
	var t types.Tool
	switch id {
	case "openai.web_search":
		var c openaitool.WebSearchConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.WebSearch(c)
	case "openai.web_search_preview":
		var c openaitool.WebSearchPreviewConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.WebSearchPreview(c) //nolint:staticcheck // exercises the deprecated tool
	case "openai.file_search":
		var c openaitool.FileSearchConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.FileSearch(c)
	case "openai.code_interpreter":
		var c openaitool.CodeInterpreterConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.CodeInterpreter(c)
	case "openai.image_generation":
		var c openaitool.ImageGenerationConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.ImageGeneration(c)
	case "openai.mcp":
		var c openaitool.MCPConfig
		if !decode(&c) {
			return t, false
		}
		t = openaitool.MCP(c)
	case "openai.local_shell":
		t = responses.NewLocalShellTool()
	case "openai.apply_patch":
		t = responses.NewApplyPatchTool()
	case "openai.computer":
		t = types.Tool{ProviderExecuted: false}
	case "openai.programmatic_tool_calling":
		t = types.Tool{ProviderExecuted: true}
	case "openai.tool_search":
		var c struct {
			Execution   string                 `json:"execution"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}
		if !decode(&c) {
			return t, false
		}
		t = openaitool.ToolSearch(openaitool.ToolSearchArgs{Execution: c.Execution, Description: c.Description, Parameters: c.Parameters})
	case "openai.custom":
		var c openaitool.CustomTool
		if !decode(&c) {
			return t, false
		}
		t = c.ToTool(name)
	case "openai.computer_use":
		t = types.Tool{Type: "provider"} // an id the TS provider does not know (an unsupported tool)
	case "openai.shell":
		env, _ := args["environment"].(map[string]interface{})
		if len(args) == 0 {
			t = responses.NewShellTool()
			break
		}
		typ, _ := env["type"].(string)
		if len(args) != 1 || len(env) != 1 {
			return t, false
		}
		switch typ {
		case "containerAuto":
			t = responses.NewShellTool(responses.WithShellEnvironment(responses.ShellEnvironment{Type: "container_auto"}))
		default:
			return t, false
		}
	default:
		return t, false
	}
	t.Name, t.ProviderID = name, id
	return t, true
}

func TestTSCasesChat(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gpt-3.5-turbo"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).ChatModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should pass headers": tsProviderHeaders,
		"should stream text after Azure content filter chunks": "Go chat text parts carry no block id (TS id '0'); CanonStream ignores text ids by design",
	})
}

// tsProviderHeaders is the skip reason of the TS tests that build their own
// provider (organization, project, provider headers) and check the TS test
// user agent.
const tsProviderHeaders = "the TS test builds a provider with organization, project and headers and checks the TS test user agent (ai-sdk-openai/0.0.0-test); the harness builds a default provider"

func TestTSCasesResponses(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	cases := tsfixture.LoadCases(t, "testdata/tscases/responses.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "gpt-4o"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).ResponsesModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should parse compaction output item from real fixture":  "Go keeps the v0.5.2 flat compaction metadata keys next to the TS providerMetadata.openai object",
		"should stream compaction output item from real fixture": "Go keeps the v0.5.2 flat compaction metadata keys next to the TS providerMetadata.openai object",
		`should use "azure" as providerMetadata key when provider includes "azure"`: "the TS test builds an azure.responses model; the harness builds a default openai provider (the azure key is covered by the azure provider cases)",
	})
}
