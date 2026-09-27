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

// TestSmoothStream_EmitsIncrementallyViaEmitter verifies that, when an
// emitter is installed in ctx (as StreamText's processStream now does around
// every ExperimentalTransform call), SmoothStream sends each detected match
// through the emitter immediately rather than batching all of a single
// input delta's matches into its return value.
func TestSmoothStream_EmitsIncrementallyViaEmitter(t *testing.T) {
	t.Parallel()

	var delayCalls int
	fn, err := SmoothStream(SmoothStreamOptions{
		internalDelay: func(context.Context, *int) { delayCalls++ },
	})
	if err != nil {
		t.Fatalf("SmoothStream() error = %v", err)
	}

	var emitted []provider.StreamChunk
	emit := func(c provider.StreamChunk) { emitted = append(emitted, c) }
	ctx := WithStreamTransformEmitter(context.Background(), emit)

	returned := fn(ctx, textChunk("1", "one two three "))

	if len(returned) != 0 {
		t.Fatalf("expected nothing in the return value when an emitter is installed, got %+v", returned)
	}
	if len(emitted) != 3 {
		t.Fatalf("expected 3 emitted word chunks, got %d: %+v", len(emitted), emitted)
	}
	var gotTexts []string
	for _, c := range emitted {
		gotTexts = append(gotTexts, c.Text)
	}
	want := []string{"one ", "two ", "three "}
	for i, w := range want {
		if gotTexts[i] != w {
			t.Fatalf("emitted[%d] = %q, want %q (all: %v)", i, gotTexts[i], w, gotTexts)
		}
	}
	// One delay between each pair of matches (and after the last, matching
	// the pre-existing batch-mode behavior/test expectations).
	if delayCalls != 3 {
		t.Fatalf("delay calls = %d, want 3", delayCalls)
	}
}

// TestStreamText_SmoothStreamDeliversChunksIncrementally is an end-to-end
// test: StreamText with SmoothStream as an ExperimentalTransform should
// deliver OnChunk callbacks spaced apart in real time by (approximately) the
// configured delay, rather than delivering every smoothed chunk from a
// single provider delta all at once. This is the behavior the emitter hook
// exists for — see WithStreamTransformEmitter / StreamTransformEmitterFromContext.
func TestStreamText_SmoothStreamDeliversChunksIncrementally(t *testing.T) {
	t.Parallel()

	delayMs := 30
	transform, err := SmoothStream(SmoothStreamOptions{DelayInMs: &delayMs})
	if err != nil {
		t.Fatalf("SmoothStream() error = %v", err)
	}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				// A single large delta: word-chunking splits it into 4 words.
				{Type: provider.ChunkTypeText, Text: "one two three four "},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	var times []time.Time
	_, err = StreamText(context.Background(), StreamTextOptions{
		Model:                 model,
		Prompt:                "hi",
		ExperimentalTransform: []StreamTransformFunc{transform},
		OnChunk: func(c provider.StreamChunk) {
			if c.Type != provider.ChunkTypeText {
				return
			}
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
		},
		OnEnd: func(*StreamTextResult) {},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	// Give the background processStream goroutine time to finish (OnEnd
	// fires synchronously from it, but sleep defensively for the timing
	// assertion below rather than relying on that alone).
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(times) != 4 {
		t.Fatalf("expected 4 text chunks, got %d", len(times))
	}
	// Consecutive chunks must be spaced apart by a meaningful fraction of the
	// configured delay — proving they were delivered incrementally rather
	// than all at once (which would show ~0ms gaps) after a single combined
	// wait.
	const minGap = 10 * time.Millisecond
	for i := 1; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		if gap < minGap {
			t.Errorf("gap between chunk %d and %d = %v, want >= %v (chunks arrived in a batch, not incrementally)", i-1, i, gap, minGap)
		}
	}
}
