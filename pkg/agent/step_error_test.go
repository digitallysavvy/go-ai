package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS #23758: ToolLoopAgent onStepError and onError.

func TestToolLoopAgentGenerateReportsStepErrorAndError(t *testing.T) {
	boom := errors.New("provider failed")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, boom
		},
	}
	var mu sync.Mutex
	var order []string
	var stepErr, callErr error
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		OnStepError: func(ctx context.Context, e ai.OnStepErrorEvent) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "step")
			stepErr = e.Error
		},
		ErrorHandler: func(ctx context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "error")
			callErr = err
		},
	})
	_, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(order) != 2 || order[0] != "step" || order[1] != "error" || !errors.Is(stepErr, boom) || !errors.Is(callErr, boom) {
		t.Fatalf("order=%v stepErr=%v callErr=%v", order, stepErr, callErr)
	}
}

func TestToolLoopAgentGenerateDoesNotReportAbortToErrorHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			cancel()
			return nil, c.Err()
		},
	}
	var stepErr error
	called := false
	a := NewToolLoopAgent(AgentConfig{
		Model:        model,
		OnStepError:  func(ctx context.Context, e ai.OnStepErrorEvent) { stepErr = e.Error },
		ErrorHandler: func(ctx context.Context, err error) { called = true },
	})
	if _, err := a.Generate(ctx, AgentGenerateOptions{Prompt: "hi"}); err == nil {
		t.Fatal("expected an error")
	}
	if called || !errors.Is(stepErr, context.Canceled) {
		t.Fatalf("errorHandler=%v stepErr=%v", called, stepErr)
	}
}

func TestToolLoopAgentStreamReportsStepErrorAndMergesErrorHandlers(t *testing.T) {
	boom := errors.New("provider failed")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, boom
		},
	}
	var mu sync.Mutex
	var order []string
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		OnStepError: func(ctx context.Context, e ai.OnStepErrorEvent) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "step")
		},
		ErrorHandler: func(ctx context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "config")
		},
	})
	result, err := a.Stream(context.Background(), AgentStreamOptions{
		AgentGenerateOptions: AgentGenerateOptions{Prompt: "hi"},
		OnError: func(ctx context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "call")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "step" || order[1] != "config" || order[2] != "call" {
		t.Fatalf("order = %v", order)
	}
}
