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
	"github.com/digitallysavvy/go-ai/pkg/testutil"
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

func r79streamDeferredTool() types.Tool {
	return types.Tool{Name: "deferred_tool", Parameters: r71streamValueSchema, ProviderExecuted: true, SupportsDeferredResults: true}
}

func r79streamDeferredError() provider.StreamChunk {
	isErr := true
	return provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "deferred_tool", Result: "ERROR", IsError: &isErr, ProviderExecuted: true}}
}

func r79streamDescribe(content []types.ContentPart) string {
	var out []string
	for _, c := range content {
		switch p := c.(type) {
		case types.ToolCallContent:
			out = append(out, fmt.Sprintf("tool-call %s %s input=%v providerExecuted=%v dynamic=%v", p.ToolCallID, p.ToolName, p.Arguments, p.ProviderExecuted, p.Dynamic))
		case types.ToolErrorContent:
			input := fmt.Sprint(p.Input)
			if p.Input == nil {
				input = "<nil>"
			}
			out = append(out, fmt.Sprintf("tool-error %s %s input=%s error=%v providerExecuted=%v dynamic=%v", p.ToolCallID, p.ToolName, input, p.Error, p.ProviderExecuted, p.Dynamic))
		case types.TextContent:
			out = append(out, "text "+p.Text)
		default:
			out = append(out, fmt.Sprintf("%T", c))
		}
	}
	return strings.Join(out, "\n")
}

// stream-text.test.ts 29421: a provider-executed deferred call answered by a
// tool-error in the same step resolves the call; the run ends after one step.
func TestR79StreamDeferredToolErrorSameStep(t *testing.T) {
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "deferred_tool", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{ "value": "test" }`, ProviderExecuted: true}},
		r79streamDeferredError(),
		{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Final response"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, _ := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "p", Tools: []types.Tool{r79streamDeferredTool()}, StopWhen: []StopCondition{StepCountIs(2)}})
	if n := len(r.Steps()); n != 1 {
		t.Fatalf("steps = %d, want 1", n)
	}
	want := "tool-call call-1 deferred_tool input=map[value:test] providerExecuted=true dynamic=false\n" +
		"tool-error call-1 deferred_tool input=map[value:test] error=ERROR providerExecuted=true dynamic=false\n" +
		"text Final response"
	if got := r79streamDescribe(r.Content()); got != want {
		t.Fatalf("content =\n%s\nwant\n%s", got, want)
	}
}

// stream-text.test.ts 29526: the deferred call's tool-error arrives in the
// next step and resolves it; the run ends after two steps. The final step's
// content has the call (from the step it was made in) before its error,
// whose input is undefined.
func TestR79StreamDeferredToolErrorLaterStep(t *testing.T) {
	first := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "deferred_tool", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{ "value": "test" }`, ProviderExecuted: true}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	}
	second := []provider.StreamChunk{
		r79streamDeferredError(),
		{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Final response"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, _ := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, first, second), Prompt: "p", Tools: []types.Tool{r79streamDeferredTool()}, StopWhen: []StopCondition{StepCountIs(3)}})
	if n := len(r.Steps()); n != 2 {
		t.Fatalf("steps = %d, want 2", n)
	}
	want := "tool-call call-1 deferred_tool input=map[value:test] providerExecuted=true dynamic=false\n" +
		"tool-error call-1 deferred_tool input=<nil> error=ERROR providerExecuted=true dynamic=false\n" +
		"text Final response"
	if got := r79streamDescribe(r.Content()); got != want {
		t.Fatalf("content =\n%s\nwant\n%s", got, want)
	}
}

// stream-text.test.ts 18225: a text part id that repeats in a later step is
// remapped to the next generated id (id-0 and id-1 are the step response
// ids), the first one is kept, and the step texts are unchanged.
func TestR79StreamMultiStepTextIDRemapped(t *testing.T) {
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}}
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "0"}, {Type: provider.ChunkTypeText, ID: "0", Text: "Let me check."}, {Type: provider.ChunkTypeTextEnd, ID: "0"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{ "value": "test" }`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage}},
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "0"}, {Type: provider.ChunkTypeText, ID: "0", Text: "It is sunny."}, {Type: provider.ChunkTypeTextEnd, ID: "0"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}})
	r, parts := r73streamParts(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(2)},
		Internal: &InternalOptions{GenerateID: r73streamCounterIDs("id")}})
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError {
			t.Fatalf("error part: %v", p.Err)
		}
	}
	got := fmt.Sprint(r73streamIDParts(parts, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd))
	if want := "[text-start:0: text:0:Let me check. text-end:0: text-start:id-2: text:id-2:It is sunny. text-end:id-2:]"; got != want {
		t.Fatalf("text parts = %s\nwant %s", got, want)
	}
	if steps := r.Steps(); len(steps) != 2 || steps[0].Text != "Let me check." || steps[1].Text != "It is sunny." {
		t.Fatalf("steps = %+v", steps)
	}
}

// stream-text.test.ts 4162: without stream retries an error part reaches
// onChunk before onError.
func TestR79StreamOnChunkBeforeOnErrorWithoutRetries(t *testing.T) {
	var mu sync.Mutex
	var order []string
	add := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeError, Err: errors.New("provider error"), Text: "provider error"}}),
		Prompt: "test-input",
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeError {
				add("onChunk")
			}
		},
		OnError: func(context.Context, error) { add("onError") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(order); got != "[onChunk onError]" {
		t.Fatalf("order = %s, want [onChunk onError]", got)
	}
}

// stream-text.test.ts 4189: with streamRetries 1 the model is called twice,
// onError fires for both errors, the full stream carries the last one and
// the finish reason is error.
func TestR79StreamFinalErrorAfterRetriesExhausted(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		e := fmt.Errorf("provider error %d", n)
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeError, Err: e, Text: e.Error()}}), nil
	}}
	retries := 1
	var errs []string
	r, parts := r73streamParts(t, StreamTextOptions{Model: model, Prompt: "test-input", StreamRetries: &retries, OnError: func(_ context.Context, err error) {
		mu.Lock()
		errs = append(errs, err.Error())
		mu.Unlock()
	}})
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if got := fmt.Sprint(errs); got != "[provider error 1 provider error 2]" {
		t.Fatalf("onError = %s", got)
	}
	var streamErrs []string
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError {
			streamErrs = append(streamErrs, p.Err.Error())
		}
	}
	if got := fmt.Sprint(streamErrs); got != "[provider error 2]" {
		t.Fatalf("stream errors = %s, want [provider error 2]", got)
	}
	if r.FinishReason() != types.FinishReasonError {
		t.Fatalf("finish reason = %s, want error", r.FinishReason())
	}
}

// stream-text.test.ts 12611 (Go form: OnError panics): the error part is
// still in the full stream, the finish reason is error, one step is recorded
// and the step/finish callbacks each run once.
func TestR79StreamOnErrorPanicKeepsErrorAndFinish(t *testing.T) {
	providerErr := errors.New("provider error")
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeError, Err: providerErr, Text: providerErr.Error()},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError, RawFinishReason: "error", Usage: r72streamTestUsage},
	}
	var mu sync.Mutex
	stepFinish, finish := 0, 0
	r, parts := r73streamParts(t, StreamTextOptions{
		Model:        r71streamModel(nil, step),
		Prompt:       "test-input",
		OnError:      func(context.Context, error) { panic("callback error") },
		OnStepFinish: func(context.Context, types.StepResult, interface{}) { mu.Lock(); stepFinish++; mu.Unlock() },
		OnFinish:     func(*StreamTextResult) { mu.Lock(); finish++; mu.Unlock() },
	})
	found := false
	for _, p := range parts {
		if p.Type == provider.ChunkTypeError && errors.Is(p.Err, providerErr) {
			found = true
		}
	}
	if !found {
		t.Fatalf("full stream has no provider error part: %v", r73streamNames(parts))
	}
	if r.FinishReason() != types.FinishReasonError {
		t.Fatalf("finish reason = %s, want error", r.FinishReason())
	}
	if n := len(r.Steps()); n != 1 {
		t.Fatalf("steps = %d, want 1", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if stepFinish != 1 || finish != 1 {
		t.Fatalf("onStepFinish = %d, onFinish = %d, want 1 each", stepFinish, finish)
	}
}

// stream-text.test.ts 22669: onStepEnd is preferred over the deprecated
// onStepFinish (only onStepEnd runs).
func TestR79StreamOnStepEndPreferredOverOnStepFinish(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	r, _ := r73streamParts(t, StreamTextOptions{
		Model:  r71streamModel(nil, r71streamHelloWorld),
		Prompt: "test-input",
		OnStepEnd: func(context.Context, types.StepResult, interface{}) {
			mu.Lock()
			calls = append(calls, "onStepEnd")
			mu.Unlock()
		},
		OnStepFinish: func(context.Context, types.StepResult, interface{}) {
			mu.Lock()
			calls = append(calls, "onStepFinish")
			mu.Unlock()
		},
	})
	_ = r.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(calls); got != "[onStepEnd]" {
		t.Fatalf("calls = %s, want [onStepEnd]", got)
	}
}

// r79streamAbortableStream streams text-start and a delta, then blocks until
// its ctx is done (TS createAbortableModel errors the stream on abort).
type r79streamAbortableStream struct {
	ctx  context.Context
	sent int
}

func (s *r79streamAbortableStream) Next() (*provider.StreamChunk, error) {
	s.sent++
	switch s.sent {
	case 1:
		return &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "text-1"}, nil
	case 2:
		return &provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text-1", Text: "Hello"}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func (s *r79streamAbortableStream) Close() error { return nil }
func (s *r79streamAbortableStream) Err() error   { return nil }

// ui-message-stream-cancellation.node.test.ts 254: the client disconnects
// after the first text delta and the run is aborted. With a ConsumeSSEStream
// callback that keeps reading its copy, the UI stream still sees the abort:
// onEnd reports isAborted with outcome aborted and no isCancelled.
func TestR79StreamObservedAbortWhenConsumeSSEStreamKeepsSourceFlowing(t *testing.T) {
	abortCtx, abort := context.WithCancel(context.Background())
	defer abort()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		return &r79streamAbortableStream{ctx: ctx}, nil
	}}
	var mu sync.Mutex
	aborted := false
	r, err := StreamText(abortCtx, StreamTextOptions{Model: model, Prompt: "Hello", OnError: func(context.Context, error) {}, OnAbort: func(context.Context, []types.StepResult) {
		mu.Lock()
		aborted = true
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan map[string]interface{}, 1)
	resp, err := CreateUIMessageStreamResponseWithInit(context.Background(), r, &UIMessageStreamResponseInit{
		ConsumeSSEStream: func(rd io.Reader) error {
			_, err := io.Copy(io.Discard, rd)
			return err
		},
	}, UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) { ended <- e }})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "text-delta") {
		n, err := resp.Body.Read(buf)
		seen.Write(buf[:n])
		if err != nil {
			t.Fatalf("body ended before a text delta: %v (%q)", err, seen.String())
		}
	}
	// The client disconnects: the server aborts the run and cancels the
	// response reader.
	abort()
	_ = resp.Body.Close()
	select {
	case e := <-ended:
		if _, ok := e["isCancelled"]; ok || e["isAborted"] != true || fmt.Sprint(e["outcome"]) != "map[status:aborted]" {
			t.Fatalf("onEnd = isAborted %v isCancelled %v outcome %v, want aborted", e["isAborted"], e["isCancelled"], e["outcome"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onEnd never ran")
	}
	mu.Lock()
	defer mu.Unlock()
	if !aborted {
		t.Fatal("onAbort did not fire")
	}
}

func r79streamFailingConversion() (*StreamTextResult, UIMessageStreamResultOptions) {
	res := &StreamTextResult{stream: testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish},
	})}
	return res, UIMessageStreamResultOptions{
		MessageMetadata: func(map[string]interface{}) map[string]interface{} { panic("metadata boom") },
	}
}

// TS JsonToSseTransformStream writes "data: [DONE]" in flush, which an
// errored UI message stream never reaches: a failed UI conversion (here a
// panicking MessageMetadata) ends the SSE stream without [DONE]. The pipe
// helper returns the failure, the response body ends with it, and the
// ConsumeSSEStream copy's reader gets it (the tee's second branch errors
// too). Go wrote [DONE] and ended the body and the copy normally.
func TestR79StreamFailedConversionEndsWithoutDone(t *testing.T) {
	t.Run("pipe", func(t *testing.T) {
		res, opts := r79streamFailingConversion()
		var body strings.Builder
		copyErr := make(chan error, 1)
		err := PipeUIMessageStreamToResponseWithInit(context.Background(), res, &body, &UIMessageStreamResponseInit{
			ConsumeSSEStream: func(r io.Reader) error {
				_, err := io.ReadAll(r)
				copyErr <- err
				return err
			},
		}, opts)
		if err == nil || !strings.Contains(err.Error(), "metadata boom") {
			t.Fatalf("Pipe = %v, want the conversion failure", err)
		}
		if strings.Contains(body.String(), "[DONE]") {
			t.Fatalf("body = %q, want no [DONE]", body.String())
		}
		select {
		case err := <-copyErr:
			if err == nil || !strings.Contains(err.Error(), "metadata boom") {
				t.Fatalf("copy read error = %v, want the conversion failure", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the consumer copy never ended")
		}
	})
	t.Run("response", func(t *testing.T) {
		res, opts := r79streamFailingConversion()
		resp, err := CreateUIMessageStreamResponseWithInit(context.Background(), res, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		if err == nil || !strings.Contains(err.Error(), "metadata boom") {
			t.Fatalf("body read error = %v, want the conversion failure", err)
		}
		if strings.Contains(string(b), "[DONE]") {
			t.Fatalf("body = %q, want no [DONE]", b)
		}
	})
}
