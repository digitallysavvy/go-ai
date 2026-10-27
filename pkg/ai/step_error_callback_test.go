package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS #23758 onStepError.

type stepLog struct {
	mu     sync.Mutex
	events []string
	errs   []OnStepErrorEvent
}

func (l *stepLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, s)
}

func twoStepTools() []types.Tool {
	return []types.Tool{{
		Name: "tool1",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
}

func TestGenerateText_OnStepErrorFiresForFailedStep(t *testing.T) {
	t.Parallel()

	boom := errors.New("provider failed")
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
				}, nil
			}
			return nil, boom
		},
	}
	log := &stepLog{}
	maxSteps := 3
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "go",
		Tools:       twoStepTools(),
		MaxSteps:    &maxSteps,
		OnStepStart: func(ctx context.Context, e OnStepStartEvent) { log.add("start") },
		OnStepEnd:   func(ctx context.Context, s types.StepResult, uc interface{}) { log.add("end") },
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) {
			log.add("error")
			log.mu.Lock()
			log.errs = append(log.errs, e)
			log.mu.Unlock()
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := log.events; len(got) != 4 || got[0] != "start" || got[1] != "end" || got[2] != "start" || got[3] != "error" {
		t.Fatalf("events = %v", got)
	}
	if e := log.errs[0]; e.StepNumber != 1 || !errors.Is(e.Error, boom) || e.CallID == "" || e.ModelID == "" {
		t.Fatalf("event = %+v", e)
	}
}

func TestGenerateText_OnStepErrorNotCalledOnSuccess(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	called := false
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "go",
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) { called = true },
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("OnStepError must not fire on success")
	}
}

func TestGenerateText_OnStepErrorFiresOnAbort(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(c context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			cancel()
			return nil, c.Err()
		},
	}
	var got error
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model:       model,
		Prompt:      "go",
		OnStepError: func(c context.Context, e OnStepErrorEvent) { got = e.Error },
	})
	if err == nil || !errors.Is(got, context.Canceled) {
		t.Fatalf("err=%v stepErr=%v", err, got)
	}
}

func TestStreamText_OnStepErrorFiresForEmptyStream(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(nil), nil
		},
	}
	log := &stepLog{}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:       model,
		Prompt:      "go",
		OnStepStart: func(ctx context.Context, e OnStepStartEvent) { log.add("start") },
		OnStepEnd:   func(ctx context.Context, s types.StepResult, uc interface{}) { log.add("end") },
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) {
			log.add("error")
			log.mu.Lock()
			log.errs = append(log.errs, e)
			log.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	if got := log.events; len(got) != 2 || got[0] != "start" || got[1] != "error" {
		t.Fatalf("events = %v", got)
	}
	if !IsNoOutputGeneratedError(log.errs[0].Error) || log.errs[0].StepNumber != 0 {
		t.Fatalf("event = %+v", log.errs[0])
	}
}

func TestStreamText_OnStepErrorFiresForContinuationOpenFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("second request failed")
	calls := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls == 2 {
				return nil, boom
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
			}), nil
		},
	}
	log := &stepLog{}
	maxSteps := 3
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:       model,
		Prompt:      "go",
		Tools:       twoStepTools(),
		MaxSteps:    &maxSteps,
		OnStepStart: func(ctx context.Context, e OnStepStartEvent) { log.add("start") },
		OnStepEnd:   func(ctx context.Context, s types.StepResult, uc interface{}) { log.add("end") },
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) {
			log.add("error")
			log.mu.Lock()
			log.errs = append(log.errs, e)
			log.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	if got := log.events; len(got) != 4 || got[2] != "start" || got[3] != "error" {
		t.Fatalf("events = %v", got)
	}
	if log.errs[0].StepNumber != 1 || !errors.Is(log.errs[0].Error, boom) {
		t.Fatalf("event = %+v", log.errs[0])
	}
}

func TestStreamText_OnStepErrorNotCalledOnSuccessOrAbort(t *testing.T) {
	t.Parallel()

	ok := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
			}), nil
		},
	}
	called := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:       ok,
		Prompt:      "go",
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) { called = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called = false
	result, err = StreamText(ctx, StreamTextOptions{
		Model:       ok,
		Prompt:      "go",
		OnStepError: func(ctx context.Context, e OnStepErrorEvent) { called = true },
	})
	if err == nil {
		_, _ = result.ReadAll()
	}
	if called {
		t.Fatal("OnStepError must not fire for an abort")
	}
}
