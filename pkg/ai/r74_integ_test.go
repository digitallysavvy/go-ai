package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r74integUsageSteps() []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: r71streamInt64(3), OutputTokens: r71streamInt64(10)}},
	}
}

// r74 gen->stream CR-2: TS asLanguageModelUsage fills totalTokens = input +
// output for every model response, in streamText too.
func TestR74Integ_StreamUsageTotalTokensFilled(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r74integUsageSteps()), Prompt: "test-input"})
	if got := r.Steps()[0].Usage.TotalTokens; got == nil || *got != 13 {
		t.Fatalf("step total tokens = %v, want 13", got)
	}
	if got := r.TotalUsage().TotalTokens; got == nil || *got != 13 {
		t.Fatalf("total usage total tokens = %v, want 13", got)
	}
	if got := r.Usage().TotalTokens; got == nil || *got != 13 {
		t.Fatalf("usage total tokens = %v, want 13", got)
	}

	// A provider-set total is kept.
	steps := r74integUsageSteps()
	steps[len(steps)-1].Usage.TotalTokens = r71streamInt64(20)
	r = r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input"})
	if got := r.TotalUsage().TotalTokens; got == nil || *got != 20 {
		t.Fatalf("provider total tokens = %v, want 20", got)
	}
}

// The hand-built single-step read (readAllLegacy) fills it the same way.
func TestR74Integ_LegacyReadUsageTotalTokensFilled(t *testing.T) {
	legacy := &StreamTextResult{stream: testutil.NewMockTextStream(r74integUsageSteps())}
	if _, err := legacy.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := legacy.Usage().TotalTokens; got == nil || *got != 13 {
		t.Fatalf("legacy usage total tokens = %v, want 13", got)
	}
}
