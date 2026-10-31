package ai

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// stalledTextStream delivers its chunks, then blocks in Next() until Close()
// is called (a provider body that stays open and ignores the abort signal).
type stalledTextStream struct {
	mu     sync.Mutex
	chunks []provider.StreamChunk
	idx    int
	closed chan struct{}
	once   sync.Once
}

func newStalledTextStream(chunks ...provider.StreamChunk) *stalledTextStream {
	return &stalledTextStream{chunks: chunks, closed: make(chan struct{})}
}

func (s *stalledTextStream) Next() (*provider.StreamChunk, error) {
	s.mu.Lock()
	if s.idx < len(s.chunks) {
		c := s.chunks[s.idx]
		s.idx++
		s.mu.Unlock()
		return &c, nil
	}
	s.mu.Unlock()
	<-s.closed
	return nil, io.EOF
}
func (s *stalledTextStream) Err() error { return nil }
func (s *stalledTextStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// Ports TS #22184: aborting while a read is pending on a stalled provider body
// releases the read, closes the provider stream, and settles the result.
func TestStreamText_AbortReleasesStalledProviderBody(t *testing.T) {
	stream := newStalledTextStream(
		provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
	)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return stream, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	aborted := 0
	got := make(chan struct{}, 1)
	result, err := StreamText(ctx, StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeText {
				select {
				case got <- struct{}{}:
				default:
				}
			}
		},
		OnAbortEvent: func(context.Context, GenerateTextAbortEvent) {
			mu.Lock()
			aborted++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no text chunk")
	}
	cancel()

	done := make(chan struct{})
	go func() { _ = result.Text(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("result did not settle after abort")
	}
	if !errors.Is(result.Err(), context.Canceled) {
		t.Fatalf("Err() = %v, want context.Canceled", result.Err())
	}
	select {
	case <-stream.closed:
	case <-time.After(time.Second):
		t.Fatal("provider stream was not closed on abort")
	}
	mu.Lock()
	defer mu.Unlock()
	if aborted != 1 {
		t.Fatalf("OnAbort called %d times, want 1", aborted)
	}
}
