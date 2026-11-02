package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r78streamChunks returns a closed channel holding the given chunks.
func r78streamChunks(chunks ...UIMessageChunk) <-chan UIMessageChunk {
	ch := make(chan UIMessageChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// r78streamReadBody reads body fully, failing the test when that takes
// longer than the timeout.
func r78streamReadBody(t *testing.T, body io.Reader) string {
	t.Helper()
	type res struct {
		b   []byte
		err error
	}
	done := make(chan res, 1)
	go func() {
		b, err := io.ReadAll(body)
		done <- res{b, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("read body: %v", r.err)
		}
		return string(r.b)
	case <-time.After(5 * time.Second):
		t.Fatal("the response body did not end while ConsumeSSEStream was still running")
		return ""
	}
}

// r78streamSignalWriter records writes and closes done once [DONE] was
// written.
type r78streamSignalWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	once sync.Once
	done chan struct{}
}

func (w *r78streamSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if bytes.Contains(w.buf.Bytes(), []byte("data: [DONE]\n\n")) {
		w.once.Do(func() { close(w.done) })
	}
	return len(p), nil
}

func (w *r78streamSignalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TS create-ui-message-stream-response.test.ts 215: "should not block the
// response when consumeSseStream takes time". The consumer reads its copy and
// then waits; the response body must end without waiting for it. The
// "waits before reading" case is the same rule for a consumer that does not
// read yet: TS tee() buffers each branch, so the response is not held up.
func TestR78StreamConsumeSSEStreamDoesNotBlockResponse(t *testing.T) {
	const want = "data: {\"delta\":\"test-data\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"
	for _, readFirst := range []bool{true, false} {
		name := "reads then waits"
		if !readFirst {
			name = "waits before reading"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			consumed := make(chan string, 1)
			calls := 0
			resp, err := CreateUIMessageChunksResponse(
				r78streamChunks(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "test-data"}),
				&UIMessageStreamResponseInit{
					Status: 200,
					ConsumeSSEStream: func(r io.Reader) error {
						calls++
						var b []byte
						var err error
						if readFirst {
							b, err = io.ReadAll(r)
							<-release
						} else {
							<-release
							b, err = io.ReadAll(r)
						}
						consumed <- string(b)
						return err
					},
				})
			if err != nil {
				t.Fatalf("CreateUIMessageChunksResponse() error = %v", err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			if got := r78streamReadBody(t, resp.Body); got != want {
				t.Fatalf("body = %q, want %q", got, want)
			}
			close(release)
			select {
			case got := <-consumed:
				if got != want {
					t.Fatalf("consumed = %q, want %q", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ConsumeSSEStream did not finish")
			}
			if calls != 1 {
				t.Fatalf("ConsumeSSEStream calls = %d, want 1", calls)
			}
		})
	}
}

// The StreamTextResult response helper (TS toUIMessageStreamResponse ->
// createUIMessageStreamResponse) follows the same rule.
func TestR78StreamResultResponseDoesNotWaitForConsumeSSEStream(t *testing.T) {
	res := &StreamTextResult{stream: testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})}
	release := make(chan struct{})
	consumed := make(chan string, 1)
	resp, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(r io.Reader) error {
			<-release
			b, err := io.ReadAll(r)
			consumed <- string(b)
			return err
		},
	})
	if err != nil {
		t.Fatalf("CreateUIMessageStreamResponseWithInit() error = %v", err)
	}
	body := r78streamReadBody(t, resp.Body)
	if !bytes.HasSuffix([]byte(body), []byte("data: [DONE]\n\n")) {
		t.Fatalf("body = %q, want it to end with [DONE]", body)
	}
	close(release)
	select {
	case got := <-consumed:
		if got != body {
			t.Fatalf("consumed = %q, want the body %q", got, body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ConsumeSSEStream did not finish")
	}
}

// The Pipe* helpers write the whole stream without waiting for a consumer
// that has not read yet (TS tee), and then return its error (Go-only return
// value, as in v0.5).
func TestR78StreamPipeWritesBeforeSlowConsumer(t *testing.T) {
	const want = "data: {\"delta\":\"a\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"
	w := &r78streamSignalWriter{done: make(chan struct{})}
	release := make(chan struct{})
	consumed := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- PipeUIMessageChunksToResponse(
			r78streamChunks(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "a"}),
			w,
			&UIMessageStreamResponseInit{ConsumeSSEStream: func(r io.Reader) error {
				<-release
				b, _ := io.ReadAll(r)
				consumed <- string(b)
				return errors.New("consume failed")
			}})
	}()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the response writes waited for ConsumeSSEStream to read")
	}
	if got := w.String(); got != want {
		t.Fatalf("response = %q, want %q", got, want)
	}
	select {
	case err := <-errCh:
		t.Fatalf("PipeUIMessageChunksToResponse returned %v before ConsumeSSEStream finished", err)
	default:
	}
	close(release)
	if got := <-consumed; got != want {
		t.Fatalf("consumed = %q, want %q", got, want)
	}
	if err := <-errCh; err == nil || err.Error() != "consume failed" {
		t.Fatalf("PipeUIMessageChunksToResponse() = %v, want the consumer error", err)
	}
}

func r78streamUnknownCall(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "unknown", Arguments: map[string]interface{}{"value": "v"}, RawArguments: `{ "value": "v" }`}}
}

func r78streamContentKinds(content []types.ContentPart) []string {
	var out []string
	for _, c := range content {
		switch p := c.(type) {
		case types.ToolCallContent:
			out = append(out, "tool-call:"+p.ToolCallID)
		case types.ToolErrorContent:
			out = append(out, "tool-error:"+p.ToolCallID)
		case types.ToolResultContent:
			out = append(out, "tool-result:"+p.ToolCallID)
		default:
			out = append(out, fmt.Sprintf("%T", c))
		}
	}
	return out
}

// TS stream-language-model-call.ts 742-757: an invalid client call is
// followed by its tool-error right away, in stream position, before the
// later parts of the model call. Valid calls run when the model call ends
// (executeToolsFromStream on model-call-end). Go emitted every tool error
// after the model stream ended, so a later valid call came before the
// invalid call's error, in the full stream and in the step content.
func TestR78StreamInvalidToolErrorFollowsItsCall(t *testing.T) {
	step := []provider.StreamChunk{
		r78streamUnknownCall("call-1"),
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}, RawArguments: `{ "value": "v" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
	}
	r, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "p", Tools: []types.Tool{r71streamTool1(true, nil, nil)}})
	want := "[start start-step tool-call:call-1:unknown tool-result:call-1:<nil> tool-call:call-2:tool1 tool-result:call-2:v-result finish-step:tool-calls finish:tool-calls]"
	if got := fmt.Sprint(r73streamNames(parts)); got != want {
		t.Fatalf("full stream = %s\nwant          %s", got, want)
	}
	if got, want := fmt.Sprint(r78streamContentKinds(r.Steps()[0].Content)), "[tool-call:call-1 tool-error:call-1 tool-call:call-2 tool-result:call-2]"; got != want {
		t.Fatalf("step content = %s, want %s", got, want)
	}
	// The step's tool messages answer both calls, in call order.
	var toolParts []string
	for _, m := range r.Steps()[0].Response.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok {
				toolParts = append(toolParts, tr.ToolCallID)
			}
		}
	}
	if got := fmt.Sprint(toolParts); got != "[call-1 call-2]" {
		t.Fatalf("tool message parts = %s, want [call-1 call-2]", got)
	}
}

// With an enforced tool choice the model call's tool-error for an invalid
// call comes before the tool choice violation error part (TS emits it at the
// tool-call chunk, the violation at the finish chunk). Go skipped every tool
// result after a violation, so the invalid call got no tool-error. With tool
// choice none the call is withheld and gets none.
func TestR78StreamInvalidToolErrorBeforeToolChoiceViolation(t *testing.T) {
	step := []provider.StreamChunk{
		r78streamUnknownCall("call-1"),
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
	}
	r, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "p", Tools: []types.Tool{r71streamTool1(true, nil, nil)}, ToolChoice: types.SpecificToolChoice("tool1")})
	if got, want := fmt.Sprint(r73streamNames(parts)), "[start start-step tool-call:call-1:unknown tool-result:call-1:<nil> error finish-step:error finish:error]"; got != want {
		t.Fatalf("full stream = %s\nwant          %s", got, want)
	}
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolResult {
			if p.ToolResult.Error == nil || p.ToolResult.Error.Error() != "Model tried to call unavailable tool 'unknown'. Available tools: tool1." || !p.ToolResult.Dynamic {
				t.Fatalf("tool-error = %+v", p.ToolResult)
			}
		}
		if p.Type == provider.ChunkTypeError {
			var v *ToolChoiceViolationError
			if !errors.As(p.Err, &v) {
				t.Fatalf("error part = %v, want ToolChoiceViolationError", p.Err)
			}
		}
	}
	if got, want := fmt.Sprint(r78streamContentKinds(r.Steps()[0].Content)), "[tool-call:call-1 tool-error:call-1]"; got != want {
		t.Fatalf("step content = %s, want %s", got, want)
	}

	_, parts = r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "p", Tools: []types.Tool{r71streamTool1(true, nil, nil)}, ToolChoice: types.NoneToolChoice()})
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolResult || p.Type == provider.ChunkTypeToolCall {
			t.Fatalf("tool choice none: tool part %s forwarded", tsPartName(&p))
		}
	}
}
