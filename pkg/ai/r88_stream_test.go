package ai

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// ---------------------------------------------------------------------------
// TS 9754014925: ping parts
// ---------------------------------------------------------------------------

func r88streamModel(chunks ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	call := 0
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		i := call
		call++
		if i >= len(chunks) {
			i = len(chunks) - 1
		}
		return testutil.NewMockTextStream(chunks[i]), nil
	}}
}

func r88streamStart(t *testing.T, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func r88streamParts(r *StreamTextResult) []provider.StreamChunk {
	var parts []provider.StreamChunk
	for c := range r.Chunks() {
		parts = append(parts, c)
	}
	return parts
}

func r88streamCount(parts []provider.StreamChunk, typ provider.ChunkType) int {
	n := 0
	for _, p := range parts {
		if p.Type == typ {
			n++
		}
	}
	return n
}

func r88streamUIChunks(t *testing.T, r *StreamTextResult) []UIMessageChunk {
	t.Helper()
	ch, errCh := r.ToUIMessageStream(context.Background())
	var out []UIMessageChunk
	for c := range ch {
		out = append(out, c)
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

var r88streamFinishStop = provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}}

// stream-text.test.ts 'ping forwarding' > should forward ping chunks without
// enabling raw chunks.
func TestR88streamPingForwardedWithoutRawChunks(t *testing.T) {
	r := r88streamStart(t, StreamTextOptions{
		Model:  r88streamModel([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypePing}, r88streamFinishStop}),
		Prompt: "test prompt",
	})
	parts := r88streamParts(r)
	if n := r88streamCount(parts, provider.ChunkTypePing); n != 1 {
		t.Fatalf("ping parts = %d, want 1: %v", n, parts)
	}
	if c := r.Content(); len(c) != 0 {
		t.Fatalf("content = %#v, want empty", c)
	}
	for _, c := range r88streamUIChunks(t, r) {
		if c["type"] == "ping" {
			t.Fatalf("UI stream has a ping chunk: %v", c)
		}
	}
}

// r88streamBlockingStream is a stream whose chunks are released one by one.
type r88streamBlockingStream struct {
	ch chan provider.StreamChunk
}

func (s *r88streamBlockingStream) Next() (*provider.StreamChunk, error) {
	c, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return &c, nil
}
func (s *r88streamBlockingStream) Err() error   { return nil }
func (s *r88streamBlockingStream) Close() error { return nil }

// stream-text.test.ts 'ping forwarding' > does not hold heartbeats behind
// buffered tool input during retries (TS stream-text.ts 2885-2890).
func TestR88streamPingNotHeldBehindBufferedToolInput(t *testing.T) {
	stream := &r88streamBlockingStream{ch: make(chan provider.StreamChunk, 8)}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypeStreamStart}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool"}}
	stream.ch <- provider.StreamChunk{Type: provider.ChunkTypePing}
	sawPing := make(chan struct{})
	var once sync.Once
	retries := 1
	r := r88streamStart(t, StreamTextOptions{
		Model:         &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return stream, nil }},
		Prompt:        "test",
		StreamRetries: &retries,
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypePing {
				once.Do(func() { close(sawPing) })
			}
		},
	})
	done := make(chan error, 1)
	go func() { done <- r.ConsumeStream() }()
	select {
	case <-sawPing:
	case <-time.After(5 * time.Second):
		close(stream.ch)
		<-done
		t.Fatal("ping was held behind the buffered tool input")
	}
	stream.ch <- r88streamFinishStop
	close(stream.ch)
	<-done
	if c := r.Content(); len(c) != 0 {
		t.Fatalf("content = %#v, want empty", c)
	}
}

// stream-text.test.ts 'ping forwarding' > delivers a heartbeat once while
// discarding failed-attempt tool input and content.
func TestR88streamPingDeliveredOnceAcrossStreamRetry(t *testing.T) {
	retries := 0
	streamRetries := 1
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "failed"},
				{Type: provider.ChunkTypeText, ID: "failed", Text: "partial"},
				{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool"}},
				{Type: provider.ChunkTypePing},
				{Type: provider.ChunkTypeError, Err: errors.New("provider error"), Text: "provider error"},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "recovered"},
			{Type: provider.ChunkTypeText, ID: "recovered", Text: "recovered"},
			{Type: provider.ChunkTypeTextEnd, ID: "recovered"},
			r88streamFinishStop,
		}), nil
	}}
	r := r88streamStart(t, StreamTextOptions{
		Model: model, Prompt: "test", MaxRetries: &retries, StreamRetries: &streamRetries,
		OnError: func(context.Context, error) {},
	})
	parts := r88streamParts(r)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if n := r88streamCount(parts, provider.ChunkTypePing); n != 1 {
		t.Fatalf("ping parts = %d, want 1", n)
	}
	if n := r88streamCount(parts, provider.ChunkTypeToolInputStart); n != 0 {
		t.Fatalf("tool-input-start parts = %d, want 0", n)
	}
	if got := r.Text(); got != "recovered" {
		t.Fatalf("text = %q", got)
	}
	content := r.Content()
	if len(content) != 1 {
		t.Fatalf("content = %#v", content)
	}
	if text, ok := content[0].(types.TextContent); !ok || text.Text != "recovered" {
		t.Fatalf("content = %#v", content)
	}
}

// stream-text.test.ts 'ping forwarding' > does not treat a ping-only
// incomplete stream as generated output.
func TestR88streamPingOnlyStreamIsNoOutput(t *testing.T) {
	r := r88streamStart(t, StreamTextOptions{
		Model:   r88streamModel([]provider.StreamChunk{{Type: provider.ChunkTypePing}}),
		Prompt:  "test",
		OnError: func(context.Context, error) {},
	})
	parts := r88streamParts(r)
	if r88streamCount(parts, provider.ChunkTypePing) != 1 {
		t.Fatalf("parts = %v, want a ping", parts)
	}
	found := false
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError && IsNoOutputGeneratedError(streamChunkError(&p)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("parts = %v, want a NoOutputGeneratedError error part", parts)
	}
	if err := r.OutputErr(); !IsNoOutputGeneratedError(err) {
		t.Fatalf("OutputErr = %v, want NoOutputGeneratedError", err)
	}
}

// stream-text-timeout.test.ts it.each(['raw', 'ping']) should not reset
// chunkMs for non-output chunks: the ping at 40ms does not extend the 50ms
// chunk timeout, so the stream times out before the chunk at 70ms.
func TestR88streamPingDoesNotResetChunkTimeout(t *testing.T) {
	var signal context.Context
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		signal = ctx
		return &delayedTextStream{chunks: []delayedChunk{
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}},
			{delay: 40 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypePing}},
			{delay: 30 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: " world"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}},
			{chunk: r88streamFinishStop},
		}}, nil
	}}
	limit := 50 * time.Millisecond
	r := r88streamStart(t, StreamTextOptions{Model: model, Prompt: "test-input", Timeout: &TimeoutConfig{PerChunk: &limit}, OnError: func(context.Context, error) {}})
	_ = r.ConsumeStream()
	var timeoutErr *TimeoutError
	if !errors.As(r.Err(), &timeoutErr) {
		t.Fatalf("Err = %v, want a TimeoutError", r.Err())
	}
	if signal == nil || signal.Err() == nil {
		t.Fatal("model call context was not cancelled")
	}
}

// smooth-stream.test.ts 'forwards pings without flushing a partial word'.
func TestR88streamSmoothStreamForwardsPingWithoutFlushing(t *testing.T) {
	zero := 0
	smooth, err := SmoothStream(SmoothStreamOptions{DelayInMs: &zero})
	if err != nil {
		t.Fatal(err)
	}
	var out []provider.StreamChunk
	for _, c := range []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hel"},
		{Type: provider.ChunkTypePing},
		{Type: provider.ChunkTypeText, ID: "1", Text: "lo "},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	} {
		out = append(out, smooth(context.Background(), c)...)
	}
	var got []string
	for _, c := range out {
		got = append(got, string(c.Type)+":"+c.Text)
	}
	want := []string{"text-start:", "ping:", "text:Hello ", "text-end:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunks = %q, want %q", got, want)
	}
}

