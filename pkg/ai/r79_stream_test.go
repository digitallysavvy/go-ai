package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Reviewer item 4 (r79): the inline invalid tool errors reset at a stream
// retry boundary. TS stream-text.ts flushes the held tool parts at
// model-call-end and returns; an error part read after it (no tool parts
// held) is retried while the budget remains, and the step's recorded content
// restarts at the attempt boundary. So an invalid call answered in the first
// attempt must leave nothing in the step that the retry produced.
func TestR79StreamRetryDropsInlineInvalidToolError(t *testing.T) {
	retries := 1
	first := []provider.StreamChunk{
		r78streamUnknownCall("call-1"),
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
		{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
	}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model:         r71streamModel(nil, first, r71streamHelloWorld),
		Prompt:        "p",
		Tools:         []types.Tool{r71streamTool1(true, nil, nil)},
		StreamRetries: &retries,
	})
	// The first attempt's call and its tool-error were already forwarded
	// (TS flushes them at model-call-end, before the error part).
	if got, want := fmt.Sprint(r73streamNames(parts)), "[start start-step tool-call:call-1:unknown tool-result:call-1:<nil> text-start:1 text-delta:1:Hello text-delta:1:,  text-delta:1:world! text-end:1 finish-step:stop finish:stop]"; got != want {
		t.Fatalf("full stream = %s\nwant          %s", got, want)
	}
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := steps[0]
	if got := fmt.Sprint(r78streamContentKinds(step.Content)); got != "[types.TextContent]" {
		t.Fatalf("step content = %s, want [types.TextContent]", got)
	}
	if len(step.ToolCalls) != 0 || len(step.ToolResults) != 0 {
		t.Fatalf("step tool calls = %+v, tool results = %+v, want none", step.ToolCalls, step.ToolResults)
	}
	for _, m := range step.Response.Messages {
		if m.Role == types.RoleTool {
			t.Fatalf("response has a tool message: %+v", m)
		}
	}
	if r.FinishReason() != types.FinishReasonStop {
		t.Fatalf("finish reason = %s, want stop", r.FinishReason())
	}
}

// r79streamBigDelta is larger than a bufio.Writer's buffer, so writing its
// event goes straight to the failing writer.
var r79streamBigDelta = strings.Repeat("x", 5000)

// r79streamFailWriter accepts ok writes, then fails every later one (a client
// that disconnected).
type r79streamFailWriter struct {
	mu sync.Mutex
	ok int
}

func (w *r79streamFailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ok == 0 {
		return 0, errors.New("client disconnected")
	}
	w.ok--
	return len(p), nil
}

// TS pipeUIMessageStreamToResponse / createUIMessageStreamResponse tee the
// SSE stream for consumeSseStream. A client disconnect cancels only the
// response branch (writeToServerResponse reader.cancel), and a tee cancels
// its source only when both branches are cancelled, so the consumeSseStream
// copy still gets every event through [DONE] (the TS stopping-streams docs
// pass consumeStream for exactly this). Go closed the copy and cancelled the
// source at the first failed write.
func TestR79StreamConsumeSSEStreamOutlivesClientDisconnect(t *testing.T) {
	chunks := make(chan UIMessageChunk)
	go func() {
		defer close(chunks)
		for _, c := range []UIMessageChunk{{"type": "start"}, {"type": "text-start", "id": "1"}, {"type": "text-delta", "id": "1", "delta": r79streamBigDelta}, {"type": "text-end", "id": "1"}, {"type": "finish"}} {
			chunks <- c
		}
	}()
	copied := make(chan string, 1)
	err := PipeUIMessageChunksToResponse(chunks, &r79streamFailWriter{ok: 2}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			b, err := io.ReadAll(r)
			copied <- string(b)
			return err
		},
	})
	if err == nil || err.Error() != "client disconnected" {
		t.Fatalf("Pipe error = %v, want the write error", err)
	}
	want := "data: {\"type\":\"start\"}\n\n" +
		"data: {\"id\":\"1\",\"type\":\"text-start\"}\n\n" +
		"data: {\"delta\":\"" + r79streamBigDelta + "\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n" +
		"data: {\"id\":\"1\",\"type\":\"text-end\"}\n\n" +
		"data: {\"type\":\"finish\"}\n\n" +
		"data: [DONE]\n\n"
	select {
	case got := <-copied:
		if got != want {
			t.Fatalf("consumer copy = %q\nwant %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the consumer copy never ended")
	}
}

// The StreamTextResult helpers keep the model stream running for the
// consumer's copy after the response fails: its copy ends with the finish
// chunk and [DONE], and the UI stream's onEnd sees a completed stream rather
// than a cancelled one.
func TestR79StreamResultConsumeSSEStreamOutlivesClientDisconnect(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	copied := make(chan string, 1)
	ended := make(chan map[string]interface{}, 1)
	err = PipeUIMessageStreamToResponseWithInit(context.Background(), r, &r79streamFailWriter{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(rd io.Reader) error {
			b, err := io.ReadAll(rd)
			copied <- string(b)
			return err
		},
	}, UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) { ended <- e }})
	if err == nil {
		t.Fatal("Pipe error = nil, want the write error")
	}
	select {
	case got := <-copied:
		if !strings.Contains(got, "data: {\"delta\":\"world!\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n") || !strings.HasSuffix(got, "data: {\"finishReason\":\"stop\",\"type\":\"finish\"}\n\ndata: [DONE]\n\n") {
			t.Fatalf("consumer copy = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the consumer copy never ended")
	}
	select {
	case e := <-ended:
		if _, cancelled := e["isCancelled"]; cancelled || fmt.Sprint(e["outcome"]) != "map[status:completed]" {
			t.Fatalf("onEnd = cancelled %v outcome %v, want completed", e["isCancelled"], e["outcome"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onEnd never ran")
	}
}

// Once the consumer returns as well, both tee branches are gone: the drain
// stops reading chunks.
func TestR79StreamDrainStopsWhenConsumerReturns(t *testing.T) {
	chunks := make(chan UIMessageChunk)
	consumerDone := make(chan struct{})
	release := make(chan struct{})
	go func() {
		chunks <- UIMessageChunk{"type": "start"}
		<-release
	}()
	err := PipeUIMessageChunksToResponse(chunks, &r79streamFailWriter{}, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			defer close(consumerDone)
			buf := make([]byte, 64)
			_, err := r.Read(buf)
			return err
		},
	})
	if err == nil {
		t.Fatal("Pipe error = nil, want the write error")
	}
	close(release)
	<-consumerDone
	// Give the drain a moment to observe the stop.
	time.Sleep(50 * time.Millisecond)
	select {
	case chunks <- UIMessageChunk{"type": "finish"}:
		t.Fatal("chunks still read after the consumer returned")
	case <-time.After(100 * time.Millisecond):
	}
}
