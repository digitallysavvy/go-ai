package ai

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 77 integrator survivor test (gen fix 1): a ValidateInput that
// transforms its input map in place must not change the approval's
// inputSchemaInput, which is the input the schema received (TS
// parseResult.rawValue, a value the schema does not mutate).
func TestR77Integ_InPlaceValidateInputKeepsInputSchemaInput(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		ValidateInput: func(v interface{}) (interface{}, error) {
			m := v.(map[string]interface{})
			if s, ok := m["count"].(string); ok {
				n, _ := strconv.Atoi(s)
				m["count"] = float64(n)
			}
			return m, nil
		},
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "tool1", RawArguments: `{"count":"3"}`}}}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Tools: []types.Tool{tool}, Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Steps[0].Content {
		if req, ok := p.(types.ToolApprovalRequestContent); ok {
			if want := map[string]interface{}{"count": "3"}; !reflect.DeepEqual(req.InputSchemaInput, want) {
				t.Fatalf("InputSchemaInput = %#v, want %#v", req.InputSchemaInput, want)
			}
			return
		}
	}
	t.Fatalf("no approval request in %#v", res.Steps[0].Content)
}

// Round 77 integrator survivor test (stream fix 3): a nil
// map[string]interface{} is JSON null, so it neither replaces nor creates
// message metadata (TS updateMessageMetadata skips null).
func TestR77Integ_NilMapMetadataIsSkipped(t *testing.T) {
	s := &uiMessageCallbackState{message: UIMessageChunk{}}
	s.mergeMetadata(map[string]interface{}(nil))
	if _, ok := s.message["metadata"]; ok {
		t.Fatalf("metadata = %#v, want none", s.message["metadata"])
	}
	s.message["metadata"] = map[string]interface{}{"a": 1.0}
	s.mergeMetadata(map[string]interface{}(nil))
	if !reflect.DeepEqual(s.message["metadata"], map[string]interface{}{"a": 1.0}) {
		t.Fatalf("metadata = %#v, want it unchanged", s.message["metadata"])
	}
}

// Round 77 integrator survivor test (gen fix 1): after a repair, the
// approval's inputSchemaInput is the repaired call's input before the
// schema transformed it (TS doParseToolCall on the repaired call sets
// parseResult.rawValue).
func TestR77Integ_RepairedCallInputSchemaInputIsRawInput(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		ValidateInput: func(v interface{}) (interface{}, error) {
			m, _ := v.(map[string]interface{})
			s, ok := m["count"].(string)
			if !ok {
				return nil, r77integErrCount
			}
			n, _ := strconv.Atoi(s)
			return map[string]interface{}{"count": float64(n)}, nil
		},
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "tool1", RawArguments: `{"cnt":"3"}`}}}, nil
		},
	}
	repair := func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
		c := o.ToolCall
		c.Arguments, c.RawArguments = nil, `{"count":"3"}`
		return &c, nil
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Tools: []types.Tool{tool}, Prompt: "go", RepairToolCall: repair})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Steps[0].Content {
		if req, ok := p.(types.ToolApprovalRequestContent); ok {
			if want := map[string]interface{}{"count": "3"}; !reflect.DeepEqual(req.InputSchemaInput, want) {
				t.Fatalf("InputSchemaInput = %#v, want %#v", req.InputSchemaInput, want)
			}
			return
		}
	}
	t.Fatalf("no approval request in %#v", res.Steps[0].Content)
}

var r77integErrCount = r77integError("count must be a string")

type r77integError string

func (e r77integError) Error() string { return string(e) }
