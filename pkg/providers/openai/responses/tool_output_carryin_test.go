package responses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func toolPrompt(toolName string, out *types.ToolResultOutput) types.Prompt {
	return types.Prompt{Messages: []types.Message{{
		Role:    types.RoleTool,
		Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: toolName, Output: out}},
	}}}
}

// TS custom tool outputs default to "" for an unknown output type.
func TestCustomToolOutputUnknownTypeSendsEmptyString(t *testing.T) {
	input, _, err := ConvertPromptToInputWithOptions(
		toolPrompt("grammar_tool", &types.ToolResultOutput{Type: "mystery", Value: "x"}),
		"system", ConvertOptions{CustomToolNames: map[string]bool{"grammar_tool": true}})
	if err != nil {
		t.Fatal(err)
	}
	custom, ok := input[0].(CustomToolCallOutput)
	if !ok || custom.Output != "" {
		t.Fatalf("input[0] = %#v, want empty-string custom output", input[0])
	}
}

// TS resolveFullMediaType throws for inline bytes whose bare media type cannot
// be detected; the error must surface from tool-result content blocks too.
func TestToolResultDataFileMediaTypeResolveErrorSurfaces(t *testing.T) {
	for _, tool := range []string{"fn_tool", "grammar_tool"} {
		out := &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.FileContentBlock{MediaType: "image", FileData: types.FileData{
				Type: types.FileDataTypeData, DataString: "AAAA",
			}},
		}}
		_, _, err := ConvertPromptToInputWithOptions(toolPrompt(tool, out), "system",
			ConvertOptions{CustomToolNames: map[string]bool{"grammar_tool": true}})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) {
			t.Fatalf("%s: err = %v, want UnsupportedFunctionalityError", tool, err)
		}
	}
}

func TestXAIToolResultDataFileResolveErrorSurfaces(t *testing.T) {
	prompt := toolPrompt("t", &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AAAA"}},
	}})
	_, _, err := ConvertPromptToXAIInput(prompt)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestXAIToolResultUnknownTypeSendsEmptyString(t *testing.T) {
	input, _, err := ConvertPromptToXAIInput(toolPrompt("t", &types.ToolResultOutput{Type: "mystery", Value: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if out := input[0].(map[string]interface{})["output"]; out != "" {
		t.Fatalf("output = %#v, want empty string", out)
	}
}

// TS tool_search_output always carries `tools`, even when the search loaded
// nothing; the API rejects the item without it.
func TestToolSearchOutputEmptyToolsStillSent(t *testing.T) {
	out, convErr := convertToolSearchOutput(types.ToolResultContent{
		ToolCallID: "c1", ToolName: "openai.tool_search",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"tools": []interface{}{}}},
	}, "client")
	if convErr != nil {
		t.Fatal(convErr)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"tools":[]`) {
		t.Fatalf("item = %s, want tools:[]", b)
	}
}
