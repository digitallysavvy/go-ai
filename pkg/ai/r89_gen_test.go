package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 89 gen tests (TS 382d0b8eaf).

// r89genStreamObjectStepEnd runs StreamObject over chunks and returns the
// step end event.
func r89genStreamObjectStepEnd(t *testing.T, chunks []provider.StreamChunk) ObjectOnStepFinishEvent {
	t.Helper()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}
	var mu sync.Mutex
	var got ObjectOnStepFinishEvent
	called := false
	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "prompt",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			got, called = e, true
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("OnStepEnd was not called")
	}
	return got
}

// TS GenerateObjectStepEndEvent.msToFirstChunk (structured-output-events.ts
// 157-158): streamObject's step end event carries the time from the start of
// the stream to the first chunk (stream-object.ts 657, 703-706, 875);
// generateObject passes undefined (generate-object.ts 467).
func TestR89genObjectStepEndMsToFirstChunk(t *testing.T) {
	t.Run("streamObject", func(t *testing.T) {
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: `{"x":1}`},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		if got.MsToFirstChunk == nil || *got.MsToFirstChunk < 0 {
			t.Fatalf("MsToFirstChunk = %v, want a duration", got.MsToFirstChunk)
		}
	})
	t.Run("streamObject without chunks after stream-start", func(t *testing.T) {
		// TS isFirstChunk is set only by a part that passes the stream-start
		// check, so a stream of stream-start alone leaves it undefined.
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
		})
		if got.MsToFirstChunk != nil {
			t.Fatalf("MsToFirstChunk = %d, want nil", *got.MsToFirstChunk)
		}
	})
	t.Run("streamObject ignores parts TS drops before timing", func(t *testing.T) {
		// TS's first transformer forwards only text-delta, response-metadata,
		// finish, error and stream-start, so a text-start or reasoning part
		// does not start the clock; the stream here has none of the timed
		// parts.
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeReasoning, Reasoning: "think"},
		})
		if got.MsToFirstChunk != nil {
			t.Fatalf("MsToFirstChunk = %d, want nil", *got.MsToFirstChunk)
		}
	})
	t.Run("generateObject", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:         `{"x":1}`,
					Content:      []types.ContentPart{types.TextContent{Text: `{"x":1}`}},
					FinishReason: types.FinishReasonStop,
				}, nil
			},
		}
		var mu sync.Mutex
		var got ObjectOnStepFinishEvent
		called := false
		if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model:  model,
			Prompt: "prompt",
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
			OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
				mu.Lock()
				defer mu.Unlock()
				got, called = e, true
			},
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if !called || got.MsToFirstChunk != nil {
			t.Fatalf("called = %v, MsToFirstChunk = %v, want nil", called, got.MsToFirstChunk)
		}
	})
}
