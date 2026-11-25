package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type errChunkModel struct{ wfMockModel }

func (m *errChunkModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeError, Text: "provider exploded"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError},
	}), nil
}

// TS WorkflowAgent.stream calls onError for errors raised while the stream is
// being consumed, not only for failures before it starts.
func TestWorkflowStreamOnErrorFiresForMidStreamErrors(t *testing.T) {
	var mu sync.Mutex
	var agentErrs, callErrs int
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &errChunkModel{},
		OnError: func(context.Context, error) {
			mu.Lock()
			agentErrs++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Prompt: "hi",
		OnError: func(context.Context, error) {
			mu.Lock()
			callErrs++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	if agentErrs != 1 || callErrs != 1 {
		t.Fatalf("onError calls agent=%d call=%d, want 1/1", agentErrs, callErrs)
	}
}
