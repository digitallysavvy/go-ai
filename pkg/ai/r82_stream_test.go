package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var r82streamValueSchema = map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
	"required":   []interface{}{"value"},
}

// r82streamErrors collects the errors OnError sees.
type r82streamErrors struct {
	mu   sync.Mutex
	errs []error
}

func (e *r82streamErrors) onError(_ context.Context, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errs = append(e.errs, err)
}

func (e *r82streamErrors) all() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.errs...)
}

// stream-text.test.ts 1591: result.stream > should use fallback response
// metadata when response metadata is not provided: finish-step carries the
// generated id, the model id and a timestamp of "now".
func TestR82streamFallbackResponseMetadata(t *testing.T) {
	ids := 2000
	before := time.Now()
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
			{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			r77streamFinishStop(),
		}),
		Prompt: "test-input",
		Internal: &InternalOptions{GenerateID: func() string {
			id := fmt.Sprintf("id-%d", ids)
			ids++
			return id
		}},
	})
	r75streamAssertJSON(t, "parts", r77streamPartsJSON(parts, false), `[
		{"type":"start"},{"type":"start-step"},
		{"type":"text-start","id":"1"},
		{"type":"text-delta","id":"1","text":"Hello"},
		{"type":"text-delta","id":"1","text":", "},
		{"type":"text-delta","id":"1","text":"world!"},
		{"type":"text-end","id":"1"},
		{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-2000","modelId":"mock-model-id"},"usage":{"inputTokens":3,"outputTokens":10,"totalTokens":13},"hasTimeBetweenOutputChunks":true},
		{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokens":3,"outputTokens":10,"totalTokens":13}}]`)
	ts := parts[7].Response.Timestamp
	if ts.Before(before.Add(-time.Second)) || ts.After(time.Now().Add(time.Second)) {
		t.Fatalf("fallback timestamp %v is not the current time", ts)
	}
}

// stream-text.test.ts 2144: tool choice enforcement > should surface an
// error when a different tool is called instead of the required tool.
func TestR82streamToolChoiceDifferentTool(t *testing.T) {
	var errs r82streamErrors
	executed := 0
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool2", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
		}),
		Tools: []types.Tool{
			{Name: "tool1", Parameters: r82streamValueSchema},
			{Name: "tool2", Parameters: r82streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed++
				return "r", nil
			}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "tool1"},
		Prompt:     "test-input",
		OnError:    errs.onError,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	got := errs.all()
	if len(got) != 1 {
		t.Fatalf("onError calls = %d, want 1 (%v)", len(got), got)
	}
	var v *ToolChoiceViolationError
	if !errors.As(got[0], &v) {
		t.Fatalf("onError error = %T %v", got[0], got[0])
	}
	if v.Error() != "Model response did not contain a call to the required tool 'tool1'." {
		t.Fatalf("message = %q", v.Error())
	}
	if v.ToolChoice.Type != types.ToolChoiceTool || v.ToolChoice.ToolName != "tool1" || v.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("violation = %+v", v)
	}
	if len(v.Content) != 1 {
		t.Fatalf("content = %#v", v.Content)
	}
	tc, ok := v.Content[0].(types.ToolCallContent)
	if !ok || tc.ToolCallID != "call-1" || tc.ToolName != "tool2" || tc.Input != `{ "value": "value" }` {
		t.Fatalf("content[0] = %#v", v.Content[0])
	}
	if fr := r.FinishReason(); fr != types.FinishReasonError {
		t.Fatalf("finishReason = %q, want error", fr)
	}
	if executed != 0 {
		t.Fatalf("tool2 executed %d times", executed)
	}
}

// stream-text.test.ts 2205: tool choice enforcement > should enforce the tool
// choice returned by prepareStep.
func TestR82streamToolChoiceFromPrepareStep(t *testing.T) {
	var errs r82streamErrors
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "No tool call."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			r77streamFinishStop(),
		}),
		Tools:      []types.Tool{{Name: "tool1", Parameters: r82streamValueSchema}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			step.ToolChoice = types.ToolChoice{Type: types.ToolChoiceRequired}
			return step
		},
		Prompt:  "test-input",
		OnError: errs.onError,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	got := errs.all()
	var v *ToolChoiceViolationError
	if len(got) != 1 || !errors.As(got[0], &v) || v.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("onError = %v, want one required-tool-choice violation", got)
	}
}

// stream-text.test.ts 2569: result.stream > should pass through
// providerMetadata on tool-input-start.
func TestR82streamToolInputStartProviderMetadata(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}},
			{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool"}, ProviderMetadata: json.RawMessage(`{"testProvider":{"someKey":"someValue"}}`)},
			{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{"value":"test"}`},
			{Type: provider.ChunkTypeToolInputEnd, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{"value":"test"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}),
		Tools:      []types.Tool{{Name: "test-tool", Parameters: r82streamValueSchema}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		Prompt:     "test-input",
	})
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolInputStart {
			r75streamAssertJSON(t, "tool-input-start providerMetadata", json.RawMessage(p.ProviderMetadata), `{"testProvider":{"someKey":"someValue"}}`)
			return
		}
	}
	t.Fatal("no tool-input-start part")
}

// r82streamAttemptModel returns a model whose n-th doStream (1-based) opens
// steps[n-1] (the last one repeats) with request body "request-<n>".
func r82streamAttemptModel(calls *int, steps ...func(call int) []provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		*calls++
		call := *calls
		mu.Unlock()
		i := min(call, len(steps)) - 1
		return &requestBodyTextStream{TextStream: testutil.NewMockTextStream(steps[i](call)), body: fmt.Sprintf("request-%d", call)}, nil
	}}
}

func r82streamMeta(id, model string, ms int64, call int) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
		ID: id, ModelID: model, Timestamp: time.UnixMilli(ms), Headers: map[string]string{"call": fmt.Sprintf("%d", call)},
	}}
}

func r82streamTextStream(t *testing.T, r *StreamTextResult) []string {
	t.Helper()
	var out []string
	s := r.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			return out
		}
		if c.Type == provider.ChunkTypeText {
			out = append(out, c.Text)
		}
	}
}

// stream-text.test.ts 3154: errors > should preserve attempt isolation when
// a transform reconstructs recovered parts, and 3358: should use request and
// response metadata from the recovered attempt.
func TestR82streamRecoveredAttemptIsolation(t *testing.T) {
	type obj struct {
		X string `json:"x"`
	}
	retries := 1
	calls := 0
	model := r82streamAttemptModel(&calls,
		func(call int) []provider.StreamChunk {
			return []provider.StreamChunk{
				r82streamMeta("failed-id", "failed-model", 1, call),
				{Type: provider.ChunkTypeTextStart, ID: "failed-text"},
				{Type: provider.ChunkTypeText, ID: "failed-text", Text: `{"x":`},
				{Type: provider.ChunkTypeError, Err: errors.New("provider error")},
			}
		},
		func(call int) []provider.StreamChunk {
			return []provider.StreamChunk{
				r82streamMeta("recovered-id", "recovered-model", 2, call),
				{Type: provider.ChunkTypeTextStart, ID: "recovered-text"},
				{Type: provider.ChunkTypeText, ID: "recovered-text", Text: `{"x":"ok"}`},
				{Type: provider.ChunkTypeTextEnd, ID: "recovered-text"},
				r77streamFinishStop(),
			}
		})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "test-input",
		Output:        ObjectOutput[obj](ObjectOutputOptions{Schema: SchemaFor[obj]()}),
		StreamRetries: &retries,
		Include:       &IncludeOptions{RequestBody: true},
		ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
			if c.Type == provider.ChunkTypeTextStart {
				return []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: c.ID, ProviderMetadata: c.ProviderMetadata}}
			}
			return []provider.StreamChunk{c}
		}},
		OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r82streamTextStream(t, r); fmt.Sprint(got) != fmt.Sprint([]string{`{"x":`, `{"x":"ok"}`}) {
		t.Fatalf("textStream = %q", got)
	}
	if out, ok := r.Output().(obj); !ok || out.X != "ok" {
		t.Fatalf("output = %#v", r.Output())
	}
	final := r.FinalStep()
	if final.Text != `{"x":"ok"}` {
		t.Fatalf("final text = %q", final.Text)
	}
	if final.Request.Body != "request-2" {
		t.Fatalf("request body = %v", final.Request.Body)
	}
	resp := final.Response
	if resp.ID != "recovered-id" || resp.ModelID != "recovered-model" || !resp.Timestamp.Equal(time.UnixMilli(2)) || fmt.Sprint(resp.Headers) != "map[call:2]" {
		t.Fatalf("response = id %q model %q ts %v headers %v", resp.ID, resp.ModelID, resp.Timestamp, resp.Headers)
	}
	r75streamAssertJSON(t, "response messages", r76streamRoleText(resp.Messages), `[{"role":"assistant","content":[{"type":"text","text":"{\"x\":\"ok\"}"}]}]`)
	r75streamAssertJSON(t, "responseMessages", r76streamRoleText(r.ResponseMessages()), `[{"role":"assistant","content":[{"type":"text","text":"{\"x\":\"ok\"}"}]}]`)
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

// TS takes the step's response headers from each doStream result, so a
// recovered attempt without headers has none; the failed attempt's headers
// must not carry over.
func TestR82streamRecoveredAttemptWithoutHeaders(t *testing.T) {
	retries := 1
	calls := 0
	model := r82streamAttemptModel(&calls,
		func(call int) []provider.StreamChunk {
			return []provider.StreamChunk{
				r82streamMeta("failed-id", "failed-model", 1, call),
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "partial"},
				{Type: provider.ChunkTypeError, Err: errors.New("provider error")},
			}
		},
		func(int) []provider.StreamChunk {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "2"},
				{Type: provider.ChunkTypeText, ID: "2", Text: "recovered"},
				{Type: provider.ChunkTypeTextEnd, ID: "2"},
				r77streamFinishStop(),
			}
		})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input", StreamRetries: &retries, OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	resp := r.FinalStep().Response
	if resp.ID == "failed-id" || resp.ModelID == "failed-model" || len(resp.Headers) != 0 {
		t.Fatalf("response kept the failed attempt: id %q model %q headers %v", resp.ID, resp.ModelID, resp.Headers)
	}
	if resp.ModelID != "mock-model-id" {
		t.Fatalf("model id = %q, want the model's own id", resp.ModelID)
	}
}

// stream-text.test.ts 3259: errors > should reset failed-attempt state when
// the recovered stream ends without output.
func TestR82streamRecoveredStreamWithoutOutput(t *testing.T) {
	retries := 1
	calls := 0
	var errs r82streamErrors
	model := r82streamAttemptModel(&calls,
		func(int) []provider.StreamChunk {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "failed-text"},
				{Type: provider.ChunkTypeText, ID: "failed-text", Text: "partial"},
				{Type: provider.ChunkTypeError, Err: errors.New("provider error")},
			}
		},
		func(int) []provider.StreamChunk { return nil })
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input", StreamRetries: &retries, OnError: errs.onError,
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []provider.StreamChunk
	s := r.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	sawPartial := false
	var terminal *provider.StreamChunk
	for i := range parts {
		if parts[i].Type == provider.ChunkTypeText && parts[i].ID == "failed-text" && parts[i].Text == "partial" {
			sawPartial = true
		}
		if parts[i].Type == provider.ChunkTypeError && terminal == nil {
			terminal = &parts[i]
		}
	}
	if !sawPartial {
		t.Fatalf("no partial text-delta in %v", r73streamNames(parts))
	}
	const msg = "No output generated. The model stream ended without a finish chunk."
	if terminal == nil || !IsNoOutputGeneratedError(terminal.Err) {
		t.Fatalf("terminal error part = %+v (parts %v)", terminal, r73streamNames(parts))
	}
	if rerr := r.Err(); rerr == nil || rerr.Error() != msg {
		t.Fatalf("Err() = %v, want %q", rerr, msg)
	}
	got := errs.all()
	if len(got) != 2 || got[0].Error() != "provider error" || got[1].Error() != msg {
		t.Fatalf("onError = %v", got)
	}
}

// TS createStepResponse takes each step's headers from that step's doStream
// result: a second step without headers reports none, and so does the
// result's response (the last step's).
func TestR82streamStepHeadersDoNotCarryOver(t *testing.T) {
	calls := 0
	model := r82streamAttemptModel(&calls,
		func(call int) []provider.StreamChunk {
			return []provider.StreamChunk{
				r82streamMeta("id-1", "m", 1, call),
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}, RawArguments: `{"value":"v"}`}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}
		},
		func(int) []provider.StreamChunk {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "2"},
				{Type: provider.ChunkTypeText, ID: "2", Text: "done"},
				{Type: provider.ChunkTypeTextEnd, ID: "2"},
				r77streamFinishStop(),
			}
		})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input",
		Tools: []types.Tool{{Name: "tool1", Parameters: r82streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	steps := r.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	if fmt.Sprint(steps[0].Response.Headers) != "map[call:1]" || len(steps[1].Response.Headers) != 0 {
		t.Fatalf("step headers = %v, %v", steps[0].Response.Headers, steps[1].Response.Headers)
	}
	if h := r.ResponseHeaders(); len(h) != 0 {
		t.Fatalf("result response headers = %v, want the last step's (none)", h)
	}
}

// r82streamProcess applies chunks to a fresh message state (TS
// processUIMessageStream) and returns the final message.
func r82streamProcess(t *testing.T, chunks []UIMessageChunk) UIMessage {
	t.Helper()
	state := newUIMessageCallbackState(nil, "msg-123")
	for _, c := range chunks {
		var failed error
		state.apply(c, func(err error) string {
			failed = err
			return ""
		})
		if failed != nil {
			t.Fatalf("chunk %v: %v", c["type"], failed)
		}
	}
	msg, err := UIMessageFromChunk(state.responseMessage())
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// process-ui-message-stream.test.ts 7293: dynamic tool errors after input
// streaming > clears raw input on tool input error / tool output error, and
// the message validates without warnings.
func TestR82streamDynamicToolErrorClearsRawInput(t *testing.T) {
	for name, terminal := range map[string]UIMessageChunk{
		"tool input error": {"type": "tool-input-error", "toolCallId": "call-1", "toolName": "cityAttractions",
			"input": map[string]interface{}{"cities": []interface{}{"San Francisco"}}, "errorText": "Invalid input for tool cityAttractions", "dynamic": true},
		"tool output error": {"type": "tool-output-error", "toolCallId": "call-1", "errorText": "Tool execution failed", "dynamic": true},
	} {
		t.Run(name, func(t *testing.T) {
			setupLogWarnings(t)
			warned := 0
			SetLogWarnings(func(LogWarningsOptions) { warned++ })
			msg := r82streamProcess(t, []UIMessageChunk{
				{"type": "start"}, {"type": "start-step"},
				{"type": "tool-input-start", "toolCallId": "call-1", "toolName": "cityAttractions", "dynamic": true},
				{"type": "tool-input-delta", "toolCallId": "call-1", "inputTextDelta": `{ "cities": ["San Francisco"] }`},
				terminal,
				{"type": "finish-step"}, {"type": "finish"},
			})
			b, _ := json.Marshal(msg)
			var m map[string]interface{}
			_ = json.Unmarshal(b, &m)
			var tool map[string]interface{}
			for _, p := range m["parts"].([]interface{}) {
				if pm := p.(map[string]interface{}); pm["toolCallId"] == "call-1" {
					tool = pm
				}
			}
			if tool == nil || tool["type"] != "dynamic-tool" || tool["state"] != "output-error" {
				t.Fatalf("tool part = %v", tool)
			}
			if _, ok := tool["rawInput"]; ok {
				t.Fatalf("rawInput kept: %v", tool)
			}
			if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: []interface{}{msg}}); err != nil {
				t.Fatalf("ValidateUIMessages: %v", err)
			}
			if warned != 0 {
				t.Fatalf("logWarnings called %d times", warned)
			}
		})
	}
}

func r82streamCollect(t *testing.T, out <-chan UIMessageChunk, errCh <-chan error) ([]UIMessageChunk, error) {
	t.Helper()
	var got []UIMessageChunk
	for c := range out {
		got = append(got, c)
	}
	return got, <-errCh
}

// create-ui-message-stream.test.ts 130: should send async message
// annotation and close the stream.
func TestR82streamCreateAsyncWrite(t *testing.T) {
	wait := make(chan struct{})
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{Execute: func(w UIMessageStreamWriter) {
		<-wait
		w.Write(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "1a"})
	}})
	close(wait)
	got, err := r82streamCollect(t, out, errCh)
	if err != nil {
		t.Fatal(err)
	}
	r75streamAssertJSON(t, "chunks", got, `[{"delta":"1a","id":"1","type":"text-delta"}]`)
}

// create-ui-message-stream.test.ts 1039: reports invalid chunk processing as
// failed after completion was declared: the stream fails and onEnd's outcome
// is failed with that same error.
func TestR82streamCreateInvalidChunkAfterCompleted(t *testing.T) {
	var events []map[string]interface{}
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.SetOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeCompleted})
			w.Write(UIMessageChunk{"type": "finish"})
			w.Write(UIMessageChunk{"type": "text-delta", "id": "missing", "delta": "text"})
		},
		OnEnd: func(e map[string]interface{}) { events = append(events, e) },
	})
	_, err := r82streamCollect(t, out, errCh)
	if err == nil {
		t.Fatal("stream did not fail")
	}
	if len(events) != 1 {
		t.Fatalf("onEnd calls = %d", len(events))
	}
	outcome, _ := events[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != err {
		t.Fatalf("outcome = %#v, want failed with %v", outcome, err)
	}
}

// create-sse-stream-with-keep-alive.test.ts 125: should preserve source
// chunks and completion (the opening comment first).
func TestR82streamKeepAlivePreservesChunks(t *testing.T) {
	keepAlive := 100 * time.Millisecond
	var buf bytes.Buffer
	if err := PipeUIMessageChunksToResponse(r73streamChunkChan(
		UIMessageChunk{"type": "text-delta", "id": "1", "delta": "data 1"},
		UIMessageChunk{"type": "text-delta", "id": "1", "delta": "data 2"},
	), &buf, &UIMessageStreamResponseInit{KeepAliveMs: &keepAlive}); err != nil {
		t.Fatal(err)
	}
	want := ": stream-open\n\n" +
		"data: {\"delta\":\"data 1\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n" +
		"data: {\"delta\":\"data 2\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n" +
		"data: [DONE]\n\n"
	if buf.String() != want {
		t.Fatalf("body = %q", buf.String())
	}
}

// create-sse-stream-with-keep-alive.test.ts 88: should reset the keep-alive
// timer after source activity.
func TestR82streamKeepAliveResetsAfterChunk(t *testing.T) {
	keepAlive := 400 * time.Millisecond
	buf := &syncBuffer{}
	chunks := make(chan UIMessageChunk)
	done := make(chan error, 1)
	go func() {
		done <- PipeUIMessageChunksToResponse(chunks, buf, &UIMessageStreamResponseInit{KeepAliveMs: &keepAlive})
	}()
	waitForContains(t, buf, []byte(sseStreamOpenComment), time.Second)
	time.Sleep(200 * time.Millisecond)
	chunks <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": "data"}
	waitForContains(t, buf, []byte("data"), time.Second)
	// The original timer would fire 200ms from here; the reset one 400ms.
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(buf.String(), sseKeepAliveComment) {
		t.Fatalf("keep-alive before the reset interval elapsed: %q", buf.String())
	}
	waitForContains(t, buf, []byte(sseKeepAliveComment), 2*time.Second)
	close(chunks)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// pipe-ui-message-stream-to-response.test.ts 31 (headers, status and the
// encoded stream), 123 (an error chunk) and 151 (a toUIMessageStream source
// with sendStart false). Go writes the chunk keys sorted (known difference
// "UI message stream JSON key order").
func TestR82streamPipeChunksToResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := PipeUIMessageChunksToResponse(r73streamChunkChan(
		UIMessageChunk{"type": "text-start", "id": "1"},
		UIMessageChunk{"type": "text-delta", "id": "1", "delta": "test-data"},
		UIMessageChunk{"type": "text-end", "id": "1"},
	), rec, &UIMessageStreamResponseInit{Status: 200, StatusText: "OK", Headers: map[string]string{"Custom-Header": "test"}}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	gotHeaders := map[string]string{}
	for k := range rec.Header() {
		gotHeaders[strings.ToLower(k)] = rec.Header().Get(k)
	}
	r75streamAssertJSON(t, "headers", gotHeaders, `{"cache-control":"no-cache","connection":"keep-alive","content-type":"text/event-stream","custom-header":"test","x-accel-buffering":"no","x-vercel-ai-ui-message-stream":"v1"}`)
	if want := "data: {\"id\":\"1\",\"type\":\"text-start\"}\n\ndata: {\"delta\":\"test-data\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: {\"id\":\"1\",\"type\":\"text-end\"}\n\ndata: [DONE]\n\n"; rec.Body.String() != want {
		t.Fatalf("31 body = %q", rec.Body.String())
	}

	var buf bytes.Buffer
	if err := PipeUIMessageChunksToResponse(r73streamChunkChan(UIMessageChunk{"type": "error", "errorText": "Custom error message"}), &buf, &UIMessageStreamResponseInit{Status: 200}); err != nil {
		t.Fatal(err)
	}
	if want := "data: {\"errorText\":\"Custom error message\",\"type\":\"error\"}\n\ndata: [DONE]\n\n"; buf.String() != want {
		t.Fatalf("123 body = %q", buf.String())
	}

	f := false
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStart},
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
	})
	ui, uiErr := ToUIMessageStream(context.Background(), src, UIMessageStreamResultOptions{SendStart: &f})
	buf.Reset()
	if err := PipeUIMessageChunksToResponse(ui, &buf, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-uiErr; err != nil {
		t.Fatal(err)
	}
	if want := "data: {\"id\":\"t1\",\"type\":\"text-start\"}\n\ndata: {\"delta\":\"Hello\",\"id\":\"t1\",\"type\":\"text-delta\"}\n\ndata: {\"id\":\"t1\",\"type\":\"text-end\"}\n\ndata: [DONE]\n\n"; buf.String() != want {
		t.Fatalf("151 body = %q", buf.String())
	}
}

func r82streamI64(v int64) *int64 { return &v }

// TS testUsage / testUsage2 as LanguageModelUsage (asLanguageModelUsage
// fills noCache and text tokens from the totals).
var (
	r82streamTestUsage = &types.Usage{InputTokens: r82streamI64(3), OutputTokens: r82streamI64(10), TotalTokens: r82streamI64(13),
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: r82streamI64(3)},
		OutputDetails: &types.OutputTokenDetails{TextTokens: r82streamI64(10)}}
	r82streamTestUsage2 = &types.Usage{InputTokens: r82streamI64(3), OutputTokens: r82streamI64(10), TotalTokens: r82streamI64(13),
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: r82streamI64(3), CacheReadTokens: r82streamI64(0), CacheWriteTokens: r82streamI64(0)},
		OutputDetails: &types.OutputTokenDetails{TextTokens: r82streamI64(10), ReasoningTokens: r82streamI64(10)}}
)

// r82streamTwoStepRun is the TS "options.stopWhen > 2 steps: initial,
// tool-result" setup.
func r82streamTwoStepRun(t *testing.T) (*StreamTextResult, OnFinishEvent, []OnStepFinishEvent) {
	t.Helper()
	calls := 0
	model := r82streamAttemptModel(&calls,
		func(int) []provider.StreamChunk {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.UnixMilli(0), Headers: map[string]string{"call": "1"}}},
				{Type: provider.ChunkTypeReasoningStart, ID: "0"},
				{Type: provider.ChunkTypeReasoning, ID: "0", Reasoning: "thinking"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r82streamTestUsage},
			}
		},
		func(int) []provider.StreamChunk {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id", Timestamp: time.UnixMilli(1000), Headers: map[string]string{"call": "2"}}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r82streamTestUsage2},
			}
		})
	var mu sync.Mutex
	var end OnFinishEvent
	var stepEvents []OnStepFinishEvent
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input",
		Tools: []types.Tool{{Name: "tool1", Parameters: r82streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(),
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
	})
	if calls != 2 {
		t.Fatalf("doStream calls = %d", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	return r, end, stepEvents
}

// stream-text.test.ts 15255 (totalUsage), 15274 (usage), 15293
// (finalStep.usage), 15313 (onFinish usage, totalUsage and finalStep), 15335
// (finishReason) and 15339 (text) of "2 steps: initial, tool-result > value
// promises".
func TestR82streamTwoStepValuePromises(t *testing.T) {
	r, end, _ := r82streamTwoStepRun(t)
	const total = `{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26}`
	r75streamAssertJSON(t, "15255 totalUsage", r.TotalUsage(), total)
	r75streamAssertJSON(t, "15274 usage", r.Usage(), total)
	const final = `{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13}`
	r75streamAssertJSON(t, "15293 finalStep.usage", r.FinalStep().Usage, final)
	r75streamAssertJSON(t, "15313 onFinish usage", end.Usage, total)
	r75streamAssertJSON(t, "15313 onFinish totalUsage", end.TotalUsage, total)
	r75streamAssertJSON(t, "15313 onFinish finalStep.usage", end.FinalStep.Usage, final)
	if len(end.Steps) != 2 || end.Steps[1].Response.ID != end.FinalStep.Response.ID {
		t.Fatalf("15313 finalStep is not the last step: %d steps", len(end.Steps))
	}
	if fr := r.FinishReason(); fr != types.FinishReasonStop {
		t.Fatalf("15335 finishReason = %q", fr)
	}
	if txt := r.Text(); txt != "Hello, world!" {
		t.Fatalf("15339 text = %q", txt)
	}
	steps := r.Steps()
	if fmt.Sprint(steps[0].Response.Headers, steps[1].Response.Headers) != "map[call:1] map[call:2]" {
		t.Fatalf("step headers = %v %v", steps[0].Response.Headers, steps[1].Response.Headers)
	}
}

// stream-text.test.ts 15343 (result.steps) and 15553 (responseMessages) of
// "2 steps: initial, tool-result > value promises": each step's content,
// finish reasons, model, response id/model/timestamp/headers/messages (the
// step's own), step numbers, usage and warnings.
func TestR82streamTwoStepSteps(t *testing.T) {
	r, end, stepEvents := r82streamTwoStepRun(t)
	steps := r.Steps()
	if len(steps) != 2 || len(end.Steps) != 2 || len(stepEvents) != 2 {
		t.Fatalf("steps = %d, onEnd steps = %d, onStepFinish = %d", len(steps), len(end.Steps), len(stepEvents))
	}
	type view struct {
		CallID          string      `json:"callId"`
		StepNumber      int         `json:"stepNumber"`
		Model           interface{} `json:"model"`
		Content         interface{} `json:"content"`
		FinishReason    string      `json:"finishReason"`
		RawFinishReason string      `json:"rawFinishReason"`
		ResponseID      string      `json:"responseId"`
		ResponseModel   string      `json:"responseModelId"`
		Timestamp       string      `json:"timestamp"`
		Headers         interface{} `json:"headers"`
		Messages        interface{} `json:"messages"`
		Usage           interface{} `json:"usage"`
		Warnings        int         `json:"warnings"`
		ToolExecution   []string    `json:"toolExecutionMs"`
	}
	toView := func(s types.StepResult) view {
		var ids []string
		for id := range s.Performance.ToolExecutionMs {
			ids = append(ids, id)
		}
		return view{s.CallID, s.StepNumber, s.Model, s.Content, string(s.FinishReason), s.RawFinishReason, s.Response.ID, s.Response.ModelID,
			s.Response.Timestamp.UTC().Format(time.RFC3339Nano), s.Response.Headers, s.Response.Messages, s.Usage, len(s.Warnings), ids}
	}
	const want = `[{"callId":"test-telemetry-call-id","stepNumber":0,"model":{"provider":"mock-provider","modelId":"mock-model-id"},` +
		`"content":[{"type":"reasoning","text":"thinking"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}},` +
		`{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"},"output":{"type":"text","value":"result1"}}],` +
		`"finishReason":"tool-calls","rawFinishReason":"","responseId":"id-0","responseModelId":"mock-model-id","timestamp":"1970-01-01T00:00:00Z","headers":{"call":"1"},` +
		`"messages":[{"role":"assistant","content":[{"type":"reasoning","text":"thinking"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}],` +
		`"tool_calls":[{"arguments":{"value":"value"},"id":"call-1","toolName":"tool1"}]},` +
		`{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"result1"}}]}],` +
		`"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":0,"toolExecutionMs":["call-1"]},` +
		`{"callId":"test-telemetry-call-id","stepNumber":1,"model":{"provider":"mock-provider","modelId":"mock-model-id"},` +
		`"content":[{"type":"text","text":"Hello, world!"}],"finishReason":"stop","rawFinishReason":"stop","responseId":"id-1","responseModelId":"mock-model-id",` +
		`"timestamp":"1970-01-01T00:00:01Z","headers":{"call":"2"},"messages":[{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}],` +
		`"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":0,"toolExecutionMs":null}]`
	r74streamAssertJSON(t, "15343 steps", []view{toView(steps[0]), toView(steps[1])}, want)
	r74streamAssertJSON(t, "onEnd steps", []view{toView(end.Steps[0]), toView(end.Steps[1])}, want)
	r74streamAssertJSON(t, "15553 responseMessages", r.ResponseMessages(),
		`[{"role":"assistant","content":[{"type":"reasoning","text":"thinking"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}],`+
			`"tool_calls":[{"arguments":{"value":"value"},"id":"call-1","toolName":"tool1"}]},`+
			`{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"result1"}}]},`+
			`{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}]`)
}

func r82streamRawModel(captured *[]bool, raws ...interface{}) *testutil.MockLanguageModel {
	var mu sync.Mutex
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		*captured = append(*captured, o.IncludeRawChunks)
		mu.Unlock()
		chunks := []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}}
		for _, raw := range raws {
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw})
		}
		chunks = append(chunks,
			provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "test-id", ModelID: "test-model", Timestamp: time.UnixMilli(0)}},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r82streamTestUsage},
		)
		return testutil.NewMockTextStream(chunks), nil
	}}
}

// stream-text.test.ts raw chunks forwarding: 24638 (forwarded with
// include.rawChunks), 24690 (not forwarded when disabled), 24731 / 24950
// (the flag reaches the model), 24762 (onChunk gets the raw parts in order),
// 25065 (experimental_include alias) and 25095 (include wins over it).
func TestR82streamRawChunksForwarding(t *testing.T) {
	tr, f := true, false
	raw := map[string]interface{}{"type": "raw-data", "content": "should appear"}
	var flags []bool
	_, parts := r73streamParts(t, StreamTextOptions{Model: r82streamRawModel(&flags, raw), Prompt: "test prompt", Include: &IncludeOptions{RawChunks: &tr}})
	var raws []interface{}
	for _, p := range parts {
		if p.Type == provider.ChunkTypeRaw {
			raws = append(raws, p.Raw)
		}
	}
	r75streamAssertJSON(t, "24638 raw parts", raws, `[{"content":"should appear","type":"raw-data"}]`)

	_, parts = r73streamParts(t, StreamTextOptions{Model: r82streamRawModel(&flags, raw), Prompt: "test prompt", Include: &IncludeOptions{RawChunks: &f}})
	for _, p := range parts {
		if p.Type == provider.ChunkTypeRaw {
			t.Fatal("24690: raw part forwarded with rawChunks false")
		}
	}

	var mu sync.Mutex
	var onChunk []provider.StreamChunk
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags,
		map[string]interface{}{"type": "stream-start", "data": "start"},
		map[string]interface{}{"type": "response-metadata", "id": "test-id", "modelId": "test-model"},
		map[string]interface{}{"type": "text-delta", "content": "Hello"},
		map[string]interface{}{"type": "text-delta", "content": ", world!"},
		map[string]interface{}{"type": "finish", "reason": "stop"},
	), Prompt: "test prompt", Include: &IncludeOptions{RawChunks: &tr}, Internal: r72streamIDs(),
		OnChunk: func(c provider.StreamChunk) { mu.Lock(); onChunk = append(onChunk, c); mu.Unlock() }})
	mu.Lock()
	var names []string
	var rawValues []interface{}
	for i := range onChunk {
		// OnChunk keeps the Go-only stream-start marker (migration guide
		// #streamtext-full-stream-markers).
		if n := strings.SplitN(tsPartName(&onChunk[i]), ":", 2)[0]; n != "" && n != "stream-start" {
			names = append(names, n)
		}
		if onChunk[i].Type == provider.ChunkTypeRaw {
			rawValues = append(rawValues, onChunk[i].Raw)
		}
	}
	mu.Unlock()
	if want := "start start-step raw raw raw raw raw text-start text-delta text-end finish-step finish"; strings.Join(names, " ") != want {
		t.Fatalf("24762 onChunk = %q", strings.Join(names, " "))
	}
	r75streamAssertJSON(t, "24762 raw values", rawValues, `[{"data":"start","type":"stream-start"},{"id":"test-id","modelId":"test-model","type":"response-metadata"},{"content":"Hello","type":"text-delta"},{"content":", world!","type":"text-delta"},{"reason":"stop","type":"finish"}]`)

	flags = nil
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags), Prompt: "p", Include: &IncludeOptions{RawChunks: &tr}})
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags), Prompt: "p", Include: &IncludeOptions{RawChunks: &f}})
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags), Prompt: "p"})
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags), Prompt: "p", ExperimentalInclude: &IncludeOptions{RawChunks: &tr}})
	r71streamRun(t, StreamTextOptions{Model: r82streamRawModel(&flags), Prompt: "p", Include: &IncludeOptions{RawChunks: &f}, ExperimentalInclude: &IncludeOptions{RawChunks: &tr}})
	if fmt.Sprint(flags) != "[true false false true false]" {
		t.Fatalf("24731/24950/25065/25095 includeRawChunks = %v", flags)
	}
}

// stream-text.test.ts 1806: tool choice enforcement > should reject tool
// calls with none ($toolChoice, $stepToolChoice, $finishReason): one
// violation with the step's finish reason, finishReason error, the usage
// kept, and neither execute nor the input callbacks run.
func TestR82streamToolChoiceNoneRejectsCalls(t *testing.T) {
	for _, tc := range []struct {
		toolChoice, stepToolChoice types.ToolChoiceType
		finishReason               types.FinishReason
	}{
		{types.ToolChoiceNone, "", types.FinishReasonStop},
		{types.ToolChoiceNone, "", types.FinishReasonToolCalls},
		{types.ToolChoiceNone, "", types.FinishReasonInterrupted},
		{types.ToolChoiceAuto, types.ToolChoiceNone, types.FinishReasonToolCalls},
	} {
		t.Run(fmt.Sprintf("%s,%s,%s", tc.toolChoice, tc.stepToolChoice, tc.finishReason), func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			rec := func(name string) {
				mu.Lock()
				calls = append(calls, name)
				mu.Unlock()
			}
			var errs r82streamErrors
			opts := StreamTextOptions{
				Model: r71streamModel(nil, []provider.StreamChunk{
					{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1"}},
					{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: "{}"},
					{Type: provider.ChunkTypeToolInputEnd, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1"}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
					{Type: provider.ChunkTypeFinish, FinishReason: tc.finishReason, RawFinishReason: string(tc.finishReason), Usage: r82streamTestUsage},
				}),
				Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"},
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						rec("execute")
						return "tool-result", nil
					},
					OnInputStart:     func(context.Context, types.OnInputStartOptions) error { rec("onInputStart"); return nil },
					OnInputDelta:     func(context.Context, types.OnInputDeltaOptions) error { rec("onInputDelta"); return nil },
					OnInputAvailable: func(context.Context, types.OnInputAvailableOptions) error { rec("onInputAvailable"); return nil },
				}},
				ToolChoice: types.ToolChoice{Type: tc.toolChoice},
				Prompt:     "test-input",
				OnError:    errs.onError,
			}
			if tc.stepToolChoice != "" {
				opts.PrepareStep = func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
					s.ToolChoice = types.ToolChoice{Type: tc.stepToolChoice}
					return s
				}
			}
			r, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			_ = r.ConsumeStream()
			got := errs.all()
			var v *ToolChoiceViolationError
			if len(got) != 1 || !errors.As(got[0], &v) {
				t.Fatalf("onError = %v", got)
			}
			if v.Error() != "Model response contained a tool call even though tool choice was none." || v.ToolChoice.Type != types.ToolChoiceNone || v.FinishReason != tc.finishReason {
				t.Fatalf("violation = %q %+v", v.Error(), v)
			}
			if len(v.Content) != 1 {
				t.Fatalf("content = %#v", v.Content)
			}
			if _, ok := v.Content[0].(types.ToolCallContent); !ok {
				t.Fatalf("content[0] = %#v", v.Content[0])
			}
			if fr := r.FinishReason(); fr != types.FinishReasonError {
				t.Fatalf("finishReason = %q", fr)
			}
			r75streamAssertJSON(t, "usage", r.Usage(), `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`)
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != 0 {
				t.Fatalf("tool callbacks ran: %v", calls)
			}
		})
	}
}

// stream-text.test.ts 2013: tool choice enforcement > should preserve
// interrupted output without enforcing tool choice (required and a specific
// tool).
func TestR82streamToolChoiceInterruptedNotEnforced(t *testing.T) {
	for _, choice := range []types.ToolChoice{{Type: types.ToolChoiceRequired}, {Type: types.ToolChoiceTool, ToolName: "tool1"}} {
		t.Run(string(choice.Type), func(t *testing.T) {
			var errs r82streamErrors
			var mu sync.Mutex
			var finishes []string
			add := func(s string) { mu.Lock(); finishes = append(finishes, s); mu.Unlock() }
			r, err := StreamText(context.Background(), StreamTextOptions{
				Model: r71streamModel(nil, []provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "Let me check"},
					{Type: provider.ChunkTypeTextEnd, ID: "1"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonInterrupted, Usage: r82streamTestUsage, ProviderMetadata: json.RawMessage(`{"test":{"partial":true}}`)},
				}),
				Prompt:         "test-input",
				Tools:          []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}}},
				ToolChoice:     choice,
				OnError:        errs.onError,
				OnEndEvent:     func(_ context.Context, e OnFinishEvent) { add("onEnd:" + string(e.FinishReason)) },
				OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { add("onStepEnd:" + string(e.FinishReason)) },
				OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
					add("onLanguageModelCallEnd:" + string(e.FinishReason))
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var ends []string
			s := r.FullStream()
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				switch c.Type {
				case provider.ChunkTypeError:
					t.Fatalf("error part: %v", c.Err)
				case provider.ChunkTypeFinishStep, provider.ChunkTypeFinish:
					ends = append(ends, fmt.Sprintf("%s:%s:%q", c.Type, c.FinishReason, c.RawFinishReason))
				}
			}
			if fmt.Sprint(ends) != `[finish-step:interrupted:"" finish:interrupted:""]` {
				t.Fatalf("finish parts = %v", ends)
			}
			if r.Text() != "Let me check" || r.FinishReason() != types.FinishReasonInterrupted {
				t.Fatalf("text %q finishReason %q", r.Text(), r.FinishReason())
			}
			r75streamAssertJSON(t, "usage", r.Usage(), `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`)
			r75streamAssertJSON(t, "providerMetadata", json.RawMessage(r.ProviderMetadata()), `{"test":{"partial":true}}`)
			if got := errs.all(); len(got) != 0 {
				t.Fatalf("onError = %v", got)
			}
			mu.Lock()
			sort.Strings(finishes)
			if fmt.Sprint(finishes) != "[onEnd:interrupted onLanguageModelCallEnd:interrupted onStepEnd:interrupted]" {
				t.Fatalf("callbacks = %v", finishes)
			}
			mu.Unlock()
			ui, uiErr := r.ToUIMessageStream(context.Background())
			found := false
			for c := range ui {
				if c["type"] == "finish" && c["finishReason"] == "interrupted" && len(c) == 2 {
					found = true
				}
			}
			<-uiErr
			if !found {
				t.Fatal("no {type: finish, finishReason: interrupted} UI chunk")
			}
		})
	}
}

// r82streamSteerModel answers call n (1-based) with steps(n) and records
// the prompts.
func r82streamSteerModel(prompts *[][]types.Message, steps func(call int) []provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		*prompts = append(*prompts, o.Prompt.Messages)
		n := len(*prompts)
		mu.Unlock()
		return testutil.NewMockTextStream(steps(n)), nil
	}}
}

func r82streamSteerText(text string) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: text},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r82streamTestUsage},
	}
}

func r82streamSteerToolCall() []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Paris"}, RawArguments: `{ "city": "Paris" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r82streamTestUsage},
	}
}

func r82streamWeather(execute func() string) types.Tool {
	return types.Tool{Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return execute(), nil
		}}
}

func r82streamHasUserText(msgs []types.Message, text string) bool {
	for _, m := range msgs {
		if m.Role != types.RoleUser {
			continue
		}
		for _, p := range m.Content {
			if tc, ok := p.(types.TextContent); ok && tc.Text == text {
				return true
			}
		}
	}
	return false
}

func r82streamOutcome(t *testing.T, h *SteerHandle) SteeringOutcome {
	t.Helper()
	if h == nil {
		t.Fatal("steer was never called")
	}
	return waitOutcome(t, h)
}

// stream-text-steering.test.ts 177 (before the stream starts: first step),
// 230 (approval pending: waiting-for-tool-results), 259 (aborted), 324
// (after the stream finished), 341 (kept out of response messages) and 440
// (no continuation after a provider error: reason error).
func TestR82streamSteeringOutcomes(t *testing.T) {
	t.Run("177 before start", func(t *testing.T) {
		s := NewSteering()
		h := s.Steer("Answer in French.")
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerText("Il fait beau.") }),
			Prompt: "What is the weather in Paris?", ExperimentalSteering: s})
		if last := prompts[0][len(prompts[0])-1]; !r82streamHasUserText([]types.Message{last}, "Answer in French.") {
			t.Fatalf("last prompt message = %+v", last)
		}
		if o := r82streamOutcome(t, h); !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("230 approval pending", func(t *testing.T) {
		s := NewSteering()
		var h *SteerHandle
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk {
			h = s.Steer("Hurry up.")
			return r82streamSteerToolCall()
		}), Prompt: "What is the weather in Paris?", Tools: []types.Tool{r82streamWeather(func() string { return "sunny" })},
			ToolApproval: map[string]types.ToolApprovalValue{"weather": types.ToolApprovalStatusUserApproval},
			StopWhen:     []StopCondition{IsStepCount(5)}, ExperimentalSteering: s})
		if len(prompts) != 1 {
			t.Fatalf("doStream calls = %d", len(prompts))
		}
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringWaitingForToolResults {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("259 aborted", func(t *testing.T) {
		s := NewSteering()
		var h *SteerHandle
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var prompts [][]types.Message
		r, err := StreamText(ctx, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerToolCall() }),
			Prompt: "What is the weather in Paris?", Tools: []types.Tool{r82streamWeather(func() string {
				h = s.Steer("I meant Paris, Texas.")
				cancel()
				return "sunny"
			})}, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: s})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if len(prompts) != 1 {
			t.Fatalf("doStream calls = %d", len(prompts))
		}
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringAborted {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("324 after finish", func(t *testing.T) {
		s := NewSteering()
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerText("done") }), Prompt: "hello", ExperimentalSteering: s})
		if o := r82streamOutcome(t, s.Steer("too late")); o.Delivered || o.Reason != SteeringFinished {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("341 response messages", func(t *testing.T) {
		s := NewSteering()
		var prompts [][]types.Message
		r := r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(n int) []provider.StreamChunk {
			if n == 1 {
				s.Steer("Also tell me about Tokyo.")
				return r82streamSteerText("It is sunny in Paris.")
			}
			return r82streamSteerText("Tokyo is rainy.")
		}), Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: s})
		var roles []string
		for _, m := range r.ResponseMessages() {
			roles = append(roles, string(m.Role))
		}
		if fmt.Sprint(roles) != "[assistant assistant]" {
			t.Fatalf("response message roles = %v", roles)
		}
	})
	t.Run("440 provider error", func(t *testing.T) {
		s := NewSteering()
		var h *SteerHandle
		var prompts [][]types.Message
		r, err := StreamText(context.Background(), StreamTextOptions{Model: r82streamSteerModel(&prompts, func(n int) []provider.StreamChunk {
			if n == 1 {
				h = s.Steer("Also tell me about Tokyo.")
				return []provider.StreamChunk{
					{Type: provider.ChunkTypeStreamStart},
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "It is"},
					{Type: provider.ChunkTypeError, Err: errors.New("provider failure")},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError, RawFinishReason: "error", Usage: r82streamTestUsage},
				}
			}
			return r82streamSteerText("Tokyo is rainy.")
		}), Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(5)}, OnError: func(context.Context, error) {}, ExperimentalSteering: s})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if len(prompts) != 1 {
			t.Fatalf("doStream calls = %d", len(prompts))
		}
		if fr := r.FinishReason(); fr != types.FinishReasonError {
			t.Fatalf("finishReason = %q", fr)
		}
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringError {
			t.Fatalf("outcome = %+v", o)
		}
	})
}

// r82streamErrStream returns stream-start, then fails.
type r82streamErrStream struct{ sent bool }

func (s *r82streamErrStream) Next() (*provider.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart}, nil
	}
	return nil, errors.New("provider stream failure")
}
func (s *r82streamErrStream) Err() error   { return nil }
func (s *r82streamErrStream) Close() error { return nil }

// stream-text-steering.test.ts 290 (the provider stream fails: error), 480
// (invalid arguments: error), 499 (invalid prompt: error), 368 (PrepareStep
// sees the steering messages once and the rebuilt prompts keep them), 535
// (removed by PrepareStep) and 569 (two streams at once).
func TestR82streamSteeringErrorsAndPrepareStep(t *testing.T) {
	t.Run("290 stream fails", func(t *testing.T) {
		s := NewSteering()
		var h *SteerHandle
		r, err := StreamText(context.Background(), StreamTextOptions{Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			h = s.Steer("during the failure")
			return &r82streamErrStream{}, nil
		}}, Prompt: "hello", StopWhen: []StopCondition{IsStepCount(5)}, OnError: func(context.Context, error) {}, ExperimentalSteering: s})
		if err != nil {
			t.Fatal(err)
		}
		_ = r.ConsumeStream()
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringError {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("480 invalid arguments", func(t *testing.T) {
		s := NewSteering()
		h := s.Steer("hello")
		bad := -1
		var prompts [][]types.Message
		if _, err := StreamText(context.Background(), StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerText("unused") }),
			Prompt: "hello", MaxRetries: &bad, ExperimentalSteering: s}); err == nil {
			t.Fatal("StreamText accepted maxRetries -1")
		}
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringError {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("499 invalid prompt", func(t *testing.T) {
		s := NewSteering()
		h := s.Steer("hello")
		var prompts [][]types.Message
		r, err := StreamText(context.Background(), StreamTextOptions{Model: r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerText("unused") }),
			Messages: []types.Message{}, OnError: func(context.Context, error) {}, ExperimentalSteering: s})
		if err == nil {
			_ = r.ConsumeStream()
		}
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringError {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("368 prepareStep keeps them", func(t *testing.T) {
		s := NewSteering()
		steered := false
		var prompts [][]types.Message
		var seen []string
		r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(n int) []provider.StreamChunk {
			if n < 3 {
				return r82streamSteerToolCall()
			}
			return r82streamSteerText("It is sunny in Paris, Texas.")
		}), Prompt: "What is the weather in Paris?", Tools: []types.Tool{r82streamWeather(func() string {
			if !steered {
				steered = true
				s.Steer("I meant Paris, Texas.")
			}
			return "sunny"
		})}, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: s,
			PrepareStep: func(_ context.Context, p PrepareStepOptions) PrepareStepOptions {
				var texts []string
				for _, m := range p.ExperimentalSteeringMessages {
					for _, c := range m.Content {
						if tc, ok := c.(types.TextContent); ok {
							texts = append(texts, tc.Text)
						}
					}
				}
				if p.ExperimentalSteeringMessages == nil {
					seen = append(seen, "undefined")
				} else {
					seen = append(seen, strings.Join(texts, "|"))
				}
				msgs := append([]types.Message(nil), p.InitialMessages...)
				for _, st := range p.Steps {
					msgs = append(msgs, st.ExperimentalSteeringMessages...)
					msgs = append(msgs, st.Response.Messages...)
				}
				msgs = append(msgs, p.ExperimentalSteeringMessages...)
				p.Messages = msgs
				return p
			}})
		if fmt.Sprint(seen) != "[undefined I meant Paris, Texas. undefined]" {
			t.Fatalf("prepareStep steering messages = %q", seen)
		}
		for _, call := range []int{1, 2} {
			if !r82streamHasUserText(prompts[call], "I meant Paris, Texas.") {
				t.Fatalf("call %d prompt lacks the steering message", call)
			}
		}
	})
	t.Run("535 removed by prepareStep", func(t *testing.T) {
		s := NewSteering()
		var h *SteerHandle
		var prompts [][]types.Message
		r := r71streamRun(t, StreamTextOptions{Model: r82streamSteerModel(&prompts, func(n int) []provider.StreamChunk {
			if n == 1 {
				return r82streamSteerToolCall()
			}
			return r82streamSteerText("It is sunny in Paris.")
		}), Prompt: "What is the weather in Paris?", Tools: []types.Tool{r82streamWeather(func() string {
			h = s.Steer("I meant Paris, Texas.")
			return "sunny"
		})}, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: s,
			PrepareStep: func(_ context.Context, p PrepareStepOptions) PrepareStepOptions {
				p.Messages = append(append([]types.Message(nil), p.InitialMessages...), p.ResponseMessages...)
				return p
			}})
		if o := r82streamOutcome(t, h); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
			t.Fatalf("outcome = %+v", o)
		}
		if steps := r.Steps(); len(steps) != 2 || steps[1].ExperimentalSteeringMessages != nil {
			t.Fatalf("steps = %d, step 1 steering = %v", len(steps), steps[1].ExperimentalSteeringMessages)
		}
	})
	t.Run("569 two streams at once", func(t *testing.T) {
		s := NewSteering()
		var prompts [][]types.Message
		model := r82streamSteerModel(&prompts, func(int) []provider.StreamChunk { return r82streamSteerText("first") })
		first, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hello", ExperimentalSteering: s})
		if err != nil {
			t.Fatal(err)
		}
		_, err = StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hello", ExperimentalSteering: s})
		if err == nil || !strings.Contains(err.Error(), "The steering object was already used by a generation.") {
			t.Fatalf("second StreamText err = %v", err)
		}
		_ = first.ConsumeStream()
	})
}

// TS stream-text.ts: "Chunk timeout tracking starts after semantic output
// begins" (resetChunkTimeout runs on the first output chunk; only
// startFirstChunkTimeout runs when the stream opens). With chunkMs alone, a
// slow first chunk is not a chunk timeout.
func TestR82streamChunkTimeoutStartsAfterFirstOutput(t *testing.T) {
	perChunk := 50 * time.Millisecond
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &delayedTextStream{chunks: []delayedChunk{
			{delay: 5 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1"}}},
			{delay: 150 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}},
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}},
		}}, nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test", Timeout: &TimeoutConfig{PerChunk: &perChunk}, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v, want no chunk timeout before the first output", err)
	}
	if r.Text() != "Hello" {
		t.Fatalf("text = %q", r.Text())
	}
}

// r82streamTimedStream sends its chunks, each after its delay, then blocks
// until ctx is done (TS: a stream that errors with the abort reason).
type r82streamTimedStream struct {
	ctx    context.Context
	chunks []delayedChunk
	idx    int
}

func (s *r82streamTimedStream) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		<-s.ctx.Done()
		return nil, context.Cause(s.ctx)
	}
	dc := s.chunks[s.idx]
	s.idx++
	select {
	case <-time.After(dc.delay):
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	}
	c := dc.chunk
	return &c, nil
}
func (s *r82streamTimedStream) Err() error   { return nil }
func (s *r82streamTimedStream) Close() error { return nil }

func r82streamDur(ms int) *time.Duration {
	d := time.Duration(ms) * time.Millisecond
	return &d
}

func r82streamLastPart(t *testing.T, r *StreamTextResult) []string {
	t.Helper()
	var parts []string
	s := r.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			return parts
		}
		parts = append(parts, string(c.Type))
	}
}

// stream-text-timeout.test.ts 661: should start chunkMs only after output
// begins in the next step / retry (firstChunkMs 150, chunkMs 50: the next
// attempt's 100ms wait before output is no chunk timeout; 50ms after its
// output stops, it is).
func TestR82streamChunkTimeoutStartsInNextAttempt(t *testing.T) {
	for _, continuation := range []string{"step", "retry"} {
		t.Run(continuation, func(t *testing.T) {
			var mu sync.Mutex
			attempts := 0
			model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				mu.Lock()
				attempts++
				n := attempts
				mu.Unlock()
				if n == 1 {
					if continuation == "step" {
						return testutil.NewMockTextStream([]provider.StreamChunk{
							{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "slow", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
							{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r82streamTestUsage},
						}), nil
					}
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeTextStart, ID: "1"},
						{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
						{Type: provider.ChunkTypeTextEnd, ID: "1"},
						{Type: provider.ChunkTypeError, Err: errors.New("retryable error")},
					}), nil
				}
				return &r82streamTimedStream{ctx: ctx, chunks: []delayedChunk{
					{delay: 100 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "2"}},
					{chunk: provider.StreamChunk{Type: provider.ChunkTypeText, ID: "2", Text: "Next"}},
				}}, nil
			}}
			retries := 0
			if continuation == "retry" {
				retries = 1
			}
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test",
				Tools: []types.Tool{{Name: "slow", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					select {
					case <-time.After(100 * time.Millisecond):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return "done", nil
				}}},
				Timeout:       &TimeoutConfig{FirstChunk: r82streamDur(150), PerChunk: r82streamDur(50)},
				StreamRetries: &retries, StopWhen: []StopCondition{IsStepCount(2)}, OnError: func(context.Context, error) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			parts := r82streamLastPart(t, r)
			if attempts != 2 {
				t.Fatalf("attempts = %d", attempts)
			}
			var te *TimeoutError
			if !errors.As(r.Err(), &te) || te.Reason != TimeoutReasonChunk {
				t.Fatalf("Err() = %v, want a chunk timeout", r.Err())
			}
			if !strings.Contains(strings.Join(parts, " "), "text") || parts[len(parts)-1] != "abort" {
				t.Fatalf("parts = %v, want the next attempt's text then abort", parts)
			}
		})
	}
}

// stream-text-timeout.test.ts 730: should re-arm firstChunkMs after retrying
// an attempt that produced output.
func TestR82streamFirstChunkTimeoutRearmedOnRetry(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeError, Err: errors.New("retryable error")},
			}), nil
		}
		return &r82streamTimedStream{ctx: ctx}, nil
	}}
	retries := 1
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test",
		Timeout: &TimeoutConfig{FirstChunk: r82streamDur(50)}, StreamRetries: &retries, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	var te *TimeoutError
	if attempts != 2 || !errors.As(r.Err(), &te) || te.Reason != TimeoutReasonFirstChunk {
		t.Fatalf("attempts = %d, Err() = %v, want a first-chunk timeout", attempts, r.Err())
	}
}

// stream-text-timeout.test.ts 814: should keep chunkMs active during
// provider-executed tools without resetting on their results.
func TestR82streamChunkTimeoutProviderExecutedTool(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
		return &r82streamTimedStream{ctx: ctx, chunks: []delayedChunk{
			{chunk: provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "slow", Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true, ProviderExecutedSet: true}}},
			{delay: 40 * time.Millisecond, chunk: provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "slow", Result: "progress", ProviderExecuted: true, Preliminary: true}}},
		}}, nil
	}}
	start := time.Now()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test",
		Tools:   []types.Tool{{Name: "slow", Parameters: map[string]interface{}{"type": "object"}}},
		Timeout: &TimeoutConfig{PerChunk: r82streamDur(50)}, OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	var te *TimeoutError
	if !errors.As(r.Err(), &te) || te.Reason != TimeoutReasonChunk {
		t.Fatalf("Err() = %v, want a chunk timeout", r.Err())
	}
	if el := time.Since(start); el > 85*time.Millisecond {
		t.Fatalf("timed out after %v: the provider tool result reset chunkMs", el)
	}
}

// r82streamStepToolChoices runs StreamText with per-call legacy OTel and
// returns the ai.prompt.toolChoice / ai.prompt.messages / ai.prompt.tools
// attributes of each ai.streamText.doStream span ("-" when absent).
func r82streamStepToolChoices(t *testing.T, opts StreamTextOptions) [][3]string {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	opts.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
	}}
	r := r71streamRun(t, opts)
	_ = r.Text()
	var out [][3]string
	for _, s := range rec.Ended() {
		if s.Name() != "ai.streamText.doStream" {
			continue
		}
		got := [3]string{"-", "-", "-"}
		for _, kv := range s.Attributes() {
			switch kv.Key {
			case "ai.prompt.toolChoice":
				got[0] = kv.Value.AsString()
			case "ai.prompt.messages":
				got[1] = "set"
			case "ai.prompt.tools":
				got[2] = "set"
			}
		}
		out = append(out, got)
	}
	return out
}

// CR r82 carry->stream item 1: TS stepToolChoice is prepareToolChoice(...)
// ({type: auto} when unset), recorded as ai.prompt.toolChoice on every step
// span (legacy-open-telemetry.test.ts.snap "streamText > should record
// telemetry data when enabled"). Item 2: a WorkflowAgent run
// (OmitStepPromptTelemetry) records no step prompt, tools or tool choice
// (TS model-call-iterator.ts onStepStart).
func TestR82streamTelemetryStepToolChoiceDefaultsToAuto(t *testing.T) {
	got := r82streamStepToolChoices(t, StreamTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "hi"})
	if fmt.Sprint(got) != `[[{"type":"auto"} set -]]` {
		t.Fatalf("one step: %v", got)
	}
	twoSteps := func(internal *InternalOptions) StreamTextOptions {
		return StreamTextOptions{Prompt: "hi", Internal: internal, StopWhen: []StopCondition{IsStepCount(2)},
			Model: r71streamModel(nil, r82streamSteerToolCall(), r82streamSteerText("done")),
			Tools: []types.Tool{r82streamWeather(func() string { return "sunny" })}}
	}
	got = r82streamStepToolChoices(t, twoSteps(nil))
	if fmt.Sprint(got) != `[[{"type":"auto"} set set] [{"type":"auto"} set set]]` {
		t.Fatalf("two steps: %v", got)
	}
	got = r82streamStepToolChoices(t, twoSteps(&InternalOptions{OmitStepPromptTelemetry: true}))
	if fmt.Sprint(got) != `[[- - -] [- - -]]` {
		t.Fatalf("workflow steps: %v", got)
	}
}

// r82streamStalledStream sends one text delta, then stalls forever (it
// ignores the ctx, like a stalled HTTP body).
type r82streamStalledStream struct {
	sent  int
	block chan struct{}
}

func (s *r82streamStalledStream) Next() (*provider.StreamChunk, error) {
	s.sent++
	switch s.sent {
	case 1:
		return &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"}, nil
	case 2:
		return &provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}, nil
	}
	<-s.block
	return nil, io.EOF
}
func (s *r82streamStalledStream) Err() error   { return nil }
func (s *r82streamStalledStream) Close() error { return nil }

// TS #22184 (stream-text.test.ts "abort signal" with a stalled body): the
// abort ends the run at once, with the abort part and onAbort, even though
// the provider read never returns.
func TestR82streamAbortEndsStalledRead(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aborted := make(chan struct{}, 1)
	r, err := StreamText(ctx, StreamTextOptions{Prompt: "p",
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &r82streamStalledStream{block: block}, nil
		}},
		OnAbortEvent: func(context.Context, GenerateTextAbortEvent) { aborted <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []string, 1)
	go func() { done <- r82streamLastPart(t, r) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case parts := <-done:
		if parts[len(parts)-1] != "abort" {
			t.Fatalf("parts = %v", parts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stalled read held the stream open after the abort")
	}
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("onAbort not called")
	}
}

// r82streamEndless sends text-start, then "hello" deltas until closed.
type r82streamEndless struct {
	mu     sync.Mutex
	n      int
	closed int
}

func (s *r82streamEndless) Next() (*provider.StreamChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed > 0 {
		return nil, io.EOF
	}
	s.n++
	if s.n == 1 {
		return &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "text"}, nil
	}
	return &provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text", Text: "hello"}, nil
}
func (s *r82streamEndless) Err() error { return nil }
func (s *r82streamEndless) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

// stream-text-with-minimal-retention.test.ts 322: cancels the upstream
// stream and rejects metadata when iteration stops early (Go: closing the
// claimed stream cancels the generation; Err reports the abort).
func TestR82streamLowMemoryEarlyStopCancels(t *testing.T) {
	src := &r82streamEndless{}
	r, err := StreamTextLowMemory(context.Background(), StreamTextOptions{Prompt: "test",
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return src, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Stream()
	if err != nil {
		t.Fatal(err)
	}
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if c.Type == provider.ChunkTypeText {
			if c.Text != "hello" {
				t.Fatalf("delta = %q", c.Text)
			}
			break
		}
	}
	_ = s.Close()
	done := make(chan error, 1)
	go func() { done <- r.Err() }()
	select {
	case err := <-done:
		if err == nil || !isAbortErr(context.Background(), err) {
			t.Fatalf("Err() = %v, want an abort", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("metadata never settled after the consumer stopped")
	}
	deadline := time.Now().Add(time.Second)
	for {
		src.mu.Lock()
		closed := src.closed
		src.mu.Unlock()
		if closed > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upstream stream not closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A failed DoStream with no Audio stream rejects the accessors with the
// provider error instead of panicking in the pipe goroutine on the nil
// Audio's Cancel (TS cancels `audio` only when it exists to be cancelled).
func TestR82streamNilAudioFailureDoesNotPanic(t *testing.T) {
	boom := errors.New("auth failed")
	tr, err := ExperimentalStreamTranslate(context.Background(), StreamTranslateOptions{TargetLanguage: "es",
		Model: &mockSpeechTranslationModel{doStreamFn: func(context.Context, *provider.SpeechTranslationStreamOptions) (*provider.SpeechTranslationStreamResult, error) {
			return nil, boom
		}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.TranslationText(); !errors.Is(err, boom) {
		t.Fatalf("translation err = %v", err)
	}
	ts, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: &mockTranscriptionStreamerModel{doStreamFn: func(context.Context, *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return nil, boom
		}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Text(); !errors.Is(err, boom) {
		t.Fatalf("transcription err = %v", err)
	}
}
