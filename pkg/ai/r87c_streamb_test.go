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

// r87cstreambTimestamp renders a response timestamp like the TS snapshot. A
// fallback timestamp (no response-metadata part) is the current time in Go
// and renders "<now>"; TS mocks the clock there and shows Date(0).
func r87cstreambTimestamp(ts time.Time) string {
	if d := time.Since(ts); d > -time.Minute && d < time.Minute {
		return "<now>"
	}
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
		m := set(map[string]interface{}{"type": "finish-step", "finishReason": string(c.FinishReason), "rawFinishReason": c.RawFinishReason, "response": resp, "usage": c.Usage})
		if c.RawFinishReason == "" {
			delete(m, "rawFinishReason")
		}
		return m
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

// r87cstreambCallOptions projects the doStream call options onto the TS
// LanguageModelV4CallOptions snapshot keys that are defined.
func r87cstreambCallOptions(t *testing.T, o *provider.GenerateOptions) map[string]interface{} {
	t.Helper()
	var prompt []interface{}
	if o.Prompt.System != "" {
		prompt = append(prompt, map[string]interface{}{"role": "system", "content": o.Prompt.System})
	}
	if msgs, ok := r87cstreambMessages(t, o.Prompt.Messages).([]interface{}); ok {
		prompt = append(prompt, msgs...)
	}
	if o.Prompt.Text != "" {
		prompt = append(prompt, map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": o.Prompt.Text}}})
	}
	m := map[string]interface{}{"includeRawChunks": o.IncludeRawChunks, "prompt": prompt, "toolChoice": o.ToolChoice}
	if o.ToolChoice.Type == "" {
		delete(m, "toolChoice")
	}
	add := func(k string, v interface{}, set bool) {
		if set {
			m[k] = v
		}
	}
	add("temperature", o.Temperature, o.Temperature != nil)
	add("maxOutputTokens", o.MaxTokens, o.MaxTokens != nil)
	add("topP", o.TopP, o.TopP != nil)
	add("topK", o.TopK, o.TopK != nil)
	add("frequencyPenalty", o.FrequencyPenalty, o.FrequencyPenalty != nil)
	add("presencePenalty", o.PresencePenalty, o.PresencePenalty != nil)
	add("stopSequences", o.StopSequences, o.StopSequences != nil)
	add("seed", o.Seed, o.Seed != nil)
	add("headers", o.Headers, o.Headers != nil)
	add("reasoning", o.Reasoning, o.Reasoning != nil)
	add("providerOptions", o.ProviderOptions, o.ProviderOptions != nil)
	if len(o.Tools) > 0 {
		var tools []interface{}
		for _, tool := range o.Tools {
			tm := map[string]interface{}{"type": "function", "name": tool.Name, "inputSchema": tool.Parameters}
			if tool.Description != "" {
				tm["description"] = tool.Description
			}
			tools = append(tools, tm)
		}
		m["tools"] = tools
	}
	if rf := o.ResponseFormat; rf != nil {
		f := map[string]interface{}{"type": rf.Type}
		if rf.Schema != nil {
			f["schema"] = rf.Schema
		}
		if rf.Name != "" {
			f["name"] = rf.Name
		}
		if rf.Description != "" {
			f["description"] = rf.Description
		}
		m["responseFormat"] = f
	}
	return m
}

// r87cstreambPartials runs StreamText over one model step and returns the
// partial outputs in publish order (TS partialOutputStream), recorded with
// r85streamPartialRecorder.
func r87cstreambPartials(t *testing.T, chunks []provider.StreamChunk, output interface{}) ([]interface{}, *StreamTextResult) {
	t.Helper()
	var res *StreamTextResult
	rec := &r85streamPartialRecorder{chunks: chunks, ready: make(chan struct{}), res: &res}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) { return rec, nil }}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "prompt", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	res = r
	close(rec.ready)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	rec.observe()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]interface{}{}, rec.partials...), r
}

func r87cstreambHelloWorldStep() []provider.StreamChunk {
	return r87cstreambJoin(r87cstreambText("1", "Hello, ", "world!"), []provider.StreamChunk{r87cstreambFinish(types.FinishReasonStop, "stop")})
}

// stream-text.test.ts 22811: options.output > text output (default) > should
// send partial output stream.
func TestR87cstreambDefaultTextPartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambHelloWorldStep(), nil)
	r87cstreambAssert(t, "partialOutputStream", got, `["Hello, ","Hello, world!"]`)
}

// stream-text.test.ts 22838: options.output > text output (default) > should
// resolve output promise with the correct content.
func TestR87cstreambDefaultTextOutput(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambHelloWorldStep()), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Output(); got != "Hello, world!" {
		t.Fatalf("output = %#v, want \"Hello, world!\"", got)
	}
}

// stream-text.test.ts 22861: options.output > text output > should send
// partial output stream.
func TestR87cstreambTextOutputPartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambHelloWorldStep(), TextOutput())
	r87cstreambAssert(t, "partialOutputStream", got, `["Hello, ","Hello, world!"]`)
}

// stream-text.test.ts 22933: options.output > text output > should resolve
// output promise with the correct content.
func TestR87cstreambTextOutputOutput(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambHelloWorldStep()), Prompt: "prompt", Output: TextOutput()})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Output(); got != "Hello, world!" {
		t.Fatalf("output = %#v, want \"Hello, world!\"", got)
	}
}

// r87cstreambZodValueSchema is the JSON schema zod produces for
// z.object({ value: z.string() }).
var r87cstreambZodValueSchema = map[string]interface{}{
	"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object",
	"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"},
}

func r87cstreambValueOutput() Output[map[string]interface{}, map[string]interface{}] {
	return ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(r87cstreambZodValueSchema)})
}

func r87cstreambObjectStep(deltas ...string) []provider.StreamChunk {
	return r87cstreambJoin(r87cstreambText("1", deltas...), []provider.StreamChunk{r87cstreambFinish(types.FinishReasonStop, "stop")})
}

var r87cstreambSixDeltas = []string{`{ `, `"value": `, `"Hello, `, `world`, `!"`, ` }`}

// stream-text.test.ts 22957: options.output > object output > should set
// responseFormat to json and send schema as part of the responseFormat.
func TestR87cstreambObjectOutputCallOptions(t *testing.T) {
	var got map[string]interface{}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			got = r87cstreambCallOptions(t, o)
			return testutil.NewMockTextStream(r87cstreambObjectStep(r87cstreambSixDeltas...)), nil
		}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Output: r87cstreambValueOutput(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r87cstreambAssert(t, "callOptions", got, `{
		"includeRawChunks":false,
		"prompt":[{"content":[{"text":"prompt","type":"text"}],"role":"user"}],
		"responseFormat":{"schema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"},"type":"json"},
		"toolChoice":{"type":"auto"}}`)
}

// stream-text.test.ts 23042: options.output > object output > should send
// valid partial text fragments (the text after a ':' is combined with the
// next delta).
func TestR87cstreambObjectOutputTextFragments(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(r87cstreambSixDeltas...)), Output: r87cstreambValueOutput(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r87cstreambTextStream(t, r); !reflect.DeepEqual(got, []string{`{ `, `"value": "Hello, `, `world`, `!"`, ` }`}) {
		t.Fatalf("textStream = %q", got)
	}
}

// stream-text.test.ts 23079: options.output > object output > should send
// partial output stream.
func TestR87cstreambObjectOutputPartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambObjectStep(r87cstreambSixDeltas...), r87cstreambValueOutput())
	r87cstreambAssert(t, "partialOutputStream", got, `[{},{"value":"Hello, "},{"value":"Hello, world"},{"value":"Hello, world!"}]`)
}

var r87cstreambFiveDeltas = []string{`{ `, `"value": `, `"Hello, `, `world!" `, `}`}

// stream-text.test.ts 23217: options.output > object output > should resolve
// text promise with the correct content.
func TestR87cstreambObjectOutputText(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(r87cstreambFiveDeltas...)), Output: r87cstreambValueOutput(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Text(); got != `{ "value": "Hello, world!" }` {
		t.Fatalf("text = %q", got)
	}
}

// stream-text.test.ts 23244: options.output > object output > should resolve
// output promise with the correct content.
func TestR87cstreambObjectOutputOutput(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(r87cstreambFiveDeltas...)), Output: r87cstreambValueOutput(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r87cstreambAssert(t, "output", r.Output(), `{"value":"Hello, world!"}`)
}

// stream-text.test.ts 23271: options.output > object output > should expose
// parsed output to onEnd after the stream completes.
func TestR87cstreambObjectOutputOnEnd(t *testing.T) {
	var mu sync.Mutex
	var got interface{}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  r71streamModel(nil, r87cstreambObjectStep(`{ "value": "Hello, world!" }`)),
		Output: r87cstreambValueOutput(), Prompt: "prompt",
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			got = e.Output
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	r87cstreambAssert(t, "onEnd output", got, `{"value":"Hello, world!"}`)
}

// r87cstreambToolResult is the TS tool-result entry of toolResults.
func r87cstreambToolResult(tr types.ToolResult) map[string]interface{} {
	return r87cstreambPart(provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &tr})
}

// r87cstreambEnd projects an onEnd / onFinish event onto the TS snapshot.
func r87cstreambEnd(t *testing.T, e OnFinishEvent) map[string]interface{} {
	t.Helper()
	calls := func(cs []types.ToolCall) []interface{} {
		out := []interface{}{}
		for i := range cs {
			out = append(out, r87cstreambToolCall(&cs[i]))
		}
		return out
	}
	results := func(rs []types.ToolResult) []interface{} {
		out := []interface{}{}
		for _, r := range rs {
			out = append(out, r87cstreambToolResult(r))
		}
		return out
	}
	list := func(v interface{}, n int) interface{} {
		if n == 0 {
			return []interface{}{}
		}
		return v
	}
	m := map[string]interface{}{
		"callId":             e.CallID,
		"content":            r87cstreambContent(t, e.Content),
		"dynamicToolCalls":   calls(e.DynamicToolCalls),
		"dynamicToolResults": results(e.DynamicToolResults),
		"files":              list(e.Files, len(e.Files)),
		"finalStep":          r87cstreambStep(t, e.FinalStep),
		"finishReason":       string(e.FinishReason),
		"model":              map[string]interface{}{"provider": e.Model.Provider, "modelId": e.Model.ModelID},
		"rawFinishReason":    e.RawFinishReason,
		"reasoning":          list(e.Reasoning, len(e.Reasoning)),
		"request":            r87cstreambNorm(t, e.Request),
		"responseMessages":   r87cstreambMessages(t, e.ResponseMessages),
		"runtimeContext":     e.RuntimeContext,
		"sources":            list(e.Sources, len(e.Sources)),
		"staticToolCalls":    calls(e.StaticToolCalls),
		"staticToolResults":  results(e.StaticToolResults),
		"stepNumber":         e.StepNumber,
		"steps":              r87cstreambSteps(t, e.Steps),
		"text":               e.Text,
		"toolCalls":          calls(e.ToolCalls),
		"toolResults":        results(e.ToolResults),
		"toolsContext":       e.ToolsContext,
		"totalUsage":         e.TotalUsage,
		"usage":              e.Usage,
		"warnings":           list(e.Warnings, len(e.Warnings)),
	}
	if e.Output != nil {
		m["output"] = e.Output
	}
	if e.ReasoningText != "" {
		m["reasoningText"] = e.ReasoningText
	}
	if e.RawFinishReason == "" {
		delete(m, "rawFinishReason")
	}
	if e.RuntimeContext == nil {
		m["runtimeContext"] = map[string]interface{}{}
	}
	if e.ToolsContext == nil {
		m["toolsContext"] = map[string]interface{}{}
	}
	if len(e.ProviderMetadata) > 0 {
		m["providerMetadata"] = e.ProviderMetadata
	}
	resp := map[string]interface{}{"id": e.Response.ID, "modelId": e.Response.ModelID, "timestamp": r87cstreambTimestamp(e.Response.Timestamp), "messages": r87cstreambMessages(t, e.Response.Messages)}
	if len(e.Response.Headers) > 0 {
		resp["headers"] = e.Response.Headers
	}
	m["response"] = resp
	return m
}

// stream-text.test.ts 23461: options.output > object output > should call
// onFinish with the correct content.
func TestR87cstreambObjectOutputOnFinish(t *testing.T) {
	var mu sync.Mutex
	var got OnFinishEvent
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  r71streamModel(nil, r87cstreambObjectStep(r87cstreambFiveDeltas...)),
		Output: r87cstreambValueOutput(), Prompt: "prompt",
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			got = e
		},
		Internal: r87cstreambIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	const text = `{ \"value\": \"Hello, world!\" }`
	msgs := `[{"content":[{"text":"` + text + `","type":"text"}],"role":"assistant"}]`
	step := `{"callId":"test-telemetry-call-id","content":[{"text":"` + text + `","type":"text"}],"finishReason":"stop",
		"model":{"modelId":"mock-model-id","provider":"mock-provider"},"toolExecutionMsKeys":[],"rawFinishReason":"stop","request":{},
		"response":{"id":"id-0","messages":` + msgs + `,"modelId":"mock-model-id","timestamp":"<now>"},
		"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":` + r87cstreambUsageJSON + `,"warnings":[]}`
	r87cstreambAssert(t, "onFinish", r87cstreambEnd(t, got), `{
		"callId":"test-telemetry-call-id",
		"content":[{"text":"`+text+`","type":"text"}],
		"dynamicToolCalls":[],"dynamicToolResults":[],"files":[],
		"finalStep":`+step+`,
		"finishReason":"stop",
		"model":{"modelId":"mock-model-id","provider":"mock-provider"},
		"output":{"value":"Hello, world!"},
		"rawFinishReason":"stop","reasoning":[],"request":{},
		"response":{"id":"id-0","messages":`+msgs+`,"modelId":"mock-model-id","timestamp":"<now>"},
		"responseMessages":`+msgs+`,
		"runtimeContext":{},"sources":[],"staticToolCalls":[],"staticToolResults":[],"stepNumber":0,
		"steps":[`+step+`],
		"text":"`+text+`","toolCalls":[],"toolResults":[],"toolsContext":{},
		"totalUsage":`+r87cstreambUsageJSON+`,"usage":`+r87cstreambUsageJSON+`,"warnings":[]}`)
}

// r87cstreambContentElement is the JSON schema zod produces for
// z.object({ content: z.string() }).
var r87cstreambContentElement = schema.NewSimpleJSONSchema(map[string]interface{}{
	"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object",
	"properties": map[string]interface{}{"content": map[string]interface{}{"type": "string"}}, "required": []interface{}{"content"},
})

func r87cstreambArrayOutput() Output[[]map[string]interface{}, []map[string]interface{}] {
	return ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{ElementSchema: r87cstreambContentElement})
}

// r87cstreambArray3 is the TS 'array with 3 elements' stream.
var r87cstreambArray3 = []string{`{"elements":[`, `{`, `"content":`, `"element 1"`, `},`, `{ `, `"content": `, `"element 2"`, `},`, `{`, `"content":`, `"element 3"`, `}`, `]`, `}`}

// r87cstreambArray2 is the TS 'array with 2 elements streamed in 1 chunk' stream.
var r87cstreambArray2 = []string{`{"elements":[{"content":"element 1"},{"content":"element 2"}]}`}

func r87cstreambArrayRun(t *testing.T, deltas []string) *StreamTextResult {
	t.Helper()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(deltas...)), Output: r87cstreambArrayOutput(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func r87cstreambElements(t *testing.T, r *StreamTextResult) []interface{} {
	t.Helper()
	out := []interface{}{}
	ch := ElementStream[map[string]interface{}](r, ElementStreamOptions[map[string]interface{}]{ElementSchema: r87cstreambContentElement})
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e.Element)
		case <-timeout:
			t.Fatal("elementStream did not end")
		}
	}
}

// stream-text.test.ts 23812: options.output > array output > array with 3
// elements > should stream only complete objects in partialObjectStream.
func TestR87cstreambArray3PartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambObjectStep(r87cstreambArray3...), r87cstreambArrayOutput())
	r87cstreambAssert(t, "partialOutputStream", got, `[[],[{"content":"element 1"}],[{"content":"element 1"},{"content":"element 2"}],[{"content":"element 1"},{"content":"element 2"},{"content":"element 3"}]]`)
}

// stream-text.test.ts 23845: options.output > array output > array with 3
// elements > should stream individual elements in elementStream.
func TestR87cstreambArray3ElementStream(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray3)
	r87cstreambAssert(t, "elementStream", r87cstreambElements(t, r), `[{"content":"element 1"},{"content":"element 2"},{"content":"element 3"}]`)
}

// stream-text.test.ts 23862: options.output > array output > array with 3
// elements > should resolve output promise with the correct content.
func TestR87cstreambArray3Output(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray3)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r87cstreambAssert(t, "output", r.Output(), `[{"content":"element 1"},{"content":"element 2"},{"content":"element 3"}]`)
}

// stream-text.test.ts 23870: options.output > array output > array with 3
// elements > should resolve text promise with the correct text.
func TestR87cstreambArray3Text(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray3)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Text(); got != `{"elements":[{"content":"element 1"},{ "content": "element 2"},{"content":"element 3"}]}` {
		t.Fatalf("text = %q", got)
	}
}

// stream-text.test.ts 23913: options.output > array output > array with 2
// elements streamed in 1 chunk > should stream only complete objects in
// partialObjectStream.
func TestR87cstreambArray2PartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambObjectStep(r87cstreambArray2...), r87cstreambArrayOutput())
	r87cstreambAssert(t, "partialOutputStream", got, `[[{"content":"element 1"},{"content":"element 2"}]]`)
}

// stream-text.test.ts 23929: options.output > array output > array with 2
// elements streamed in 1 chunk > should stream individual elements in
// elementStream.
func TestR87cstreambArray2ElementStream(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray2)
	r87cstreambAssert(t, "elementStream", r87cstreambElements(t, r), `[{"content":"element 1"},{"content":"element 2"}]`)
}

// stream-text.test.ts 23943: options.output > array output > array with 2
// elements streamed in 1 chunk > should resolve output promise with the
// correct content.
func TestR87cstreambArray2Output(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray2)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r87cstreambAssert(t, "output", r.Output(), `[{"content":"element 1"},{"content":"element 2"}]`)
}

// stream-text.test.ts 23950: options.output > array output > array with 2
// elements streamed in 1 chunk > should resolve text promise with the
// correct text.
func TestR87cstreambArray2Text(t *testing.T) {
	r := r87cstreambArrayRun(t, r87cstreambArray2)
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Text(); got != `{"elements":[{"content":"element 1"},{"content":"element 2"}]}` {
		t.Fatalf("text = %q", got)
	}
}

func r87cstreambWeather() Output[string, string] {
	return ChoiceOutput[string](ChoiceOutputOptions[string]{Options: []string{"sunny", "rainy", "snowy"}})
}

var r87cstreambSunny = []string{`{ `, `"result": `, `"su`, `nny`, `"`, ` }`}

// stream-text.test.ts 24119: options.output > choice output > should stream
// an choice value.
func TestR87cstreambChoicePartialOutputStream(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambObjectStep(r87cstreambSunny...), r87cstreambWeather())
	r87cstreambAssert(t, "partialOutputStream", got, `["sunny"]`)
}

// stream-text.test.ts 24154: options.output > choice output > should resolve
// text promise with the correct text.
func TestR87cstreambChoiceText(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(r87cstreambSunny...)), Output: r87cstreambWeather(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Text(); got != `{ "result": "sunny" }` {
		t.Fatalf("text = %q", got)
	}
}

// stream-text.test.ts 24184: options.output > choice output > should resolve
// output promise with the correct content.
func TestR87cstreambChoiceOutput(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreambObjectStep(r87cstreambSunny...)), Output: r87cstreambWeather(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got := r.Output(); got != "sunny" {
		t.Fatalf("output = %#v, want sunny", got)
	}
}

// stream-text.test.ts 24282: options.output > choice output > should handle
// non-ambiguous values ("foo" + "bar" against foobar / barfoo).
func TestR87cstreambChoiceNonAmbiguous(t *testing.T) {
	got, _ := r87cstreambPartials(t, r87cstreambObjectStep(`{ `, `"result": `, `"foo`, `bar`, `"`, ` }`),
		ChoiceOutput[string](ChoiceOutputOptions[string]{Options: []string{"foobar", "barfoo"}}))
	r87cstreambAssert(t, "partialOutputStream", got, `["foobar"]`)
}

// stream-text.test.ts 24518: options.activeTools > should filter available
// tools to only the ones in activeTools.
func TestR87cstreambActiveTools(t *testing.T) {
	var got map[string]interface{}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			got = r87cstreambCallOptions(t, o)
			return testutil.NewMockTextStream(r87cstreambJoin(r87cstreambText("1", "Hello", ", ", "world!"), []provider.StreamChunk{r87cstreambFinish(types.FinishReasonStop, "stop")})), nil
		}}
	exec := func(out string) types.ToolExecutor {
		return func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return out, nil
		}
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Tools: []types.Tool{
			{Name: "tool1", Parameters: r87cstreambZodValueSchema, Execute: exec("result1")},
			{Name: "tool2", Parameters: r87cstreambZodValueSchema, Execute: exec("result2")},
		},
		Prompt:      "test-input",
		ActiveTools: []string{"tool1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r87cstreambAssert(t, "tools", got["tools"], `[{"inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"},"name":"tool1","type":"function"}]`)
}

// r87cstreambInterleavedRun is the TS 'mixed text and reasoning blocks'
// beforeEach.
func r87cstreambInterleavedRun(t *testing.T) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	text := func(id, d string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: d}
	}
	reason := func(id, d string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Text: d}
	}
	mark := func(typ provider.ChunkType, id string) provider.StreamChunk {
		return provider.StreamChunk{Type: typ, ID: id}
	}
	return r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			mark(provider.ChunkTypeReasoningStart, "0"), mark(provider.ChunkTypeTextStart, "1"),
			reason("0", "Thinking..."), text("1", "Hello"), text("1", ", "),
			mark(provider.ChunkTypeTextStart, "2"), text("2", "This "), text("2", "is "),
			mark(provider.ChunkTypeReasoningStart, "3"), reason("0", "I'm thinking..."), reason("3", "Separate thoughts"),
			text("2", "a"), text("1", "world!"), mark(provider.ChunkTypeReasoningEnd, "0"), text("2", " test."),
			mark(provider.ChunkTypeTextEnd, "2"), mark(provider.ChunkTypeReasoningEnd, "3"), mark(provider.ChunkTypeTextEnd, "1"),
			r87cstreambFinish(types.FinishReasonStop, "stop"),
		}),
		Prompt:   "test-input",
		Internal: r87cstreambIDs(),
	})
}

// stream-text.test.ts 25171: mixed multi content streaming with interleaving
// parts > mixed text and reasoning blocks > should return the full stream
// with the correct parts.
func TestR87cstreambInterleavedFullStream(t *testing.T) {
	_, parts := r87cstreambInterleavedRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[
		{"type":"start"},{"type":"start-step","request":{},"warnings":[]},
		{"id":"0","type":"reasoning-start"},{"id":"1","type":"text-start"},
		{"id":"0","text":"Thinking...","type":"reasoning-delta"},
		{"id":"1","text":"Hello","type":"text-delta"},{"id":"1","text":", ","type":"text-delta"},
		{"id":"2","type":"text-start"},{"id":"2","text":"This ","type":"text-delta"},{"id":"2","text":"is ","type":"text-delta"},
		{"id":"3","type":"reasoning-start"},
		{"id":"0","text":"I'm thinking...","type":"reasoning-delta"},{"id":"3","text":"Separate thoughts","type":"reasoning-delta"},
		{"id":"2","text":"a","type":"text-delta"},{"id":"1","text":"world!","type":"text-delta"},
		{"id":"0","type":"reasoning-end"},{"id":"2","text":" test.","type":"text-delta"},
		{"id":"2","type":"text-end"},{"id":"3","type":"reasoning-end"},{"id":"1","type":"text-end"},
		{"finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"finishReason":"stop","rawFinishReason":"stop","totalUsage":`+r87cstreambUsageJSON+`,"type":"finish"}]`)
}

// stream-text.test.ts 25373: mixed multi content streaming with interleaving
// parts > mixed text and reasoning blocks > should return the step content
// parts in the correct order.
func TestR87cstreambInterleavedSteps(t *testing.T) {
	r, _ := r87cstreambInterleavedRun(t)
	content := `[{"text":"Thinking...I'm thinking...","type":"reasoning"},{"text":"Hello, world!","type":"text"},{"text":"This is a test.","type":"text"},{"text":"Separate thoughts","type":"reasoning"}]`
	r87cstreambAssert(t, "steps", r87cstreambSteps(t, r.Steps()), `[{"callId":"test-telemetry-call-id","content":`+content+`,"finishReason":"stop",
		"model":{"modelId":"mock-model-id","provider":"mock-provider"},"toolExecutionMsKeys":[],"rawFinishReason":"stop","request":{},
		"response":{"id":"id-0","messages":[{"content":`+content+`,"role":"assistant"}],"modelId":"mock-model-id","timestamp":"<now>"},
		"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":`+r87cstreambUsageJSON+`,"warnings":[]}]`)
}

// r87cstreambAbortRec records the abort / error / end callbacks of the TS
// 'abort signal' blocks.
type r87cstreambAbortRec struct {
	mu     sync.Mutex
	errs   []error
	aborts []GenerateTextAbortEvent
	ends   int
}

func (a *r87cstreambAbortRec) options(opts StreamTextOptions) StreamTextOptions {
	opts.OnError = func(_ context.Context, err error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.errs = append(a.errs, err)
	}
	opts.OnAbortEvent = func(_ context.Context, e GenerateTextAbortEvent) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.aborts = append(a.aborts, e)
	}
	opts.OnEndEvent = func(context.Context, OnFinishEvent) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.ends++
	}
	return opts
}

// r87cstreambAbortSecondStep is the TS 'abort in 2nd step' beforeEach: step
// 1 calls tool1, step 2 streams "Hello" and then the caller aborts and the
// provider stream fails with the abort.
func r87cstreambAbortSecondStep(t *testing.T) (*StreamTextResult, *r87cstreambAbortRec, []provider.StreamChunk) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var mu sync.Mutex
	call := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			call++
			n := call
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
					r87cstreambCall("call-1", "tool1", `{ "value": "value" }`),
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}}), nil
			}
			return &abortingStream{ctx: ctx, cancel: cancel, chunks: []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
				{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}}}, nil
		}}
	rec := &r87cstreambAbortRec{}
	r, err := StreamText(ctx, rec.options(StreamTextOptions{
		Model: model,
		Tools: []types.Tool{{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		Prompt:   "prompt",
		Internal: r87cstreambIDs(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	s := r.FullStream()
	var parts []provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	return r, rec, parts
}

// r87cstreambAbortStepJSON is the completed first step TS onAbort receives.
const r87cstreambAbortStepJSON = `{"callId":"test-telemetry-call-id",
	"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},
		{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],
	"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"toolExecutionMsKeys":["call-1"],"request":{},
	"response":{"id":"id-0","messages":[
		{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},
		{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],
		"modelId":"mock-model-id","timestamp":"<now>"},
	"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":` + r87cstreambUsageJSON + `,"warnings":[]}`

// stream-text.test.ts 25915: abort signal > abort in 2nd step > should not
// call onError for abort errors.
func TestR87cstreambAbortSecondStepNoOnError(t *testing.T) {
	_, rec, _ := r87cstreambAbortSecondStep(t)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errs) != 0 {
		t.Fatalf("onError calls = %v, want []", rec.errs)
	}
}

// stream-text.test.ts 25920: abort signal > abort in 2nd step > should call
// onAbort when the abort signal is triggered.
func TestR87cstreambAbortSecondStepOnAbort(t *testing.T) {
	_, rec, _ := r87cstreambAbortSecondStep(t)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.aborts) != 1 {
		t.Fatalf("onAbort calls = %d, want 1", len(rec.aborts))
	}
	e := rec.aborts[0]
	if e.CallID != "test-telemetry-call-id" || !errors.Is(e.Reason, context.Canceled) || abortReasonMessage(e.Reason) != "AbortError: This operation was aborted" {
		t.Fatalf("onAbort callId %q reason %v", e.CallID, e.Reason)
	}
	r87cstreambAssert(t, "onAbort steps", r87cstreambSteps(t, e.Steps), "["+r87cstreambAbortStepJSON+"]")
}

// stream-text.test.ts 26040: abort signal > abort in 2nd step > should not
// call onEnd when aborting after a completed step.
func TestR87cstreambAbortSecondStepNoOnEnd(t *testing.T) {
	_, rec, _ := r87cstreambAbortSecondStep(t)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.ends != 0 {
		t.Fatalf("onEnd calls = %d, want 0", rec.ends)
	}
}

// stream-text.test.ts 26045: abort signal > abort in 2nd step > should
// reject result promises when aborting after a completed step (Go: the
// result's Err and ConsumeStream report the abort).
func TestR87cstreambAbortSecondStepRejects(t *testing.T) {
	r, _, _ := r87cstreambAbortSecondStep(t)
	if err := r.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Err() = %v, want the abort", err)
	}
	if err := r.ConsumeStream(); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConsumeStream() = %v, want the abort", err)
	}
}

// stream-text.test.ts 26059: abort signal > abort in 2nd step > should only
// stream initial chunks in full stream. The first step and the abort part
// are the TS snapshot. Known difference "Abort keeps the parts produced
// before it" (known-differences.mdx): TS also drops the second step's
// start-step, text-start and text-delta, which were still queued in its pipe
// chain when the abort cancelled the reader; Go delivers the parts the model
// produced before the abort, between the first step and the abort part.
func TestR87cstreambAbortSecondStepFullStream(t *testing.T) {
	_, _, parts := r87cstreambAbortSecondStep(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[
		{"type":"start"},{"type":"start-step","request":{},"warnings":[]},
		{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},
		{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},
		{"finishReason":"tool-calls","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"type":"start-step","request":{},"warnings":[]},{"id":"1","type":"text-start"},{"id":"1","text":"Hello","type":"text-delta"},
		{"reason":"AbortError: This operation was aborted","type":"abort"}]`)
}

// r87cstreambAbortDuringTool is the TS 'abort during tool execution'
// beforeEach: tool1 aborts the call while it runs and returns "result1".
func r87cstreambAbortDuringTool(t *testing.T) (*StreamTextResult, *r87cstreambAbortRec) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var mu sync.Mutex
	call := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			call++
			n := call
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
					r87cstreambCall("call-1", "tool1", `{ "value": "value" }`),
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}}), nil
			}
			return testutil.NewMockTextStream(r87cstreambJoin([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}}},
				r87cstreambText("1", "Hello"),
				[]provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}})), nil
		}}
	rec := &r87cstreambAbortRec{}
	r, err := StreamText(ctx, rec.options(StreamTextOptions{
		Model: model,
		Tools: []types.Tool{{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel()
			return "result1", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		Prompt:   "prompt",
		Internal: r87cstreambIDs(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	return r, rec
}

// stream-text.test.ts 26290: abort signal > abort during tool execution >
// should not call onError for abort errors.
func TestR87cstreambAbortDuringToolNoOnError(t *testing.T) {
	_, rec := r87cstreambAbortDuringTool(t)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errs) != 0 {
		t.Fatalf("onError calls = %v, want []", rec.errs)
	}
}

// stream-text.test.ts 26295: abort signal > abort during tool execution >
// should call onAbort when the abort signal is triggered during tool call.
func TestR87cstreambAbortDuringToolOnAbort(t *testing.T) {
	_, rec := r87cstreambAbortDuringTool(t)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.aborts) != 1 {
		t.Fatalf("onAbort calls = %d, want 1", len(rec.aborts))
	}
	e := rec.aborts[0]
	if e.CallID != "test-telemetry-call-id" || !errors.Is(e.Reason, context.Canceled) || len(e.Steps) != 0 {
		t.Fatalf("onAbort = callId %q reason %v steps %d, want test-telemetry-call-id / abort / []", e.CallID, e.Reason, len(e.Steps))
	}
}
