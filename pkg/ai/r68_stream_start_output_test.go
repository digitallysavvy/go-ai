package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR68StreamTextStartEventCarriesOutput: TS streamText's start event
// carries the call's output specification (`output`), as the step start
// events do.
func TestR68StreamTextStartEventCarriesOutput(t *testing.T) {
	output := TextOutput()
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var got []OnStartEvent
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "hi", Output: output,
		OnStart: func(_ context.Context, e OnStartEvent) { got = append(got, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d start events, want 1", len(got))
	}
	if got[0].Output != output {
		t.Fatalf("start event Output = %#v, want the call's output", got[0].Output)
	}
}
