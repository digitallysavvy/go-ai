package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

const r74provaClaude = "anthropic.claude-3-5-sonnet-20241022-v2:0"

func r74provaToolSpecNames(t *testing.T, body map[string]interface{}) []string {
	t.Helper()
	var names []string
	cfg, _ := body["toolConfig"].(map[string]interface{})
	list, _ := cfg["tools"].([]interface{})
	for _, tv := range list {
		spec := tv.(map[string]interface{})["toolSpec"].(map[string]interface{})
		names = append(names, spec["name"].(string))
	}
	return names
}

// The Go bash constructor is named after its id ("anthropic.bash_20250124"),
// which is not a valid Bedrock tool name, so its toolSpec is "bash". The
// model's "bash" call must come back under the tool's Name (or GenerateText
// cannot find the tool), and history calls of the tool must be sent as
// "bash", the tool the request declares.
func TestR74prova_BedrockDefaultNamedProviderToolRoundTrip(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"tu_2","name":"bash","input":{"command":"ls"}}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, r74provaClaude)
	bash := anthropictools.Bash20250124()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Tools: []types.Tool{bash},
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "run"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "tu_1", ToolName: bash.Name, Input: `{"command":"pwd"}`}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "tu_1", ToolName: bash.Name, Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "/"}}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r74provaToolSpecNames(t, body); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("toolSpec names = %v", got)
	}
	msgs := body["messages"].([]interface{})
	use := msgs[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["toolUse"].(map[string]interface{})
	if use["name"] != "bash" {
		t.Errorf("history toolUse name = %v, want bash", use["name"])
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != bash.Name {
		t.Fatalf("tool calls = %+v, want one call named %s", res.ToolCalls, bash.Name)
	}
	if tc, ok := res.Content[0].(types.ToolCallContent); !ok || tc.ToolName != bash.Name {
		t.Fatalf("content = %+v", res.Content)
	}
}

func TestR74prova_BedrockDefaultNamedProviderToolStreamName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockStart", `{"contentBlockIndex":0,"start":{"toolUse":{"toolUseId":"tu_1","name":"bash"}}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":"{\"command\":\"ls\"}"}}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"tool_use"}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, r74provaClaude)
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{anthropictools.Bash20250124()}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var names []string
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.ToolCall != nil && c.ToolCall.ToolName != "" {
			names = append(names, string(c.Type)+":"+c.ToolCall.ToolName)
		}
	}
	if len(names) == 0 {
		t.Fatal("no tool chunks")
	}
	for _, n := range names {
		if !strings.HasSuffix(n, ":anthropic.bash_20250124") {
			t.Fatalf("tool chunk names = %v, want anthropic.bash_20250124", names)
		}
	}
}

// TS sends toolSpec name tool.name: a provider tool renamed by the caller
// keeps its name and needs no mapping.
func TestR74prova_BedrockRenamedProviderToolKeepsName(t *testing.T) {
	shell := anthropictools.Bash20250124()
	shell.Name = "shell"
	result := prepareBedrockTools([]types.Tool{shell}, types.ToolChoice{}, false, r74provaClaude, "", nil, nil, false)
	if result.Err != nil || len(result.Warnings) != 0 || len(result.ToolNames) != 0 {
		t.Fatalf("err %v warnings %v names %v", result.Err, result.Warnings, result.ToolNames)
	}
	if name := result.ToolConfig.Tools[0]["toolSpec"].(map[string]interface{})["name"]; name != "shell" {
		t.Fatalf("toolSpec name = %v, want shell", name)
	}
}

// TS amazon-bedrock-prepare-tools runs prepareAnthropicTools over the
// provider tools: invalid args reject the call (validateTypes) and its
// warnings come before Bedrock's own "tool <id>" warnings.
func TestR74prova_BedrockRunsAnthropicToolPreparation(t *testing.T) {
	bad := anthropictools.Advisor20260301(anthropictools.Advisor20260301Args{MaxTokens: func() *int { v := 10; return &v }()})
	result := prepareBedrockTools([]types.Tool{bad}, types.ToolChoice{}, false, r74provaClaude, "", nil, nil, false)
	var tv *providererrors.TypeValidationError
	if !errors.As(result.Err, &tv) {
		t.Fatalf("err = %v, want a TypeValidationError", result.Err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request sent despite invalid tool args")
	}))
	defer server.Close()
	if _, err := newHTTPTestBedrockModel(t, server, r74provaClaude).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{bad}}); !errors.As(err, &tv) {
		t.Fatalf("DoGenerate err = %v, want a TypeValidationError", err)
	}

	unknown := types.Tool{Type: types.ToolTypeProviderDefined, Name: "search", ProviderID: "openai.web_search"}
	result = prepareBedrockTools([]types.Tool{anthropictools.Bash20250124(), unknown}, types.ToolChoice{Type: types.ToolChoiceRequired}, true, "us.anthropic.claude-opus-5-5", "", nil, nil, true)
	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"provider-defined tool openai.web_search", "toolChoice", "tool openai.web_search"}
	if len(features) != len(want) {
		t.Fatalf("warnings = %v, want %v", features, want)
	}
	for i := range want {
		if features[i] != want[i] {
			t.Fatalf("warnings = %v, want %v", features, want)
		}
	}
}

func r74provaTSShapedTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	if args == nil {
		args = map[string]interface{}{}
	}
	return types.Tool{Name: name, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}, true
}

// Replays amazon-bedrock-chat-language-model.test.ts with the provider tools
// exactly as TS passes them (custom names, id, args) and no name aliases.
func TestR74prova_TSCasesChatWithTSShapedTools(t *testing.T) {
	tsfixture.ProviderToolHook = r74provaTSShapedTool
	tsfixture.StreamBodyEncoder = encodeTSStream
	defer func() { tsfixture.ProviderToolHook, tsfixture.StreamBodyEncoder = nil, nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat.json.gz")
	tsfixture.EvalPathSnapshots(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "anthropic.claude-3-haiku-20240307-v1:0"
		}
		m, err := New(Config{APIKey: "k", Region: "us-east-1", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should expose the raw response headers":         "the TS mock server answers with text/event-stream headers; Go reads the AWS event stream",
		"should include response headers in rawResponse": "the TS mock server answers with text/event-stream headers; Go reads the AWS event stream",
		"should handle Anthropic provider-defined tools": "the TS test mocks the Anthropic prepareTools result (tool_choice, betas, schema)",
	})
}

// Replays amazon-bedrock-prepare-tools.test.ts with TS-shaped provider tools.
func TestR74prova_TSCasesPrepareToolsWithTSShapedTools(t *testing.T) {
	tsfixture.ProviderToolHook = r74provaTSShapedTool
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		hasChoice := false
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice, hasChoice = tsfixture.BuildToolChoice(tc), true
		}
		modelID, _ := in["modelId"].(string)
		family, _ := in["modelFamily"].(string)
		var budget *int
		if v, ok := in["reasoningBudgetTokens"].(float64); ok {
			n := int(v)
			budget = &n
		}
		var disable *bool
		if v, ok := in["disableParallelToolUse"].(bool); ok {
			disable = &v
		}
		out := prepareBedrockTools(ts, choice, hasChoice, modelID, family, budget, disable, in["rejectsForcedToolUse"] == true)
		if out.Err != nil {
			return nil, nil, out.Err
		}
		toolConfig := map[string]interface{}{}
		if len(out.ToolConfig.Tools) > 0 {
			toolConfig["tools"] = out.ToolConfig.Tools
			if out.ToolConfig.ToolChoice != nil {
				toolConfig["toolChoice"] = out.ToolConfig.ToolChoice
			}
		}
		betas := out.Betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"toolConfig":      toolConfig,
			"additionalTools": out.AdditionalTools,
			"betas":           map[string]interface{}{"__set": betas},
			"toolWarnings":    tsfixture.WarningsToTS(out.Warnings),
		}, nil, nil
	}, nil)
}
