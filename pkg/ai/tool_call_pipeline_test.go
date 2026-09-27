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
		{
			// TS parseToolCall/doParseToolCall check `providerExecuted &&
			// dynamic` together (e.g. mcp_tool_use sets both; Anthropic
			// web_search/web_fetch set only providerExecuted and rely on a
			// matching registered tool). A providerExecuted call that is NOT
			// dynamic and has no matching tool must still raise
			// NoSuchToolError, not be silently accepted.
			name:    "provider-executed non-dynamic tool calls without a matching tool are still NoSuchToolError",
			call:    types.ToolCall{ID: "1", ToolName: "remote", RawArguments: `{"q":1}`, ProviderExecuted: true},
			tools:   tools,
			wantErr: IsNoSuchToolError,
			wantMsg: "Model tried to call unavailable tool 'remote'. Available tools: testTool.",
		},
		{
			name:    "provider-executed non-dynamic tool calls without a matching tool are NoSuchToolError even with no tools registered",
			call:    types.ToolCall{ID: "1", ToolName: "remote", RawArguments: `{"q":1}`, ProviderExecuted: true},
			wantErr: IsNoSuchToolError,
			wantMsg: "Model tried to call unavailable tool 'remote'. No tools are available.",
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

// TestParseToolCallRejectsSchemaConstraintViolations exercises pkg/schema's
// extended JSON Schema keyword support (SLICE SC) through ParseToolCall: a
// tool call whose input violates minLength/pattern/minimum/maxItems must
// come back invalid with an InvalidToolInputError, the same way an args type
// mismatch already did before those keywords were supported.
func TestParseToolCallRejectsSchemaConstraintViolations(t *testing.T) {
	ctx := context.Background()

	tool := types.Tool{
		Name: "createUser",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{"type": "string", "minLength": 3},
				"code":     map[string]interface{}{"type": "string", "pattern": "^[A-Z]{3}$"},
				"age":      map[string]interface{}{"type": "integer", "minimum": 18},
				"tags":     map[string]interface{}{"type": "array", "maxItems": 2},
			},
			"required": []interface{}{"username"},
		},
	}
	tools := []types.Tool{tool}

	tests := []struct {
		name string
		args string
	}{
		{name: "minLength violated", args: `{"username":"ab"}`},
		{name: "pattern violated", args: `{"username":"abc","code":"abcd"}`},
		{name: "minimum violated", args: `{"username":"abc","age":10}`},
		{name: "maxItems violated", args: `{"username":"abc","tags":["a","b","c"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseToolCall(ctx, ParseToolCallOptions{
				ToolCall: types.ToolCall{ID: "1", ToolName: "createUser", RawArguments: tt.args},
				Tools:    tools,
			})
			if err != nil {
				t.Fatalf("unexpected top-level error: %v", err)
			}
			if !got.Invalid {
				t.Fatalf("expected invalid tool call for %s, got valid: %+v", tt.args, got)
			}
			if !IsInvalidToolInputError(got.Error) {
				t.Fatalf("expected InvalidToolInputError, got %T: %v", got.Error, got.Error)
			}
		})
	}

	t.Run("valid input satisfying every constraint is accepted", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "1", ToolName: "createUser", RawArguments: `{"username":"abc","code":"XYZ","age":21,"tags":["a"]}`},
			Tools:    tools,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Invalid || got.Error != nil {
			t.Fatalf("expected valid call, got invalid: %v", got.Error)
		}
	})
}

// TestParseToolCallAppliesSchemaDefaults verifies doParseToolCall fills JSON
// Schema "default" values before validating and before the tool sees its
// input -- mirroring TS's zod .parse(), which fills .default() values as
// part of safeParseJSON/safeValidateTypes (see
// generate-text/parse-tool-call.ts's doParseToolCall).
func TestParseToolCallAppliesSchemaDefaults(t *testing.T) {
	ctx := context.Background()

	var gotArgs map[string]interface{}
	tool := types.Tool{
		Name: "search",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":  map[string]interface{}{"type": "string"},
				"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
				"limit":  map[string]interface{}{"type": "integer", "default": 10},
			},
			"required": []interface{}{"query"},
		},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			gotArgs = input
			return "ok", nil
		},
	}

	got, err := ParseToolCall(ctx, ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "1", ToolName: "search", RawArguments: `{"query":"widgets"}`},
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Invalid || got.Error != nil {
		t.Fatalf("expected valid call, got invalid: %v", got.Error)
	}
	if got.Arguments["region"] != "us-east-1" {
		t.Fatalf("Arguments[region] = %v, want us-east-1 (default should be filled)", got.Arguments["region"])
	}
	if got.Arguments["limit"] != int64(10) && got.Arguments["limit"] != 10 && got.Arguments["limit"] != float64(10) {
		t.Fatalf("Arguments[limit] = %v (%T), want 10 (default should be filled)", got.Arguments["limit"], got.Arguments["limit"])
	}

	// Execute the tool the way the agent loop does (passing the parsed
	// call's Arguments, not the raw un-defaulted args), and confirm the
	// defaulted arguments reach it.
	if _, err := tool.Execute(ctx, got.Arguments, types.ToolExecutionOptions{}); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if gotArgs["region"] != "us-east-1" {
		t.Fatalf("tool saw region = %v, want us-east-1", gotArgs["region"])
	}

	t.Run("explicit value is not overridden by default", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "1", ToolName: "search", RawArguments: `{"query":"widgets","region":"eu-west-1"}`},
			Tools:    []types.Tool{tool},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Arguments["region"] != "eu-west-1" {
			t.Fatalf("Arguments[region] = %v, want eu-west-1 (explicit value must win)", got.Arguments["region"])
		}
	})
}

func ptrFloat(v float64) *float64 { return &v }
