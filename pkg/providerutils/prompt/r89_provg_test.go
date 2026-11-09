package prompt

import (
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convert-to-google-messages.ts: provider-executed code_execution calls
// and results are codeExecutionInputSchema.parse / codeExecutionOutputSchema
// .parse (required strings, extra keys stripped); invalid values throw.
func TestR89provgGoogleCodeExecutionSchemas(t *testing.T) {
	convert := func(parts ...types.ContentPart) (GooglePrompt, error) {
		return ConvertToGoogleMessages([]types.Message{{Role: types.RoleAssistant, Content: parts}}, GoogleMessagesOptions{IncludeFunctionCallIDs: true})
	}
	call := func(input string) types.ContentPart {
		return types.ToolCallContent{ToolCallID: "c1", ToolName: "code_execution", Input: input, ProviderExecuted: true}
	}
	result := func(v interface{}) types.ContentPart {
		return types.ToolResultContent{ToolCallID: "c1", ToolName: "code_execution", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: v}}
	}
	got, err := convert(call(`{"language":"PYTHON","code":"print(1)","extra":1}`),
		result(map[string]interface{}{"outcome": "OUTCOME_OK", "output": "1", "x": 2}))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{
		{"executableCode": map[string]interface{}{"language": "PYTHON", "code": "print(1)"}},
		{"codeExecutionResult": map[string]interface{}{"outcome": "OUTCOME_OK", "output": "1"}},
	}
	if !reflect.DeepEqual(got.Contents[0]["parts"], want) {
		t.Fatalf("parts = %#v", got.Contents[0]["parts"])
	}
	for name, part := range map[string]types.ContentPart{
		"input missing code":   call(`{"language":"PYTHON"}`),
		"input not JSON":       call(`not json`),
		"output wrong type":    result(map[string]interface{}{"outcome": "OUTCOME_OK", "output": 3}),
		"output missing field": result(map[string]interface{}{"outcome": "OUTCOME_OK"}),
	} {
		_, err := convert(part)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if name != "input not JSON" && !providererrors.IsValidationError(err) {
			t.Fatalf("%s: error = %T %v", name, err, err)
		}
	}
}
