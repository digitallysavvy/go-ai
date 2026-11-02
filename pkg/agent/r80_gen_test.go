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

// Generate (GenerateText) and the legacy loop agree that any tool-result or
// tool-error part (value or pointer) answers a deferred provider call: the
// run ends after one step.
func TestR80GenDeferredCallResolvedByAnyResultPart(t *testing.T) {
	parts := map[string]types.ContentPart{
		"tool-result":         types.ToolResultContent{ToolCallID: "call-1", ToolName: "code_execution", Result: "ok"},
		"tool-result pointer": &types.ToolResultContent{ToolCallID: "call-1", ToolName: "code_execution", Result: "ok"},
		"tool-error":          types.ToolErrorContent{ToolCallID: "call-1", ToolName: "code_execution", Error: "failed"},
		"tool-error pointer":  &types.ToolErrorContent{ToolCallID: "call-1", ToolName: "code_execution", Error: "failed"},
	}
	for name, part := range parts {
		for _, mode := range []string{"Generate", "GenerateAgent"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				calls := 0
				model := &testutil.MockLanguageModel{
					DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
						calls++
						return &types.GenerateResult{
							ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "code_execution", Arguments: map[string]interface{}{}, ProviderExecuted: true}},
							Content: []types.ContentPart{
								types.ToolCallContent{ToolCallID: "call-1", ToolName: "code_execution", Input: "{}", ProviderExecuted: true},
								part,
							},
							FinishReason: types.FinishReasonStop,
						}, nil
					},
				}
				a := NewToolLoopAgent(AgentConfig{
					Model:    model,
					Tools:    []types.Tool{{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true}},
					MaxSteps: 3,
				})
				var err error
				if mode == "Generate" {
					_, err = a.Generate(context.Background(), AgentGenerateOptions{Prompt: "x"})
				} else {
					_, err = a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "x"})
				}
				if err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatalf("model calls = %d, want 1", calls)
				}
			})
		}
	}
}
