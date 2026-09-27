package ai

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// delayedChunk pairs a chunk with the delay to wait (relative to the
// previous Next() call returning) before delivering it.
type delayedChunk struct {
	delay time.Duration
	chunk provider.StreamChunk
}

// delayedTextStream is a provider.TextStream whose Next() calls block for a
// configurable, per-chunk delay before returning — used to exercise
// FirstChunk/PerChunk timeout behavior deterministically.
type delayedTextStream struct {
	chunks []delayedChunk
	idx    int
}

func (s *delayedTextStream) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	dc := s.chunks[s.idx]
	s.idx++
	if dc.delay > 0 {
		time.Sleep(dc.delay)
	}
	c := dc.chunk
	return &c, nil
}

func (s *delayedTextStream) Err() error   { return nil }
func (s *delayedTextStream) Close() error { return nil }

// TestStreamText_FirstChunkTimeoutFiresOnNonOutputOnly verifies that
// TimeoutConfig.FirstChunk aborts the step when only non-output chunks
// (e.g. a response-metadata chunk) arrive before the deadline — mirrors TS
// stream-text-timeout.test.ts "should abort when only non-output chunks
// arrive before firstChunkMs".
func TestStreamText_FirstChunkTimeoutFiresOnNonOutputOnly(t *testing.T) {
	t.Parallel()

	firstChunkMs := 40 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedTextStream{chunks: []delayedChunk{
				{delay: 10 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "resp_1"}}},
				// Nothing semantic ever arrives within firstChunkMs of the
				// step starting; the deadline should fire here.
				{delay: 200 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: "too late"}},
				{delay: 0, chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}},
			}}, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Timeout: &TimeoutConfig{FirstChunk: &firstChunkMs},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, _ = result.ReadAll()

	var timeoutErr *TimeoutError
	if !errors.As(result.Err(), &timeoutErr) || timeoutErr.Reason != TimeoutReasonFirstChunk {
		t.Fatalf("expected a TimeoutReasonFirstChunk TimeoutError, got %v", result.Err())
	}
}

// TestStreamText_FirstChunkTimeoutDisarmsOnFirstOutputChunk verifies that
// once a semantic output chunk (a non-empty text delta) arrives, the
// FirstChunk deadline is disarmed and does not fire even though the
// provider is slower than firstChunkMs afterward and no PerChunk is
// configured. Mirrors TS "should disarm firstChunkMs before forwarding the
// first text-delta".
func TestStreamText_FirstChunkTimeoutDisarmsOnFirstOutputChunk(t *testing.T) {
	t.Parallel()

	firstChunkMs := 40 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedTextStream{chunks: []delayedChunk{
				{delay: 5 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: "hi"}},
				// Slower than firstChunkMs, but FirstChunk no longer applies
				// once a semantic chunk has arrived and PerChunk isn't set.
				{delay: 100 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: " there"}},
				{delay: 0, chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}},
			}}, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Timeout: &TimeoutConfig{FirstChunk: &firstChunkMs},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v (want no timeout once FirstChunk disarmed)", err)
	}
	if text != "hi there" {
		t.Fatalf("text = %q, want %q", text, "hi there")
	}
}

// TestStreamText_PerChunkTimeoutIgnoresNonOutputChunks verifies that
// TimeoutConfig.PerChunk's deadline is only reset by semantic output
// chunks, not by metadata/empty-delta chunks arriving in between — mirrors
// TS "should not reset chunkMs for non-output chunks".
func TestStreamText_PerChunkTimeoutIgnoresNonOutputChunks(t *testing.T) {
	t.Parallel()

	perChunkMs := 60 * time.Millisecond
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &delayedTextStream{chunks: []delayedChunk{
				{delay: 5 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: "hi"}},
				// A burst of non-output chunks, each individually well
				// within perChunkMs of the previous one, but the total time
				// since the last OUTPUT chunk exceeds perChunkMs.
				{delay: 30 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1"}}},
				{delay: 30 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r2"}}},
				{delay: 30 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, Text: " there"}},
				{delay: 0, chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: "stop"}},
			}}, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Timeout: &TimeoutConfig{PerChunk: &perChunkMs},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, _ = result.ReadAll()

	var timeoutErr *TimeoutError
	if !errors.As(result.Err(), &timeoutErr) || timeoutErr.Reason != TimeoutReasonChunk {
		t.Fatalf("expected a TimeoutReasonChunk TimeoutError (90ms since the last output chunk > 60ms), got %v", result.Err())
	}
}
