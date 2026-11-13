package prompt

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS validateTypes reports zod's issues for an invalid caller tool output,
// also when the value carries a `__proto__` key (an ordinary own property
// of the caller's object, not a secureJsonParse rejection).
func TestR98provaToolOutputPrototypeKeyInvalidReportsZodIssues(t *testing.T) {
	value := []interface{}{map[string]interface{}{"type": "web_search_result", "__proto__": map[string]interface{}{}}}
	_, err := validateAnthropicToolOutput(value, zWebSearchOutput)
	var verr *providererrors.TypeValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %T %v, want a TypeValidationError", err, err)
	}
	if strings.Contains(err.Error(), "JSON parsing failed") || !strings.Contains(err.Error(), `"url"`) {
		t.Fatalf("err = %v, want the zod issues (missing url), not a JSON parse failure", err)
	}
}

// TS parses the file part options (shouldEnableCitations /
// getDocumentMetadata) before convertBytesDataToString decodes the payload,
// so invalid options fail first.
func TestR98provaTextDocumentOptionsBeforeDecode(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.FileContent{
		MediaType:       "text/plain",
		FileData:        types.FileData{Type: types.FileDataTypeData, DataString: "%%%"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"title": 5}},
	}}}}
	_, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("err = %T %v, want the provider options InvalidArgumentError", err, err)
	}
}

// TS secureJsonParse("null") is null, so reading .errorCode / .type of it
// throws a TypeError instead of falling back to 'unavailable' / 'unknown'.
func TestR98provaNullErrorValueTypeError(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		{"web_fetch", "Cannot read properties of null (reading 'errorCode')"},
		{"web_search", "Cannot read properties of null (reading 'errorCode')"},
		{"tool_search_tool_regex", "Cannot read properties of null (reading 'errorCode')"},
		{"code_execution", "Cannot read properties of null (reading 'type')"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			msgs := []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolResultContent{
					ToolCallID: "c1", ToolName: tc.tool,
					Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: "null"},
				}}},
			}
			_, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	// A string that parses to another non-object still falls back.
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "web_fetch",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: "5"},
		}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := out.Messages[1]["content"].([]map[string]interface{})
	inner, _ := content[0]["content"].(map[string]interface{})
	if inner["error_code"] != "unavailable" {
		t.Fatalf("error_code = %#v, want unavailable", inner["error_code"])
	}
}

// TS getAnthropicCaller: `caller.toolId` is truthy, not necessarily a
// string, and the orphan warning prints it with String().
func TestR98provaCallerTruthyToolID(t *testing.T) {
	caller := anthropicCaller(map[string]interface{}{"anthropic": map[string]interface{}{
		"caller": map[string]interface{}{"type": "code_execution_20250825", "toolId": float64(7)},
	}})
	if caller == nil || caller["tool_id"] != float64(7) {
		t.Fatalf("caller = %#v, want tool_id 7", caller)
	}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
			ToolCallID: "t1", ToolName: "f", Arguments: map[string]interface{}{},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{
				"caller": map[string]interface{}{"type": "code_execution_20250825", "toolId": float64(7)},
			}},
		}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "next"}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range out.Warnings {
		if w.Message == "Omitted caller metadata for tool t1 because source code execution tool 7 is missing from the conversation history." {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want the orphaned caller warning naming tool 7", out.Warnings)
	}
}
