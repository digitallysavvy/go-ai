package ai

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// These tests exercise the StreamText/bootstrapAndStream split: StreamText()
// must return almost immediately, matching TS streamText() (a plain
// synchronous function — packages/ai/src/generate-text/stream-text.ts —
// that hands back a result before any I/O). Everything that can block on
// network or tool execution (resuming approved tool calls, the first
// provider stream request) now runs in the background, so these cover the
// concurrency review explicitly asked for: an early Close(), a consumer
// that abandons Stream() mid-way, and the total-timeout context's lifetime.

// TestStreamText_ReturnsBeforeFirstProviderRequestCompletes is the direct
// regression test for review finding F6 (approval resume / the first model
// request must not run before StreamText returns): a slow DoStream call must
// not block the caller from getting a *StreamTextResult back.
func TestStreamText_ReturnsBeforeFirstProviderRequestCompletes(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			<-release
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	start := time.Now()
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("StreamText() took %v to return; it must not block on the provider request", elapsed)
	}

	close(release)
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if result.Text() != "ok" {
		t.Fatalf("Text() = %q, want %q", result.Text(), "ok")
	}
}

// TestStreamText_ReturnsBeforeSlowResumedToolExecution is the same
// regression for the resume-approvals path specifically: a slow approved
// tool call must not block StreamText() either.
func TestStreamText_ReturnsBeforeSlowResumedToolExecution(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var executed int32
	tool := types.Tool{
		Name:         "slowTool",
		Parameters:   valueStringSchema(),
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			<-release
			atomic.AddInt32(&executed, 1)
			return "done", nil
		},
	}

	start := time.Now()
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    newRecordingModel("hello"),
		Tools:    []types.Tool{tool},
		Messages: approvalHistory("slowTool", map[string]interface{}{"value": "v"}, "", true, ""),
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("StreamText() took %v to return; it must not block on resumed tool execution", elapsed)
	}
	if atomic.LoadInt32(&executed) != 0 {
		t.Fatal("tool executed before StreamText() returned — resume ran synchronously")
	}

	close(release)
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}
}

// TestStreamText_CloseInterruptsInFlightBootstrap checks that Close(),
// called immediately after StreamText returns (before bootstrapAndStream has
// obtained a stream), promptly cancels the in-flight first provider request
// instead of hanging or leaking the goroutine, and that Err() surfaces the
// resulting cancellation instead of the caller blocking forever.
func TestStreamText_CloseInterruptsInFlightBootstrap(t *testing.T) {
	t.Parallel()

	unblocked := make(chan struct{})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			<-ctx.Done()
			close(unblocked)
			return nil, ctx.Err()
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	closeStart := time.Now()
	if err := result.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(closeStart); elapsed > 200*time.Millisecond {
		t.Fatalf("Close() took %v, want near-instant return", elapsed)
	}

	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not cancel the in-flight provider request")
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := result.ReadAll()
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected the cancelled bootstrap to surface an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadAll() hung after Close() cancelled bootstrap")
	}
}

// TestStreamText_ConsumerAbandonsStreamStillCompletes checks that
// processStream (running via bootstrapAndStream, in the background) still
// runs to completion — closing processingDone and settling all metadata —
// even when a consumer reads part of Stream() and then stops without
// draining or closing it. chunkBuffer.push() never blocks on a lagging
// reader, so this must not deadlock.
func TestStreamText_ConsumerAbandonsStreamStillCompletes(t *testing.T) {
	t.Parallel()

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "a"},
					{Type: provider.ChunkTypeText, Text: "b"},
					{Type: provider.ChunkTypeText, Text: "c"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		},
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read exactly one chunk, then abandon the stream entirely.
	stream := result.Stream()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = result.ReadAll()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReadAll() hung after the stream consumer abandoned Stream() mid-way")
	}
	if result.Text() != "abc" {
		t.Fatalf("Text() = %q, want %q", result.Text(), "abc")
	}
}

// TestStreamText_TotalTimeoutSurvivesAcrossReturn is a regression test: the
// total-timeout context's cancel must be scoped to bootstrapAndStream's
// goroutine lifetime, not to the StreamText() call's own stack frame. Before
// that fix, `defer cancel()` fired as soon as StreamText returned (which now
// happens almost immediately), cancelling the very context the first
// provider request was using and aborting the stream right after it started.
func TestStreamText_TotalTimeoutSurvivesAcrossReturn(t *testing.T) {
	t.Parallel()

	total := 500 * time.Millisecond
	chunkDelay := 20 * time.Millisecond
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				return &delayedChunksTextStream{
					delay: chunkDelay,
					chunks: []provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: "a"},
						{Type: provider.ChunkTypeText, Text: "b"},
						{Type: provider.ChunkTypeText, Text: "c"},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
					},
				}, nil
			},
		},
		Prompt:  "hi",
		Timeout: &TimeoutConfig{Total: &total},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v, want the stream to complete well within the total timeout", err)
	}
	if result.Text() != "abc" {
		t.Fatalf("Text() = %q, want %q", result.Text(), "abc")
	}
}

// delayedChunksTextStream yields each chunk after a fixed delay, honoring
// ctx cancellation is not needed here since StreamText's chunk reads don't
// pass a per-call ctx into TextStream.Next(); the delay simply models a slow
// provider so the total timeout has time to (incorrectly) fire if its
// context were cancelled too early.
type delayedChunksTextStream struct {
	mu     sync.Mutex
	delay  time.Duration
	chunks []provider.StreamChunk
	i      int
}

func (s *delayedChunksTextStream) Next() (*provider.StreamChunk, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return &c, nil
}

func (s *delayedChunksTextStream) Close() error { return nil }
func (s *delayedChunksTextStream) Err() error   { return nil }
