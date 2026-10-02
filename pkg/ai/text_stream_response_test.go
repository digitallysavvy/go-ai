package ai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestToTextStream_StandaloneEmitsOnlyTextDeltas(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "text-1"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "hello"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: ""},
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "thinking"},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: " world"},
		{Type: provider.ChunkTypeFinish},
	})

	text, errs := ToTextStream(context.Background(), stream)
	var got []string
	for delta := range text {
		got = append(got, delta)
	}
	if len(got) != 3 || got[0] != "hello" || got[1] != "" || got[2] != " world" {
		t.Fatalf("text deltas = %#v", got)
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestPipeTextStreamToWriter_Standalone(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeToolCall},
		{Type: provider.ChunkTypeText, Text: "b"},
		{Type: provider.ChunkTypeFinish},
	})
	var buf bytes.Buffer
	if err := PipeTextStreamToWriter(context.Background(), stream, &buf); err != nil {
		t.Fatalf("PipeTextStreamToWriter() error = %v", err)
	}
	if got := buf.String(); got != "ab" {
		t.Fatalf("written text = %q", got)
	}
}

// failingWriter always fails on Write, so tests can verify a write error
// surfaces as a real error rather than being swallowed by a deferred flush
// (hand-off/WG-MISC item 7f6650b).
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestPipeTextStreamToWriter_SurfacesWriteError(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeFinish},
	})
	err := PipeTextStreamToWriter(context.Background(), stream, failingWriter{})
	if err == nil {
		t.Fatal("expected a write error, got nil")
	}
}

// closeTrackingTextStream wraps a provider.TextStream and records how many
// times Close was called, so tests can verify a write failure releases the
// underlying stream instead of silently abandoning it mid-stream.
type closeTrackingTextStream struct {
	provider.TextStream
	closeCalls int
}

func (s *closeTrackingTextStream) Close() error {
	s.closeCalls++
	return s.TextStream.Close()
}

// Ported from TS 33e94baaf4 (#21578): a write failure (the Go analog of a
// client disconnect) must close/cancel the source stream so any upstream
// resources it holds are released, instead of abandoning it mid-stream.
func TestPipeTextStreamToWriter_ClosesStreamOnWriteFailure(t *testing.T) {
	stream := &closeTrackingTextStream{
		TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "a"},
			{Type: provider.ChunkTypeText, Text: "b"},
			{Type: provider.ChunkTypeFinish},
		}),
	}

	if err := PipeTextStreamToWriter(context.Background(), stream, failingWriter{}); err == nil {
		t.Fatal("expected a write error, got nil")
	}
	if stream.closeCalls != 1 {
		t.Fatalf("Close() calls = %d, want 1", stream.closeCalls)
	}
}

// Ported from TS 33e94baaf4 (#21578), the ctx-cancellation analog of
// TestPipeTextStreamToWriter_ClosesStreamOnWriteFailure: a caller commonly
// passes an *http.Request's Context() here, which net/http cancels the
// moment the client disconnects -- the Go-idiomatic signal for exactly the
// client-disconnect condition TS's fix addresses, distinct from (and more
// reliable than) a failed Write call. Returning on ctx.Done() without
// closing stream would leak its upstream resources (e.g. an open provider
// HTTP connection) for as long as the provider keeps producing.
func TestPipeTextStreamToWriter_ClosesStreamOnContextCancellation(t *testing.T) {
	stream := &closeTrackingTextStream{
		TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "a"},
			{Type: provider.ChunkTypeText, Text: "b"},
			{Type: provider.ChunkTypeFinish},
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := PipeTextStreamToWriter(ctx, stream, &buf)
	if err == nil {
		t.Fatal("expected a context error, got nil")
	}
	if stream.closeCalls != 1 {
		t.Fatalf("Close() calls = %d, want 1", stream.closeCalls)
	}
}

// flushRecorder wraps a bytes.Buffer and implements http.Flusher, recording
// the buffer's content at each Flush() call, so tests can verify writes
// reach the consumer incrementally (per SSE chunk / text delta) rather than
// only once the whole stream has been read (audit row b9ac19f, WG-MISC).
type flushRecorder struct {
	buf          bytes.Buffer
	flushSnaps   []string
	flushedCount int
}

func (f *flushRecorder) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *flushRecorder) Flush() {
	f.flushedCount++
	f.flushSnaps = append(f.flushSnaps, f.buf.String())
}

// TestPipeTextStreamToWriter_FlushesIncrementally verifies that each text
// chunk is flushed to the underlying writer (and, when it implements
// http.Flusher, that Flush is called) as soon as it's written, instead of
// batching output in bufio's default 4 KiB block and only flushing once at
// the very end.
func TestPipeTextStreamToWriter_FlushesIncrementally(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "a"},
		{Type: provider.ChunkTypeText, Text: "b"},
		{Type: provider.ChunkTypeText, Text: "c"},
		{Type: provider.ChunkTypeFinish},
	})
	rec := &flushRecorder{}
	if err := PipeTextStreamToWriter(context.Background(), stream, rec); err != nil {
		t.Fatalf("PipeTextStreamToWriter() error = %v", err)
	}
	if rec.flushedCount != 3 {
		t.Fatalf("flushedCount = %d, want 3 (one per text chunk)", rec.flushedCount)
	}
	want := []string{"a", "ab", "abc"}
	for i, snap := range rec.flushSnaps {
		if snap != want[i] {
			t.Errorf("flush[%d] snapshot = %q, want %q (writes must reach the consumer incrementally)", i, snap, want[i])
		}
	}
}

func TestCreateTextStreamResponseFromStream_Standalone(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "ok"},
		{Type: provider.ChunkTypeFinish},
	})
	httpRes, err := CreateTextStreamResponseFromStream(context.Background(), stream, &TextStreamResponseInit{
		Status:  http.StatusCreated,
		Headers: map[string]string{"X-Standalone": "text"},
	})
	if err != nil {
		t.Fatalf("CreateTextStreamResponseFromStream() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	body, err := io.ReadAll(httpRes.Body)
	if err != nil {
		t.Fatalf("read body = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestCreateTextStreamResponse_Defaults(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateTextStreamResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("CreateTextStreamResponse() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
	if got := httpRes.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	_, err = io.ReadAll(httpRes.Body)
	if err != nil {
		t.Fatalf("read body = %v", err)
	}
}

func TestCreateTextStreamResponse_WithInit(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := CreateTextStreamResponseWithInit(context.Background(), res, &TextStreamResponseInit{
		Status:     http.StatusAccepted,
		StatusText: "Accepted",
		Headers:    map[string]string{"X-Test": "go"},
	})
	if err != nil {
		t.Fatalf("CreateTextStreamResponseWithInit() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", got, http.StatusAccepted)
	}
	if got := httpRes.Status; got != "Accepted" {
		t.Fatalf("status text = %q", got)
	}
	if got := httpRes.Header.Get("X-Test"); got != "go" {
		t.Fatalf("header X-Test = %q", got)
	}
}

func TestStreamTextResult_ToTextStreamResponse(t *testing.T) {
	stream := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "ok"},
		{Type: provider.ChunkTypeFinish},
	})
	res := &StreamTextResult{stream: stream}
	httpRes, err := res.ToTextStreamResponse(context.Background(), &TextStreamResponseInit{
		Status:  http.StatusCreated,
		Headers: map[string]string{"X-Method": "text"},
	})
	if err != nil {
		t.Fatalf("ToTextStreamResponse() error = %v", err)
	}
	if got := httpRes.StatusCode; got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := httpRes.Header.Get("X-Method"); got != "text" {
		t.Fatalf("header X-Method = %q", got)
	}
}
