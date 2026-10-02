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
				StopWhen:    []StopCondition{IsLoopFinished()},
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

// HANDOFF.md item 7: InstructionMessages' per-message ProviderOptions must
// reach provider.GenerateOptions.Prompt.Messages unchanged, for both
// GenerateText and StreamText.
func TestGenerateTextInstructionMessagesProviderOptionsReachPrompt(t *testing.T) {
	t.Parallel()
	providerOpts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}

	var captured map[string]interface{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, m := range opts.Prompt.Messages {
				if m.Role == types.RoleSystem {
					captured = m.ProviderOptions
				}
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "instructions"}}, ProviderOptions: providerOpts},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if captured == nil || captured["anthropic"] == nil {
		t.Fatalf("expected ProviderOptions to reach the prompt, got %+v", captured)
	}
}

func TestStreamTextInstructionMessagesProviderOptionsReachPrompt(t *testing.T) {
	t.Parallel()
	providerOpts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}

	var captured map[string]interface{}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			for _, m := range opts.Prompt.Messages {
				if m.Role == types.RoleSystem {
					captured = m.ProviderOptions
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
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "instructions"}}, ProviderOptions: providerOpts},
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if captured == nil || captured["anthropic"] == nil {
		t.Fatalf("expected ProviderOptions to reach the prompt, got %+v", captured)
	}
}

// TestStreamTextInvalidToolCallErrorChunkOrder pins down the exact chunk
// order for an invalid (unrepairable — no RepairToolCall configured) tool
// call streamed alongside a valid one, per review MUST-CHECK item 1: TS's
// run-tools-transformation forwards every tool-call part as it streams
// (invalid ones included, since the client needs to render them), then
// executes/synthesizes results for the whole step only once that step's
// provider stream ends — never after later steps' chunks. This asserts Go
// matches that: both tool-call chunks arrive first, then both tool-result
// chunks (the invalid call's synthesized error and the valid call's real
// result, still within step 1), then step 1's own finish-step chunk (mirrors
// TS's finish-step being enqueued in flush(), after every other part of the
// step, stream-text.ts:3020-3033), and only then does step 2's own output
// begin.
func TestStreamTextInvalidToolCallErrorChunkOrder(t *testing.T) {
	t.Parallel()

	step := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			step++
			if step == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "bogus-1", ToolName: "bogus", RawArguments: `{}`}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "valid-1", ToolName: "testTool", RawArguments: `{"value":"x"}`}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := pipelineTestTool("testTool", func(map[string]interface{}) (interface{}, error) {
		return "ok", nil
	})

	result, err := StreamText(context.Background(), StreamTextOptions{
		StopWhen: []StopCondition{IsLoopFinished()},
		Model:    model,
		Prompt:   "hi",
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	type seen struct {
		typ       provider.ChunkType
		toolName  string
		hasError  bool
		finishRsn types.FinishReason
	}
	var order []seen
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		s := seen{typ: chunk.Type}
		if chunk.ToolCall != nil {
			s.toolName = chunk.ToolCall.ToolName
		}
		if chunk.ToolResult != nil {
			s.toolName = chunk.ToolResult.ToolName
			s.hasError = chunk.ToolResult.Error != nil
		}
		if chunk.Type == provider.ChunkTypeFinish || chunk.Type == provider.ChunkTypeFinishStep {
			s.finishRsn = chunk.FinishReason
		}
		order = append(order, s)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	// Find the indices of the two tool-call chunks, the two tool-result
	// chunks, the tool-calls finish chunk, and step 2's first text chunk.
	idx := func(pred func(seen) bool) int {
		for i, s := range order {
			if pred(s) {
				return i
			}
		}
		return -1
	}
	bogusCall := idx(func(s seen) bool { return s.typ == provider.ChunkTypeToolCall && s.toolName == "bogus" })
	validCall := idx(func(s seen) bool { return s.typ == provider.ChunkTypeToolCall && s.toolName == "testTool" })
	toolCallsFinish := idx(func(s seen) bool {
		return s.typ == provider.ChunkTypeFinishStep && s.finishRsn == types.FinishReasonToolCalls
	})
	bogusResult := idx(func(s seen) bool { return s.typ == provider.ChunkTypeToolResult && s.toolName == "bogus" })
	validResult := idx(func(s seen) bool {
		return s.typ == provider.ChunkTypeToolResult && s.toolName == "testTool" && !s.hasError
	})
	step2Text := idx(func(s seen) bool { return s.typ == provider.ChunkTypeText })

	if bogusCall < 0 || validCall < 0 || toolCallsFinish < 0 || bogusResult < 0 || validResult < 0 || step2Text < 0 {
		t.Fatalf("missing expected chunk(s), got order = %+v", order)
	}
	if bogusCall >= validCall || validCall >= bogusResult {
		t.Fatalf("expected both tool-call chunks before the tool-result chunks, got order = %+v", order)
	}
	if !order[bogusResult].hasError {
		t.Fatalf("expected the invalid call's tool-result chunk to carry an error, got %+v", order[bogusResult])
	}
	if bogusResult >= toolCallsFinish || validResult >= toolCallsFinish {
		t.Fatalf("expected both tool-result chunks before step 1's finish-step chunk, got order = %+v", order)
	}
	if bogusResult >= step2Text || validResult >= step2Text {
		t.Fatalf("expected step 1's tool-result chunks (including the invalid call's synthesized error) before step 2's text — invalid calls must not be deferred past the stream, got order = %+v", order)
	}
}
