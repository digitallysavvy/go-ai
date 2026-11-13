package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var errR95streamAbortReason = errors.New("r95stream: caller reason")

// r95streamBlockingTextStream returns its parts, then blocks until ctx ends
// (a provider stream waiting on its socket).
type r95streamBlockingTextStream struct {
	ctx   context.Context
	parts []provider.StreamChunk
	i     int
}

func (s *r95streamBlockingTextStream) Next() (*provider.StreamChunk, error) {
	if s.i < len(s.parts) {
		p := s.parts[s.i]
		s.i++
		return &p, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func (s *r95streamBlockingTextStream) Err() error   { return nil }
func (s *r95streamBlockingTextStream) Close() error { return nil }

// TestR95streamStreamTextAbortMidStreamReason: TS streamText cancelOnAbort
// rejects the result promises with abortSignal.reason whenever the signal
// fires, also while the model stream is being read (not only while tools run
// or during a step's end callbacks): Err() and OutputErr() report the cancel
// cause, not context.Canceled.
func TestR95streamStreamTextAbortMidStreamReason(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	model := &testutil.MockLanguageModel{DoStreamFunc: func(streamCtx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		return &r95streamBlockingTextStream{ctx: streamCtx, parts: []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "t"},
			{Type: provider.ChunkTypeText, ID: "t", Text: "hi"},
		}}, nil
	}}
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "x", OnChunk: func(c provider.StreamChunk) {
		if c.Type == provider.ChunkTypeText {
			cancel(errR95streamAbortReason)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Text()
	if !errors.Is(r.Err(), errR95streamAbortReason) {
		t.Fatalf("Err() = %v, want the abort reason", r.Err())
	}
	if !errors.Is(r.OutputErr(), errR95streamAbortReason) {
		t.Fatalf("OutputErr() = %v, want the abort reason", r.OutputErr())
	}
}

// TestR95streamStreamTextAbortBeforeOpenReason: a cancel with a cause before
// the first model call opens ends the run with the cause (TS cancelOnAbort in
// the outer stream's start), also without call retries (the model call's own
// context error is not the reason). A queued steering message is not
// delivered because the run was aborted (TS abort(): steeringRun.end('aborted')).
func TestR95streamStreamTextAbortBeforeOpenReason(t *testing.T) {
	for _, retries := range []*int{nil, r95streamInt(0)} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errR95streamAbortReason)
		model := &testutil.MockLanguageModel{DoStreamFunc: func(streamCtx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, streamCtx.Err()
		}}
		steering := NewSteering()
		queued := steering.Queue("later")
		r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "x", MaxRetries: retries, ExperimentalSteering: steering})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Text()
		if !errors.Is(r.Err(), errR95streamAbortReason) {
			t.Fatalf("maxRetries %v: Err() = %v, want the abort reason", retries, r.Err())
		}
		if !errors.Is(r.OutputErr(), errR95streamAbortReason) {
			t.Fatalf("maxRetries %v: OutputErr() = %v, want the abort reason", retries, r.OutputErr())
		}
		if got := queued.Outcome().Reason; got != SteeringAborted {
			t.Fatalf("maxRetries %v: steering reason = %q, want %q", retries, got, SteeringAborted)
		}
	}
}

// TestR95streamStreamTextAbortDuringToolInputReason: a cancel with a cause
// while a tool input callback runs ends the run with the cause.
func TestR95streamStreamTextAbortDuringToolInputReason(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	tool := types.Tool{
		Name:       "t",
		Parameters: map[string]interface{}{"type": "object"},
		OnInputAvailable: func(cbCtx context.Context, _ types.OnInputAvailableOptions) error {
			cancel(errR95streamAbortReason)
			<-cbCtx.Done()
			return cbCtx.Err()
		},
	}
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Text()
	if !errors.Is(r.Err(), errR95streamAbortReason) {
		t.Fatalf("Err() = %v, want the abort reason", r.Err())
	}
}

func r95streamInt(v int) *int { return &v }
