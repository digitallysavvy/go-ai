package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from convert-to-openai-responses-input.test.ts (TS #23441): the shell
// tool result's maxOutputLength is echoed as shell_call_output.max_output_length.
func shellResultPart(value map[string]interface{}) types.ToolResultContent {
	return types.ToolResultContent{
		ToolCallID: "call-shell",
		ToolName:   "openai.shell",
		Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: value},
	}
}

func shellOutputValue(withMax bool) map[string]interface{} {
	v := map[string]interface{}{"output": []interface{}{map[string]interface{}{
		"stdout": "hi\n", "stderr": "",
		"outcome": map[string]interface{}{"type": "exit", "exitCode": 0.0},
	}}}
	if withMax {
		v["maxOutputLength"] = 10000.0
	}
	return v
}

func TestConvertShellOutput_MaxOutputLengthRoundTrip(t *testing.T) {
	for name, tc := range map[string]struct {
		withMax bool
		want    string
	}{
		"included": {true, `{"type":"shell_call_output","call_id":"call-shell","max_output_length":10000,"output":[{"stdout":"hi\n","stderr":"","outcome":{"type":"exit","exit_code":0}}]}`},
		"omitted":  {false, `{"type":"shell_call_output","call_id":"call-shell","output":[{"stdout":"hi\n","stderr":"","outcome":{"type":"exit","exit_code":0}}]}`},
	} {
		input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{
			Role:    types.RoleTool,
			Content: []types.ContentPart{shellResultPart(shellOutputValue(tc.withMax))},
		}}}, "system", ConvertOptions{HasShellTool: true, Store: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(input) != 1 {
			t.Fatalf("%s: input = %#v", name, input)
		}
		raw, _ := json.Marshal(input[0])
		if string(raw) != tc.want {
			t.Errorf("%s: got %s\nwant %s", name, raw, tc.want)
		}
	}
}

func TestConvertShellOutput_MaxOutputLengthAssistantHistoryStoreFalse(t *testing.T) {
	input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant,
		ToolCalls: []types.ToolCall{{
			ID: "call_shell", ToolName: "openai.shell", ProviderExecuted: true,
			Arguments:        map[string]interface{}{"action": map[string]interface{}{"commands": []interface{}{"printf hello"}, "timeoutMs": 1000.0, "maxOutputLength": 10000.0}},
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "shell_item"}},
		}},
		Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "call_shell", ToolName: "openai.shell",
				Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: shellOutputValue(true)}},
		},
	}}}, "system", ConvertOptions{HasShellTool: true, Store: false})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	s := string(raw)
	if !strings.Contains(s, `"max_output_length":10000,"output"`) || !strings.Contains(s, `"timeout_ms":1000`) {
		t.Fatalf("input = %s", s)
	}
}
