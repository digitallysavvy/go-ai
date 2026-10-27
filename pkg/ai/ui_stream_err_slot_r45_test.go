package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// errCh holds one error. When a step callback and its onError both panic,
// that failure fills it; a later stream error must not block the converter
// goroutine (out would never close and the consumer would hang).
func TestToUIMessageStreamStreamErrorAfterCallbackFailureDoesNotBlock(t *testing.T) {
	stream := &failingAfterChunksStream{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
	}, err: errors.New("connection reset")}
	out, errCh := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		OnStepEnd: func(map[string]interface{}) { panic("step callback") },
		OnError: func(err error) string {
			if err.Error() == "step callback" {
				panic("onError")
			}
			return err.Error()
		},
	})
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the UI stream never closed")
	}
	if err := <-errCh; err == nil {
		t.Fatal("expected the callback failure")
	}
}

type failingAfterChunksStream struct {
	chunks []provider.StreamChunk
	err    error
}

func (s *failingAfterChunksStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		return nil, s.err
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &c, nil
}
func (s *failingAfterChunksStream) Err() error   { return s.err }
func (s *failingAfterChunksStream) Close() error { return nil }

// An OnError that panics while the converter reports a stream error fails
// the stream instead of crashing the program from the converter goroutine.
func TestToUIMessageStreamPanickingOnErrorFailsTheStream(t *testing.T) {
	stream := &failingAfterChunksStream{err: errors.New("connection reset")}
	out, errCh := ToUIMessageStream(context.Background(), stream, UIMessageStreamResultOptions{
		OnError: func(error) string { panic("onError failed") },
	})
	for range out {
	}
	if err := <-errCh; err == nil {
		t.Fatal("expected an error")
	}
}
