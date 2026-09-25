package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func pipelineTestTool(name string, exec func(args map[string]interface{}) (interface{}, error)) types.Tool {
	tool := types.Tool{
		Name: name,
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"value"},
		},
	}
	if exec != nil {
		tool.Execute = func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return exec(input)
		}
	}
	return tool
}

// Ported from parse-tool-call.test.ts.
func TestParseToolCall(t *testing.T) {
	tools := []types.Tool{pipelineTestTool("testTool", nil)}
	ctx := context.Background()

	tests := []struct {
		name      string
		call      types.ToolCall
		tools     []types.Tool
		wantValid bool
		wantErr   func(error) bool
		wantMsg   string
	}{
		{
			name:      "should successfully parse a valid tool call",
			call:      types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":"x"}`},
			tools:     tools,
			wantValid: true,
		},
		{
			name:      "should successfully process empty tool calls for tools that have no inputSchema",
			call:      types.ToolCall{ID: "1", ToolName: "noSchema", RawArguments: ""},
			tools:     []types.Tool{{Name: "noSchema"}},
			wantValid: true,
		},
		{
			name:    "should return an invalid tool call when tools is null",
			call:    types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{}`},
			wantErr: IsNoSuchToolError,
			wantMsg: "Model tried to call unavailable tool 'testTool'. No tools are available.",
		},
		{
			name:    "should return an invalid tool call when tool is not found",
			call:    types.ToolCall{ID: "1", ToolName: "nonExistentTool", RawArguments: `{}`},
			tools:   tools,
			wantErr: IsNoSuchToolError,
			wantMsg: "Model tried to call unavailable tool 'nonExistentTool'. Available tools: testTool.",
		},
		{
			name:    "should return an invalid tool call when args are invalid",
			call:    types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":1}`},
			tools:   tools,
			wantErr: IsInvalidToolInputError,
		},
		{
			name:    "should return an invalid tool call when the input is not valid JSON",
			call:    types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":`},
			tools:   tools,
			wantErr: IsInvalidToolInputError,
		},
		{
			name:      "provider-executed dynamic tool calls are accepted without a tool",
			call:      types.ToolCall{ID: "1", ToolName: "remote", RawArguments: `{"q":1}`, ProviderExecuted: true, Dynamic: true},
			tools:     tools,
			wantValid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: tt.call, Tools: tt.tools})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantValid {
				if got.Invalid || got.Error != nil {
					t.Fatalf("expected valid call, got invalid: %v", got.Error)
				}
				return
			}
			if !got.Invalid || !got.Dynamic {
				t.Fatalf("expected invalid dynamic call, got %+v", got)
			}
			if !tt.wantErr(got.Error) {
				t.Fatalf("unexpected error type: %T %v", got.Error, got.Error)
			}
			if tt.wantMsg != "" && got.Error.Error() != tt.wantMsg {
				t.Fatalf("message = %q, want %q", got.Error.Error(), tt.wantMsg)
			}
		})
	}
}

// Ported from parse-tool-call.test.ts "tool call repair".
func TestParseToolCallRepair(t *testing.T) {
	tools := []types.Tool{pipelineTestTool("testTool", nil)}
	messages := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}

	t.Run("should repair a tool call with an invalid tool name and pass instructions", func(t *testing.T) {
		var seen ToolCallRepairOptions
		got, err := ParseToolCall(context.Background(), ParseToolCallOptions{
			ToolCall:     types.ToolCall{ID: "1", ToolName: "unknownTool", RawArguments: `{"value":"x"}`},
			Tools:        tools,
			Instructions: "be nice",
			Messages:     messages,
			RepairToolCall: func(_ context.Context, opts ToolCallRepairOptions) (*types.ToolCall, error) {
				seen = opts
				schema, err := opts.InputSchema("testTool")
				if err != nil || schema["type"] != "object" {
					t.Fatalf("InputSchema = %v, %v", schema, err)
				}
				repaired := opts.ToolCall
				repaired.ToolName = "testTool"
				return &repaired, nil
			},
		})
		if err != nil || got.Invalid || got.ToolName != "testTool" {
			t.Fatalf("got %+v, err %v", got, err)
		}
		if !IsNoSuchToolError(seen.Error) || seen.Instructions != "be nice" || seen.System != "be nice" || len(seen.Messages) != 1 {
			t.Fatalf("repair options = %+v", seen)
		}
	})

	t.Run("should return invalid tool call when repair returns null", func(t *testing.T) {
		got, _ := ParseToolCall(context.Background(), ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":1}`},
			Tools:    tools,
			RepairToolCall: func(context.Context, ToolCallRepairOptions) (*types.ToolCall, error) {
				return nil, nil
			},
		})
		if !got.Invalid || !IsInvalidToolInputError(got.Error) {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("should return invalid tool call with ToolCallRepairError when repair throws", func(t *testing.T) {
		got, _ := ParseToolCall(context.Background(), ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":1}`},
			Tools:    tools,
			RepairToolCall: func(context.Context, ToolCallRepairOptions) (*types.ToolCall, error) {
				return nil, errors.New("boom")
			},
		})
		var repairErr *ToolCallRepairError
		if !got.Invalid || !errors.As(got.Error, &repairErr) || !IsInvalidToolInputError(repairErr.OriginalError) {
			t.Fatalf("got %+v", got)
		}
		if got.Error.Error() != "Error repairing tool call: boom" {
			t.Fatalf("message = %q", got.Error.Error())
		}
	})

	// TS 6317504: stop pending tool-call repairs when generation is cancelled.
	t.Run("should stop a pending repair when cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		go func() {
			<-started
			cancel()
		}()
		done := make(chan struct{})
		var err error
		go func() {
			defer close(done)
			_, err = ParseToolCall(ctx, ParseToolCallOptions{
				ToolCall: types.ToolCall{ID: "1", ToolName: "testTool", RawArguments: `{"value":1}`},
				Tools:    tools,
				RepairToolCall: func(context.Context, ToolCallRepairOptions) (*types.ToolCall, error) {
					close(started)
					select {} // never returns
				},
			})
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ParseToolCall did not return after cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func toolCallModel(calls []types.ToolCall, responseModelID string) *testutil.MockLanguageModel {
	step := 0
	var mu sync.Mutex
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			step++
			if step == 1 {
				result := &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}
				if responseModelID != "" {
					result.ResponseMetadata = &types.ResponseMetadata{ID: "resp-1", ModelID: responseModelID}
				}
				return result, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
}

func TestGenerateTextRepairToolCall(t *testing.T) {
	var executed []map[string]interface{}
	tool := pipelineTestTool("testTool", func(args map[string]interface{}) (interface{}, error) {
		executed = append(executed, args)
		return "ok", nil
	})
	for _, tc := range []struct {
		name string
		set  func(*GenerateTextOptions, ToolCallRepairFunction)
	}{
		{"stable", func(o *GenerateTextOptions, f ToolCallRepairFunction) { o.RepairToolCall = f }},
		{"deprecated alias", func(o *GenerateTextOptions, f ToolCallRepairFunction) { o.ExperimentalRepairToolCall = f }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executed = nil
			var instructions string
			opts := GenerateTextOptions{
				Model:    toolCallModel([]types.ToolCall{{ID: "c1", ToolName: "testTool", RawArguments: `{"value":1}`}}, ""),
				Prompt:   "hi",
				System:   "sys",
				Tools:    []types.Tool{tool},
				StopWhen: []StopCondition{StepCountIs(2)},
			}
			tc.set(&opts, func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
				instructions = o.Instructions
				repaired := o.ToolCall
				repaired.RawArguments = `{"value":"fixed"}`
				return &repaired, nil
			})
			result, err := GenerateText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if instructions != "sys" {
				t.Fatalf("repair instructions = %q", instructions)
			}
			if len(executed) != 1 || executed[0]["value"] != "fixed" {
				t.Fatalf("executed = %v", executed)
			}
			if result.Steps[0].ToolCalls[0].Invalid {
				t.Fatal("repaired call must be valid")
			}
		})
	}
}

// TS generate-text.test.ts "should add tool error parts for invalid tool calls"
// and 7bd6bdd "no synthesized client tool-error for invalid provider-executed calls".
func TestGenerateTextInvalidToolCalls(t *testing.T) {
	executed := false
	tool := pipelineTestTool("testTool", func(map[string]interface{}) (interface{}, error) {
		executed = true
		return "ok", nil
	})
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: toolCallModel([]types.ToolCall{
			{ID: "c1", ToolName: "testTool", RawArguments: `{"value":1}`},
			{ID: "c2", ToolName: "remote", RawArguments: `{bad`, ProviderExecuted: true, Dynamic: true},
		}, ""),
		Prompt:   "hi",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatal("invalid tool call must not execute")
	}
	step := result.Steps[0]
	if len(step.ToolResults) != 1 {
		t.Fatalf("tool results = %+v", step.ToolResults)
	}
	tr := step.ToolResults[0]
	if tr.ToolCallID != "c1" || tr.Error == nil || !tr.Dynamic || !IsInvalidToolInputError(tr.Error) {
		t.Fatalf("tool result = %+v", tr)
	}
	if !step.ToolCalls[1].Invalid || !step.ToolCalls[1].ProviderExecuted {
		t.Fatalf("provider-executed call = %+v", step.ToolCalls[1])
	}
}

// TS cd06458 / 8ade040: onInputStart before onInputAvailable in non-streaming
// calls, with tool-specific context.
func TestGenerateTextToolInputCallbacks(t *testing.T) {
	var mu sync.Mutex
	var events []string
	var startOpts types.OnInputStartOptions
	var availOpts types.OnInputAvailableOptions
	record := func(s string) {
		mu.Lock()
		events = append(events, s)
		mu.Unlock()
	}
	tool := pipelineTestTool("testTool", func(map[string]interface{}) (interface{}, error) {
		record("execute")
		return "ok", nil
	})
	tool.OnInputStart = func(_ context.Context, o types.OnInputStartOptions) error {
		record("start:" + o.ToolCallID)
		startOpts = o
		return nil
	}
	tool.OnInputDelta = func(context.Context, types.OnInputDeltaOptions) error {
		record("delta")
		return nil
	}
	tool.OnInputAvailable = func(_ context.Context, o types.OnInputAvailableOptions) error {
		record("available:" + o.ToolCallID)
		availOpts = o
		return nil
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: toolCallModel([]types.ToolCall{
			{ID: "c1", ToolName: "testTool", RawArguments: `{"value":"a"}`},
			{ID: "c2", ToolName: "testTool", RawArguments: `{"value":1}`}, // invalid: no callbacks
		}, ""),
		Prompt:       "hi",
		Tools:        []types.Tool{tool},
		ToolsContext: map[string]interface{}{"testTool": "tool-ctx"},
		StopWhen:     []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"start:c1", "available:c1", "execute"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if startOpts.Context != "tool-ctx" || len(startOpts.Messages) != 1 {
		t.Fatalf("start options = %+v", startOpts)
	}
	if availOpts.Input["value"] != "a" || availOpts.Context != "tool-ctx" || availOpts.ToolCallID != "c1" {
		t.Fatalf("available options = %+v", availOpts)
	}
}

// TS generate-text.test.ts onLanguageModelCallStart / onLanguageModelCallEnd;
// 015acb4 response model ID on the end event.
func TestGenerateTextLanguageModelCallCallbacks(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var starts []LanguageModelCallStartEvent
	var ends []LanguageModelCallEndEvent
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	tool := pipelineTestTool("testTool", func(map[string]interface{}) (interface{}, error) { return "ok", nil })
	for _, useAlias := range []bool{false, true} {
		order, starts, ends = nil, nil, nil
		model2 := toolCallModel([]types.ToolCall{{ID: "c1", ToolName: "testTool", RawArguments: `{"value":"a"}`}}, "resolved-model")
		model2.ModelName = "requested-model"
		opts := GenerateTextOptions{
			Model:       model2,
			Prompt:      "hi",
			Tools:       []types.Tool{tool},
			StopWhen:    []StopCondition{StepCountIs(2)},
			Temperature: ptrFloat(0.5),
			OnStepStart: func(context.Context, OnStepStartEvent) { record("stepStart") },
			OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) {
				record("toolStart")
			},
		}
		startFn := func(_ context.Context, e LanguageModelCallStartEvent) {
			record("lmStart")
			starts = append(starts, e)
		}
		endFn := func(_ context.Context, e LanguageModelCallEndEvent) {
			record("lmEnd")
			ends = append(ends, e)
		}
		if useAlias {
			opts.ExperimentalOnLanguageModelCallStart = startFn
			opts.ExperimentalOnLanguageModelCallEnd = endFn
		} else {
			opts.OnLanguageModelCallStart = startFn
			opts.OnLanguageModelCallEnd = endFn
		}
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		want := "stepStart,lmStart,lmEnd,toolStart,stepStart,lmStart,lmEnd"
		if got := strings.Join(order, ","); got != want {
			t.Fatalf("order = %s, want %s", got, want)
		}
		if starts[0].ModelID != "requested-model" || starts[0].Temperature == nil || *starts[0].Temperature != 0.5 || len(starts[0].Tools) != 1 {
			t.Fatalf("start event = %+v", starts[0])
		}
		if ends[0].ModelID != "resolved-model" || ends[0].ResponseID != "resp-1" || ends[0].FinishReason != types.FinishReasonToolCalls {
			t.Fatalf("end event = %+v", ends[0])
		}
		if ends[1].ModelID != "requested-model" {
			t.Fatalf("second end event should fall back to the requested model: %+v", ends[1])
		}
	}
}

func ptrFloat(v float64) *float64 { return &v }
