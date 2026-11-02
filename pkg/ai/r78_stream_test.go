package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
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
// that has not read yet (TS tee), and return once the response is written,
// without waiting for the consumer or returning its error (owner ruling
// 2026-11-02: TS pipeUIMessageStreamToResponse does not await
// consumeSseStream).
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
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("PipeUIMessageChunksToResponse() = %v, want nil (the consumer error is not returned)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PipeUIMessageChunksToResponse waited for ConsumeSSEStream")
	}
	if got := w.String(); got != want {
		t.Fatalf("response = %q, want %q", got, want)
	}
	close(release)
	if got := <-consumed; got != want {
		t.Fatalf("consumed = %q, want %q", got, want)
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

// stream-text.test.ts 7347 / 7367: result.toUIMessageStreamResponse masks a
// stream error by default and uses the onError text when given (Go: sorted
// JSON keys, known difference "UI message stream JSON key order").
func TestR78StreamToUIMessageStreamResponseErrorText(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    []UIMessageStreamResultOptions
		errText string
	}{
		{"mask by default", nil, "An error occurred."},
		{"custom", []UIMessageStreamResultOptions{{OnError: func(err error) string { return "custom error message: " + err.Error() }}}, "custom error message: error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := StreamText(context.Background(), StreamTextOptions{
				Model:    r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeError, Text: "error"}}),
				Prompt:   "test-input",
				Internal: r71streamInternal,
				OnError:  func(context.Context, error) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := r.ToUIMessageStreamResponse(context.Background(), nil, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			want := "data: {\"type\":\"start\"}\n\n" +
				"data: {\"type\":\"start-step\"}\n\n" +
				"data: {\"errorText\":\"" + tc.errText + "\",\"type\":\"error\"}\n\n" +
				"data: {\"type\":\"finish-step\"}\n\n" +
				"data: {\"finishReason\":\"error\",\"type\":\"finish\"}\n\n" +
				"data: [DONE]\n\n"
			if got := r78streamReadBody(t, resp.Body); got != want {
				t.Fatalf("body =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// stream-text.test.ts 32784 / 32819 / 32852 / 32880: provider-executed tool
// (MCP) approval > when a provider-executed tool emits tool-approval-request:
// the UI message stream, the content, the response messages and the finish
// reason.
func TestR78StreamProviderExecutedApprovalRequest(t *testing.T) {
	mcp := types.Tool{Name: "mcp_tool", Type: types.ToolTypeProviderDefined, ProviderID: "test.mcp_tool", ProviderExecuted: true,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}}}
	// A fresh chunk list per run: the mock hands out pointers into it.
	opts := func() StreamTextOptions {
		step := []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, RawArguments: `{ "query": "test" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
			{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}
		return StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input", Internal: r71streamInternal, Tools: []types.Tool{mcp}}
	}
	r72streamAssertUI(t, opts(), `[
		{"type":"start"},
		{"type":"start-step"},
		{"input":{"query":"test"},"providerExecuted":true,"toolCallId":"mcp-call-1","toolName":"mcp_tool","type":"tool-input-available"},
		{"approvalId":"mcp-approval-1","toolCallId":"mcp-call-1","type":"tool-approval-request"},
		{"type":"finish-step"},
		{"finishReason":"tool-calls","type":"finish"}]`)

	r := r71streamRun(t, opts())
	content := r.Steps()[0].Content
	if len(content) != 2 {
		t.Fatalf("content = %#v", content)
	}
	call, ok := content[0].(types.ToolCallContent)
	if !ok || call.ToolCallID != "mcp-call-1" || call.ToolName != "mcp_tool" || !call.ProviderExecuted || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"query": "test"}) {
		t.Fatalf("content[0] = %#v", content[0])
	}
	req, ok := content[1].(types.ToolApprovalRequestContent)
	if !ok || req.ApprovalID != "mcp-approval-1" || req.ToolCallID != "mcp-call-1" {
		t.Fatalf("content[1] = %#v", content[1])
	}
	msgs := r.ResponseMessages()
	if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant || len(msgs[0].Content) != 2 {
		t.Fatalf("response messages = %#v", msgs)
	}
	if c, ok := msgs[0].Content[0].(types.ToolCallContent); !ok || c.ToolCallID != "mcp-call-1" || !c.ProviderExecuted {
		t.Fatalf("response content[0] = %#v", msgs[0].Content[0])
	}
	if c, ok := msgs[0].Content[1].(types.ToolApprovalRequestContent); !ok || c.ApprovalID != "mcp-approval-1" || c.ToolCallID != "mcp-call-1" || c.IsAutomatic {
		t.Fatalf("response content[1] = %#v", msgs[0].Content[1])
	}
	if r.FinishReason() != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q", r.FinishReason())
	}
}

// handle-ui-message-stream-finish.test.ts 176: a UI message processing
// failure (a text delta without its start), before or after the owner
// declared a completed outcome, fails the stream; onEnd runs once with
// outcome failed and that error.
func TestR78StreamHandleFinishProcessingFailure(t *testing.T) {
	for _, input := range [][]UIMessageChunk{
		{{"type": "text-delta", "id": "missing", "delta": "text"}},
		{{"type": "finish"}, {"type": "text-delta", "id": "missing", "delta": "text"}},
	} {
		var mu sync.Mutex
		var ends []map[string]interface{}
		out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
			GenerateMessageID: func() string { return "msg-processing-error" },
			OnEnd: func(e map[string]interface{}) {
				mu.Lock()
				defer mu.Unlock()
				ends = append(ends, e)
			},
			Execute: func(w UIMessageStreamWriter) {
				w.SetOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeCompleted})
				for _, c := range input {
					w.Write(c)
				}
			},
		})
		for range out {
		}
		var errs []error
		for err := range errCh {
			errs = append(errs, err)
		}
		if len(errs) != 1 || errs[0] == nil {
			t.Fatalf("%v: stream errors = %v, want one processing error", input, errs)
		}
		mu.Lock()
		if len(ends) != 1 {
			mu.Unlock()
			t.Fatalf("%v: onEnd calls = %d, want 1", input, len(ends))
		}
		outcome, _ := ends[0]["outcome"].(map[string]interface{})
		mu.Unlock()
		if outcome["status"] != "failed" || outcome["error"] != errs[0] || len(outcome) != 2 {
			t.Fatalf("%v: outcome = %#v, want failed with the stream error %v", input, outcome, errs[0])
		}
	}
}

// handle-ui-message-stream-finish.test.ts 660: onStepFinish runs once at the
// finish-step chunk, with the response message and the original messages;
// the chunks pass through unchanged.
func TestR78StreamHandleFinishOnStepFinish(t *testing.T) {
	input := []UIMessageChunk{
		{"type": "start", "messageId": "msg-step-1"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "Step 1 text"},
		{"type": "text-end", "id": "text-1"},
		{"type": "finish-step"},
		{"type": "finish"},
	}
	user := UIMessageChunk{"id": "user-msg-1", "role": "user", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "Hello"}}}
	var mu sync.Mutex
	var steps []map[string]interface{}
	out, errs := r75streamRunUIChunks(t, input, UIMessageStreamOptions{
		OriginalMessages:  []UIMessageChunk{user},
		GenerateMessageID: func() string { return "msg-step-1" },
		OnStepFinish: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			steps = append(steps, e)
		},
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	r75streamAssertJSON(t, "output", out, string(r76streamMustJSON(t, input)))
	mu.Lock()
	defer mu.Unlock()
	if len(steps) != 1 {
		t.Fatalf("onStepFinish calls = %d, want 1", len(steps))
	}
	e := steps[0]
	if e["isContinuation"] != false {
		t.Errorf("isContinuation = %v", e["isContinuation"])
	}
	resp, _ := e["responseMessage"].(UIMessageChunk)
	if resp == nil || resp["id"] != "msg-step-1" || resp["role"] != "assistant" {
		t.Fatalf("responseMessage = %#v", e["responseMessage"])
	}
	messages, _ := e["messages"].([]UIMessageChunk)
	if len(messages) != 2 || messages[1]["id"] != "msg-step-1" {
		t.Fatalf("messages = %#v", e["messages"])
	}
	r75streamAssertJSON(t, "messages[0]", messages[0], string(r76streamMustJSON(t, user)))
}

// create-ui-message-stream.test.ts 999: when onError itself fails (panics in
// Go) while handling an execute failure, the stream fails with that error,
// and onEnd runs once with outcome failed and that error. (The merged-stream
// half has no Go form: a merged channel cannot fail.)
func TestR78StreamOnErrorFailureFailsOnce(t *testing.T) {
	onErrorErr := errors.New("onError failure")
	var mu sync.Mutex
	var ends []map[string]interface{}
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(UIMessageStreamWriter) { panic(errors.New("execute failure")) },
		OnError: func(error) string { panic(onErrorErr) },
		OnEnd: func(e map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			ends = append(ends, e)
		},
	})
	var chunks []UIMessageChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !errors.Is(errs[0], onErrorErr) {
		t.Fatalf("stream errors = %v, want [onError failure]", errs)
	}
	if len(chunks) != 0 {
		t.Fatalf("chunks = %v, want none", chunks)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d, want 1", len(ends))
	}
	outcome, _ := ends[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != errs[0] || len(outcome) != 2 {
		t.Fatalf("outcome = %#v, want failed with %v", outcome, errs[0])
	}
}

// r78streamReadLast runs ReadUIMessages and returns the last snapshot as
// JSON-decoded data.
func r78streamReadLast(t *testing.T, seed string, chunks ...UIMessageChunk) map[string]interface{} {
	t.Helper()
	opts := ReadUIMessagesOptions{Stream: r76streamChan(chunks...), TerminateOnError: true}
	if seed != "" {
		var m UIMessage
		if err := json.Unmarshal([]byte(seed), &m); err != nil {
			t.Fatal(err)
		}
		opts.Message = &m
	}
	msgs, errs := ReadUIMessages(context.Background(), opts)
	var last UIMessage
	n := 0
	for m := range msgs {
		last = m
		n++
	}
	for err := range errs {
		t.Fatalf("error = %v", err)
	}
	if n == 0 {
		t.Fatal("no snapshots")
	}
	b, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// read-ui-message-stream.test.ts 70: interleaved tool inputs keep their own
// encoding: a text input stays text, json (or unset) input is parsed, a JSON
// string literal becomes the string.
func TestR78StreamReadUIMessagesInterleavedInputEncodings(t *testing.T) {
	for _, enc := range []interface{}{nil, "json"} {
		start := func(id, name string) UIMessageChunk {
			c := UIMessageChunk{"type": "tool-input-start", "toolCallId": id, "toolName": name}
			if enc != nil {
				c["inputEncoding"] = enc
			}
			return c
		}
		last := r78streamReadLast(t, "",
			UIMessageChunk{"type": "start", "messageId": "message-1"},
			UIMessageChunk{"type": "tool-input-start", "toolCallId": "text", "toolName": "exec", "inputEncoding": "text"},
			start("json", "weather"),
			start("encoded-string", "legacy"),
			UIMessageChunk{"type": "tool-input-delta", "toolCallId": "text", "inputTextDelta": `"quoted`},
			UIMessageChunk{"type": "tool-input-delta", "toolCallId": "json", "inputTextDelta": `{"city":"Paris"}`},
			UIMessageChunk{"type": "tool-input-delta", "toolCallId": "encoded-string", "inputTextDelta": `"already encoded"`},
			UIMessageChunk{"type": "tool-input-delta", "toolCallId": "text", "inputTextDelta": `"`},
		)
		parts, _ := last["parts"].([]interface{})
		want := []struct {
			id    string
			input interface{}
		}{{"text", `"quoted"`}, {"json", map[string]interface{}{"city": "Paris"}}, {"encoded-string", "already encoded"}}
		if len(parts) != len(want) {
			t.Fatalf("encoding %v: parts = %v", enc, parts)
		}
		for i, w := range want {
			p := parts[i].(map[string]interface{})
			if p["toolCallId"] != w.id || !reflect.DeepEqual(p["input"], w.input) {
				t.Errorf("encoding %v: part %d = %v, want toolCallId %s input %#v", enc, i, p, w.id, w.input)
			}
		}
	}
}

// read-ui-message-stream.test.ts 132 / 167: a seeded assistant message is
// kept when the stream starts with its id or with no start chunk, and
// replaced (metadata dropped) when the stream starts another message.
func TestR78StreamReadUIMessagesSeededMessage(t *testing.T) {
	rest := []UIMessageChunk{
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": " continued"},
		{"type": "text-end", "id": "text-1"},
		{"type": "finish"},
	}
	seed := `{"id":"resumed-response","role":"assistant","metadata":{"source":"seed"},"parts":[{"type":"text","text":"Partial response"}]}`
	want := map[string]interface{}{
		"id": "resumed-response", "role": "assistant", "metadata": map[string]interface{}{"source": "seed"},
		"parts": []interface{}{
			map[string]interface{}{"type": "text", "text": "Partial response"},
			map[string]interface{}{"type": "text", "text": " continued", "state": "done"},
		},
	}
	for name, chunks := range map[string][]UIMessageChunk{
		"the same message id": append([]UIMessageChunk{{"type": "start", "messageId": "resumed-response"}}, rest...),
		"no message id":       rest,
	} {
		if got := r78streamReadLast(t, seed, chunks...); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: last = %v\nwant %v", name, got, want)
		}
	}

	got := r78streamReadLast(t, `{"id":"previous-response","role":"assistant","metadata":{"source":"previous"},"parts":[{"type":"text","text":"Previous response"}]}`,
		UIMessageChunk{"type": "start", "messageId": "resumed-response"},
		UIMessageChunk{"type": "text-start", "id": "text-1"},
		UIMessageChunk{"type": "text-delta", "id": "text-1", "delta": "Resumed response"},
		UIMessageChunk{"type": "text-end", "id": "text-1"},
		UIMessageChunk{"type": "finish"},
	)
	wantNew := map[string]interface{}{
		"id": "resumed-response", "role": "assistant",
		"parts": []interface{}{map[string]interface{}{"type": "text", "text": "Resumed response", "state": "done"}},
	}
	if !reflect.DeepEqual(got, wantNew) {
		t.Errorf("different id: last = %v\nwant %v", got, wantNew)
	}
}

// stream-text.test.ts 17879 / 17883: options.stopWhen > 2 stop conditions:
// every condition is evaluated (TS Promise.all) with the steps so far, and
// one true condition ends the loop after the first step.
func TestR78StreamTwoStopConditions(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	cond := func(n int, stop bool) StopCondition {
		return func(s StopConditionState) string {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, fmt.Sprintf("%d:%d", n, len(s.Steps)))
			if stop {
				return "stop"
			}
			return ""
		}
	}
	r := r71streamRun(t, StreamTextOptions{
		Model:    r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "value"}), r73streamTextStep("Hello")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r71streamTool1(true, nil, nil)},
		StopWhen: []StopCondition{cond(0, false), cond(1, true)},
		Internal: r71streamInternal,
	})
	if n := len(r.Steps()); n != 1 {
		t.Fatalf("steps = %d, want 1", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(calls); got != "[0:1 1:1]" {
		t.Fatalf("stop condition calls = %s, want [0:1 1:1]", got)
	}
}
