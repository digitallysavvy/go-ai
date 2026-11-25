package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-text.test.ts / stream-text.test.ts "should reject tool calls
// with none" (#23843).

const toolChoiceNoneMessage = "Model response contained a tool call even though tool choice was none."

type noneToolProbe struct {
	executed, inputStart, inputAvailable atomic.Int32
}

func (p *noneToolProbe) tool() types.Tool {
	return types.Tool{
		Name: "tool1",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			p.executed.Add(1)
			return "tool-result", nil
		},
		OnInputStart: func(ctx context.Context, opts types.OnInputStartOptions) error {
			p.inputStart.Add(1)
			return nil
		},
		OnInputAvailable: func(ctx context.Context, opts types.OnInputAvailableOptions) error {
			p.inputAvailable.Add(1)
			return nil
		},
	}
}

func (p *noneToolProbe) assertIdle(t *testing.T) {
	t.Helper()
	if p.executed.Load() != 0 || p.inputStart.Load() != 0 || p.inputAvailable.Load() != 0 {
		t.Fatalf("tool ran: executed=%d inputStart=%d inputAvailable=%d", p.executed.Load(), p.inputStart.Load(), p.inputAvailable.Load())
	}
}

func TestGenerateText_ToolChoiceNoneRejectsToolCalls(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		toolChoice types.ToolChoice
		stepChoice *types.ToolChoice
		finish     types.FinishReason
	}{
		{"none/stop", types.NoneToolChoice(), nil, types.FinishReasonStop},
		{"none/tool-calls", types.NoneToolChoice(), nil, types.FinishReasonToolCalls},
		{"none/interrupted", types.NoneToolChoice(), nil, types.FinishReasonInterrupted},
		{"auto then none via prepareStep", types.AutoToolChoice(), func() *types.ToolChoice { c := types.NoneToolChoice(); return &c }(), types.FinishReasonToolCalls},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := &noneToolProbe{}
			var modelCallEnds atomic.Int32
			model := &testutil.MockLanguageModel{
				ToolSupport: true,
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					return &types.GenerateResult{
						FinishReason: tc.finish,
						ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
					}, nil
				},
			}
			opts := GenerateTextOptions{
				Model:      model,
				Prompt:     "test-input",
				Tools:      []types.Tool{probe.tool()},
				ToolChoice: tc.toolChoice,
				OnLanguageModelCallEnd: func(ctx context.Context, e LanguageModelCallEndEvent) {
					modelCallEnds.Add(1)
				},
			}
			if tc.stepChoice != nil {
				choice := *tc.stepChoice
				opts.PrepareStep = func(ctx context.Context, o PrepareStepOptions) PrepareStepOptions {
					o.ToolChoice = choice
					return o
				}
			}
			_, err := GenerateText(context.Background(), opts)
			var violation *ToolChoiceViolationError
			if !errors.As(err, &violation) {
				t.Fatalf("error = %v, want *ToolChoiceViolationError", err)
			}
			if violation.Error() != toolChoiceNoneMessage || violation.ToolChoice.Type != types.ToolChoiceNone || violation.FinishReason != tc.finish {
				t.Fatalf("violation = %+v (%q)", violation, violation.Error())
			}
			if len(violation.Content) == 0 {
				t.Fatal("violation must carry the response content")
			}
			probe.assertIdle(t)
			if modelCallEnds.Load() != 1 {
				t.Fatalf("model call end events = %d", modelCallEnds.Load())
			}
		})
	}
}

func TestGenerateText_ToolChoiceNoneDoesNotRepairOrRefine(t *testing.T) {
	t.Parallel()

	var repaired, refined atomic.Int32
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{
					{ID: "call-1", ToolName: "tool1", RawArguments: "invalid json"},
					{ID: "call-2", ToolName: "tool2", Arguments: map[string]interface{}{"value": "x"}, RawArguments: `{"value":"x"}`},
				},
			}, nil
		},
	}
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:      model,
		Prompt:     "test-input",
		Tools:      []types.Tool{{Name: "tool1", Parameters: schema}, {Name: "tool2", Parameters: schema}},
		ToolChoice: types.NoneToolChoice(),
		RepairToolCall: func(ctx context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			repaired.Add(1)
			return nil, nil
		},
		ExperimentalRefineToolInput: map[string]ToolInputRefiner{
			"tool2": func(ctx context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
				refined.Add(1)
				return o.ToolCall.Arguments, nil
			},
		},
	})
	if !IsToolChoiceViolationError(err) {
		t.Fatalf("error = %v", err)
	}
	if repaired.Load() != 0 || refined.Load() != 0 {
		t.Fatalf("repair=%d refine=%d", repaired.Load(), refined.Load())
	}
}

func TestGenerateText_ToolChoiceNoneAllowsTextAndKeepsToolDefinitions(t *testing.T) {
	t.Parallel()

	var sawTools int
	var sawChoice types.ToolChoiceType
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			sawTools, sawChoice = len(opts.Tools), opts.ToolChoice.Type
			return &types.GenerateResult{Text: "No tool call.", FinishReason: types.FinishReasonStop}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:      model,
		Prompt:     "test-input",
		Tools:      []types.Tool{{Name: "tool1"}},
		ToolChoice: types.NoneToolChoice(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "No tool call." || sawTools != 1 || sawChoice != types.ToolChoiceNone {
		t.Fatalf("text=%q tools=%d choice=%q", result.Text, sawTools, sawChoice)
	}
}

func noneToolStreamModel(finish types.FinishReason) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			call := &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1"}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: "{}"},
				{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: call},
				{Type: provider.ChunkTypeFinish, FinishReason: finish, Usage: &types.Usage{}},
			}), nil
		},
	}
}

func TestStreamText_ToolChoiceNoneRejectsAndWithholdsToolParts(t *testing.T) {
	t.Parallel()

	for _, finish := range []types.FinishReason{types.FinishReasonStop, types.FinishReasonToolCalls, types.FinishReasonInterrupted} {
		finish := finish
		t.Run(string(finish), func(t *testing.T) {
			t.Parallel()
			probe := &noneToolProbe{}
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:      noneToolStreamModel(finish),
				Prompt:     "test-input",
				Tools:      []types.Tool{probe.tool()},
				ToolChoice: types.NoneToolChoice(),
			})
			if err != nil {
				t.Fatal(err)
			}
			var sawToolPart bool
			stream := result.NewStream()
			for {
				chunk, nextErr := stream.Next()
				if nextErr != nil {
					break
				}
				switch chunk.Type {
				case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeToolResult:
					sawToolPart = true
				}
			}
			_, readErr := result.ReadAll()
			var violation *ToolChoiceViolationError
			if !errors.As(readErr, &violation) || violation.Error() != toolChoiceNoneMessage || violation.FinishReason != finish {
				t.Fatalf("error = %v", readErr)
			}
			if sawToolPart {
				t.Fatal("tool parts must be withheld with tool choice none")
			}
			probe.assertIdle(t)
			if result.FinishReason() != types.FinishReasonError {
				t.Fatalf("finish reason = %q", result.FinishReason())
			}
		})
	}
}

func TestStreamText_ToolChoiceNonePrepareStepAutoAllowsToolCalls(t *testing.T) {
	t.Parallel()

	probe := &noneToolProbe{}
	maxSteps := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      noneToolStreamModel(types.FinishReasonToolCalls),
		Prompt:     "test-input",
		Tools:      []types.Tool{probe.tool()},
		ToolChoice: types.NoneToolChoice(),
		MaxSteps:   &maxSteps,
		PrepareStep: func(ctx context.Context, o PrepareStepOptions) PrepareStepOptions {
			o.ToolChoice = types.AutoToolChoice()
			return o
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if probe.executed.Load() != 1 || probe.inputAvailable.Load() != 1 {
		t.Fatalf("executed=%d inputAvailable=%d", probe.executed.Load(), probe.inputAvailable.Load())
	}
}
