package ai

// Round 87c (streamb): ports of the TS stream-text.test.ts cases at TS line
// 20263 and later that Appendix A of ts-test-port-coverage.md still lists as
// partial, plus the partial ui-message-stream / ui rows. Each test names its
// TS case (`stream-text.test.ts <line>`, the Appendix A row line) and
// compares the same inputs against the TS inline snapshot, translated to Go:
// TS `undefined` fields are left out, Go nil maps stand for TS `{}`, a TS
// tool-error part is a Go tool-result chunk whose ToolResult.Error is set,
// and performance timings (TS mocks the clock) are compared by their keys.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r87cstreambUsage is TS testUsage: 3 input tokens (all noCache), 10 output
// tokens (all text), no provider total.
func r87cstreambUsage() *types.Usage {
	in, out, noCache, text := int64(3), int64(10), int64(3), int64(10)
	return &types.Usage{InputTokens: &in, OutputTokens: &out,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &noCache},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &text}}
}

// r87cstreambUsageJSON is the TS snapshot of testUsage after the SDK adds
// totalTokens (undefined fields left out).
const r87cstreambUsageJSON = `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`

func r87cstreambMeta(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: id, ModelID: "mock-model-id", Timestamp: time.Unix(0, 0).UTC()}}
}

func r87cstreambFinish(reason types.FinishReason, raw string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: reason, RawFinishReason: raw, Usage: r87cstreambUsage()}
}

func r87cstreambCall(id, name, input string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: name, RawArguments: input}}
}

func r87cstreambText(id string, deltas ...string) []provider.StreamChunk {
	out := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: id}}
	for _, d := range deltas {
		out = append(out, provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: d})
	}
	return append(out, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id})
}

// r87cstreambDefaultStream is TS createTestModel's default stream.
func r87cstreambDefaultStream() []provider.StreamChunk {
	out := []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}}, r87cstreambMeta("id-0")}
	out = append(out, r87cstreambText("1", "Hello", ", ", "world!")...)
	fin := r87cstreambFinish(types.FinishReasonStop, "stop")
	fin.ProviderMetadata = json.RawMessage(`{"testProvider":{"testKey":"testValue"}}`)
	return append(out, fin)
}

func r87cstreambJoin(parts ...[]provider.StreamChunk) []provider.StreamChunk {
	var out []provider.StreamChunk
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// r87cstreambIDs is TS mockId({prefix}) for generateId (id-0, id-1, ...)
// and the fixed telemetry call id of defaultSettings.
func r87cstreambIDs() *InternalOptions {
	return &InternalOptions{GenerateID: r73streamCounterIDs("id"), GenerateCallID: func() string { return "test-telemetry-call-id" }}
}

// r87cstreambNorm round-trips v through JSON into generic values.
func r87cstreambNorm(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

func r87cstreambAssert(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want JSON: %v\n%s", label, err, want)
	}
	g := r87cstreambNorm(t, got)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		t.Fatalf("%s:\n got  %s\n want %s", label, gb, wb)
	}
}

func r87cstreambErrText(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}

func r87cstreambTimestamp(ts time.Time) string {
	return ts.UTC().Format("2006-01-02T15:04:05.000Z")
}

// r87cstreambToolCall is the TS tool-call part (stream part or content).
func r87cstreambToolCall(c *types.ToolCall) map[string]interface{} {
	m := map[string]interface{}{"type": "tool-call", "toolCallId": c.ID, "toolName": c.ToolName, "input": c.Arguments}
	if c.Arguments == nil {
		m["input"] = c.RawArguments
	}
	if c.ProviderExecuted {
		m["providerExecuted"] = true
	}
	if len(c.ProviderMetadata) > 0 {
		m["providerMetadata"] = c.ProviderMetadata
	}
	if c.Title != "" {
		m["title"] = c.Title
	}
	if c.Dynamic {
		m["dynamic"] = true
	}
	if c.Invalid {
		m["invalid"] = true
		m["error"] = r87cstreambErrText(c.Error)
	}
	return m
}

// r87cstreambPart projects a full-stream part onto the TS TextStreamPart
// snapshot keys. Go-only lifecycle markers return nil.
func r87cstreambPart(c provider.StreamChunk) map[string]interface{} {
	var meta interface{}
	if len(c.ProviderMetadata) > 0 {
		meta = c.ProviderMetadata
	}
	set := func(m map[string]interface{}) map[string]interface{} {
		if meta != nil {
			m["providerMetadata"] = meta
		}
		return m
	}
	switch c.Type {
	case provider.ChunkTypeResponseMetadata, provider.ChunkTypeFirstChunk, provider.ChunkTypeStreamFinish, provider.ChunkTypeStreamStart:
		return nil
	case provider.ChunkTypeStart:
		return map[string]interface{}{"type": "start"}
	case provider.ChunkTypeStartStep:
		m := map[string]interface{}{"type": "start-step", "warnings": c.Warnings, "request": map[string]interface{}{}}
		if c.Warnings == nil {
			m["warnings"] = []interface{}{}
		}
		if c.Request != nil {
			m["request"] = c.Request
		}
		return m
	case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd:
		return set(map[string]interface{}{"type": string(c.Type), "id": c.ID})
	case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
		return set(map[string]interface{}{"type": string(c.Type), "id": c.ID})
	case provider.ChunkTypeText:
		return set(map[string]interface{}{"type": "text-delta", "id": c.ID, "text": c.Text})
	case provider.ChunkTypeReasoning:
		text := c.Text
		if text == "" {
			text = c.Reasoning
		}
		return set(map[string]interface{}{"type": "reasoning-delta", "id": c.ID, "text": text})
	case provider.ChunkTypeToolInputStart:
		m := set(map[string]interface{}{"type": "tool-input-start", "id": c.ID})
		if c.ToolCall != nil {
			m["toolName"] = c.ToolCall.ToolName
			if c.ToolCall.ProviderExecuted {
				m["providerExecuted"] = true
			}
			if c.ToolCall.Dynamic {
				m["dynamic"] = true
			}
			if c.ToolCall.Title != "" {
				m["title"] = c.ToolCall.Title
			}
		}
		return m
	case provider.ChunkTypeToolInputDelta:
		return set(map[string]interface{}{"type": "tool-input-delta", "id": c.ID, "delta": c.Text})
	case provider.ChunkTypeToolInputEnd:
		return set(map[string]interface{}{"type": "tool-input-end", "id": c.ID})
	case provider.ChunkTypeToolCall:
		return r87cstreambToolCall(c.ToolCall)
	case provider.ChunkTypeToolResult:
		tr := c.ToolResult
		m := map[string]interface{}{"toolCallId": tr.ToolCallID, "toolName": tr.ToolName, "input": tr.Input, "dynamic": tr.Dynamic}
		if tr.Error != nil {
			m["type"], m["error"] = "tool-error", tr.Error.Error()
		} else {
			m["type"], m["output"] = "tool-result", tr.Result
		}
		if tr.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if tr.Preliminary {
			m["preliminary"] = true
		}
		if tr.Title != "" {
			m["title"] = tr.Title
		}
		return set(m)
	case provider.ChunkTypeToolApprovalRequest:
		a := c.ToolApprovalRequest
		m := map[string]interface{}{"type": "tool-approval-request", "approvalId": a.ApprovalID, "toolCall": r87cstreambToolCall(&a.ToolCall)}
		if a.IsAutomatic {
			m["isAutomatic"] = true
		}
		return m
	case provider.ChunkTypeFinishStep:
		resp := map[string]interface{}{}
		if c.Response != nil {
			resp = map[string]interface{}{"id": c.Response.ID, "modelId": c.Response.ModelID, "timestamp": r87cstreambTimestamp(c.Response.Timestamp)}
			if len(c.Response.Headers) > 0 {
				resp["headers"] = c.Response.Headers
			}
		}
		return set(map[string]interface{}{"type": "finish-step", "finishReason": string(c.FinishReason), "rawFinishReason": c.RawFinishReason, "response": resp, "usage": c.Usage})
	case provider.ChunkTypeFinish:
		m := map[string]interface{}{"type": "finish", "finishReason": string(c.FinishReason), "rawFinishReason": c.RawFinishReason, "totalUsage": c.Usage}
		if c.RawFinishReason == "" {
			delete(m, "rawFinishReason")
		}
		return m
	case provider.ChunkTypeError:
		return map[string]interface{}{"type": "error", "error": r87cstreambErrText(c.Err)}
	case provider.ChunkTypeAbort:
		m := map[string]interface{}{"type": "abort"}
		if c.AbortReason != "" {
			m["reason"] = c.AbortReason
		}
		return m
	default:
		b, _ := json.Marshal(c)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		m["type"] = string(c.Type)
		return m
	}
}

func r87cstreambParts(parts []provider.StreamChunk) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, p := range parts {
		if m := r87cstreambPart(p); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// r87cstreambMessages projects model messages onto TS ModelMessage JSON (the
// Go-only legacy tool_calls key dropped).
func r87cstreambMessages(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	out := r87cstreambNorm(t, msgs)
	list, _ := out.([]interface{})
	for _, m := range list {
		if mm, ok := m.(map[string]interface{}); ok {
			delete(mm, "tool_calls")
		}
	}
	if list == nil {
		return []interface{}{}
	}
	return list
}

// r87cstreambStep projects a step result onto the TS DefaultStepResult
// snapshot keys. Performance is reduced to its toolExecutionMs keys.
func r87cstreambStep(t *testing.T, s types.StepResult) map[string]interface{} {
	t.Helper()
	m := map[string]interface{}{
		"callId":          s.CallID,
		"content":         r87cstreambContent(t, s.Content),
		"finishReason":    string(s.FinishReason),
		"model":           map[string]interface{}{"provider": s.Model.Provider, "modelId": s.Model.ModelID},
		"rawFinishReason": s.RawFinishReason,
		"request":         r87cstreambNorm(t, s.Request),
		"stepNumber":      s.StepNumber,
		"usage":           s.Usage,
		"warnings":        s.Warnings,
		"runtimeContext":  s.RuntimeContext,
		"toolsContext":    s.ToolsContext,
	}
	if s.Warnings == nil {
		m["warnings"] = []interface{}{}
	}
	if s.RuntimeContext == nil {
		m["runtimeContext"] = map[string]interface{}{}
	}
	if s.ToolsContext == nil {
		m["toolsContext"] = map[string]interface{}{}
	}
	if s.RawFinishReason == "" {
		delete(m, "rawFinishReason")
	}
	if len(s.ProviderMetadata) > 0 {
		m["providerMetadata"] = s.ProviderMetadata
	}
	resp := map[string]interface{}{"id": s.Response.ID, "modelId": s.Response.ModelID, "timestamp": r87cstreambTimestamp(s.Response.Timestamp), "messages": r87cstreambMessages(t, s.Response.Messages)}
	if len(s.Response.Headers) > 0 {
		resp["headers"] = s.Response.Headers
	}
	m["response"] = resp
	keys := []string{}
	for k := range s.Performance.ToolExecutionMs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	m["toolExecutionMsKeys"] = keys
	return m
}

// r87cstreambContent projects content parts onto TS content JSON (a
// tool-error keeps its error message as "error", Go's errorText is the TS
// model output text and is dropped; a tool-result's raw output is "output").
func r87cstreambContent(t *testing.T, content []types.ContentPart) interface{} {
	t.Helper()
	out := []interface{}{}
	for _, p := range content {
		m, _ := r87cstreambNorm(t, p).(map[string]interface{})
		switch v := p.(type) {
		case types.ToolErrorContent:
			delete(m, "errorText")
			m["dynamic"] = v.Dynamic
			if v.Err != nil {
				m["error"] = v.Err.Error()
			}
		case types.ToolResultContent:
			// Go keeps the raw tool output in Result and the model output
			// in Output; TS content has the raw output as "output".
			m["dynamic"] = v.Dynamic
			delete(m, "result")
			m["output"] = r87cstreambNorm(t, v.Result)
		}
		out = append(out, m)
	}
	return out
}

// r87cstreambSteps projects all steps.
func r87cstreambSteps(t *testing.T, steps []types.StepResult) []map[string]interface{} {
	t.Helper()
	out := []map[string]interface{}{}
	for _, s := range steps {
		out = append(out, r87cstreambStep(t, s))
	}
	return out
}

// stream-text.test.ts 20263: options.timeout > should clean up step timeout
// when doStream throws an error (maxRetries 0, stepMs 10000): consumeStream
// resolves right away instead of waiting for the step timer.
func TestR87cstreambStepTimeoutCleanedUpOnDoStreamError(t *testing.T) {
	zero := 0
	step := 10 * time.Second
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("Fail immediately")
		}},
		Prompt:     "test-input",
		MaxRetries: &zero,
		Timeout:    &TimeoutConfig{PerStep: &step},
		OnError:    func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = r.ConsumeStream()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeStream waited for the step timeout after doStream failed")
	}
}

// stream-text.test.ts 20281: tool callbacks > should invoke callbacks in the
// correct order with the step tool context.
func TestR87cstreambToolCallbacksStepToolContext(t *testing.T) {
	const id = "call_O17Uplv4lJvD6DVdIvFFeRMw"
	var mu sync.Mutex
	var recorded []map[string]interface{}
	rec := func(typ string, options map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, map[string]interface{}{"type": typ, "options": options})
	}
	msgs := func(m []types.Message) interface{} { return r87cstreambMessages(t, m) }
	chunks := []provider.StreamChunk{r87cstreambMeta("id-0"),
		{Type: provider.ChunkTypeToolInputStart, ID: id, ToolCall: &types.ToolCall{ID: id, ToolName: "test-tool"}}}
	for _, d := range []string{`{"`, "value", `":"`, "Spark", "le", " Day", `"}`} {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: id, Text: d})
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: id},
		r87cstreambCall(id, "test-tool", `{"value":"Sparkle Day"}`),
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()})
	tool := types.Tool{
		Name: "test-tool",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required": []interface{}{"value"}, "additionalProperties": false},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"prefix": map[string]interface{}{"type": "string"}}, "required": []interface{}{"prefix"}}),
		OnInputAvailable: func(_ context.Context, o types.OnInputAvailableOptions) error {
			rec("onInputAvailable", map[string]interface{}{"context": o.Context, "input": o.Input, "messages": msgs(o.Messages), "toolCallId": o.ToolCallID})
			return nil
		},
		OnInputStart: func(_ context.Context, o types.OnInputStartOptions) error {
			rec("onInputStart", map[string]interface{}{"context": o.Context, "messages": msgs(o.Messages), "toolCallId": o.ToolCallID})
			return nil
		},
		OnInputDelta: func(_ context.Context, o types.OnInputDeltaOptions) error {
			rec("onInputDelta", map[string]interface{}{"context": o.Context, "inputTextDelta": o.InputTextDelta, "messages": msgs(o.Messages), "toolCallId": o.ToolCallID})
			return nil
		},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:          r71streamModel(nil, chunks),
		Tools:          []types.Tool{tool},
		RuntimeContext: map[string]interface{}{"prefix": "runtime-context"},
		ToolsContext:   map[string]interface{}{"test-tool": map[string]interface{}{"prefix": "initial-tool-context"}},
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ToolsContext: map[string]interface{}{"test-tool": map[string]interface{}{"prefix": "step-tool-context"}}}
		},
		ToolChoice: types.RequiredToolChoice(),
		Prompt:     "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	ctxJSON := `"context":{"prefix":"step-tool-context"},"messages":[{"content":[{"type":"text","text":"test-input"}],"role":"user"}],"toolCallId":"` + id + `"`
	delta := func(d string) string {
		b, _ := json.Marshal(d)
		return `{"type":"onInputDelta","options":{` + ctxJSON + `,"inputTextDelta":` + string(b) + `}}`
	}
	want := `[{"type":"onInputStart","options":{` + ctxJSON + `}},` +
		delta(`{"`) + "," + delta("value") + "," + delta(`":"`) + "," + delta("Spark") + "," + delta("le") + "," + delta(" Day") + "," + delta(`"}`) + "," +
		`{"type":"onInputAvailable","options":{` + ctxJSON + `,"input":{"value":"Sparkle Day"}}}]`
	mu.Lock()
	defer mu.Unlock()
	r87cstreambAssert(t, "recorded calls", recorded, want)
}

var _ = strings.ToUpper

// r87cstreambToolErrorRun is the TS 'tool execution errors' beforeEach:
// tool1 throws "test error" (defaultSettings).
func r87cstreambToolErrorRun(t *testing.T) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	return r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{r87cstreambMeta("id-0"),
			r87cstreambCall("call-1", "tool1", `{ "value": "value" }`), r87cstreambFinish(types.FinishReasonStop, "stop")}),
		Tools: []types.Tool{{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("test error")
		}}},
		Prompt:   "prompt",
		Internal: r87cstreambIDs(),
	})
}

const r87cstreambToolErrorMessages = `[
	{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}]},
	{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"error-text","value":"Error: test error"}}]}]`

// stream-text.test.ts 20704: tool execution errors > should include tool
// error part in the full stream.
func TestR87cstreambToolErrorFullStream(t *testing.T) {
	_, parts := r87cstreambToolErrorRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[
		{"type":"start"},
		{"type":"start-step","request":{},"warnings":[]},
		{"type":"tool-call","input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1"},
		{"type":"tool-error","dynamic":false,"error":"test error","input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1"},
		{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"usage":`+r87cstreambUsageJSON+`},
		{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":`+r87cstreambUsageJSON+`}]`)
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFinishStep {
			if p.Performance == nil || len(p.Performance.ToolExecutionMs) != 1 {
				t.Fatalf("finish-step performance = %+v, want toolExecutionMs for call-1", p.Performance)
			}
			if _, ok := p.Performance.ToolExecutionMs["call-1"]; !ok || p.Performance.TimeBetweenOutputChunksMs != nil {
				t.Fatalf("finish-step performance = %+v", p.Performance)
			}
		}
	}
}

// stream-text.test.ts 20803: tool execution errors > should include the
// error part in the step stream.
func TestR87cstreambToolErrorSteps(t *testing.T) {
	r, _ := r87cstreambToolErrorRun(t)
	r87cstreambAssert(t, "steps", r87cstreambSteps(t, r.Steps()), `[{
		"callId":"test-telemetry-call-id",
		"content":[
			{"type":"tool-call","input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1"},
			{"type":"tool-error","dynamic":false,"error":"test error","input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1"}],
		"finishReason":"stop",
		"model":{"modelId":"mock-model-id","provider":"mock-provider"},
		"toolExecutionMsKeys":["call-1"],
		"rawFinishReason":"stop",
		"request":{},
		"response":{"id":"id-0","messages":`+r87cstreambToolErrorMessages+`,"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},
		"runtimeContext":{},"stepNumber":0,"toolsContext":{},
		"usage":`+r87cstreambUsageJSON+`,
		"warnings":[]}]`)
}

// stream-text.test.ts 20916: tool execution errors > should include error
// result in response messages.
func TestR87cstreambToolErrorResponseMessages(t *testing.T) {
	r, _ := r87cstreambToolErrorRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), r87cstreambToolErrorMessages)
}

// r87cstreambTextStream collects result.textStream.
func r87cstreambTextStream(t *testing.T, r *StreamTextResult) []string {
	t.Helper()
	s := r.Stream()
	out := []string{}
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			out = append(out, c.Text)
		}
	}
	return out
}

// stream-text.test.ts 21034: options.transform > with base transformation >
// should transform the stream.
func TestR87cstreambBaseTransformTextStream(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:                 r71streamModel(nil, r87cstreambDefaultStream()),
		ExperimentalTransform: []StreamTransformFunc{r72streamUpperTransform},
		Prompt:                "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r87cstreambTextStream(t, r); !reflect.DeepEqual(got, []string{"HELLO", ", ", "WORLD!"}) {
		t.Fatalf("textStream = %q", got)
	}
}

// stream-text.test.ts 21056: options.transform > with base transformation >
// result.responseMessages should be transformed (result.response: any id,
// timestamp and model id, no headers, the transformed assistant text).
func TestR87cstreambBaseTransformResponse(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:                 r71streamModel(nil, r87cstreambDefaultStream()),
		ExperimentalTransform: []StreamTransformFunc{r72streamUpperTransform},
		Prompt:                "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	resp := r.Response()
	if resp.ID == "" || resp.ModelID == "" || resp.Timestamp.IsZero() || len(resp.Headers) != 0 {
		t.Fatalf("response = %+v", resp)
	}
	r87cstreambAssert(t, "response.messages", r87cstreambMessages(t, resp.Messages), `[{"role":"assistant","content":[{"type":"text","text":"HELLO, WORLD!"}]}]`)
}

// stream-text.test.ts 21264: options.transform > with base transformation >
// result.steps should be transformed.
func TestR87cstreambBaseTransformSteps(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, r87cstreambJoin(
			[]provider.StreamChunk{r87cstreambMeta("id-0")},
			r87cstreambText("1", "Hello, ", "world!"),
			[]provider.StreamChunk{r87cstreambCall("call-1", "tool1", `{ "value": "value" }`), r87cstreambFinish(types.FinishReasonStop, "stop")})),
		Tools: []types.Tool{{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		ExperimentalTransform: []StreamTransformFunc{r72streamUpperTransform},
		Prompt:                "test-input",
		Internal:              r71streamInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := r.Steps()
	r87cstreambAssert(t, "steps", r87cstreambSteps(t, steps), `[{
		"callId":"test-telemetry-call-id",
		"content":[
			{"type":"text","text":"HELLO, WORLD!"},
			{"type":"tool-call","input":{"value":"VALUE"},"toolCallId":"call-1","toolName":"tool1"},
			{"type":"tool-result","dynamic":false,"input":{"value":"VALUE"},"output":"RESULT1","toolCallId":"call-1","toolName":"tool1"}],
		"finishReason":"stop",
		"model":{"modelId":"mock-model-id","provider":"mock-provider"},
		"toolExecutionMsKeys":["call-1"],
		"rawFinishReason":"stop",
		"request":{},
		"response":{"id":"id-0","messages":[
			{"role":"assistant","content":[{"type":"text","text":"HELLO, WORLD!"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"VALUE"}}]},
			{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"RESULT1"}}]}],
			"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},
		"runtimeContext":{},"stepNumber":0,"toolsContext":{},
		"usage":`+r87cstreambUsageJSON+`,
		"warnings":[]}]`)
	if steps[0].Performance.TimeBetweenOutputChunksMs == nil {
		t.Fatal("performance.timeBetweenOutputChunksMs missing (TS reports it for two text deltas)")
	}
}

// stream-text.test.ts 22636: options.transform > with transformation that
// aborts stream > options.onStepEnd should be called (step number 0).
func TestR87cstreambOnStepEndCalled(t *testing.T) {
	var mu sync.Mutex
	var got []int
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, r87cstreambJoin(r87cstreambText("1", "Hello, "), []provider.StreamChunk{r87cstreambFinish(types.FinishReasonStop, "stop")})),
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, e.StepNumber)
		},
		Prompt:   "test-input",
		Internal: r71streamInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("onStepEnd step numbers = %v, want [0]", got)
	}
}
