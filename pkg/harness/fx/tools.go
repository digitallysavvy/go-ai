package fx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Port of TS fx-tools.ts.

// toolRequiresApproval mirrors TS fxToolRequiresApproval.
func toolRequiresApproval(mode harness.PermissionMode, kind harness.BuiltinToolUseKind) bool {
	return mode != harness.PermissionModeAllowAll &&
		(kind == harness.BuiltinToolUseKindBash ||
			(kind == harness.BuiltinToolUseKindEdit && mode == harness.PermissionModeAllowReads))
}

// jsonValue mirrors TS fxJsonValue: the JSON round-trip of value, null for
// nil, and the value's text when it cannot be serialized.
func jsonValue(value any) json.RawMessage {
	switch v := value.(type) {
	case nil:
		return json.RawMessage("null")
	case json.RawMessage:
		if len(v) == 0 {
			return json.RawMessage("null")
		}
		var out any
		if json.Unmarshal(v, &out) != nil {
			return json.RawMessage("null")
		}
		data, _ := jsstr.Marshal(out)
		return data
	}
	data, err := jsstr.Marshal(value)
	if err != nil {
		data, _ = jsstr.Marshal(fmt.Sprint(value))
	}
	return data
}

// decodeJSONValue turns a stored JSON value into the Go value a stream part
// carries.
func decodeJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// submissionModelOutput extracts the model-facing output a submission
// carries (TS submission.toolResult?.output).
func submissionModelOutput(tr any) *types.ToolResultOutput {
	switch r := tr.(type) {
	case *harness.ModelToolResult:
		if r != nil {
			return &r.Output
		}
	case harness.ModelToolResult:
		return &r.Output
	case *types.ToolResultContent:
		if r != nil {
			return r.Output
		}
	case types.ToolResultContent:
		return r.Output
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }

// toToolResult mirrors TS toFxToolResult.
func toToolResult(s harness.ToolResultSubmission) (*fxToolResult, error) {
	var submittedError *bool
	if s.IsError {
		submittedError = boolPtr(true)
	}
	result := &fxToolResult{Type: "libfx.tool-result", Images: []fxToolImage{}, IsError: submittedError}
	output := submissionModelOutput(s.ToolResult)
	if output == nil {
		if text, ok := s.Output.(string); ok {
			result.Text = text
		} else {
			result.Text = string(jsonValue(s.Output))
		}
		return result, nil
	}
	o := *output
	if o.Type == types.ToolResultOutputExecutionDenied && o.Reason == "" {
		o.Reason = "Tool execution was denied."
	}
	normalized, err := harness.NormalizeToolModelOutput(o)
	if err != nil {
		return nil, err
	}
	parts, isError, err := harness.ConvertToolModelOutput(normalized)
	if err != nil {
		return nil, err
	}
	var text []string
	for _, p := range parts {
		if p.Type == harness.ToolModelOutputContentText {
			if p.Text != "" {
				text = append(text, p.Text)
			}
			continue
		}
		result.Images = append(result.Images, fxToolImage{Type: "image", Data: p.Data, MimeType: p.MediaType})
	}
	result.Text = strings.Join(text, "\n")
	switch {
	case isError:
		result.IsError = boolPtr(true)
	case o.Type == types.ToolResultOutputContent:
		result.IsError = submittedError
	default:
		result.IsError = boolPtr(s.IsError)
	}
	return result, nil
}
