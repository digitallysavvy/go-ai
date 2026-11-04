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
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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
			m["dynamic"] = c.ToolCall.Dynamic
			if c.ToolCall.ProviderExecuted {
				m["providerExecuted"] = true
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
// Go-only legacy tool_calls key dropped, a file part's fileData as TS data).
func r87cstreambMessages(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	out := r87cstreambNorm(t, msgs)
	list, _ := out.([]interface{})
	for _, m := range list {
		if mm, ok := m.(map[string]interface{}); ok {
			delete(mm, "tool_calls")
			parts, _ := mm["content"].([]interface{})
			for _, p := range parts {
				// A Go file part keeps TS `data` ({type, data}) in fileData
				// and the legacy raw bytes in data.
				if pm, ok := p.(map[string]interface{}); ok && pm["fileData"] != nil {
					pm["data"] = pm["fileData"]
					delete(pm, "fileData")
				}
			}
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

// r87cstreambInvalidCallRun is the TS 'single invalid tool call' beforeEach:
// cityAttractions expects {city} and the model sends {cities}.
func r87cstreambInvalidCallRun(t *testing.T) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	input := `{ "cities": "San Francisco" }`
	return r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: input},
			{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
			r87cstreambCall("call-1", "cityAttractions", input),
			r87cstreambFinish(types.FinishReasonStop, "stop"),
		}),
		Prompt:   "test-input",
		Internal: r87cstreambIDs(),
		Tools: []types.Tool{{Name: "cityAttractions", Parameters: map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object",
			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}}}},
	})
}

// r87cstreambInvalidCallError is the invalid call's error message: the TS
// text up to "Error message: ", then Go's validator text (known difference
// "the validator text inside a TypeValidationError is Go's, not zod's issue
// list", known-differences.mdx).
const r87cstreambInvalidCallError = `Invalid input for tool cityAttractions: AI_TypeValidationError: Type validation failed: Value: {\"cities\":\"San Francisco\"}.\nError message: Error: $.city: required: property is missing`

const r87cstreambInvalidCallParts = `{"dynamic":true,"error":"` + r87cstreambInvalidCallError + `","input":{"cities":"San Francisco"},"invalid":true,"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-call"},
	{"dynamic":true,"error":"AI_InvalidToolInputError: ` + r87cstreambInvalidCallError + `","input":{"cities":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-error"}`

// stream-text.test.ts 26705: invalid tool calls > single invalid tool call >
// should add tool call and result error parts to the content.
func TestR87cstreambInvalidToolCallContent(t *testing.T) {
	r, _ := r87cstreambInvalidCallRun(t)
	r87cstreambAssert(t, "content", r87cstreambContent(t, r.Content()), "["+r87cstreambInvalidCallParts+"]")
	var inv *InvalidToolInputError
	for _, c := range r.Content() {
		if tc, ok := c.(types.ToolCallContent); ok {
			if msg, _ := tc.Error.(string); !strings.HasPrefix(msg, "Invalid input for tool cityAttractions: ") {
				t.Fatalf("tool-call error = %v", tc.Error)
			}
		}
		if te, ok := c.(types.ToolErrorContent); ok && !errors.As(te.Err, &inv) {
			t.Fatalf("tool-error Err = %T, want *InvalidToolInputError (TS AI_InvalidToolInputError)", te.Err)
		}
	}
}

// stream-text.test.ts 26757: invalid tool calls > single invalid tool call >
// should add tool call and result error parts to the full stream.
func TestR87cstreambInvalidToolCallFullStream(t *testing.T) {
	_, parts := r87cstreambInvalidCallRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[
		{"type":"start"},{"request":{},"type":"start-step","warnings":[]},
		{"dynamic":false,"id":"call-1","toolName":"cityAttractions","type":"tool-input-start"},
		{"delta":"{ \"cities\": \"San Francisco\" }","id":"call-1","type":"tool-input-delta"},
		{"id":"call-1","type":"tool-input-end"},
		`+r87cstreambInvalidCallParts+`,
		{"finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"finishReason":"stop","rawFinishReason":"stop","totalUsage":`+r87cstreambUsageJSON+`,"type":"finish"}]`)
}

// r87cstreambPreliminaryRun is the TS 'single tool with preliminary results'
// beforeEach (cityAttractions yields loading, then success).
func r87cstreambPreliminaryRun(t *testing.T) *StreamTextResult {
	t.Helper()
	tool := cityAttractionsTool()
	tool.Parameters = map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object",
		"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}}
	r, _ := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			r87cstreambCall("call-1", "cityAttractions", `{ "city": "San Francisco" }`), r87cstreambFinish(types.FinishReasonStop, "stop")}),
		Prompt:   "test-input",
		Internal: r87cstreambIDs(),
		Tools:    []types.Tool{tool},
	})
	return r
}

const r87cstreambPreliminaryContent = `[{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-call"},
	{"dynamic":false,"input":{"city":"San Francisco"},"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-result"}]`

// stream-text.test.ts 27189: tools with preliminary results > single tool
// with preliminary results > should only include final tool result in
// content.
func TestR87cstreambPreliminaryContent(t *testing.T) {
	r := r87cstreambPreliminaryRun(t)
	r87cstreambAssert(t, "content", r87cstreambContent(t, r.Content()), r87cstreambPreliminaryContent)
}

// stream-text.test.ts 27221: tools with preliminary results > single tool
// with preliminary results > should only include final tool result in step
// content.
func TestR87cstreambPreliminaryStepContent(t *testing.T) {
	r := r87cstreambPreliminaryRun(t)
	r87cstreambAssert(t, "steps", r87cstreambSteps(t, r.Steps()), `[{"callId":"test-telemetry-call-id","content":`+r87cstreambPreliminaryContent+`,
		"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"toolExecutionMsKeys":["call-1"],"rawFinishReason":"stop","request":{},
		"response":{"id":"id-0","messages":[
			{"content":[{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-call"}],"role":"assistant"},
			{"content":[{"output":{"type":"json","value":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"}},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-result"}],"role":"tool"}],
			"modelId":"mock-model-id","timestamp":"<now>"},
		"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":`+r87cstreambUsageJSON+`,"warnings":[]}]`)
}

// stream-text.test.ts 27670: programmatic tool calling > should stop an
// interrupted stream with a deferred provider tool result.
func TestR87cstreambInterruptedDeferredProviderResult(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "program", RawArguments: "{}", ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonInterrupted, Usage: r87cstreambUsage()},
			}), nil
		}}
	var errs []error
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Run a program",
		Tools: []types.Tool{{Name: "program", Type: "provider", ProviderID: "test.program", ProviderExecuted: true, SupportsDeferredResults: true,
			Parameters: map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object", "properties": map[string]interface{}{}}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, err)
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
	if calls != 1 || len(r.Steps()) != 1 || r.FinishReason() != types.FinishReasonInterrupted || len(errs) != 0 {
		t.Fatalf("doStream calls %d, steps %d, finishReason %q, onError %v; want 1, 1, interrupted, none", calls, len(r.Steps()), r.FinishReason(), errs)
	}
}

// The full stdout and final text of the TS dice game fixture.
const (
	r87cstreambDiceStdout = "=== DICE GAME: First to 3 Wins ===\n\n--- Round 1 ---\nPlayer 1 rolls: 5\nPlayer 2 rolls: 6\nPlayer 2 wins this round!\nScore: Player 1 = 0, Player 2 = 1\n\n--- Round 2 ---\nPlayer 1 rolls: 4\nPlayer 2 rolls: 6\nPlayer 2 wins this round!\nScore: Player 1 = 0, Player 2 = 2\n\n--- Round 3 ---\nPlayer 1 rolls: 5\nPlayer 2 rolls: 2\nPlayer 1 wins this round!\nScore: Player 1 = 1, Player 2 = 2\n\n--- Round 4 ---\nPlayer 1 rolls: 6\nPlayer 2 rolls: 6\nDraw! No one gets a point.\nScore: Player 1 = 1, Player 2 = 2\n\n--- Round 5 ---\nPlayer 1 rolls: 3\nPlayer 2 rolls: 2\nPlayer 1 wins this round!\nScore: Player 1 = 2, Player 2 = 2\n\n--- Round 6 ---\nPlayer 1 rolls: 6\nPlayer 2 rolls: 6\nDraw! No one gets a point.\nScore: Player 1 = 2, Player 2 = 2\n\n--- Round 7 ---\nPlayer 1 rolls: 2\nPlayer 2 rolls: 3\nPlayer 2 wins this round!\nScore: Player 1 = 2, Player 2 = 3\n\n========================================\n🏆 GAME OVER: Player 2 wins the game! (3-2)\n========================================\n\n📊 Analysis:\nTotal rounds played: 7\nPlayer 2 had better results - likely using the loaded die! 🎲\n"
	r87cstreambDiceFinal  = "## Game Results\n\n**Player 2 wins the game 3-2!** 🏆\n\nThe game lasted 7 rounds with the following breakdown:\n- **Player 1**: 2 round wins\n- **Player 2**: 3 round wins\n- **Draws**: 2 rounds (both rolling 6)\n\nBased on the results, **Player 2 appears to be the one using the loaded die**."
)

// r87cstreambDiceRun runs the TS 'dice game fixture' (r81streamDiceSteps
// with the full stdout and final text; the code_execution input is the
// parsed {code}: TS zod strips the fixture's extra "type" key, known
// difference "TS zod object schemas strip keys").
func r87cstreambDiceRun(t *testing.T) (*StreamTextResult, []*provider.GenerateOptions, OnFinishEvent) {
	t.Helper()
	steps := r81streamDiceSteps()
	last := steps[4]
	tr := *last[1].ToolResult
	tr.Result = map[string]interface{}{"type": "code_execution_result", "stdout": r87cstreambDiceStdout, "stderr": "", "return_code": float64(0), "content": []interface{}{}}
	last[1].ToolResult = &tr
	last[3].Text = r87cstreambDiceFinal
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, o)
			return testutil.NewMockTextStream(steps[len(calls)-1]), nil
		}}
	var end OnFinishEvent
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Play a dice game between two players.",
		Tools: []types.Tool{
			{Name: "code_execution", Type: "provider", ProviderID: "anthropic.code_execution_20250825", ProviderExecuted: true, SupportsDeferredResults: true,
				Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string"}}, "required": []interface{}{"code"}}},
			{Name: "rollDie", Description: "Roll a die and return the result.",
				Parameters:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{"player": map[string]interface{}{"type": "string", "enum": []interface{}{"player1", "player2"}}}, "required": []interface{}{"player"}, "additionalProperties": false},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": []interface{}{"code_execution_20250825"}}},
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					if in["player"] == "player1" {
						return 6, nil
					}
					return 3, nil
				}},
		},
		StopWhen: []StopCondition{IsStepCount(10)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			if s.StepNumber > 0 && len(s.Steps) > 0 {
				if a, ok := s.Steps[len(s.Steps)-1].ProviderMetadata["anthropic"].(map[string]interface{}); ok {
					if c, ok := a["container"].(map[string]interface{}); ok && c["id"] != nil {
						s.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"container": map[string]interface{}{"id": c["id"]}}}
					}
				}
			}
			return s
		},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			end = e
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
	return r, append([]*provider.GenerateOptions(nil), calls...), end
}

func r87cstreambRollCall(id, player string) string {
	return `{"input":{"player":"` + player + `"},"toolCallId":"` + id + `","toolName":"rollDie","type":"tool-call"}`
}

func r87cstreambRollResult(id string, value int) string {
	return `{"output":{"type":"json","value":` + map[int]string{6: "6", 3: "3"}[value] + `},"toolCallId":"` + id + `","toolName":"rollDie","type":"tool-result"}`
}

func r87cstreambRollTurn(id, player string, value int) string {
	return `{"content":[` + r87cstreambRollCall(id, player) + `],"role":"assistant"},{"content":[` + r87cstreambRollResult(id, value) + `],"role":"tool"}`
}

const r87cstreambDiceFirstAssistant = `{"content":[{"text":"I'll help you simulate this game between two players where one is using a loaded die.","type":"text"},
	{"input":{"code":"game_loop()"},"providerExecuted":true,"toolCallId":"srvtoolu_01MzSrFWsmzBdcoQkGWLyRjK","toolName":"code_execution","type":"tool-call"},
	{"input":{"player":"player1"},"toolCallId":"toolu_019jKkXz4jAdwHweHBw92CVY","toolName":"rollDie","type":"tool-call"}],"role":"assistant"}`

// stream-text.test.ts 28263: programmatic tool calling > 5 steps:
// code_execution triggers client tool across multiple turns (dice game
// fixture) > step inputs > should include all previous messages in step 3
// prompt (round 2).
func TestR87cstreambDiceStep3Prompt(t *testing.T) {
	_, calls, _ := r87cstreambDiceRun(t)
	if len(calls) != 5 {
		t.Fatalf("doStream calls = %d, want 5", len(calls))
	}
	r87cstreambAssert(t, "step 3 prompt", r87cstreambCallOptions(t, calls[2])["prompt"], `[
		{"content":[{"text":"Play a dice game between two players.","type":"text"}],"role":"user"},
		`+r87cstreambDiceFirstAssistant+`,
		{"content":[`+r87cstreambRollResult("toolu_019jKkXz4jAdwHweHBw92CVY", 6)+`],"role":"tool"},
		`+r87cstreambRollTurn("toolu_015dGLMbwBKv1ZRQr6KdJzeH", "player2", 3)+`]`)
}

// r87cstreambDiceResponseMessages is the TS result.responseMessages snapshot.
func r87cstreambDiceResponseMessages() string {
	stdout, _ := json.Marshal(r87cstreambDiceStdout)
	final, _ := json.Marshal(r87cstreambDiceFinal)
	return `[` + r87cstreambDiceFirstAssistant + `,
		{"content":[` + r87cstreambRollResult("toolu_019jKkXz4jAdwHweHBw92CVY", 6) + `],"role":"tool"},
		` + r87cstreambRollTurn("toolu_015dGLMbwBKv1ZRQr6KdJzeH", "player2", 3) + `,
		` + r87cstreambRollTurn("toolu_01YYqBNq5mk1wMtv3PAqY44m", "player1", 6) + `,
		` + r87cstreambRollTurn("toolu_018WxjDkQG8h7i63poySGT2x", "player2", 3) + `,
		{"content":[{"output":{"type":"json","value":{"content":[],"return_code":0,"stderr":"","stdout":` + string(stdout) + `,"type":"code_execution_result"}},
			"toolCallId":"srvtoolu_01MzSrFWsmzBdcoQkGWLyRjK","toolName":"code_execution","type":"tool-result"},
			{"text":` + string(final) + `,"type":"text"}],"role":"assistant"}]`
}

// stream-text.test.ts 28380: programmatic tool calling > 5 steps:
// code_execution triggers client tool across multiple turns (dice game
// fixture) > result.responseMessages > should contain response messages from
// all steps.
func TestR87cstreambDiceResponseMessages(t *testing.T) {
	r, _, _ := r87cstreambDiceRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), r87cstreambDiceResponseMessages())
}

// stream-text.test.ts 28904: programmatic tool calling > 5 steps:
// code_execution triggers client tool across multiple turns (dice game
// fixture) > onFinish callback > should contain response messages from all
// steps (9 messages).
func TestR87cstreambDiceOnFinishResponseMessages(t *testing.T) {
	_, _, end := r87cstreambDiceRun(t)
	if len(end.ResponseMessages) != 9 {
		t.Fatalf("onFinish responseMessages = %d, want 9", len(end.ResponseMessages))
	}
	r87cstreambAssert(t, "onFinish responseMessages", r87cstreambMessages(t, end.ResponseMessages), r87cstreambDiceResponseMessages())
}

// r87cstreambUserApprovalRun is the TS 'when a single tool needs approval
// (user-defined)' beforeEach.
func r87cstreambUserApprovalRun(t *testing.T) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	return r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			r87cstreambCall("call-1", "tool1", `{ "value": "value" }`),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}}),
		Prompt:   "test-input",
		Internal: r87cstreambIDs(),
		Tools: []types.Tool{{Name: "tool1", Parameters: r87cstreambZodValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
	})
}

const r87cstreambApprovalCall = `{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}`

const r87cstreambApprovalRequest = `{"approvalId":"id-1","toolCall":` + r87cstreambApprovalCall + `,"type":"tool-approval-request"}`

// stream-text.test.ts 30190: tool execution approval > when a single tool
// needs approval (user-defined) > should add tool approval requests to the
// full stream.
func TestR87cstreambUserApprovalFullStream(t *testing.T) {
	_, parts := r87cstreambUserApprovalRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},
		`+r87cstreambApprovalCall+`,`+r87cstreambApprovalRequest+`,
		{"finishReason":"tool-calls","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"finishReason":"tool-calls","totalUsage":`+r87cstreambUsageJSON+`,"type":"finish"}]`)
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFinishStep && (p.Performance == nil || len(p.Performance.ToolExecutionMs) != 0) {
			t.Fatalf("finish-step toolExecutionMs = %+v, want {}", p.Performance)
		}
	}
}

// r87cstreambUI runs StreamText and returns its UI message stream chunks.
func r87cstreambUI(t *testing.T, opts StreamTextOptions) []map[string]interface{} {
	t.Helper()
	chunks, err := r72streamUI(t, opts)
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

// stream-text.test.ts 30292: tool execution approval > when a single tool
// needs approval (user-defined) > should add tool approval requests to the
// UI message stream.
func TestR87cstreambUserApprovalUIStream(t *testing.T) {
	model := r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
		r87cstreambCall("call-1", "tool1", `{ "value": "value" }`),
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}})
	got := r87cstreambUI(t, StreamTextOptions{
		Model: model, Prompt: "test-input", Internal: r87cstreambIDs(),
		Tools: []types.Tool{{Name: "tool1", Parameters: r87cstreambZodValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
	})
	r87cstreambAssert(t, "ui stream", got, `[{"type":"start"},{"type":"start-step"},
		{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
		{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},
		{"type":"finish-step"},{"finishReason":"tool-calls","type":"finish"}]`)
}

// stream-text.test.ts 30326: tool execution approval > when a single tool
// needs approval (user-defined) > should add tool approval requests to the
// content.
func TestR87cstreambUserApprovalContent(t *testing.T) {
	r, _ := r87cstreambUserApprovalRun(t)
	r87cstreambAssert(t, "content", r87cstreambContent(t, r.Content()), `[`+r87cstreambApprovalCall+`,`+r87cstreambApprovalRequest+`]`)
}

// stream-text.test.ts 30359: tool execution approval > when a single tool
// needs approval (user-defined) > should add tool approval requests to the
// response messages.
func TestR87cstreambUserApprovalResponseMessages(t *testing.T) {
	r, _ := r87cstreambUserApprovalRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), `[{"content":[`+r87cstreambApprovalCall+`,
		{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"}],"role":"assistant"}]`)
}

// r87cstreambExecRec records tool1's executions.
type r87cstreambExecRec struct {
	mu    sync.Mutex
	calls []map[string]interface{}
}

func (e *r87cstreambExecRec) tool(t *testing.T, result func(map[string]interface{}) interface{}) types.Tool {
	return types.Tool{Name: "tool1", Parameters: r87cstreambZodValueSchema, Execute: func(_ context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.calls = append(e.calls, map[string]interface{}{"input": in, "toolCallId": o.ToolCallID, "messages": o.Messages != nil, "sandbox": o.ExperimentalSandbox})
		return result(in), nil
	}}
}

// r87cstreambPromptRec records the prompt of every doStream call and serves
// the given steps (the last one repeats).
func r87cstreambPromptModel(t *testing.T, prompts *[]interface{}, mu *sync.Mutex, steps ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	n := 0
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			*prompts = append(*prompts, r87cstreambCallOptions(t, o)["prompt"])
			i := n
			if i >= len(steps) {
				i = len(steps) - 1
			}
			n++
			return testutil.NewMockTextStream(steps[i]), nil
		}}
}

var r87cstreambHelloStep = []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
	{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
	{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r87cstreambUsage()}}

// r87cstreambAutoApprovedRun is the TS 'when a single tool is automatically
// approved' beforeEach.
func r87cstreambAutoApprovedRun(t *testing.T) (*StreamTextResult, *r87cstreambExecRec, []interface{}) {
	t.Helper()
	var mu sync.Mutex
	var prompts []interface{}
	exec := &r87cstreambExecRec{}
	reason := "trusted internal tool"
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r87cstreambPromptModel(t, &prompts, &mu,
			[]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}}, r87cstreambCall("call-1", "tool1", `{ "value": "value" }`),
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}},
			r87cstreambHelloStep),
		Prompt:       "test-input",
		StopWhen:     []StopCondition{StepCountIs(3)},
		Internal:     r87cstreambIDs(),
		Tools:        []types.Tool{exec.tool(t, func(map[string]interface{}) interface{} { return "result1" })},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved, Reason: &reason}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return r, exec, append([]interface{}(nil), prompts...)
}

// stream-text.test.ts 30737: tool execution approval > when a single tool is
// automatically approved > should continue to a second step and execute the
// tool.
func TestR87cstreambAutoApprovedExecutes(t *testing.T) {
	r, exec, _ := r87cstreambAutoApprovedRun(t)
	exec.mu.Lock()
	defer exec.mu.Unlock()
	r87cstreambAssert(t, "execute calls", exec.calls, `[{"input":{"value":"value"},"messages":true,"sandbox":null,"toolCallId":"call-1"}]`)
	if r.Text() != "Hello, world!" || len(r.Steps()) != 2 {
		t.Fatalf("text %q, steps %d; want Hello, world! and 2", r.Text(), len(r.Steps()))
	}
}

const r87cstreambUserPrompt = `{"content":[{"text":"test-input","type":"text"}],"role":"user"}`

const r87cstreambToolResult1 = `{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}`

// stream-text.test.ts 30750: tool execution approval > when a single tool is
// automatically approved > should call the model again for the
// second step with the tool result.
func TestR87cstreambAutoApprovedPrompts(t *testing.T) {
	_, _, prompts := r87cstreambAutoApprovedRun(t)
	r87cstreambAssert(t, "prompts", prompts, `[[`+r87cstreambUserPrompt+`],
		[`+r87cstreambUserPrompt+`,{"content":[`+r87cstreambApprovalCall+`],"role":"assistant"},{"content":[`+r87cstreambToolResult1+`],"role":"tool"}]]`)
}

// stream-text.test.ts 30813: tool execution approval > when a single tool is
// automatically approved > should produce response
// messages with automatic approval metadata.
func TestR87cstreambAutoApprovedResponseMessages(t *testing.T) {
	r, _, _ := r87cstreambAutoApprovedRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), `[
		{"content":[`+r87cstreambApprovalCall+`,{"approvalId":"id-1","isAutomatic":true,"toolCallId":"call-1","type":"tool-approval-request"}],"role":"assistant"},
		{"content":[{"approvalId":"id-1","approved":true,"reason":"trusted internal tool","type":"tool-approval-response"},`+r87cstreambToolResult1+`],"role":"tool"},
		{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)
}

// r87cstreambApprovalFuncRun is the TS 'when a single tool has a
// user-defined approval function' beforeEach.
func r87cstreambApprovalFuncRun(t *testing.T) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	exec := &r87cstreambExecRec{}
	return r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{}},
			r87cstreambCall("call-1", "tool1", `{ "value": "value-needs-approval" }`),
			r87cstreambCall("call-2", "tool1", `{ "value": "value-no-approval" }`),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}}),
		Tools: []types.Tool{exec.tool(t, func(in map[string]interface{}) interface{} { return "result for " + in["value"].(string) })},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.SingleToolApprovalFunc(func(in map[string]interface{}, _ types.SingleToolApprovalOptions) types.ToolApprovalResult {
			if in["value"] == "value-needs-approval" {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		})},
		StopWhen: []StopCondition{StepCountIs(3)},
		Prompt:   "test-input",
		Internal: r87cstreambIDs(),
	})
}

const r87cstreambFuncCall1 = `{"input":{"value":"value-needs-approval"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}`
const r87cstreambFuncCall2 = `{"input":{"value":"value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-call"}`
const r87cstreambFuncResult2 = `{"dynamic":false,"input":{"value":"value-no-approval"},"output":"result for value-no-approval","toolCallId":"call-2","toolName":"tool1","type":"tool-result"}`
const r87cstreambFuncRequest = `{"approvalId":"id-1","toolCall":` + r87cstreambFuncCall1 + `,"type":"tool-approval-request"}`

// stream-text.test.ts 30992: tool execution approval > when a single tool has
// a user-defined approval function > should add tool approval requests to
// the full stream.
func TestR87cstreambApprovalFuncFullStream(t *testing.T) {
	_, parts := r87cstreambApprovalFuncRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},
		`+r87cstreambFuncCall1+`,`+r87cstreambFuncRequest+`,`+r87cstreambFuncCall2+`,`+r87cstreambFuncResult2+`,
		{"finishReason":"tool-calls","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"finishReason":"tool-calls","totalUsage":`+r87cstreambUsageJSON+`,"type":"finish"}]`)
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFinishStep {
			if _, ok := p.Performance.ToolExecutionMs["call-2"]; !ok || len(p.Performance.ToolExecutionMs) != 1 {
				t.Fatalf("finish-step toolExecutionMs = %v, want call-2 only", p.Performance.ToolExecutionMs)
			}
		}
	}
}

// stream-text.test.ts 31171: tool execution approval > when a single tool has
// a user-defined approval function > should only execute 1 step when the
// tool needs approval.
func TestR87cstreambApprovalFuncOneStep(t *testing.T) {
	r, _ := r87cstreambApprovalFuncRun(t)
	if n := len(r.Steps()); n != 1 {
		t.Fatalf("steps = %d, want 1", n)
	}
}

// stream-text.test.ts 31175: tool execution approval > when a single tool has
// a user-defined approval function > should have tool-calls finish reason.
func TestR87cstreambApprovalFuncFinishReason(t *testing.T) {
	r, _ := r87cstreambApprovalFuncRun(t)
	if fr := r.FinishReason(); fr != types.FinishReasonToolCalls {
		t.Fatalf("finishReason = %q, want tool-calls", fr)
	}
}

// stream-text.test.ts 31179: tool execution approval > when a single tool has
// a user-defined approval function > should add a tool approval request to
// the content.
func TestR87cstreambApprovalFuncContent(t *testing.T) {
	r, _ := r87cstreambApprovalFuncRun(t)
	r87cstreambAssert(t, "content", r87cstreambContent(t, r.Content()), `[`+r87cstreambFuncCall1+`,`+r87cstreambFuncRequest+`,`+r87cstreambFuncCall2+`,`+r87cstreambFuncResult2+`]`)
}

// stream-text.test.ts 31233: tool execution approval > when a single tool has
// a user-defined approval function > should add tool approval requests to
// the response messages.
func TestR87cstreambApprovalFuncResponseMessages(t *testing.T) {
	r, _ := r87cstreambApprovalFuncRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), `[
		{"content":[`+r87cstreambFuncCall1+`,{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},`+r87cstreambFuncCall2+`],"role":"assistant"},
		{"content":[{"output":{"type":"text","value":"result for value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-result"}],"role":"tool"}]`)
}

// r87cstreambSandbox stands in for the TS test sandbox session.
type r87cstreambSandbox struct{ description string }

// r87cstreambApprovedRun is the TS 'when a call from a single tool that needs
// approval is approved' beforeEach (an approved approval response in the
// input messages).
func r87cstreambApprovedRun(t *testing.T) (*StreamTextResult, *r87cstreambExecRec, []interface{}, []provider.StreamChunk, *r87cstreambSandbox) {
	t.Helper()
	var mu sync.Mutex
	var prompts []interface{}
	exec := &r87cstreambExecRec{}
	sandbox := &r87cstreambSandbox{description: "test sandbox"}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model:               r87cstreambPromptModel(t, &prompts, &mu, r87cstreambHelloStep),
		Tools:               []types.Tool{exec.tool(t, func(map[string]interface{}) interface{} { return "result1" })},
		ToolApproval:        map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		ExperimentalSandbox: sandbox,
		StopWhen:            []StopCondition{StepCountIs(3)},
		Internal:            r87cstreambIDs(),
		Messages:            tsApprovalHistory(true),
	})
	mu.Lock()
	defer mu.Unlock()
	return r, exec, append([]interface{}(nil), prompts...), parts, sandbox
}

// stream-text.test.ts 31421: tool execution approval > when a call from a
// single tool that needs approval is approved > should execute the tool.
func TestR87cstreambApprovedExecutes(t *testing.T) {
	_, exec, _, _, sandbox := r87cstreambApprovedRun(t)
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.calls) != 1 || exec.calls[0]["toolCallId"] != "call-1" || exec.calls[0]["messages"] != true || exec.calls[0]["sandbox"] != sandbox ||
		!reflect.DeepEqual(exec.calls[0]["input"], map[string]interface{}{"value": "value"}) {
		t.Fatalf("execute calls = %v", exec.calls)
	}
}

// stream-text.test.ts 31441: tool execution approval > when a call from a
// single tool that needs approval is approved > should call the model with a
// prompt that includes the tool result.
func TestR87cstreambApprovedPrompt(t *testing.T) {
	_, _, prompts, _, _ := r87cstreambApprovedRun(t)
	r87cstreambAssert(t, "prompts", prompts, `[[`+r87cstreambUserPrompt+`,{"content":[`+r87cstreambApprovalCall+`],"role":"assistant"},{"content":[`+r87cstreambToolResult1+`],"role":"tool"}]]`)
}

// stream-text.test.ts 31492: tool execution approval > when a call from a
// single tool that needs approval is approved > should include the tool
// result in the response messages.
func TestR87cstreambApprovedResponseMessages(t *testing.T) {
	r, _, _, _, _ := r87cstreambApprovedRun(t)
	r87cstreambAssert(t, "responseMessages", r87cstreambMessages(t, r.ResponseMessages()), `[{"content":[`+r87cstreambToolResult1+`],"role":"tool"},{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)
}

// stream-text.test.ts 31617: tool execution approval > when a call from a
// single tool that needs approval is approved > should include the tool
// result in the full stream.
func TestR87cstreambApprovedFullStream(t *testing.T) {
	_, _, _, parts, _ := r87cstreambApprovedRun(t)
	r87cstreambAssert(t, "full stream", r87cstreambParts(parts), `[{"type":"start"},
		{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},
		{"request":{},"type":"start-step","warnings":[]},
		{"id":"1","type":"text-start"},{"id":"1","text":"Hello, world!","type":"text-delta"},{"id":"1","type":"text-end"},
		{"finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"<now>"},"type":"finish-step","usage":`+r87cstreambUsageJSON+`},
		{"finishReason":"stop","rawFinishReason":"stop","totalUsage":`+r87cstreambUsageJSON+`,"type":"finish"}]`)
}

// stream-text.test.ts 33223: prepareStep with model switch and image URLs >
// should use the prepareStep model supportedUrls for download decision
// (TS 3fab50aa9b line 33225).
func TestR87cstreambPrepareStepModelSupportedURLs(t *testing.T) {
	var mu sync.Mutex
	var calls []map[string]interface{}
	var downloads []DownloadRequest
	textModel := func(name string, supported map[string][]string) *supportedURLMockModel {
		return &supportedURLMockModel{supported: supported, MockLanguageModel: &testutil.MockLanguageModel{ProviderName: name, ModelName: name,
			DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
				mu.Lock()
				calls = append(calls, r87cstreambCallOptions(t, o))
				mu.Unlock()
				return testutil.NewMockTextStream(r87cstreambText("1", "response from "+name)), nil
			}}}
	}
	with := textModel("with-image-url-support", map[string][]string{"image/*": {`^https?://.*$`}})
	without := textModel("without-image-url-support", map[string][]string{})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: with,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "Describe this image"}, types.ImageContent{URL: "https://example.com/test.jpg"}}}},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{Model: without}
		},
		ExperimentalDownload: func(_ context.Context, reqs []DownloadRequest) ([]*DownloadResult, error) {
			mu.Lock()
			defer mu.Unlock()
			downloads = append(downloads, reqs...)
			out := make([]*DownloadResult, len(reqs))
			for i, d := range reqs {
				if !d.IsURLSupportedByModel {
					out[i] = &DownloadResult{Data: []byte{1, 2, 3, 4}, MediaType: "image/png"}
				}
			}
			return out, nil
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
	r87cstreambAssert(t, "downloads", downloads, `[{"IsURLSupportedByModel":false,"URL":"https://example.com/test.jpg"}]`)
	r87cstreambAssert(t, "languageModelCalls", calls, `[{"includeRawChunks":false,"prompt":[{"content":[{"text":"Describe this image","type":"text"},
		{"data":{"data":"AQIDBA==","type":"data"},"mediaType":"image/png","type":"file"}],"role":"user"}],"toolChoice":{"type":"auto"}}]`)
	if got := r.Text(); got != "response from without-image-url-support" {
		t.Fatalf("text = %q", got)
	}
}

// r87cstreambIntegration is a TS telemetry integration object: each set
// listener records its name (or panics, the Go form of a throwing listener).
type r87cstreambIntegration struct {
	telemetry.NoopTelemetryIntegration
	rec      func(string)
	names    map[string]string
	panicked bool
}

func (i *r87cstreambIntegration) fire(event string) {
	name, ok := i.names[event]
	if !ok {
		return
	}
	if i.panicked {
		panic("integration error")
	}
	i.rec(name)
}

func (i *r87cstreambIntegration) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	i.fire("onStart")
	return ctx
}

func (i *r87cstreambIntegration) OnStepStart(ctx context.Context, _ telemetry.TelemetryStepStartEvent) context.Context {
	i.fire("onStepStart")
	return ctx
}

func (i *r87cstreambIntegration) OnToolExecutionStart(ctx context.Context, _ telemetry.TelemetryToolCallStartEvent) context.Context {
	i.fire("onToolExecutionStart")
	return ctx
}

func (i *r87cstreambIntegration) OnToolExecutionEnd(context.Context, telemetry.TelemetryToolCallFinishEvent) {
	i.fire("onToolExecutionEnd")
}

func (i *r87cstreambIntegration) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	i.fire("onStepEnd")
}

func (i *r87cstreambIntegration) OnFinish(context.Context, telemetry.TelemetryFinishEvent) {
	i.fire("onEnd")
}

// r87cstreambEvents is a concurrency-safe event log.
type r87cstreambEvents struct {
	mu   sync.Mutex
	list []string
}

func (e *r87cstreambEvents) add(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, s)
}

func (e *r87cstreambEvents) get() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.list...)
}

// r87cstreambNames maps TS listener names to the recorded event names.
func r87cstreambNames(pairs ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}

// r87cstreambConsume runs StreamText to the end and returns the result.
func r87cstreambConsume(t *testing.T, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	return r
}

// r87cstreambGlobalIntegrations registers global integrations (TS
// globalThis.AI_SDK_TELEMETRY_INTEGRATIONS) until the test ends.
func r87cstreambGlobalIntegrations(t *testing.T, integrations ...telemetry.TelemetryIntegration) {
	t.Helper()
	telemetry.RegisterTelemetryIntegration(integrations[0], integrations[1:]...)
	t.Cleanup(func() { telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{}) })
}

// stream-text.test.ts 33376: telemetry integrations > should call per-call
// integration listeners for all lifecycle events (3fab50aa9b line 33378).
func TestR87cstreambTelemetryPerCallLifecycle(t *testing.T) {
	var events r87cstreambEvents
	in := &r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "onStart", "onStepStart", "onStepStart",
		"onToolExecutionStart", "onToolExecutionStart", "onToolExecutionEnd", "onToolExecutionEnd", "onStepEnd", "onStepEnd", "onEnd", "onEnd")}
	r87cstreambConsume(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{r87cstreambMeta("id-0"), r87cstreambCall("call-1", "testTool", `{ "value": "test" }`),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r87cstreambUsage()}}),
		Tools: []types.Tool{{Name: "testTool", Parameters: r87cstreambZodValueSchema, Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return in["value"].(string) + "-result", nil
		}}},
		Prompt:    "test-input",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{in}},
	})
	if got, want := events.get(), []string{"onStart", "onStepStart", "onToolExecutionStart", "onToolExecutionEnd", "onStepEnd", "onEnd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// stream-text.test.ts 33449: telemetry integrations > should call globally
// registered integration listeners (3fab50aa9b line 33451).
func TestR87cstreambTelemetryGlobalIntegrations(t *testing.T) {
	var events r87cstreambEvents
	r87cstreambGlobalIntegrations(t, &r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "global-onStart", "onStepEnd", "global-onStepEnd", "onEnd", "global-onEnd")})
	r87cstreambConsume(t, StreamTextOptions{Model: r71streamModel(nil, r87cstreambDefaultStream()), Prompt: "test-input"})
	if got, want := events.get(), []string{"global-onStart", "global-onStepEnd", "global-onEnd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// stream-text.test.ts 33481: telemetry integrations > should only call
// per-call integrations when both global and per-call integrations are
// provided (3fab50aa9b line 33483).
func TestR87cstreambTelemetryPerCallReplacesGlobal(t *testing.T) {
	var events r87cstreambEvents
	r87cstreambGlobalIntegrations(t, &r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "global")})
	r87cstreambConsume(t, StreamTextOptions{Model: r71streamModel(nil, r87cstreambDefaultStream()), Prompt: "test-input",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "per-call")}}}})
	if got, want := events.get(), []string{"per-call"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// stream-text.test.ts 33510: telemetry integrations > should call integration
// listeners alongside user callbacks (3fab50aa9b line 33512).
func TestR87cstreambTelemetryAlongsideUserCallbacks(t *testing.T) {
	var events r87cstreambEvents
	r87cstreambConsume(t, StreamTextOptions{
		Model: r71streamModel(nil, r87cstreambDefaultStream()), Prompt: "test-input",
		OnStart:           func(context.Context, OnStartEvent) { events.add("user-onStart") },
		OnStepFinishEvent: func(context.Context, OnStepFinishEvent) { events.add("user-onStepFinish") },
		OnFinishEvent:     func(context.Context, OnFinishEvent) { events.add("user-onFinish") },
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r87cstreambIntegration{rec: events.add,
			names: r87cstreambNames("onStart", "integration-onStart", "onStepEnd", "integration-onStepEnd", "onEnd", "integration-onEnd")}}},
	})
	// TS has user-onStart first; Go starts the telemetry root before the user
	// OnStart (known difference "Telemetry start before the start
	// callbacks", as in the generateText port). Step end and end run the user
	// callback first, as TS.
	if got, want := events.get(), []string{"integration-onStart", "user-onStart", "user-onStepFinish", "integration-onStepEnd", "user-onFinish", "integration-onEnd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// stream-text.test.ts 33553: telemetry integrations > should not break
// streaming when an integration listener throws (3fab50aa9b line 33555; a
// Go listener panics).
func TestR87cstreambTelemetryThrowingIntegration(t *testing.T) {
	r := r87cstreambConsume(t, StreamTextOptions{
		Model: r71streamModel(nil, r87cstreambDefaultStream()), Prompt: "test-input",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r87cstreambIntegration{panicked: true, rec: func(string) {},
			names: r87cstreambNames("onStart", "x", "onStepEnd", "x", "onEnd", "x")}}},
	})
	if got := r.Text(); got != "Hello, world!" {
		t.Fatalf("text = %q, want Hello, world!", got)
	}
}

// stream-text.test.ts 33578: telemetry integrations > should support
// multiple per-call integrations as an array (3fab50aa9b line 33580).
func TestR87cstreambTelemetryMultiplePerCall(t *testing.T) {
	var events r87cstreambEvents
	r87cstreambConsume(t, StreamTextOptions{
		Model: r71streamModel(nil, r87cstreambDefaultStream()), Prompt: "test-input",
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
			&r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "first")},
			&r87cstreambIntegration{rec: events.add, names: r87cstreambNames("onStart", "second")}}},
	})
	if got, want := events.get(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// ui-message-stream / ui partial rows (round 87c streamb). SSE event JSON is
// compared with sorted keys: known difference "UI message stream JSON key
// order" (UIMessageChunk is a map); the values are the TS ones.
// ---------------------------------------------------------------------------

// r87cstreambClosingStream is a provider stream that blocks until it is
// closed and records the close (the Go form of a TS source stream's cancel).
type r87cstreambClosingStream struct {
	once   sync.Once
	closed chan struct{}
}

func newR87cstreambClosingStream() *r87cstreambClosingStream {
	return &r87cstreambClosingStream{closed: make(chan struct{})}
}

func (s *r87cstreambClosingStream) Next() (*provider.StreamChunk, error) {
	<-s.closed
	return nil, context.Canceled
}
func (s *r87cstreambClosingStream) Err() error { return nil }
func (s *r87cstreambClosingStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// r87cstreambReadN reads from r until it has n bytes or times out.
func r87cstreambReadN(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		ch <- res{b, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read %d bytes: %v", n, got.err)
		}
		return string(got.b)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out reading %d bytes", n)
	}
	return ""
}

// create-sse-stream-with-keep-alive.test.ts 17: createSseStreamWithKeepAlive
// > should pass through the original stream when keepAliveMs is undefined.
// TS returns the same stream object; Go has no SSE string stream to return,
// so the pass-through is checked on the output: without KeepAliveMs the SSE
// body is byte-identical to the one without any init (no comments added).
func TestR87cstreambKeepAliveUndefinedPassesThrough(t *testing.T) {
	body := func(init *UIMessageStreamResponseInit) string {
		ch := make(chan UIMessageChunk, 1)
		ch <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": "data"}
		close(ch)
		resp, err := CreateUIMessageChunksResponse(ch, init)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	want := "data: {\"delta\":\"data\",\"id\":\"1\",\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"
	if got := body(&UIMessageStreamResponseInit{KeepAliveMs: nil}); got != want {
		t.Fatalf("body without keepAliveMs = %q, want %q", got, want)
	}
	if got := body(nil); got != want {
		t.Fatalf("body without init = %q, want %q", got, want)
	}
}

// create-sse-stream-with-keep-alive.test.ts 138: createSseStreamWithKeepAlive
// > should cancel the source stream. Go: a response is cancelled through the
// request ctx that drives both StreamText and the SSE writer (known
// difference "StreamText reads the provider stream without a reader": pass
// r.Context()); cancelling it after the opening comment closes the source
// model stream and ends the body. TS also forwards the cancel reason; a Go
// stream's Close takes none.
func TestR87cstreambKeepAliveCancelsSource(t *testing.T) {
	src := newR87cstreambClosingStream()
	opened := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := StreamText(ctx, StreamTextOptions{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			close(opened)
			return src, nil
		}},
		Prompt:  "test-input",
		OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	keepAlive := 100 * time.Millisecond
	resp, err := CreateUIMessageStreamResponseWithInit(ctx, r, &UIMessageStreamResponseInit{KeepAliveMs: &keepAlive})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := r87cstreambReadN(t, resp.Body, len(sseStreamOpenComment)); got != sseStreamOpenComment {
		t.Fatalf("first read = %q, want the opening comment", got)
	}
	select {
	case <-opened: // the source exists before the response is cancelled, as in TS
	case <-time.After(5 * time.Second):
		t.Fatal("the model stream was not opened")
	}
	cancel()
	select {
	case <-src.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the response did not cancel the source stream")
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.ReadAll(resp.Body)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the response body did not end after the cancel")
	}
}

// create-ui-message-stream-response.test.ts 65: createUIMessageStreamResponse
// > should send opening and keep-alive comments for an idle stream.
func TestR87cstreambResponseKeepAliveIdle(t *testing.T) {
	keepAlive := 100 * time.Millisecond
	idle := make(chan UIMessageChunk)
	resp, err := CreateUIMessageChunksResponse(idle, &UIMessageStreamResponseInit{KeepAliveMs: &keepAlive})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := r87cstreambReadN(t, resp.Body, len(sseStreamOpenComment)); got != ": stream-open\n\n" {
		t.Fatalf("first read = %q", got)
	}
	start := time.Now()
	if got := r87cstreambReadN(t, resp.Body, len(sseKeepAliveComment)); got != ": keep-alive\n\n" {
		t.Fatalf("second read = %q", got)
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("keep-alive came after %v, want about 100ms", d)
	}
}

// create-ui-message-stream-response.test.ts 90: createUIMessageStreamResponse
// > can respond with a stream created by toUIMessageStream.
func TestR87cstreambResponseFromToUIMessageStream(t *testing.T) {
	chunks, _ := ToUIMessageStream(context.Background(), testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStart},
		{Type: provider.ChunkTypeTextStart, ID: "t1"},
		{Type: provider.ChunkTypeText, ID: "t1", Text: "Hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "t1"},
	}), UIMessageStreamResultOptions{GenerateMessageID: func() string { return "msg-123" }})
	resp, err := CreateUIMessageChunksResponse(chunks, &UIMessageStreamResponseInit{Status: 200})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := "data: {\"messageId\":\"msg-123\",\"type\":\"start\"}\n\n" +
		"data: {\"id\":\"t1\",\"type\":\"text-start\"}\n\n" +
		"data: {\"delta\":\"Hello\",\"id\":\"t1\",\"type\":\"text-delta\"}\n\n" +
		"data: {\"id\":\"t1\",\"type\":\"text-end\"}\n\n" +
		"data: [DONE]\n\n"
	if string(b) != want || resp.StatusCode != 200 {
		t.Fatalf("status %d body = %q, want %q", resp.StatusCode, b, want)
	}
}

// create-ui-message-stream-response.test.ts 153: createUIMessageStreamResponse
// > should call consumeSseStream with a teed stream.
func TestR87cstreambResponseConsumeSSEStreamTee(t *testing.T) {
	var calls int
	consumed := make(chan string, 1)
	ch := make(chan UIMessageChunk, 2)
	ch <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": "test-data-1"}
	ch <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": "test-data-2"}
	close(ch)
	resp, err := CreateUIMessageChunksResponse(ch, &UIMessageStreamResponseInit{Status: 200, ConsumeSSEStream: func(r io.Reader) error {
		calls++
		b, err := io.ReadAll(r)
		consumed <- string(b)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := "data: {\"delta\":\"test-data-1\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n" +
		"data: {\"delta\":\"test-data-2\",\"id\":\"1\",\"type\":\"text-delta\"}\n\n" +
		"data: [DONE]\n\n"
	if string(b) != want {
		t.Fatalf("response body = %q, want %q", b, want)
	}
	select {
	case got := <-consumed:
		if got != want || calls != 1 {
			t.Fatalf("consumeSseStream calls %d data %q, want 1 and %q", calls, got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consumeSseStream did not finish")
	}
}

// create-ui-message-stream.test.ts 422: createUIMessageStream > should
// support writing from delayed merged streams.
func TestR87cstreambDelayedMergedStreams(t *testing.T) {
	ch1 := make(chan UIMessageChunk)
	executed := make(chan UIMessageStreamWriter, 1)
	out, _ := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{Execute: func(w UIMessageStreamWriter) {
		w.Merge(ch1)
		executed <- w
	}})
	pull := func() UIMessageChunk {
		select {
		case c := <-out:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("pull timed out")
		}
		return nil
	}
	// The execute function is finished.
	var writer UIMessageStreamWriter
	select {
	case writer = <-executed:
	case <-time.After(5 * time.Second):
		t.Fatal("execute did not finish")
	}
	var result []UIMessageChunk
	ch1 <- UIMessageChunk{"type": "text-delta", "id": "1", "delta": "1a"}
	result = append(result, pull())
	ch2 := make(chan UIMessageChunk)
	writer.Merge(ch2)
	close(ch1)
	time.Sleep(10 * time.Millisecond) // relinquish control
	ch2 <- UIMessageChunk{"type": "text-delta", "id": "2", "delta": "2a"}
	close(ch2)
	result = append(result, pull())
	r87cstreambAssert(t, "result", result, `[{"delta":"1a","id":"1","type":"text-delta"},{"delta":"2a","id":"2","type":"text-delta"}]`)
}

// get-response-ui-message-id.test.ts 16 / 28 / 48: getResponseUIMessageId >
// should return the last assistant message id when present, should generate
// new id when last message is not from assistant, should use the
// responseMessageId when it is a string. Go's getResponseUIMessageId is
// responseUIMessageIDOK: TS's responseMessageId generator is
// GenerateMessageID and a string responseMessageId is ResponseMessageID.
func TestR87cstreambGetResponseUIMessageID(t *testing.T) {
	gen := func() string { return "new-id" }
	msg := func(id, role string) UIMessageChunk {
		return UIMessageChunk{"id": id, "role": role, "parts": []interface{}{}}
	}
	cases := []struct {
		name string
		opts UIMessageStreamResultOptions
		want string
	}{
		{"last assistant (TS 16)", UIMessageStreamResultOptions{OriginalMessages: []UIMessageChunk{msg("msg-1", "user"), msg("msg-2", "assistant")}, GenerateMessageID: gen}, "msg-2"},
		{"last not assistant (TS 28)", UIMessageStreamResultOptions{OriginalMessages: []UIMessageChunk{msg("msg-1", "assistant"), msg("msg-2", "user")}, GenerateMessageID: gen}, "new-id"},
		{"string id (TS 48)", UIMessageStreamResultOptions{OriginalMessages: []UIMessageChunk{}, ResponseMessageID: "response-id"}, "response-id"},
	}
	for _, c := range cases {
		if got, ok := responseUIMessageIDOK(c.opts); !ok || got != c.want {
			t.Errorf("%s: got %q (%v), want %q", c.name, got, ok, c.want)
		}
	}
}

// read-ui-message-stream.test.ts 665: readUIMessageStream > should cancel the
// input stream when iteration exits early. Go: the caller stops iterating by
// cancelling ctx; ReadUIMessages then closes its output and reads nothing
// more from the input channel (a channel has no cancel; its producer stops
// on the same ctx).
func TestR87cstreambReadUIMessagesEarlyExitStopsInput(t *testing.T) {
	in := make(chan UIMessageChunk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, errs := ReadUIMessages(ctx, ReadUIMessagesOptions{Stream: in})
	go func() {
		for _, c := range []UIMessageChunk{{"type": "start", "messageId": "msg-123"}, {"type": "text-start", "id": "text-1"}, {"type": "text-delta", "id": "text-1", "delta": "Hello"}} {
			select {
			case in <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	for m := range out {
		hasText := false
		for _, p := range m.Parts {
			if txt := uiPartText(p); txt != "" {
				hasText = true
			}
		}
		if hasText {
			cancel()
			break
		}
	}
	select {
	case _, ok := <-errs:
		if ok {
			t.Fatal("unexpected error after an early exit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadUIMessages did not stop after the early exit")
	}
	select {
	case in <- UIMessageChunk{"type": "text-delta", "id": "text-1", "delta": " more"}:
		t.Fatal("ReadUIMessages still reads the input after the early exit")
	case <-time.After(50 * time.Millisecond):
	}
}
