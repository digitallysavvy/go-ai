package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
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
