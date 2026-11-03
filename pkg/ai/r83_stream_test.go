package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r83streamDelayedEOF is a delayedTextStream that waits before reporting
// io.EOF after its last chunk.
type r83streamDelayedEOF struct {
	delayedTextStream
	eofDelay time.Duration
}

func (s *r83streamDelayedEOF) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		time.Sleep(s.eofDelay)
	}
	return s.delayedTextStream.Next()
}

// TS stream-text.ts clears the first-chunk and chunk timers when the model
// call ends (model-call-end, emitted for the provider finish part), so a
// pause between the finish part and the end of the stream is not a timeout.
func TestR83streamFinishPartStopsChunkTimers(t *testing.T) {
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
			chunks = append(chunks, delayedChunk{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}})
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &r83streamDelayedEOF{delayedTextStream: delayedTextStream{chunks: chunks}, eofDelay: 150 * time.Millisecond}, nil
			}}
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Timeout: tc, OnError: func(context.Context, error) {}})
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			if r.Err() != nil {
				t.Fatalf("Err() = %v, want none after the finish part", r.Err())
			}
			if got := r.FinishReason(); got != types.FinishReasonStop {
				t.Fatalf("finish reason = %q, want stop", got)
			}
		})
	}
}
