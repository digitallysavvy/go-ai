package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r80genInvalidCallModel returns one tool call to "t" whose input is not
// JSON, so the parse runs RepairToolCall.
func r80genInvalidCallModel() *testutil.MockLanguageModel {
	call := types.ToolCall{ID: "call-1", ToolName: "t", RawArguments: "invalid json"}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{call},
				Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: call.ID, ToolName: call.ToolName, Input: call.RawArguments}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
}

// TS parse-tool-call.test.ts 603 "repair rejection after cancellation" and
// 637 "aborted during repaired input validation": the call rejects with the
// abort reason. The legacy loop (GenerateAgent, Execute) returned
// `step 1 failed: context canceled` (the parse's ctx.Err()); it now carries
// the abort cause like Generate and the between-step abort.
func TestR80GenLegacyLoopRepairAbortReturnsCause(t *testing.T) {
	cancellation := errors.New("cancelled")
	tool := func(validate func(interface{}) (interface{}, error)) types.Tool {
		return types.Tool{
			Name:          "t",
			Parameters:    map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
			ValidateInput: validate,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}
	}
	for _, mode := range []string{"Generate", "GenerateAgent", "Execute"} {
		for _, name := range []string{"repair rejection", "repaired input validation"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				var validate func(interface{}) (interface{}, error)
				repair := func(ctx context.Context, o ai.ToolCallRepairOptions) (*types.ToolCall, error) {
					cancel(cancellation)
					return nil, errors.New("repair failed")
				}
				if name == "repaired input validation" {
					validate = func(v interface{}) (interface{}, error) {
						cancel(cancellation)
						return v, nil
					}
					repair = func(ctx context.Context, o ai.ToolCallRepairOptions) (*types.ToolCall, error) {
						repaired := o.ToolCall
						repaired.RawArguments = `{"value":"repaired"}`
						return &repaired, nil
					}
				}
				a := NewToolLoopAgent(AgentConfig{
					Model:          r80genInvalidCallModel(),
					Tools:          []types.Tool{tool(validate)},
					RepairToolCall: repair,
				})
				var err error
				switch mode {
				case "Generate":
					_, err = a.Generate(ctx, AgentGenerateOptions{Prompt: "x"})
				case "GenerateAgent":
					_, err = a.GenerateAgent(ctx, AgentGenerateOptions{Prompt: "x"})
				default:
					_, err = a.Execute(ctx, "x")
				}
				if !errors.Is(err, cancellation) {
					t.Fatalf("err = %v, want the abort reason", err)
				}
				if mode != "Generate" && !strings.HasPrefix(err.Error(), "step 1 failed: ") {
					t.Fatalf("err = %q, want the legacy step prefix", err)
				}
			})
		}
	}
}
