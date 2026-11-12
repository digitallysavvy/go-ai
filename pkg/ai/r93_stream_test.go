package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

var r93streamNumberArray = ArrayOutput[float64](ArrayOutputOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "number"})})

// r93streamExternal runs parts through NewStreamTextResultFromParts as a
// harness turn with output.
func r93streamExternal(parts []provider.StreamChunk, output interface{}, closesTurn bool) *StreamTextResult {
	return NewStreamTextResultFromParts(context.Background(), &r92streamParts{parts: parts}, ExternalStreamOptions{Output: output, TerminalFinishClosesTurn: closesTurn})
}

// TS harness-stream-text-result.ts elementStream pipes the turn's enriched
// parts (its createOutputTransformStream: the turn's first text block, not
// reset at step boundaries) through createElementStreamTransform, so the
// element stream follows the partial outputs the turn published.
func TestR93streamExternalElementStreamTurnFirstBlock(t *testing.T) {
	finishStep := provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	finish := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage}
	// One step: every element is streamed.
	r := r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "s"},
		{Type: provider.ChunkTypeText, ID: "s", Text: `{"elements":[1,`},
		{Type: provider.ChunkTypeText, ID: "s", Text: `2]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "s"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamJSON(r92streamElements(t, r, r93streamNumberArray)); got != `[1,2]` {
		t.Fatalf("one step: elements = %s", got)
	}
	// A tool step's text block comes first: the final step's JSON block is
	// not the turn's first text block, so TS publishes no partial and the
	// element stream is empty (as PartialOutput is).
	r = r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "a"},
		{Type: provider.ChunkTypeText, ID: "a", Text: "Let me check."},
		{Type: provider.ChunkTypeTextEnd, ID: "a"},
		finishStep,
		{Type: provider.ChunkTypeTextStart, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "b", Text: `{"elements":[1,2]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "b"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamElements(t, r, r93streamNumberArray); len(got) != 0 {
		t.Fatalf("two steps: elements = %v, want none", got)
	}
	if r.PartialOutput() != nil {
		t.Fatalf("two steps: partial = %#v, want none", r.PartialOutput())
	}
	// The JSON block of the first step keeps feeding the partial in a later
	// step (TS does not reset at start-step).
	r = r93streamExternal([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "s"},
		{Type: provider.ChunkTypeText, ID: "s", Text: `{"elements":[1,`},
		finishStep,
		{Type: provider.ChunkTypeText, ID: "s", Text: `2,3]}`},
		{Type: provider.ChunkTypeTextEnd, ID: "s"},
		finishStep, finish,
	}, r93streamNumberArray, true)
	if got := r92streamJSON(r92streamElements(t, r, r93streamNumberArray)); got != `[1,2,3]` {
		t.Fatalf("block across steps: elements = %s", got)
	}
}

// TS elementStream is its own tee of the result's stream: reading the full
// stream first does not take its parts away from a later element stream.
func TestR93streamElementStreamOwnCursor(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r92streamTextChunks(`{"elements":[1,2,3]}`)), Prompt: "numbers"})
	if err != nil {
		t.Fatal(err)
	}
	full := r91streamDrain(r.Stream())
	if len(full) == 0 {
		t.Fatal("empty full stream")
	}
	var got []float64
	for e := range ElementStream[float64](r, ElementStreamOptions[float64]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "number"})}) {
		got = append(got, e.Element)
	}
	if r92streamJSON(got) != `[1,2,3]` {
		t.Fatalf("elements after the full stream was read = %v", got)
	}
}
