package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r81genStructInput struct {
	City  string `json:"city" validate:"required"`
	Count int    `json:"count,omitempty" validate:"gte=0"`
}

// r81genCustomValidator is a user schema.Validator that is neither a JSON
// Schema nor a struct-tag schema.
type r81genCustomValidator struct{ calls *int32 }

func (v r81genCustomValidator) Validate(data interface{}) error {
	atomic.AddInt32(v.calls, 1)
	m, _ := data.(map[string]interface{})
	if _, ok := m["q"].(string); !ok {
		return errors.New("q must be a string")
	}
	return nil
}

func (v r81genCustomValidator) JSONSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

type r81genCustomSchema struct{ v r81genCustomValidator }

func (s r81genCustomSchema) Validator() schema.Validator { return s.v }

type r81genParseCase struct {
	name    string
	tool    types.Tool
	raw     string
	invalid bool
}

func r81genStructTool(executed *int32) types.Tool {
	return types.Tool{
		Name:       "weather",
		Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r81genStructInput{})),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(executed, 1)
			return "ok", nil
		},
	}
}

// TS parseToolCall validates the input with the tool's inputSchema whatever
// kind of schema it is. A struct-tag schema (which now decodes the input map
// into the struct and checks its validate: tags) and a custom validator were
// skipped when a call was parsed, so an input that breaks them reached
// Execute.
func TestR81GenParseValidatesEverySchemaKind(t *testing.T) {
	var executed int32
	structTool := r81genStructTool(&executed)
	var schemaCalls, validatorCalls int32
	cases := []r81genParseCase{
		{"struct valid", structTool, `{"city":"Berlin","count":2}`, false},
		{"struct missing required", structTool, `{"count":2}`, true},
		{"struct validate tag", structTool, `{"city":"Berlin","count":-1}`, true},
		{"struct wrong type", structTool, `{"city":5}`, true},
		{"custom schema valid", types.Tool{Name: "search", Parameters: r81genCustomSchema{r81genCustomValidator{&schemaCalls}}}, `{"q":"x"}`, false},
		{"custom schema invalid", types.Tool{Name: "search", Parameters: r81genCustomSchema{r81genCustomValidator{&schemaCalls}}}, `{"q":1}`, true},
		{"custom validator invalid", types.Tool{Name: "search", Parameters: r81genCustomValidator{&validatorCalls}}, `{}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := types.ToolCall{ID: "call-1", ToolName: tc.tool.Name, RawArguments: tc.raw}
			parsed, err := ParseToolCall(context.Background(), ParseToolCallOptions{ToolCall: call, Tools: []types.Tool{tc.tool}})
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Invalid != tc.invalid {
				t.Fatalf("invalid = %v (err %v), want %v", parsed.Invalid, parsed.Error, tc.invalid)
			}
			if tc.invalid {
				if !IsInvalidToolInputError(parsed.Error) {
					t.Fatalf("error = %T %v, want InvalidToolInputError", parsed.Error, parsed.Error)
				}
				if !strings.Contains(parsed.Error.Error(), "Invalid input for tool "+tc.tool.Name) {
					t.Fatalf("error text = %q", parsed.Error.Error())
				}
			}
		})
	}
	if schemaCalls == 0 || validatorCalls == 0 {
		t.Fatalf("custom validators called %d / %d times", schemaCalls, validatorCalls)
	}

	// End to end: GenerateText answers the invalid call with a tool error and
	// never runs Execute.
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "weather", RawArguments: `{"count":1}`}), r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{structTool},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 || len(res.Steps[0].ToolCalls) != 1 || !res.Steps[0].ToolCalls[0].Invalid {
		t.Fatalf("first step tool calls = %+v", res.Steps)
	}
	if atomic.LoadInt32(&executed) != 0 {
		t.Fatalf("Execute ran %d times for an invalid input", executed)
	}
}

// An approved call and a UI tool part are revalidated with the validator the
// call is parsed with: a struct-tag schema now rejects an input that breaks
// it there too (TS revalidates with the tool's inputSchema).
func TestR81GenStructSchemaRevalidationRejectsInvalidInput(t *testing.T) {
	var executed int32
	tool := r81genStructTool(&executed)
	bad := types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"count": float64(1)}}
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{{ToolCall: bad}},
		Tools:                 []types.Tool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
		t.Fatalf("approved = %d, invalid = %d, want 0 and 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
	}
	msgs := `[{"id":"m1","role":"assistant","parts":[{"type":"tool-weather","toolCallId":"call-1","state":"input-available","input":{"count":1}}]}]`
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs, Tools: []types.Tool{tool}}); err == nil {
		t.Fatal("ValidateUIMessages accepted an input without the required city")
	}
	good := `[{"id":"m1","role":"assistant","parts":[{"type":"tool-weather","toolCallId":"call-1","state":"input-available","input":{"city":"Berlin"}}]}]`
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: good, Tools: []types.Tool{tool}}); err != nil {
		t.Fatalf("ValidateUIMessages: %v", err)
	}
}

// A nil schema or validator in Parameters is no schema.
func TestR81GenNilSchemaNotValidated(t *testing.T) {
	var nilStruct *schema.SimpleStructSchema
	var nilValidator *schema.JSONSchemaValidator
	for _, p := range []interface{}{nilStruct, nilValidator} {
		if v := toolCallInputValidator(&types.Tool{Name: "x", Parameters: p}); v != nil {
			t.Fatalf("validator for %T = %v, want nil", p, v)
		}
	}
}
