package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r79genTestTool() types.Tool {
	return types.Tool{
		Name: "testTool",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param1": map[string]interface{}{"type": "string"},
				"param2": map[string]interface{}{"type": "number"},
			},
			"required": []interface{}{"param1", "param2"},
		},
	}
}

// InvalidToolInputError shows its cause with TS getErrorMessage (the cause's
// toString): ports of parse-tool-call.test.ts 330 (schema failure, a
// TypeValidationError of the parsed input) and 480 (input that is not JSON,
// a JSONParseError), and generate-text.test.ts 955 (empty input is `{}`).
func TestR79Gen_InvalidToolInputCauseText(t *testing.T) {
	ctx := context.Background()
	t.Run("schema failure", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: `{"param1": "test"}`}, Tools: []types.Tool{r79genTestTool()}})
		if err != nil || !got.Invalid || !got.Dynamic {
			t.Fatalf("got %#v, %v", got, err)
		}
		want := "Invalid input for tool testTool: AI_TypeValidationError: Type validation failed: Value: {\"param1\":\"test\"}.\nError message: Error: "
		if msg := got.Error.Error(); !strings.HasPrefix(msg, want) {
			t.Fatalf("error = %q, want prefix %q", msg, want)
		}
		var verr *providererrors.ValidationError
		if !errors.As(got.Error, &verr) {
			t.Fatalf("cause is not a TypeValidationError: %#v", got.Error)
		}
		part := contentPartFromToolCall(got)
		if part.Arguments["param1"] != "test" || !part.Invalid {
			t.Fatalf("part = %#v", part)
		}
	})
	t.Run("empty input", func(t *testing.T) {
		got, _ := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: ""}, Tools: []types.Tool{r79genTestTool()}})
		want := "Invalid input for tool testTool: AI_TypeValidationError: Type validation failed: Value: {}.\nError message: Error: "
		if msg := got.Error.Error(); !strings.HasPrefix(msg, want) {
			t.Fatalf("error = %q, want prefix %q", msg, want)
		}
	})
	t.Run("defaults are not in the reported value", func(t *testing.T) {
		tool := r79genTestTool()
		props := tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})
		props["param3"] = map[string]interface{}{"type": "string", "default": "d"}
		got, _ := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", Arguments: map[string]interface{}{"param1": "test"}}, Tools: []types.Tool{tool}})
		want := "Value: {\"param1\":\"test\"}.\n"
		if msg := got.Error.Error(); !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	})
	t.Run("input that is not JSON", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: "invalid json"},
			Tools:    []types.Tool{r79genTestTool()},
			RepairToolCall: func(ctx context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
				return nil, nil
			},
		})
		if err != nil || !got.Invalid || !got.Dynamic {
			t.Fatalf("got %#v, %v", got, err)
		}
		want := "Invalid input for tool testTool: AI_JSONParseError: JSON parsing failed: Text: invalid json.\nError message: "
		if msg := got.Error.Error(); !strings.HasPrefix(msg, want) {
			t.Fatalf("error = %q, want prefix %q", msg, want)
		}
		if !providererrors.IsJSONParseError(got.Error) {
			t.Fatalf("cause is not a JSONParseError: %#v", got.Error)
		}
		var syntax *json.SyntaxError
		if !errors.As(got.Error, &syntax) {
			t.Fatalf("parser error not reachable: %#v", got.Error)
		}
		// TS: the invalid call's input is the raw text when it is not JSON.
		raw, mErr := json.Marshal(contentPartFromToolCall(got))
		if mErr != nil || !strings.Contains(string(raw), `"input":"invalid json"`) {
			t.Fatalf("part JSON = %s, %v", raw, mErr)
		}
	})
	t.Run("JSON that is not an object", func(t *testing.T) {
		got, _ := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: `[1, 2]`}, Tools: []types.Tool{r79genTestTool()}})
		want := "Invalid input for tool testTool: AI_TypeValidationError: Type validation failed: Value: [1,2].\nError message: Error: tool input must be a JSON object, got []interface {}"
		if msg := got.Error.Error(); msg != want {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	})
	t.Run("provider-executed dynamic call that is not JSON", func(t *testing.T) {
		got, _ := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "mcp", RawArguments: "{", ProviderExecuted: true, Dynamic: true}})
		if !providererrors.IsJSONParseError(got.Error) || !strings.HasPrefix(got.Error.Error(), "Invalid input for tool mcp: AI_JSONParseError: JSON parsing failed: Text: {.\n") {
			t.Fatalf("error = %v", got.Error)
		}
	})
	t.Run("nil cause", func(t *testing.T) {
		if msg := (&InvalidToolInputError{ToolName: "x"}).Error(); msg != "Invalid input for tool x: unknown error" {
			t.Fatalf("error = %q", msg)
		}
	})
}

// validate-tool-approvals.ts: the approved-input mismatch is a plain Error
// ("Error: ..."), a refiner's error is shown with its toString, and a schema
// failure is a TypeValidationError of the approved input.
func TestR79Gen_ApprovalRevalidationCauseText(t *testing.T) {
	ctx := context.Background()
	exec := func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	t.Run("refiner error", func(t *testing.T) {
		tool := types.Tool{Name: "tool1", Execute: exec}
		res, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{r77genApproval(map[string]interface{}{"value": "a"}, nil)},
			Tools:                 []types.Tool{tool},
			RefineToolInput: map[string]ToolInputRefiner{
				"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
					return nil, errors.New("refine failed")
				},
			},
		})
		if err != nil || len(res.InvalidToolApprovals) != 1 {
			t.Fatalf("res %+v, %v", res, err)
		}
		if msg := res.InvalidToolApprovals[0].Error.Error(); msg != "Invalid input for tool tool1: Error: refine failed" {
			t.Fatalf("error = %q", msg)
		}
	})
	t.Run("schema failure", func(t *testing.T) {
		tool := types.Tool{Name: "tool1", Execute: exec, Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}, "extra": map[string]interface{}{"type": "string", "default": "d"}},
			"required":   []interface{}{"value"},
		}}
		res, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{r77genApproval(map[string]interface{}{"value": 1}, nil)},
			Tools:                 []types.Tool{tool},
		})
		if err != nil || len(res.InvalidToolApprovals) != 1 {
			t.Fatalf("res %+v, %v", res, err)
		}
		want := "Invalid input for tool tool1: AI_TypeValidationError: Type validation failed: Value: {\"value\":1}.\nError message: Error: "
		if msg := res.InvalidToolApprovals[0].Error.Error(); !strings.HasPrefix(msg, want) {
			t.Fatalf("error = %q, want prefix %q", msg, want)
		}
		var verr *providererrors.ValidationError
		if !errors.As(res.InvalidToolApprovals[0].Error, &verr) {
			t.Fatalf("cause is not a TypeValidationError")
		}
	})
}
