package responses

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func convertCall(t *testing.T, tool string, args map[string]interface{}) ([]interface{}, error) {
	t.Helper()
	input, _, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: tool, Arguments: args}}}},
	}, "system", ConvertOptions{HasLocalShellTool: true, HasShellTool: true, HasApplyPatchTool: true, HasComputerTool: true})
	return input, err
}

func convertResult(tool string, value interface{}) error {
	_, _, err := ConvertPromptToInputWithOptions(types.Prompt{
		Messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: tool, Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: value},
		}}}},
	}, "system", ConvertOptions{HasLocalShellTool: true, HasShellTool: true, HasApplyPatchTool: true, HasComputerTool: true})
	return err
}

func isTypeValidation(err error) bool {
	var tv *providererrors.TypeValidationError
	return errors.As(err, &tv) && strings.HasPrefix(err.Error(), "Type validation failed:")
}

// TS validates shell/local_shell/apply_patch/computer inputs and outputs with
// validateTypes and throws a TypeValidationError on a malformed payload.
func TestStrictToolPayloadsRejectMalformedR33(t *testing.T) {
	for name, tc := range map[string]struct {
		tool string
		args map[string]interface{}
	}{
		"shell commands not array": {"openai.shell", map[string]interface{}{"action": map[string]interface{}{"commands": "ls"}}},
		"shell missing action":     {"openai.shell", map[string]interface{}{}},
		"local shell missing type": {"openai.local_shell", map[string]interface{}{"action": map[string]interface{}{"command": []interface{}{"ls"}}}},
		"patch unknown op":         {"openai.apply_patch", map[string]interface{}{"callId": "x", "operation": map[string]interface{}{"type": "rename", "path": "a"}}},
		"computer missing checks":  {"openai.computer", map[string]interface{}{"status": "completed", "actions": []interface{}{}}},
		"computer bad status":      {"openai.computer", map[string]interface{}{"status": "done", "actions": []interface{}{}, "pendingSafetyChecks": []interface{}{}}},
		"computer click no button": {"openai.computer", map[string]interface{}{"status": "completed", "pendingSafetyChecks": []interface{}{}, "actions": []interface{}{map[string]interface{}{"type": "click", "x": 1, "y": 2}}}},
		"computer unknown action":  {"openai.computer", map[string]interface{}{"status": "completed", "pendingSafetyChecks": []interface{}{}, "actions": []interface{}{map[string]interface{}{"type": "zoom"}}}},
	} {
		if _, err := convertCall(t, tc.tool, tc.args); !isTypeValidation(err) {
			t.Errorf("%s: err = %v, want TypeValidationError", name, err)
		}
	}
	for name, tc := range map[string]struct {
		tool  string
		value interface{}
	}{
		"shell exit w/o code":    {"openai.shell", map[string]interface{}{"output": []interface{}{map[string]interface{}{"stdout": "", "stderr": "", "outcome": map[string]interface{}{"type": "exit"}}}}},
		"shell snake exit_code":  {"openai.shell", map[string]interface{}{"output": []interface{}{map[string]interface{}{"stdout": "", "stderr": "", "outcome": map[string]interface{}{"type": "exit", "exit_code": 0}}}}},
		"shell no stdout":        {"openai.shell", map[string]interface{}{"output": []interface{}{map[string]interface{}{"stderr": "", "outcome": map[string]interface{}{"type": "timeout"}}}}},
		"local shell output num": {"openai.local_shell", map[string]interface{}{"output": 5}},
		"patch bad status":       {"openai.apply_patch", map[string]interface{}{"status": "ok"}},
		"computer no screenshot": {"openai.computer", map[string]interface{}{"output": map[string]interface{}{"type": "computer_screenshot"}}},
		"computer bad detail":    {"openai.computer", map[string]interface{}{"output": map[string]interface{}{"type": "computer_screenshot", "imageUrl": "u", "detail": "huge"}}},
	} {
		if err := convertResult(tc.tool, tc.value); !isTypeValidation(err) {
			t.Errorf("%s: err = %v, want TypeValidationError", name, err)
		}
	}
}

// zod objects strip unknown keys, so replayed computer actions only carry the
// schema's keys, with scrollX/scrollY renamed.
func TestStrictComputerInputStripsUnknownKeysR33(t *testing.T) {
	input, err := convertCall(t, "openai.computer", map[string]interface{}{
		"status": "completed", "pendingSafetyChecks": []interface{}{map[string]interface{}{"id": "s", "extra": 1}},
		"actions": []interface{}{map[string]interface{}{"type": "scroll", "x": 1.0, "y": 2.0, "scrollX": 3.0, "scrollY": 4.0, "bogus": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cc := input[0].(ComputerCall)
	a := cc.Actions[0]
	if _, ok := a["bogus"]; ok || a["scroll_x"] != 3.0 || a["scroll_y"] != 4.0 || len(a) != 5 {
		t.Fatalf("action = %v", a)
	}
}
