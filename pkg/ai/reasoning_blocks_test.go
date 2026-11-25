package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Adjacent reasoning blocks stay separate parts, and the metadata carried by
// a block's start (redactedData) or end chunk lands on that block (TS keys
// reasoning records by id).
func TestStreamText_ReasoningBlocksStaySeparate(t *testing.T) {
	t.Parallel()
	sig := json.RawMessage(`{"anthropic":{"signature":"SIG"}}`)
	redacted := json.RawMessage(`{"anthropic":{"redactedData":"RED"}}`)
	endMeta := json.RawMessage(`{"x":{"endData":"E"}}`)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "0"},
				{Type: provider.ChunkTypeReasoning, ID: "0", Reasoning: "think"},
				{Type: provider.ChunkTypeReasoning, ID: "0", ProviderMetadata: sig},
				{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
				{Type: provider.ChunkTypeReasoningStart, ID: "1", ProviderMetadata: redacted},
				{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
				{Type: provider.ChunkTypeReasoningStart, ID: "2"},
				{Type: provider.ChunkTypeReasoning, ID: "2", Reasoning: "more"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "2", ProviderMetadata: endMeta},
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	var parts []types.ReasoningContent
	for _, c := range steps[0].Content {
		if rc, ok := c.(types.ReasoningContent); ok {
			parts = append(parts, rc)
		}
	}
	if len(parts) != 3 {
		t.Fatalf("reasoning parts = %#v, want 3", parts)
	}
	if parts[0].Text != "think" || string(parts[0].ProviderMetadata) != string(sig) {
		t.Errorf("part 0 = %#v", parts[0])
	}
	if parts[1].Text != "" || string(parts[1].ProviderMetadata) != string(redacted) {
		t.Errorf("part 1 = %#v", parts[1])
	}
	if parts[2].Text != "more" || string(parts[2].ProviderMetadata) != string(endMeta) {
		t.Errorf("part 2 = %#v", parts[2])
	}
	if len(steps[0].Reasoning) != 3 {
		t.Errorf("step.Reasoning = %#v, want 3 parts", steps[0].Reasoning)
	}
}
