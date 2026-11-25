package ai

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r79integBlockingStream sends a text delta every 5ms until ctx ends, then
// reports the ctx error; cancelled is closed when Next sees the ctx end or the
// stream is closed (the model call is over).
type r79integBlockingStream struct {
	ctx       context.Context
	n         int
	cancelled chan struct{}
	once      sync.Once
}

func (s *r79integBlockingStream) Next() (*provider.StreamChunk, error) {
	if s.n == 0 {
		s.n++
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"}, nil
	}
	select {
	case <-s.ctx.Done():
		s.once.Do(func() { close(s.cancelled) })
		return nil, s.ctx.Err()
	case <-time.After(5 * time.Millisecond):
		s.n++
		return &provider.StreamChunk{Type: provider.ChunkTypeText, Text: "b"}, nil
	}
}

func (s *r79integBlockingStream) Err() error { return nil }
func (s *r79integBlockingStream) Close() error {
	s.once.Do(func() { close(s.cancelled) })
	return nil
}

type r79integFailWriter struct{}

func (r79integFailWriter) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

// Stream fix 1 bound: after a client disconnect the source keeps running
// while the ConsumeSSEStream callback reads its copy (the model call is not
// cancelled under it). Go's StreamText reads the model whether or not the UI
// stream is read (known difference "StreamText reads the provider stream
// without a reader"), so the model call ends with the StreamText ctx; once it
// is cancelled after the callback returned, no goroutine is left behind.
func TestR79integConsumeSSEStreamSourceEndsWithCallback(t *testing.T) {
	base := runtime.NumGoroutine()
	cancelled := make(chan struct{})
	var calls atomic.Int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		calls.Add(1)
		return &r79integBlockingStream{ctx: ctx, cancelled: cancelled}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	returned := make(chan struct{})
	err = PipeUIMessageStreamToResponseWithInit(context.Background(), r, r79integFailWriter{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(rd io.Reader) error {
			defer close(returned)
			// Read a few events after the disconnect, then stop.
			buf := make([]byte, 16)
			for i := 0; i < 8; i++ {
				if _, err := rd.Read(buf); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("Pipe error = nil, want the write error")
	}
	<-returned
	select {
	case <-cancelled:
		t.Fatal("the model call was cancelled by the UI helper")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatalf("the model call kept running after its ctx was cancelled (DoStream calls %d)", calls.Load())
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Fatalf("%d goroutines left (base %d)", n, base)
	}
}
