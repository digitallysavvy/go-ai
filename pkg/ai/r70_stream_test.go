package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText pipes every run through createOutputTransformStream(output ??
// text(), retention.replay || output != null): without an output spec the
// partial output is the step's text so far (text().parsePartialOutput), and a
// low-memory run without a spec publishes none.
func TestR70StreamTextPartialOutputWithoutSpecIsText(t *testing.T) {
	chunks := func() []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hel"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "lo"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}
	}
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream(chunks()), nil
	}}

	t.Run("default retention", func(t *testing.T) {
		r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != "Hello" {
			t.Fatalf("PartialOutput() = %#v, want %q", got, "Hello")
		}
	})

	t.Run("low memory", func(t *testing.T) {
		r, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if got := r.PartialOutput(); got != nil {
			t.Fatalf("low-memory PartialOutput() = %#v, want nil", got)
		}
	})
}
