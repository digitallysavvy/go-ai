package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type blockUntilDoneModel struct{ wfMockModel }

func (m *blockUntilDoneModel) DoGenerate(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *blockUntilDoneModel) DoStream(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TS workflow-agent.test.ts "should classify timeout errors as aborts" and
// "aborted model step without calling error": a timeout or cancellation fires
// onAbort and never onError, for generate and stream.
func TestWorkflowTimeoutAndCancelAreAbortsNotErrors(t *testing.T) {
	d := 30 * time.Millisecond
	var mu sync.Mutex
	var aborts, errs int
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model:   &blockUntilDoneModel{},
		Timeout: &ai.TimeoutConfig{Total: &d},
		OnAbort: func(context.Context, []types.StepResult) { mu.Lock(); aborts++; mu.Unlock() },
		OnError: func(context.Context, error) { mu.Lock(); errs++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if aborts != 1 || errs != 0 {
			t.Fatalf("%s: onAbort=%d onError=%d, want 1/0", name, aborts, errs)
		}
		aborts, errs = 0, 0
	}
	if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p"}); err == nil {
		t.Fatal("generate: want an error")
	}
	check("generate timeout")
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p"})
	if err == nil {
		_ = st.ConsumeStream()
	}
	check("stream timeout")

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	a.Timeout = nil
	if _, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "p"}); err == nil {
		t.Fatal("cancel: want an error")
	}
	check("generate cancel")
}
