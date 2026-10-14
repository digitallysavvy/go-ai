package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports TS stream-text-timeout.test.ts 'should clear output timers on
// <terminal> before slow stream processing' (TS #22341): once the model's
// response has ended, slow downstream processing must not trip chunkMs.
func TestStreamText_ChunkTimeoutClearedAfterModelFinishBeforeSlowProcessing(t *testing.T) {
	t.Parallel()

	perChunk := 40 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedTextStream{chunks: []delayedChunk{
				{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: "hi"}},
				{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}},
			}}, nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Timeout: &TimeoutConfig{PerChunk: &perChunk},
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeFinish {
				time.Sleep(120 * time.Millisecond) // slow consumer after the model ended
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	if result.Err() != nil {
		t.Fatalf("Err() = %v, want nil: chunk timeout must not fire after the model finished", result.Err())
	}
	if result.Text() != "hi" {
		t.Fatalf("Text() = %q", result.Text())
	}
}

// Ports 'should keep chunkMs cleared during local tool execution'.
func TestStreamText_ChunkTimeoutDoesNotFireDuringLocalToolExecution(t *testing.T) {
	t.Parallel()

	perChunk := 40 * time.Millisecond
	step := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			step++
			if step == 1 {
				return &delayedTextStream{chunks: []delayedChunk{
					{chunk: provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "slow", Arguments: map[string]interface{}{}}}},
					{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}},
				}}, nil
			}
			return &delayedTextStream{chunks: []delayedChunk{
				{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: "done"}},
				{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}},
			}}, nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "hi",
		Timeout:  &TimeoutConfig{PerChunk: &perChunk},
		StopWhen: []StopCondition{IsStepCount(2)},
		Tools: []types.Tool{{
			Name:       "slow",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				time.Sleep(150 * time.Millisecond)
				return "ok", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	if result.Err() != nil || result.Text() != "done" {
		t.Fatalf("Err() = %v, Text() = %q", result.Err(), result.Text())
	}
}
