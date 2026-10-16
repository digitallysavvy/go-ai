package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A provider error while opening the first step's stream must stay that error
// when a per-step timeout is configured and has not elapsed: it is reported to
// OnError, not relabelled a step timeout or treated as an abort.
func TestStreamTextFirstStepOpenErrorWithStepTimeoutIsNotTimeout(t *testing.T) {
	stepTimeout := time.Minute
	boom := errors.New("boom")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, boom
		},
	}
	zero := 0
	var mu sync.Mutex
	var onErr error
	aborted := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:      model,
		Prompt:     "test",
		MaxRetries: &zero,
		Timeout:    &TimeoutConfig{PerStep: &stepTimeout},
		OnError: func(_ context.Context, e error) {
			mu.Lock()
			onErr = e
			mu.Unlock()
		},
		OnAbort: func(context.Context, []types.StepResult) {
			mu.Lock()
			aborted = true
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, err = result.ReadAll()
	if !errors.Is(err, boom) {
		t.Fatalf("ReadAll() error = %v, want boom", err)
	}
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("error relabelled as timeout: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if aborted {
		t.Fatal("OnAbort fired for a provider error")
	}
	if !errors.Is(onErr, boom) {
		t.Fatalf("OnError = %v, want boom", onErr)
	}
}
