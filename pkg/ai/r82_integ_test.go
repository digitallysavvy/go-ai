package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-text.ts clears the first-chunk and chunk timers on an error part
// (clearModelOutputTimeouts) and the next output chunk re-arms the chunk
// timer, so a pause after an error part is not a timeout.
func TestR82integErrorPartStopsChunkTimers(t *testing.T) {
	limit := 40 * time.Millisecond
	for name, tc := range map[string]*TimeoutConfig{
		"PerChunk":   {PerChunk: &limit},
		"FirstChunk": {FirstChunk: &limit},
	} {
		t.Run(name, func(t *testing.T) {
			var chunks []delayedChunk
			if tc.PerChunk != nil {
				chunks = append(chunks, delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "a"}})
			}
			chunks = append(chunks,
				delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeError, Text: "soft"}},
				delayedChunk{delay: 150 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "b"}},
				delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}},
			)
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &delayedTextStream{chunks: chunks}, nil
			}}
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Timeout: tc, OnError: func(context.Context, error) {}})
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			var te *TimeoutError
			if errors.As(r.Err(), &te) {
				t.Fatalf("Err() = %v, want no timeout after an error part", r.Err())
			}
			if got := r.Text(); got != "ab" && got != "b" {
				t.Fatalf("text = %q", got)
			}
		})
	}
}
