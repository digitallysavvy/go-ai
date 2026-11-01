package bedrock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// R75 item 3: forcing the Go bash constructor (Name "anthropic.bash_20250124",
// toolSpec "bash") sends additionalModelRequestFields.tool_choice.name "bash",
// the declared toolSpec; a TS-named tool is unchanged.
func TestR75prova_BedrockForcedToolChoiceOfIDNamedTool(t *testing.T) {
	bash := anthropictools.Bash20250124()
	out := prepareBedrockTools([]types.Tool{bash}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: bash.Name}, true, r74provaClaude, "", nil, nil, false)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	choice, _ := out.AdditionalTools["tool_choice"].(map[string]interface{})
	if choice["type"] != "tool" || choice["name"] != "bash" {
		t.Fatalf("tool_choice = %v", out.AdditionalTools)
	}
	spec := out.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})
	if spec["name"] != choice["name"] {
		t.Fatalf("toolSpec %v does not match tool_choice %v", spec["name"], choice["name"])
	}

	tsNamed := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124", Name: "shell"}
	out = prepareBedrockTools([]types.Tool{tsNamed}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "shell"}, true, r74provaClaude, "", nil, nil, false)
	choice, _ = out.AdditionalTools["tool_choice"].(map[string]interface{})
	if choice["name"] != "shell" {
		t.Fatalf("ts-named tool_choice = %v", out.AdditionalTools)
	}
}

func TestR75prova_BedrockForcedToolChoiceRequestBody(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"tu_1","name":"bash","input":{"command":"ls"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, r74provaClaude)
	bash := anthropictools.Bash20250124()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "run"},
		Tools:      []types.Tool{bash},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: bash.Name},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := body["additionalModelRequestFields"].(map[string]interface{})
	choice, _ := fields["tool_choice"].(map[string]interface{})
	if choice["name"] != "bash" {
		t.Fatalf("additionalModelRequestFields = %v", fields)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != bash.Name {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
}

// amazon-bedrock-prepare-tools.test.ts "should warn for unknown
// provider-defined tools on anthropic models" (TS #24073: the feature names
// the tool id), with the real Anthropic preparation, whose own
// "provider-defined tool" warning comes first (TS pushes the Anthropic
// warnings before the Bedrock toolSpec loop).
func TestR75prova_BedrockUnknownAnthropicToolWarnings(t *testing.T) {
	unknown := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.unknown_tool", Name: "unknown_tool", ProviderArgs: map[string]interface{}{}}
	out := prepareBedrockTools([]types.Tool{unknown}, types.ToolChoice{}, false, "anthropic.claude-sonnet-4-5-20250929-v1:0", "", nil, nil, false)
	if out.Err != nil || len(out.ToolConfig.Tools) != 0 {
		t.Fatalf("out = %+v", out)
	}
	if len(out.Warnings) != 2 || out.Warnings[0].Feature != "provider-defined tool anthropic.unknown_tool" || out.Warnings[1].Feature != "tool anthropic.unknown_tool" {
		t.Fatalf("warnings = %+v", out.Warnings)
	}
}
