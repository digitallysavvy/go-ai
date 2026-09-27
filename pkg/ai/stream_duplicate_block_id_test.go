package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamText_RemapsDuplicateTextAndReasoningIDsAcrossSteps verifies that
// when two steps of the same streamText call both use block ID "0" for a
// text (and reasoning) part — the common case, since most providers restart
// numbering every model call — the second step's chunks get a distinct ID
// instead of colliding with the first step's, in both the forwarded full
// stream (audit row c6d57f3 / WG5 #113) and the id staying consistent across
// a block's own start/delta/end chunks.
func TestStreamText_RemapsDuplicateTextAndReasoningIDsAcrossSteps(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningStart, ID: "0"},
					{Type: provider.ChunkTypeReasoning, ID: "0", Reasoning: "thinking-1"},
					{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
					{Type: provider.ChunkTypeTextStart, ID: "0"},
					{Type: provider.ChunkTypeText, ID: "0", Text: "step1"},
					{Type: provider.ChunkTypeTextEnd, ID: "0"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "noop",
						Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "0"},
				{Type: provider.ChunkTypeReasoning, ID: "0", Reasoning: "thinking-2"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
				{Type: provider.ChunkTypeTextStart, ID: "0"},
				{Type: provider.ChunkTypeText, ID: "0", Text: "step2"},
				{Type: provider.ChunkTypeTextEnd, ID: "0"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := types.Tool{
		Name: "noop",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	var mu sync.Mutex
	var textIDs, reasoningIDs []string
	var textDeltaIDs, textEndIDs, reasoningDeltaIDs, reasoningEndIDs []string

	maxSteps := 3
	sendReasoning := true
	done := make(chan struct{})
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		Tools:         []types.Tool{tool},
		MaxSteps:      &maxSteps,
		SendReasoning: &sendReasoning,
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			switch c.Type {
			case provider.ChunkTypeTextStart:
				textIDs = append(textIDs, c.ID)
			case provider.ChunkTypeText:
				textDeltaIDs = append(textDeltaIDs, c.ID)
			case provider.ChunkTypeTextEnd:
				textEndIDs = append(textEndIDs, c.ID)
			case provider.ChunkTypeReasoningStart:
				reasoningIDs = append(reasoningIDs, c.ID)
			case provider.ChunkTypeReasoning:
				reasoningDeltaIDs = append(reasoningDeltaIDs, c.ID)
			case provider.ChunkTypeReasoningEnd:
				reasoningEndIDs = append(reasoningEndIDs, c.ID)
			}
		},
		OnEnd: func(*StreamTextResult) { close(done) },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OnEnd")
	}
	mu.Lock()
	defer mu.Unlock()

	if len(textIDs) != 2 {
		t.Fatalf("expected 2 text-start chunks (one per step), got %v", textIDs)
	}
	if textIDs[0] == textIDs[1] {
		t.Fatalf("step 2's text ID (%q) must differ from step 1's (%q) when both providers reused \"0\"", textIDs[1], textIDs[0])
	}
	if len(reasoningIDs) != 2 {
		t.Fatalf("expected 2 reasoning-start chunks (one per step), got %v", reasoningIDs)
	}
	if reasoningIDs[0] == reasoningIDs[1] {
		t.Fatalf("step 2's reasoning ID (%q) must differ from step 1's (%q) when both providers reused \"0\"", reasoningIDs[1], reasoningIDs[0])
	}
	// The remapped ID must stay consistent across a block's own delta/end
	// chunks, not just its start chunk.
	if len(textDeltaIDs) != 2 || textDeltaIDs[0] != textIDs[0] || textDeltaIDs[1] != textIDs[1] {
		t.Fatalf("text-delta IDs %v must match text-start IDs %v", textDeltaIDs, textIDs)
	}
	if len(textEndIDs) != 2 || textEndIDs[0] != textIDs[0] || textEndIDs[1] != textIDs[1] {
		t.Fatalf("text-end IDs %v must match text-start IDs %v", textEndIDs, textIDs)
	}
	if len(reasoningDeltaIDs) != 2 || reasoningDeltaIDs[0] != reasoningIDs[0] || reasoningDeltaIDs[1] != reasoningIDs[1] {
		t.Fatalf("reasoning-delta IDs %v must match reasoning-start IDs %v", reasoningDeltaIDs, reasoningIDs)
	}
	if len(reasoningEndIDs) != 2 || reasoningEndIDs[0] != reasoningIDs[0] || reasoningEndIDs[1] != reasoningIDs[1] {
		t.Fatalf("reasoning-end IDs %v must match reasoning-start IDs %v", reasoningEndIDs, reasoningIDs)
	}
}

// TestRemapDuplicateBlockID_UsesFreshGeneratedID verifies the exact remap
// format TS's createPartIdReserver uses: on a collision, the replacement is
// a fresh ID from the call's ID generator (not a suffix of the ORIGINAL
// colliding ID, e.g. not "0-2"), with a numeric suffix appended to that
// fresh ID only if it, too, collides.
func TestRemapDuplicateBlockID_UsesFreshGeneratedID(t *testing.T) {
	t.Parallel()

	used := map[string]bool{"0": true}
	remap := map[string]string{}
	generateID := func() string { return "generated-id" }

	chunk := &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "0"}
	remapDuplicateBlockID(chunk, used, remap, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd, generateID)

	if chunk.ID != "generated-id" {
		t.Fatalf("chunk.ID = %q, want %q (a fresh generated ID, not a suffix of the original \"0\")", chunk.ID, "generated-id")
	}
	if remap["0"] != "generated-id" {
		t.Fatalf("remap[\"0\"] = %q, want %q", remap["0"], "generated-id")
	}
}

// TestRemapDuplicateBlockID_SuffixesGeneratedIDOnCollision verifies the
// fallback loop when the freshly generated ID ALSO collides: TS appends
// `-1`, `-2`, ... to the generated ID (not the original), incrementing until
// unique.
func TestRemapDuplicateBlockID_SuffixesGeneratedIDOnCollision(t *testing.T) {
	t.Parallel()

	used := map[string]bool{"0": true, "generated-id": true, "generated-id-1": true}
	remap := map[string]string{}
	generateID := func() string { return "generated-id" }

	chunk := &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "0"}
	remapDuplicateBlockID(chunk, used, remap, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd, generateID)

	if chunk.ID != "generated-id-2" {
		t.Fatalf("chunk.ID = %q, want %q", chunk.ID, "generated-id-2")
	}
}
