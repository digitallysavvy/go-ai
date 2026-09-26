package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// streamToolCallModel returns a mock model whose first DoStream call emits a
// tool-call chunk (optionally with a response-metadata chunk carrying a
// resolved model ID), and whose subsequent calls emit a plain text response.
// Mirrors toolCallModel (tool_call_pipeline_test.go) for the streaming path.
func streamToolCallModel(call types.ToolCall, responseModelID string) *testutil.MockLanguageModel {
	step := 0
	var mu sync.Mutex
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			step++
			if step == 1 {
				chunks := []provider.StreamChunk{}
				if responseModelID != "" {
					chunks = append(chunks, provider.StreamChunk{
						Type:             provider.ChunkTypeResponseMetadata,
						ResponseMetadata: &provider.ResponseMetadata{ID: "resp-1", ModelID: responseModelID},
					})
				}
				chunks = append(chunks,
					provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				)
				return testutil.NewMockTextStream(chunks), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

// TS parity: StreamText should run RepairToolCall the same way GenerateText
// does (see TestGenerateTextRepairToolCall), for both the stable field and
// its deprecated alias.
func TestStreamTextRepairToolCall(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		set  func(*StreamTextOptions, ToolCallRepairFunction)
	}{
		{"stable", func(o *StreamTextOptions, f ToolCallRepairFunction) { o.RepairToolCall = f }},
		{"deprecated alias", func(o *StreamTextOptions, f ToolCallRepairFunction) { o.ExperimentalRepairToolCall = f }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			var executed []map[string]interface{}
			tool := pipelineTestTool("testTool", func(args map[string]interface{}) (interface{}, error) {
				mu.Lock()
				executed = append(executed, args)
				mu.Unlock()
				return "ok", nil
			})

			model := streamToolCallModel(types.ToolCall{ID: "c1", ToolName: "testTool", RawArguments: `{"value":1}`}, "")

			var capturedInstructions string
			opts := StreamTextOptions{
				Model:  model,
				Prompt: "hi",
				System: "sys",
				Tools:  []types.Tool{tool},
			}
			tc.set(&opts, func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
				capturedInstructions = o.Instructions
				repaired := o.ToolCall
				repaired.RawArguments = `{"value":"fixed"}`
				return &repaired, nil
			})

			result, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatalf("StreamText() error = %v", err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}

			if capturedInstructions != "sys" {
				t.Fatalf("repair instructions = %q, want %q", capturedInstructions, "sys")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(executed) != 1 || executed[0]["value"] != "fixed" {
				t.Fatalf("executed = %v", executed)
			}
			calls := result.ToolCalls()
			if len(calls) != 1 || calls[0].Invalid {
				t.Fatalf("repaired call must be valid: %+v", calls)
			}
		})
	}
}

// TS parity: onLanguageModelCallStart / onLanguageModelCallEnd fire around
// every streamed step, in order relative to OnStepStart, and the end event
// carries the resolved (response) model ID with a fallback to the requested
// model ID for steps that don't report one — mirrors
// TestGenerateTextLanguageModelCallCallbacks for the streaming path.
func TestStreamTextLanguageModelCallCallbacks(t *testing.T) {
	t.Parallel()

	for _, useAlias := range []bool{false, true} {
		useAlias := useAlias
		t.Run(map[bool]string{false: "stable", true: "deprecated alias"}[useAlias], func(t *testing.T) {
			t.Parallel()

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
			model := streamToolCallModel(types.ToolCall{ID: "c1", ToolName: "testTool", RawArguments: `{"value":"a"}`}, "resolved-model")
			model.ModelName = "requested-model"

			startFn := func(_ context.Context, e LanguageModelCallStartEvent) {
				record("lmStart")
				mu.Lock()
				starts = append(starts, e)
				mu.Unlock()
			}
			endFn := func(_ context.Context, e LanguageModelCallEndEvent) {
				record("lmEnd")
				mu.Lock()
				ends = append(ends, e)
				mu.Unlock()
			}

			opts := StreamTextOptions{
				Model:       model,
				Prompt:      "hi",
				Tools:       []types.Tool{tool},
				Temperature: ptrFloat(0.5),
				OnStepStart: func(context.Context, OnStepStartEvent) { record("stepStart") },
			}
			if useAlias {
				opts.ExperimentalOnLanguageModelCallStart = startFn
				opts.ExperimentalOnLanguageModelCallEnd = endFn
			} else {
				opts.OnLanguageModelCallStart = startFn
				opts.OnLanguageModelCallEnd = endFn
			}

			result, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatalf("StreamText() error = %v", err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			want := "stepStart,lmStart,lmEnd,stepStart,lmStart,lmEnd"
			if got := strings.Join(order, ","); got != want {
				t.Fatalf("order = %s, want %s", got, want)
			}
			if len(starts) != 2 || len(ends) != 2 {
				t.Fatalf("expected 2 start/end events, got %d/%d", len(starts), len(ends))
			}
			if starts[0].ModelID != "requested-model" || starts[0].Temperature == nil || *starts[0].Temperature != 0.5 {
				t.Fatalf("start event = %+v", starts[0])
			}
			if ends[0].ModelID != "resolved-model" || ends[0].ResponseID != "resp-1" || ends[0].FinishReason != types.FinishReasonToolCalls {
				t.Fatalf("end event = %+v", ends[0])
			}
			if ends[1].ModelID != "requested-model" {
				t.Fatalf("second end event should fall back to the requested model: %+v", ends[1])
			}
		})
	}
}

// TS parity: StreamText's Instructions can be supplied as system messages
// (d775a57), which take precedence over the string System/Instructions
// fields and are prepended to the provider prompt as system messages.
func TestStreamTextInstructionMessages(t *testing.T) {
	t.Parallel()

	var capturedSystemTexts []string
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			for _, m := range opts.Prompt.Messages {
				if m.Role != types.RoleSystem {
					continue
				}
				for _, part := range m.Content {
					if tc, ok := part.(types.TextContent); ok {
						capturedSystemTexts = append(capturedSystemTexts, tc.Text)
					}
				}
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		// System is ignored because InstructionMessages takes precedence.
		System: "ignored-string-system",
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "message-based instructions"}}},
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	found := false
	for _, text := range capturedSystemTexts {
		if text == "message-based instructions" {
			found = true
		}
		if text == "ignored-string-system" {
			t.Fatalf("string System must not reach the provider when InstructionMessages is set: %v", capturedSystemTexts)
		}
	}
	if !found {
		t.Fatalf("expected InstructionMessages content in the provider prompt, got %v", capturedSystemTexts)
	}
}
