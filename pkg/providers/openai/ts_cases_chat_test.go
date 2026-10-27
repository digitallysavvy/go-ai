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
	}, nil)
}

func TestTSCasesResponses(t *testing.T) {
	tsfixture.ProviderToolHook = tsProviderTool
	cases := tsfixture.LoadCases(t, "testdata/tscases/responses.json.gz")
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
		"should parse compaction output item from real fixture":             "Go keeps the v0.5.2 flat compaction metadata keys next to the TS providerMetadata.openai object",
		"should stream with encrypted content include reasoning-delta part": "TS fixture concatenates several responses into one stream; Go finishes at the first response.completed",
	})
}
