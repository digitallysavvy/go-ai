package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText: when a stream retry's model call fails to open, the step's
// stream enqueues an `error` part with that error and closes, so the step ends
// with finish-step (finish reason error) and the call with finish, and
// onError sees both errors. Go ended the run with no error part, step or
// finish.
func TestR59StreamRetryReopenFailureIsAnErrorPart(t *testing.T) {
	calls := 0
	retries := 1
	var mu sync.Mutex
	var errs []string
	var stepEnds int
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:        "a",
		StreamRetries: &retries,
		OnError: func(_ context.Context, e error) {
			mu.Lock()
			errs = append(errs, e.Error())
			mu.Unlock()
		},
		OnStepFinish: func(context.Context, types.StepResult, interface{}) {
			mu.Lock()
			stepEnds++
			mu.Unlock()
		},
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "t"},
					{Type: provider.ChunkTypeText, ID: "t", Text: "hi"},
					{Type: provider.ChunkTypeError, Text: "transient"},
				}), nil
			}
			return nil, errors.New("reopen failed")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts, chunks := r58FullStreamParts(t, res)
	want := "start start-step text-start text text-end error finish-step finish"
	if parts != want {
		t.Fatalf("full stream = %q, want %q", parts, want)
	}
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError && c.Text != "reopen failed" {
			t.Fatalf("error part = %q, want the reopen error", c.Text)
		}
	}
	if fr := res.FinishReason(); fr != types.FinishReasonError {
		t.Fatalf("finish reason = %q, want error", fr)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(errs, ",") != "transient,reopen failed" {
		t.Fatalf("OnError = %q, want transient then the reopen error", errs)
	}
	if stepEnds != 1 {
		t.Fatalf("OnStepFinish calls = %d, want 1", stepEnds)
	}
}
