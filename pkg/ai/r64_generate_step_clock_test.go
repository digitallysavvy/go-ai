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

// TS generateText takes stepStartTimestampMs once, after onStepStart and just
// before onLanguageModelCallStart, and measures both responseTimeMs and
// stepTimeMs from it. A slow PrepareStep is in neither; a slow
// OnLanguageModelCallStart is in both.
func TestR64GenerateTextStepClockStartsBeforeLanguageModelCallStart(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	var mu sync.Mutex
	var callEnd int64
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Prompt: "a",
		Model:  model,
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			time.Sleep(150 * time.Millisecond)
			return PrepareStepOptions{}
		},
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) {
			time.Sleep(80 * time.Millisecond)
		},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			callEnd = e.Performance.ResponseTimeMs
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Steps[0].Performance
	if p.ResponseTimeMs < 80 {
		t.Fatalf("ResponseTimeMs = %d, want it to include the 80ms OnLanguageModelCallStart", p.ResponseTimeMs)
	}
	if p.StepTimeMs < 80 || p.StepTimeMs >= 150 {
		t.Fatalf("StepTimeMs = %d, want the callback (>= 80ms) without the 150ms PrepareStep", p.StepTimeMs)
	}
	mu.Lock()
	defer mu.Unlock()
	if callEnd < 80 {
		t.Fatalf("OnLanguageModelCallEnd ResponseTimeMs = %d, want it to include the 80ms OnLanguageModelCallStart", callEnd)
	}
}
