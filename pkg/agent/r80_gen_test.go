package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// r80genLoopModel calls tool "test_tool" on every step, or answers "done" on
// step finishAt (TS default-stop-condition.test.ts response(step)), for both
// generate and stream.
func r80genLoopModel(finishAt int) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	next := func() int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return calls
	}
	usage := types.Usage{InputTokens: intPtr(1), OutputTokens: intPtr(1), TotalTokens: intPtr(2)}
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider",
		ModelName:    "mock-model-id",
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := next()
			if step == finishAt {
				return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop, Usage: usage}, nil
			}
			id := fmt.Sprintf("call-%d", step)
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: id, ToolName: "test_tool", RawArguments: "{}"}},
				Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: id, ToolName: "test_tool", Input: "{}"}},
				FinishReason: types.FinishReasonToolCalls,
				Usage:        usage,
			}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			step := next()
			u := usage
			if step == finishAt {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &u},
				}), nil
			}
			call := types.ToolCall{ID: fmt.Sprintf("call-%d", step), ToolName: "test_tool", RawArguments: "{}"}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &call},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &u},
			}), nil
		},
	}
}

// Ports the agent cases of TS default-stop-condition.test.ts not covered by
// default_stop_condition_test.go: no warning for a tool without execute, for
// a tool that needs approval, for a custom condition (evaluated once per
// step), with warning logging disabled, or when prepareCall replaces the
// default limit; the warning survives a prepareCall that keeps it. Each runs
// through Generate, Stream and the legacy GenerateAgent.
func TestR80GenDefaultStopConditionCases(t *testing.T) {
	execTool := types.Tool{Name: "test_tool", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result", nil
	}}
	run := func(t *testing.T, mode string, cfg AgentConfig) int {
		t.Helper()
		cfg.Model = r80genLoopModel(0)
		a := NewToolLoopAgent(cfg)
		switch mode {
		case "Generate":
			r, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "Call the test tool."})
			if err != nil {
				t.Fatal(err)
			}
			return len(r.Steps)
		case "Stream":
			r, err := a.Stream(context.Background(), AgentStreamOptions{AgentGenerateOptions: AgentGenerateOptions{Prompt: "Call the test tool."}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
			return len(r.Steps())
		default:
			r, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "Call the test tool."})
			if err != nil {
				t.Fatal(err)
			}
			return len(r.Steps)
		}
	}
	for _, mode := range []string{"Generate", "Stream", "GenerateAgent"} {
		t.Run(mode+"/tool without execute", func(t *testing.T) {
			warnings := captureWarnings(t)
			noExec := types.Tool{Name: "test_tool", Parameters: map[string]interface{}{"type": "object"}, OutputSchema: map[string]interface{}{"type": "string"}}
			if n := run(t, mode, AgentConfig{Tools: []types.Tool{noExec}}); n != 1 || len(warnings()) != 0 {
				t.Fatalf("steps = %d, warnings = %v", n, warnings())
			}
		})
		t.Run(mode+"/tool needs approval", func(t *testing.T) {
			warnings := captureWarnings(t)
			approval := execTool
			approval.ToolApproval = true
			if n := run(t, mode, AgentConfig{Tools: []types.Tool{approval}}); n != 1 || len(warnings()) != 0 {
				t.Fatalf("steps = %d, warnings = %v", n, warnings())
			}
		})
		t.Run(mode+"/custom condition once per step", func(t *testing.T) {
			warnings := captureWarnings(t)
			var mu sync.Mutex
			evaluations := 0
			limit := ai.IsStepCount(2)
			stop := func(s ai.StopConditionState) string {
				mu.Lock()
				evaluations++
				mu.Unlock()
				return limit(s)
			}
			n := run(t, mode, AgentConfig{Tools: []types.Tool{execTool}, StopWhen: []ai.StopCondition{stop}})
			mu.Lock()
			defer mu.Unlock()
			if n != 2 || evaluations != 2 || len(warnings()) != 0 {
				t.Fatalf("steps = %d, evaluations = %d, warnings = %v", n, evaluations, warnings())
			}
		})
		t.Run(mode+"/disabled warning logging", func(t *testing.T) {
			warnings := captureWarnings(t)
			t.Setenv("AI_SDK_LOG_WARNINGS", "false")
			if n := run(t, mode, AgentConfig{Tools: []types.Tool{execTool}}); n != 20 || len(warnings()) != 0 {
				t.Fatalf("steps = %d, warnings = %v", n, warnings())
			}
		})
		t.Run(mode+"/prepareCall replaces the default limit", func(t *testing.T) {
			warnings := captureWarnings(t)
			prepare := func(ctx context.Context, c PrepareCallConfig) PrepareCallConfig {
				c.StopWhen = []ai.StopCondition{ai.IsStepCount(20)}
				return c
			}
			if n := run(t, mode, AgentConfig{Tools: []types.Tool{execTool}, PrepareCall: prepare}); n != 20 || len(warnings()) != 0 {
				t.Fatalf("steps = %d, warnings = %v", n, warnings())
			}
		})
		t.Run(mode+"/prepareCall keeps the default warning", func(t *testing.T) {
			warnings := captureWarnings(t)
			prepare := func(ctx context.Context, c PrepareCallConfig) PrepareCallConfig { return c }
			if n := run(t, mode, AgentConfig{Tools: []types.Tool{execTool}, PrepareCall: prepare}); n != 20 || len(warnings()) != 1 {
				t.Fatalf("steps = %d, warnings = %v", n, warnings())
			}
		})
	}
}
