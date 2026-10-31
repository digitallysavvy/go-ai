package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r56 integrator: plain StreamText fires no end event for a failed run, as TS
// streamText; only InternalOptions.EndOnError (WorkflowAgent) asks for one.
func TestR56StreamTextFailedRunFiresNoEndEvent(t *testing.T) {
	for _, endOnError := range []bool{false, true} {
		var mu sync.Mutex
		ends := 0
		zero := 0
		opts := StreamTextOptions{
			Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return nil, errors.New("boom")
			}},
			Prompt:     "hi",
			MaxRetries: &zero,
			OnEndEvent: func(context.Context, OnFinishEvent) { mu.Lock(); ends++; mu.Unlock() },
		}
		if endOnError {
			opts.Internal = &InternalOptions{EndOnError: true}
		}
		res, err := StreamText(context.Background(), opts)
		if err == nil {
			_ = res.ConsumeStream()
		}
		mu.Lock()
		got := ends
		mu.Unlock()
		want := 0
		if endOnError {
			want = 1
		}
		if got != want {
			t.Fatalf("EndOnError=%v: end events = %d, want %d", endOnError, got, want)
		}
	}
}
