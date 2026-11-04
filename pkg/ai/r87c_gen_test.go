package ai

// Round 87c gen: ports of the generate-text.test.ts rows that Appendix A of
// ts-test-port-coverage.md lists as partial. Each test names the TS case
// (`generate-text.test.ts <line>`, the Appendix A row line) and makes the TS
// assertions on the Go result.
//
// Translations used throughout (Go idiom, not behavior differences):
//   - A TS mock tool-call content part is a Go provider result's
//     ToolCallContent plus the matching GenerateResult.ToolCalls entry (the Go
//     provider contract reports tool calls in ToolCalls).
//   - TS tests run on fake timers, so every duration is 0 and a response
//     without a timestamp gets new Date(0). Go measures real time and stamps
//     time.Now(): the step views zero the durations and map a current
//     timestamp to the epoch.
//   - TS snapshots print class instances' own fields with `undefined` values;
//     the views drop absent values on both sides. TS DefaultGeneratedFile's
//     lazily decoded uint8ArrayData is not compared (base64Data is).
//   - Go's ToolCall / ToolResult structs keep their v0.5.2 JSON keys (id,
//     arguments, result); the views use the TS names (toolCallId, input,
//     output). A tool result's Title is dropped (known difference "Executed
//     tool results carry the tool title"); a nil RuntimeContext is {} (known
//     difference "RuntimeContext defaults to nil").
//   - A TS prompt string message ({role:'user', content:'test-input'}) is a
//     Go user message with one text part.
//   - A TS zod inputSchema is the equivalent JSON Schema map.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

type r87cgenObj = map[string]interface{}

// r87cgenUsage is the TS LanguageModelV4Usage with total / noCache input and
// total / text output tokens (TS testUsage is r87cgenUsage(3, 10)).
func r87cgenUsage(in, out int64) types.Usage {
	return types.Usage{InputTokens: &in, OutputTokens: &out, InputDetails: &types.InputTokenDetails{NoCacheTokens: &in}, OutputDetails: &types.OutputTokenDetails{TextTokens: &out}}
}

// r87cgenModel is TS new MockLanguageModelV4({doGenerate}) (provider
// mock-provider, model mock-model-id); gen gets the 0-based call number.
func r87cgenModel(gen func(n int, o *provider.GenerateOptions) (*types.GenerateResult, error)) *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{ModelName: "mock-model-id", ProviderName: "mock-provider", DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		return gen(i, o)
	}}
}

// r87cgenResult is TS {...dummyResponseValues, content}: finish reason stop
// (raw "stop"), testUsage, no warnings. Tool-call parts are also reported in
// ToolCalls.
func r87cgenResult(parts ...types.ContentPart) *types.GenerateResult {
	r := &types.GenerateResult{Content: parts, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r87cgenUsage(3, 10)}
	for _, p := range parts {
		if c, ok := p.(types.ToolCallContent); ok {
			r.ToolCalls = append(r.ToolCalls, types.ToolCall{ID: c.ToolCallID, ToolName: c.ToolName, RawArguments: c.Input, ProviderExecuted: c.ProviderExecuted, ProviderExecutedSet: c.ProviderExecuted})
		}
	}
	return r
}

// r87cgenToolCalls is r87cgenResult with finish reason tool-calls (raw
// undefined).
func r87cgenToolCalls(parts ...types.ContentPart) *types.GenerateResult {
	r := r87cgenResult(parts...)
	r.FinishReason, r.RawFinishReason = types.FinishReasonToolCalls, ""
	return r
}

func r87cgenCall(id, name, input string) types.ToolCallContent {
	return types.ToolCallContent{ToolCallID: id, ToolName: name, Input: input}
}

func r87cgenText(text string) types.TextContent { return types.TextContent{Text: text} }

// r87cgenSchema is the JSON Schema zod z.object({...string fields}) produces.
func r87cgenSchema(fields ...string) map[string]interface{} {
	props := map[string]interface{}{}
	required := []interface{}{}
	for _, f := range fields {
		props[f] = map[string]interface{}{"type": "string"}
		required = append(required, f)
	}
	return map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "properties": props, "required": required, "type": "object"}
}

func r87cgenTool(name string, execute func(in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error), fields ...string) types.Tool {
	t := types.Tool{Name: name, Parameters: r87cgenSchema(fields...)}
	if execute != nil {
		t.Execute = func(_ context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			return execute(in, o)
		}
	}
	return t
}

// r87cgenIDs is TS _internal {generateId: mockId({prefix: 'id'}),
// generateCallId: () => 'test-telemetry-call-id'}.
func r87cgenIDs() *InternalOptions {
	return &InternalOptions{GenerateID: r82genMockID(), GenerateCallID: func() string { return "test-telemetry-call-id" }}
}

func r87cgenWant(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	return v
}

func r87cgenRaw(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func r87cgenCheck(t *testing.T, name string, got interface{}, want string) {
	t.Helper()
	r71genCheckJSON(t, name, got, r87cgenWant(t, want))
}

// r87cgenToolCallView maps a Go ToolCall's JSON to the TS tool-call shape.
func r87cgenToolCallView(m r87cgenObj) r87cgenObj {
	out := r87cgenObj{"type": "tool-call", "toolCallId": m["id"], "toolName": m["toolName"], "input": m["arguments"]}
	if out["input"] == nil {
		out["input"] = r87cgenObj{}
	}
	for _, k := range []string{"title", "providerExecuted", "providerMetadata", "dynamic", "invalid", "error"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// r87cgenToolResultView maps a Go ToolResult's JSON to the TS tool-result
// shape (the title a local result carries is dropped, see the header).
func r87cgenToolResultView(m r87cgenObj) r87cgenObj {
	out := r87cgenObj{"type": "tool-result", "toolCallId": m["toolCallId"], "toolName": m["toolName"], "input": m["input"], "output": m["result"], "dynamic": false}
	if out["input"] == nil {
		out["input"] = r87cgenObj{}
	}
	for _, k := range []string{"dynamic", "providerExecuted", "providerMetadata", "preliminary"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// r87cgenFileView is TS DefaultGeneratedFile {base64Data, mediaType,
// providerMetadata} of a Go generated file's JSON.
func r87cgenFileView(m r87cgenObj) r87cgenObj {
	out := r87cgenObj{"mediaType": m["mediaType"]}
	if d, ok := m["data"].(r87cgenObj); ok && d["type"] == "data" {
		out["base64Data"] = d["data"]
	}
	if v, ok := m["providerMetadata"]; ok {
		out["providerMetadata"] = v
	}
	return out
}

// r87cgenPartView maps one Go content part's JSON to the TS content shape.
func r87cgenPartView(m r87cgenObj) r87cgenObj {
	switch m["type"] {
	case "file":
		return r87cgenObj{"type": "file", "file": r87cgenFileView(m)}
	case "tool-result":
		out := r87cgenObj{}
		for k, v := range m {
			out[k] = v
		}
		out["output"] = m["result"]
		delete(out, "result")
		if _, ok := out["dynamic"]; !ok {
			out["dynamic"] = false
		}
		return out
	}
	return m
}

func r87cgenContentView(t *testing.T, parts []types.ContentPart) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, p := range parts {
		out = append(out, r87cgenPartView(r87cgenRaw(t, p).(r87cgenObj)))
	}
	return out
}

// r87cgenMessagesView is the JSON of model messages without Go's legacy
// tool_calls mirror and without the empty fileData tag of a legacy file part.
func r87cgenMessagesView(t *testing.T, msgs []types.Message) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, m := range r87cgenRaw(t, msgs).([]interface{}) {
		mm := m.(r87cgenObj)
		delete(mm, "tool_calls")
		if parts, ok := mm["content"].([]interface{}); ok {
			for _, p := range parts {
				pm, ok := p.(r87cgenObj)
				if !ok {
					continue
				}
				if fd, ok := pm["fileData"].(r87cgenObj); ok && fd["type"] == "" {
					delete(pm, "fileData")
				}
			}
		}
		out = append(out, mm)
	}
	return out
}

func r87cgenTimestamp(v interface{}) interface{} {
	s, _ := v.(string)
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return v
	}
	if ts.Year() > 2000 {
		// The current time of a response without a timestamp (TS fake
		// timers: the epoch).
		ts = time.Unix(0, 0)
	}
	return ts.UTC().Format("2006-01-02T15:04:05.000Z")
}

// r87cgenStepView is the TS DefaultStepResult snapshot of a Go step.
func r87cgenStepView(t *testing.T, s types.StepResult) r87cgenObj {
	t.Helper()
	raw := r87cgenRaw(t, s).(r87cgenObj)
	out := r87cgenObj{}
	for _, k := range []string{"callId", "finishReason", "model", "providerMetadata", "rawFinishReason", "stepNumber", "usage"} {
		if v, ok := raw[k]; ok {
			out[k] = v
		}
	}
	out["content"] = r87cgenContentView(t, s.Content)
	perf := raw["performance"].(r87cgenObj)
	for k, v := range perf {
		switch val := v.(type) {
		case float64:
			perf[k] = float64(0)
		case r87cgenObj:
			for kk := range val {
				val[kk] = float64(0)
			}
		}
	}
	out["performance"] = perf
	req := r87cgenObj{}
	if s.Request.Body != nil {
		req["body"] = r87cgenRaw(t, s.Request.Body)
	}
	if s.Request.Messages != nil {
		req["messages"] = r87cgenMessagesView(t, s.Request.Messages)
	}
	out["request"] = req
	resp := raw["response"].(r87cgenObj)
	resp["timestamp"] = r87cgenTimestamp(resp["timestamp"])
	if s.Response.Messages != nil {
		resp["messages"] = r87cgenMessagesView(t, s.Response.Messages)
	}
	out["response"] = resp
	out["runtimeContext"] = r87cgenObj{}
	if s.RuntimeContext != nil {
		out["runtimeContext"] = r87cgenRaw(t, s.RuntimeContext)
	}
	out["toolsContext"] = r87cgenObj{}
	if s.ToolsContext != nil {
		out["toolsContext"] = r87cgenRaw(t, s.ToolsContext)
	}
	out["warnings"] = []interface{}{}
	if len(s.Warnings) > 0 {
		out["warnings"] = r87cgenRaw(t, s.Warnings)
	}
	return out
}

func r87cgenStepsView(t *testing.T, steps []types.StepResult) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, s := range steps {
		out = append(out, r87cgenStepView(t, s))
	}
	return out
}

func r87cgenToolCallsView(t *testing.T, calls []types.ToolCall) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, c := range calls {
		out = append(out, r87cgenToolCallView(r87cgenRaw(t, c).(r87cgenObj)))
	}
	return out
}

func r87cgenToolResultsView(t *testing.T, results []types.ToolResult) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, r := range results {
		out = append(out, r87cgenToolResultView(r87cgenRaw(t, r).(r87cgenObj)))
	}
	return out
}

func r87cgenFilesView(t *testing.T, files []types.GeneratedFileContent) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, f := range files {
		out = append(out, r87cgenFileView(r87cgenRaw(t, f).(r87cgenObj)))
	}
	return out
}

// TS modelWithSources / modelWithFiles / modelWithReasoning.
func r87cgenModelWithSources() *testutil.MockLanguageModel {
	return r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenResult(r87cgenText("Hello, world!"),
			types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value"}}`)},
			types.SourceContent{SourceType: "url", ID: "456", URL: "https://example.com/2", Title: "Example 2", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value2"}}`)},
		), nil
	})
}

func r87cgenModelWithFiles() *testutil.MockLanguageModel {
	return r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenResult(r87cgenText("Hello, world!"),
			types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}},
			types.GeneratedFileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "QkFVRw=="}},
		), nil
	})
}

func r87cgenModelWithReasoning() *testutil.MockLanguageModel {
	return r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenResult(
			types.ReasoningContent{Text: "I will open the conversation with witty banter.", ProviderMetadata: json.RawMessage(`{"testProvider":{"signature":"signature"}}`)},
			types.ReasoningContent{Text: "", ProviderMetadata: json.RawMessage(`{"testProvider":{"redactedData":"redacted-reasoning-data"}}`)},
			r87cgenText("Hello, world!"),
		), nil
	})
}

var (
	_ = errors.New
	_ = reflect.DeepEqual
	_ = strings.Contains
)

// generate-text.test.ts 94 'abort signal handling' > 'should reject when the
// abort signal fires during tool execution': the tool aborts the call with a
// reason and observes it; GenerateText rejects with that reason after one
// model call, the model call end event carries the gateway metadata, and the
// end callback never runs.
func TestR87cgenAbortDuringToolExecution(t *testing.T) {
	abortErr := errors.New("tool execution aborted")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	calls := 0
	model := r87cgenModel(func(n int, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if n == 0 {
			r := r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`))
			r.ProviderMetadata = map[string]interface{}{"gateway": map[string]interface{}{"generationId": "generation-id"}}
			return r, nil
		}
		return &types.GenerateResult{FinishReason: types.FinishReasonOther, RawFinishReason: "unknown", Usage: r87cgenUsage(3, 10)}, nil
	})
	zero := 0
	var callEnds []LanguageModelCallEndEvent
	ended := false
	tool := types.Tool{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(toolCtx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		cancel(abortErr)
		<-toolCtx.Done()
		return nil, context.Cause(toolCtx)
	}}
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model: model, Tools: []types.Tool{tool}, Prompt: "test-input",
		StopWhen: []StopCondition{IsStepCount(10)}, MaxRetries: &zero,
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { callEnds = append(callEnds, e) },
		OnEndEvent:             func(context.Context, OnFinishEvent) { ended = true },
		OnEnd:                  func(context.Context, *GenerateTextResult, interface{}) { ended = true },
	})
	if err != abortErr {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if calls != 1 {
		t.Errorf("model calls = %d, want 1", calls)
	}
	if len(callEnds) != 1 || !reflect.DeepEqual(callEnds[0].ProviderMetadata, map[string]interface{}{"gateway": map[string]interface{}{"generationId": "generation-id"}}) {
		t.Errorf("model call end events = %+v", callEnds)
	}
	if ended {
		t.Error("onEnd was called")
	}
}

// generate-text.test.ts 1357 'result.reasoningText' > 'should contain
// reasoning string from model response'.
func TestR87cgenResultReasoningText(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r87cgenModelWithReasoning(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ReasoningText != "I will open the conversation with witty banter." {
		t.Fatalf("reasoningText = %q", res.ReasoningText)
	}
}

// generate-text.test.ts 1474 'result.sources' > 'should contain sources'
// (snapshot) and 1569 'result.files' > 'should contain files' (snapshot).
func TestR87cgenResultSourcesAndFiles(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r87cgenModelWithSources(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "sources", r87cgenRaw(t, res.Sources), `[{"id":"123","providerMetadata":{"provider":{"custom":"value"}},"sourceType":"url","title":"Example","type":"source","url":"https://example.com"},{"id":"456","providerMetadata":{"provider":{"custom":"value2"}},"sourceType":"url","title":"Example 2","type":"source","url":"https://example.com/2"}]`)

	res, err = GenerateText(context.Background(), GenerateTextOptions{Model: r87cgenModelWithFiles(), Prompt: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "files", r87cgenFilesView(t, res.Files), `[{"base64Data":"AQID","mediaType":"image/png"},{"base64Data":"QkFVRw==","mediaType":"image/jpeg"}]`)
}

// generate-text.test.ts 1764 'result.files' > 'should contain file content
// parts from all steps in result.content'.
func TestR87cgenFileContentFromAllSteps(t *testing.T) {
	model := r87cgenModel(func(n int, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		switch n {
		case 0:
			return r87cgenToolCalls(types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "c3RlcC0w"}}, r87cgenCall("call-1", "tool1", "{}")), nil
		case 1:
			return r87cgenResult(types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "c3RlcC0x"}}), nil
		}
		t.Fatalf("Unexpected response count: %d", n+1)
		return nil, nil
	})
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "prompt", StopWhen: []StopCondition{IsStepCount(3)},
		Tools: []types.Tool{r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil })},
	})
	if err != nil {
		t.Fatal(err)
	}
	var files []interface{}
	for _, p := range r87cgenContentView(t, res.Content) {
		if p.(r87cgenObj)["type"] == "file" {
			f := p.(r87cgenObj)["file"].(r87cgenObj)
			files = append(files, r87cgenObj{"base64": f["base64Data"], "mediaType": f["mediaType"]})
		}
	}
	r87cgenCheck(t, "file parts", files, `[{"base64":"c3RlcC0w","mediaType":"text/plain"},{"base64":"c3RlcC0x","mediaType":"text/plain"}]`)
	n := 0
	for _, p := range res.FinalStep.Content {
		if p.ContentType() == "file" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("final step file parts = %d, want 1", n)
	}
}

// generate-text.test.ts 1835 'result.steps' > 'should add the reasoning from
// the model response to the step result', 1848 'should contain sources' and
// 1861 'should contain files' (snapshots of result.steps).
func TestR87cgenResultStepsSnapshots(t *testing.T) {
	usage := `"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`
	common := `"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"runtimeContext":{},"stepNumber":0,"toolsContext":{},` + usage + `,"warnings":[]`
	for _, tc := range []struct {
		name  string
		model *testutil.MockLanguageModel
		want  string
	}{
		{"reasoning", r87cgenModelWithReasoning(), `[{"callId":"test-telemetry-call-id","content":[{"providerMetadata":{"testProvider":{"signature":"signature"}},"text":"I will open the conversation with witty banter.","type":"reasoning"},{"providerMetadata":{"testProvider":{"redactedData":"redacted-reasoning-data"}},"text":"","type":"reasoning"},{"text":"Hello, world!","type":"text"}],` + common + `,"response":{"id":"id-0","messages":[{"content":[{"providerOptions":{"testProvider":{"signature":"signature"}},"text":"I will open the conversation with witty banter.","type":"reasoning"},{"providerOptions":{"testProvider":{"redactedData":"redacted-reasoning-data"}},"text":"","type":"reasoning"},{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"}}]`},
		{"sources", r87cgenModelWithSources(), `[{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"},{"id":"123","providerMetadata":{"provider":{"custom":"value"}},"sourceType":"url","title":"Example","type":"source","url":"https://example.com"},{"id":"456","providerMetadata":{"provider":{"custom":"value2"}},"sourceType":"url","title":"Example 2","type":"source","url":"https://example.com/2"}],` + common + `,"response":{"id":"id-0","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"}}]`},
		{"files", r87cgenModelWithFiles(), `[{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"},{"file":{"base64Data":"AQID","mediaType":"image/png"},"type":"file"},{"file":{"base64Data":"QkFVRw==","mediaType":"image/jpeg"},"type":"file"}],` + common + `,"response":{"id":"id-0","messages":[{"content":[{"text":"Hello, world!","type":"text"},{"data":"AQID","mediaType":"image/png","type":"file"},{"data":"QkFVRw==","mediaType":"image/jpeg","type":"file"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := GenerateText(context.Background(), GenerateTextOptions{Model: tc.model, Prompt: "prompt", Internal: r87cgenIDs()})
			if err != nil {
				t.Fatal(err)
			}
			r87cgenCheck(t, "steps", r87cgenStepsView(t, res.Steps), tc.want)
		})
	}
}

// generate-text.test.ts 1874 'result.steps' > 'should expose the final
// step': finalStep is the last step.
func TestR87cgenExposeFinalStep(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenResult(r87cgenText("Hello!")), nil
		}),
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.FinalStep, res.Steps[len(res.Steps)-1]) {
		t.Fatalf("finalStep = %+v, want the last step", res.FinalStep)
	}
}

// r87cgenToolsView is the TS LanguageModelV4FunctionTool list
// ({type, name, inputSchema}) of the tools a model call received; nil (TS
// undefined) when the call has no tools.
func r87cgenToolsView(t *testing.T, tools []types.Tool) interface{} {
	t.Helper()
	if len(tools) == 0 {
		return nil
	}
	out := []interface{}{}
	for _, tool := range tools {
		typ := tool.Type
		if typ == "" {
			typ = types.ToolTypeFunction
		}
		out = append(out, r87cgenObj{"type": typ, "name": tool.Name, "inputSchema": r87cgenRaw(t, tool.Parameters)})
	}
	return out
}

// r87cgenPromptView is the TS LanguageModelV4Prompt of a model call: the
// system message first (Go carries it in Prompt.System), then the messages.
func r87cgenPromptView(t *testing.T, p types.Prompt) []interface{} {
	t.Helper()
	out := []interface{}{}
	if p.System != "" {
		out = append(out, r87cgenObj{"role": "system", "content": p.System})
	}
	return append(out, r87cgenMessagesView(t, p.Messages)...)
}

// generate-text.test.ts 1964 'tool choice enforcement' > 'should reject tool
// calls with none ($toolChoice, $stepToolChoice, $finishReason)': each of the
// four table rows rejects with the ToolChoiceViolationError (TS name
// AI_ToolChoiceViolationError: errors.As finds the typed error), its message,
// tool choice, finish reason, provider, model ID and the tool-call content;
// the tool never runs, onInputStart / onInputAvailable are not called and
// the model call end event fires once.
func TestR87cgenToolChoiceNoneRejects(t *testing.T) {
	none := types.NoneToolChoice()
	for _, tc := range []struct {
		toolChoice types.ToolChoice
		stepChoice *types.ToolChoice
		finish     types.FinishReason
	}{
		{types.NoneToolChoice(), nil, types.FinishReasonStop},
		{types.NoneToolChoice(), nil, types.FinishReasonToolCalls},
		{types.NoneToolChoice(), nil, types.FinishReasonInterrupted},
		{types.AutoToolChoice(), &none, types.FinishReasonToolCalls},
	} {
		t.Run(string(tc.toolChoice.Type)+"/"+string(tc.finish), func(t *testing.T) {
			var executed, inputStart, inputAvailable, callEnds int
			model := r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
				r := r87cgenResult(r87cgenCall("call-1", "tool1", "{}"))
				r.FinishReason, r.RawFinishReason = tc.finish, string(tc.finish)
				return r, nil
			})
			tool := r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed++
				return "tool-result", nil
			})
			tool.OnInputStart = func(context.Context, types.OnInputStartOptions) error { inputStart++; return nil }
			tool.OnInputAvailable = func(context.Context, types.OnInputAvailableOptions) error { inputAvailable++; return nil }
			opts := GenerateTextOptions{
				Model: model, Tools: []types.Tool{tool}, ToolChoice: tc.toolChoice, Prompt: "test-input",
				OnLanguageModelCallEnd: func(context.Context, LanguageModelCallEndEvent) { callEnds++ },
			}
			if tc.stepChoice != nil {
				opts.PrepareStep = func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
					return PrepareStepOptions{ToolChoice: *tc.stepChoice}
				}
			}
			_, err := GenerateText(context.Background(), opts)
			var v *ToolChoiceViolationError
			if !errors.As(err, &v) {
				t.Fatalf("err = %v, want a ToolChoiceViolationError", err)
			}
			if v.Error() != "Model response contained a tool call even though tool choice was none." ||
				!reflect.DeepEqual(v.ToolChoice, types.ToolChoice{Type: types.ToolChoiceNone}) || v.FinishReason != tc.finish ||
				v.Provider != "mock-provider" || v.ModelID != "mock-model-id" {
				t.Fatalf("error = %+v (%q)", v, v.Error())
			}
			if len(v.Content) != 1 || v.Content[0].ContentType() != "tool-call" {
				t.Fatalf("content = %+v, want one tool-call part", v.Content)
			}
			if executed != 0 || inputStart != 0 || inputAvailable != 0 || callEnds != 1 {
				t.Fatalf("execute %d onInputStart %d onInputAvailable %d onLanguageModelCallEnd %d", executed, inputStart, inputAvailable, callEnds)
			}
		})
	}
}

// generate-text.test.ts 2349 'result.toolCalls' > 'should contain tool
// calls': the model gets both tools, tool choice required and the user
// prompt; result.toolCalls is the parsed call.
func TestR87cgenResultToolCalls(t *testing.T) {
	model := r87cgenModel(func(_ int, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		r87cgenCheck(t, "tools", r87cgenToolsView(t, o.Tools), `[{"type":"function","name":"tool1","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"}},{"type":"function","name":"tool2","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"somethingElse":{"type":"string"}},"required":["somethingElse"],"type":"object"}}]`)
		r87cgenCheck(t, "toolChoice", r87cgenRaw(t, o.ToolChoice), `{"type":"required"}`)
		r87cgenCheck(t, "prompt", r87cgenPromptView(t, o.Prompt), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
		return r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
	})
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", ToolChoice: types.RequiredToolChoice(),
		Tools: []types.Tool{r87cgenTool("tool1", nil, "value"), r87cgenTool("tool2", nil, "somethingElse")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "toolCalls", r87cgenToolCallsView(t, res.ToolCalls), `[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}]`)
}

// generate-text.test.ts 2494 'result.toolResults' > 'should contain tool
// results': tool choice auto, the tool gets the parsed input, and
// result.toolResults is the static result.
func TestR87cgenResultToolResults(t *testing.T) {
	model := r87cgenModel(func(_ int, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		r87cgenCheck(t, "tools", r87cgenToolsView(t, o.Tools), `[{"type":"function","name":"tool1","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"}}]`)
		r87cgenCheck(t, "toolChoice", r87cgenRaw(t, o.ToolChoice), `{"type":"auto"}`)
		r87cgenCheck(t, "prompt", r87cgenPromptView(t, o.Prompt), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
		return r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
	})
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input",
		Tools: []types.Tool{r87cgenTool("tool1", func(in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			if !reflect.DeepEqual(in, map[string]interface{}{"value": "value"}) {
				t.Errorf("args = %v", in)
			}
			return "result1", nil
		}, "value")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "toolResults", r87cgenToolResultsView(t, res.ToolResults), `[{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}]`)
}

// generate-text.test.ts 2685 / 2699 / 2734 'result.responseMessages' >
// 'should contain assistant response message when there are no tool calls',
// '... and tool message when there are tool calls with results' (the tool
// gets the parsed input and the user prompt as messages) and 'should contain
// reasoning' (snapshots).
func TestR87cgenResultResponseMessages(t *testing.T) {
	t.Run("no tool calls", func(t *testing.T) {
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return r87cgenResult(r87cgenText("Hello, world!")), nil
			}),
			Prompt: "test-input",
		})
		if err != nil {
			t.Fatal(err)
		}
		r87cgenCheck(t, "responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)
	})
	t.Run("tool calls with results", func(t *testing.T) {
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return r87cgenResult(r87cgenText("Hello, world!"), r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
			}),
			Prompt: "test-input",
			Tools: []types.Tool{r87cgenTool("tool1", func(in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
				if !reflect.DeepEqual(in, map[string]interface{}{"value": "value"}) {
					t.Errorf("args = %v", in)
				}
				r87cgenCheck(t, "tool messages", r87cgenMessagesView(t, o.Messages), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
				return "result1", nil
			}, "value")},
		})
		if err != nil {
			t.Fatal(err)
		}
		r87cgenCheck(t, "responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[{"text":"Hello, world!","type":"text"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}]`)
	})
	t.Run("reasoning", func(t *testing.T) {
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r87cgenModelWithReasoning(), Prompt: "test-input"})
		if err != nil {
			t.Fatal(err)
		}
		r87cgenCheck(t, "responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[{"providerOptions":{"testProvider":{"signature":"signature"}},"text":"I will open the conversation with witty banter.","type":"reasoning"},{"providerOptions":{"testProvider":{"redactedData":"redacted-reasoning-data"}},"text":"","type":"reasoning"},{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)
	})
}

func r87cgenHello(text string) *testutil.MockLanguageModel {
	return r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenResult(r87cgenText(text)), nil
	})
}

// generate-text.test.ts 2995 'options.onStart' > 'should pass
// runtimeContext' and 3646 'options.onStepStart' > 'should pass
// runtimeContext'.
func TestR87cgenStartEventsRuntimeContext(t *testing.T) {
	var start interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello!"), Prompt: "test-input",
		RuntimeContext: map[string]interface{}{"userId": "test-user", "sessionId": "123"},
		OnStart:        func(_ context.Context, e OnStartEvent) { start = e.RuntimeContext },
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(start, map[string]interface{}{"userId": "test-user", "sessionId": "123"}) {
		t.Errorf("onStart runtimeContext = %v", start)
	}
	var stepStart interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello!"), Prompt: "test-input",
		RuntimeContext: map[string]interface{}{"userId": "test-user", "requestId": "req-123"},
		OnStepStart:    func(_ context.Context, e OnStepStartEvent) { stepStart = e.RuntimeContext },
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stepStart, map[string]interface{}{"userId": "test-user", "requestId": "req-123"}) {
		t.Errorf("onStepStart runtimeContext = %v", stepStart)
	}
}

// generate-text.test.ts 3258 'options.onStepStart' > 'should not break
// generation when callback throws' (a Go callback panics).
func TestR87cgenOnStepStartPanicDoesNotBreakGeneration(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello, World!"), Prompt: "test-input",
		OnStepStart: func(context.Context, OnStepStartEvent) { panic(errors.New("callback error")) },
	})
	if err != nil || res.Text != "Hello, World!" {
		t.Fatalf("text = %v, err = %v", res, err)
	}
}

// generate-text.test.ts 3474 'options.onStepStart' > 'should validate model
// call settings returned from prepareStep': maxOutputTokens 0 from
// prepareStep rejects with 'maxOutputTokens must be >= 1'.
func TestR87cgenPrepareStepMaxOutputTokensValidated(t *testing.T) {
	zero := 0
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("unused"), Prompt: "test-input",
		PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{MaxOutputTokens: &zero}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "maxOutputTokens must be >= 1") {
		t.Fatalf("err = %v, want 'maxOutputTokens must be >= 1'", err)
	}
}

// generate-text.test.ts 4123 'options.onStepEnd stepNumber' > 'should call
// onStepEnd with step result': stepNumber 0.
func TestR87cgenOnStepEndStepNumber(t *testing.T) {
	stepNumber, eventStepNumber := -1, -1
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello!"), Prompt: "test-input",
		OnStepEnd:      func(_ context.Context, s types.StepResult, _ interface{}) { stepNumber = s.StepNumber },
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { eventStepNumber = e.StepNumber },
	}); err != nil {
		t.Fatal(err)
	}
	if stepNumber != 0 || eventStepNumber != 0 {
		t.Fatalf("stepNumber = %d (event %d), want 0", stepNumber, eventStepNumber)
	}
}

// generate-text.test.ts 4187 'options.onStepFinish stepNumber' > 'should
// pass correct stepNumber for each step in a multi-step tool loop'.
func TestR87cgenOnStepFinishStepNumbers(t *testing.T) {
	model := r87cgenModel(func(n int, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		switch n {
		case 0:
			return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "test" }`)), nil
		case 1:
			return r87cgenToolCalls(r87cgenCall("call-2", "tool1", `{ "value": "test2" }`)), nil
		}
		return r87cgenResult(r87cgenText("Done.")), nil
	})
	var numbers []int
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", StopWhen: []StopCondition{IsStepCount(4)},
		Tools: []types.Tool{r87cgenTool("tool1", func(in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return in["value"].(string) + "-result", nil
		}, "value")},
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) { numbers = append(numbers, s.StepNumber) },
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(numbers, []int{0, 1, 2}) {
		t.Fatalf("step numbers = %v, want [0 1 2]", numbers)
	}
}

func r87cgenValueTool(record func()) types.Tool {
	return r87cgenTool("tool1", func(in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		if record != nil {
			record()
		}
		return in["value"].(string) + "-result", nil
	}, "value")
}

// generate-text.test.ts 4580 'options.onToolExecutionStart' > 'should be
// called with correct tool name, id, and input' (snapshot of the event:
// callId, messages, toolCall, toolContext).
func TestR87cgenOnToolExecutionStartEvent(t *testing.T) {
	var events []interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "test-arg" }`)), nil
		}),
		Tools: []types.Tool{r87cgenValueTool(nil)}, Prompt: "test-input",
		Internal: &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			ev := r87cgenObj{"callId": e.CallID, "messages": r87cgenMessagesView(t, e.Messages), "toolCall": r87cgenToolCallView(r87cgenRaw(t, e.ToolCall).(r87cgenObj))}
			if e.ToolContext != nil {
				ev["toolContext"] = e.ToolContext
			}
			events = append(events, ev)
		},
	}); err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "events", events, `[{"callId":"test-telemetry-call-id","messages":[{"content":[{"type":"text","text":"test-input"}],"role":"user"}],"toolCall":{"input":{"value":"test-arg"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}}]`)
}

// generate-text.test.ts 4728 'options.onToolExecutionStart' > 'should be
// called before tool execution'.
func TestR87cgenOnToolExecutionStartBeforeExecute(t *testing.T) {
	var order []string
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "test" }`)), nil
		}),
		Tools:                []types.Tool{r87cgenValueTool(func() { order = append(order, "execute") })},
		Prompt:               "test-input",
		OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { order = append(order, "onToolExecutionStart") },
	}); err != nil {
		t.Fatal(err)
	}
	start, exec := -1, -1
	for i, e := range order {
		if e == "onToolExecutionStart" && start < 0 {
			start = i
		}
		if e == "execute" && exec < 0 {
			exec = i
		}
	}
	if start < 0 || exec < 0 || start >= exec {
		t.Fatalf("order = %v", order)
	}
}

// generate-text.test.ts 5146 'options.onToolExecutionEnd' > 'should not
// break generation when callback throws' (a Go callback panics).
func TestR87cgenOnToolExecutionEndPanicDoesNotBreakGeneration(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "test" }`)), nil
		}),
		Tools: []types.Tool{r87cgenValueTool(nil)}, Prompt: "test-input",
		OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) { panic(errors.New("callback error")) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].Result != "test-result" {
		t.Fatalf("toolResults = %+v", res.ToolResults)
	}
}

// r87cgenTwoStepResponses are the 'options.stopWhen' > '2 steps: initial,
// tool-result' model responses: a tool1 call (usage 10 / 5, response
// test-id-1-from-model at the epoch), then "Hello, world!" (testUsage,
// response test-id-2-from-model at 10 s with a custom header).
func r87cgenTwoStepResponse(n int) (*types.GenerateResult, error) {
	switch n {
	case 0:
		r := r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "value" }`))
		r.Usage = r87cgenUsage(10, 5)
		r.ResponseMetadata = &types.ResponseMetadata{ID: "test-id-1-from-model", Timestamp: time.Unix(0, 0).UTC(), ModelID: "test-response-model-id"}
		return r, nil
	case 1:
		r := r87cgenResult(r87cgenText("Hello, world!"))
		r.ResponseMetadata = &types.ResponseMetadata{ID: "test-id-2-from-model", Timestamp: time.Unix(10, 0).UTC(), ModelID: "test-response-model-id",
			Headers: map[string]string{"custom-response-header": "response-header-value"}}
		return r, nil
	}
	return nil, errors.New("Unexpected response count")
}

const (
	r87cgenTool1Schema = `{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"}`
	r87cgenTotalUsage  = `{"inputTokenDetails":{"noCacheTokens":13},"inputTokens":13,"outputTokenDetails":{"textTokens":15},"outputTokens":15,"totalTokens":28}`
	r87cgenFinalUsage  = `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`
	// The two steps of the 2-step snapshots, without the step 0 tool-call
	// title (set by the plain block's tool only).
	r87cgenTwoStepStep1    = `{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"custom-response-header":"response-header-value"},"id":"test-id-2-from-model","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"test-response-model-id","timestamp":"1970-01-01T00:00:10.000Z"},"runtimeContext":%s,"stepNumber":1,"toolsContext":{},"usage":` + r87cgenFinalUsage + `,"warnings":[]}`
	r87cgenTwoStepStep0    = `{"callId":"test-telemetry-call-id","content":[{"input":{"value":"value"},%s"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"id":"test-id-1-from-model","messages":[{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"test-response-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":%s,"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":10},"inputTokens":10,"outputTokenDetails":{"textTokens":5},"outputTokens":5,"totalTokens":15},"warnings":[]}`
	r87cgenTwoStepMessages = `[{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"},{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`
)

func r87cgenSprintf(format string, args ...interface{}) string {
	out := format
	for _, a := range args {
		out = strings.Replace(out, "%s", a.(string), 1)
	}
	return out
}

// generate-text.test.ts 'options.stopWhen' > '2 steps: initial,
// tool-result' (the shared beforeEach: the first call gets tool1, tool choice
// auto and the user prompt; the tool gets the parsed input and the user
// prompt as messages):
// generate-text.test.ts 6347 result.text, 6351 result.toolCalls, 6357
// result.finalStep.toolCalls, 6361 result.toolResults, 6367
// result.finalStep.toolResults, 6371 result.responseMessages (snapshot),
// 6469 result.totalUsage, 6488 result.usage, 6507 result.finalStep.usage,
// 6527 onFinishResult.usage / finalStep, 6547 result.steps (snapshot) and
// 6556 callbacks > onStepFinish (snapshot).
func TestR87cgenStopWhenTwoSteps(t *testing.T) {
	model := r87cgenModel(func(n int, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		if n == 0 {
			r87cgenCheck(t, "tools", r87cgenToolsView(t, o.Tools), `[{"type":"function","name":"tool1","inputSchema":`+r87cgenTool1Schema+`}]`)
			r87cgenCheck(t, "toolChoice", r87cgenRaw(t, o.ToolChoice), `{"type":"auto"}`)
			r87cgenCheck(t, "prompt", r87cgenPromptView(t, o.Prompt), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
		}
		return r87cgenTwoStepResponse(n)
	})
	tool := r87cgenTool("tool1", func(in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		if !reflect.DeepEqual(in, map[string]interface{}{"value": "value"}) {
			t.Errorf("args = %v", in)
		}
		r87cgenCheck(t, "tool messages", r87cgenMessagesView(t, o.Messages), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
		return "result1", nil
	}, "value")
	tool.Title = "Tool One"
	var onFinish OnFinishEvent
	var onStepFinish []types.StepResult
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Tools: []types.Tool{tool}, Prompt: "test-input",
		Internal:      &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) { onFinish = e },
		OnStepFinish:  func(_ context.Context, s types.StepResult, _ interface{}) { onStepFinish = append(onStepFinish, s) },
		StopWhen:      []StopCondition{IsStepCount(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(n int, id func(int) string) []string {
		out := []string{}
		for i := 0; i < n; i++ {
			out = append(out, id(i))
		}
		return out
	}
	if res.Text != "Hello, world!" {
		t.Errorf("6347 text = %q", res.Text)
	}
	if got := ids(len(res.ToolCalls), func(i int) string { return res.ToolCalls[i].ID }); !reflect.DeepEqual(got, []string{"call-1"}) {
		t.Errorf("6351 toolCalls = %v", got)
	}
	if len(res.FinalStep.ToolCalls) != 0 {
		t.Errorf("6357 finalStep.toolCalls = %v", res.FinalStep.ToolCalls)
	}
	if got := ids(len(res.ToolResults), func(i int) string { return res.ToolResults[i].ToolCallID }); !reflect.DeepEqual(got, []string{"call-1"}) {
		t.Errorf("6361 toolResults = %v", got)
	}
	if len(res.FinalStep.ToolResults) != 0 {
		t.Errorf("6367 finalStep.toolResults = %v", res.FinalStep.ToolResults)
	}
	r87cgenCheck(t, "6371 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), r87cgenTwoStepMessages)
	r87cgenCheck(t, "6469 totalUsage", r87cgenRaw(t, res.TotalUsage), r87cgenTotalUsage)
	r87cgenCheck(t, "6488 usage", r87cgenRaw(t, res.Usage), r87cgenTotalUsage)
	r87cgenCheck(t, "6507 finalStep.usage", r87cgenRaw(t, res.FinalStep.Usage), r87cgenFinalUsage)
	// 6527
	if !reflect.DeepEqual(onFinish.Usage, res.Usage) || !reflect.DeepEqual(onFinish.Usage, res.TotalUsage) || !reflect.DeepEqual(onFinish.TotalUsage, res.TotalUsage) {
		t.Errorf("6527 onFinish usage = %+v / %+v", onFinish.Usage, onFinish.TotalUsage)
	}
	if len(onFinish.Steps) == 0 || !reflect.DeepEqual(onFinish.FinalStep, onFinish.Steps[len(onFinish.Steps)-1]) || !reflect.DeepEqual(onFinish.FinalStep.Usage, res.FinalStep.Usage) {
		t.Errorf("6527 onFinish finalStep = %+v", onFinish.FinalStep)
	}
	steps := "[" + r87cgenSprintf(r87cgenTwoStepStep0, `"title":"Tool One",`, "{}") + "," + r87cgenSprintf(r87cgenTwoStepStep1, "{}") + "]"
	r87cgenCheck(t, "6547 steps", r87cgenStepsView(t, res.Steps), steps)
	r87cgenCheck(t, "6556 onStepFinish", r87cgenStepsView(t, onStepFinish), steps)
}

func r87cgenStr(s string) *string { return &s }

// generate-text.test.ts 'options.stopWhen' > '2 steps: initial, tool-result
// with prepareStep' (the shared beforeEach: modelWithFiles is replaced by
// prepareStep's model for both steps; step 0 forces tool1 with
// system-message-0, a replaced prompt and runtimeContext state2; step 1 has
// no active tools, system-message-1 and runtimeContext state3):
// generate-text.test.ts 6709 should contain all prepareStep calls, 7237
// result.text, 7241 result.toolCalls, 7247 result.finalStep.toolCalls, 7251
// result.toolResults, 7257 result.finalStep.toolResults, 7261
// result.responseMessages (snapshot), 7265 result.totalUsage, 7284
// result.usage, 7303 result.finalStep.usage, 7323 result.steps (snapshot),
// 7327 onStepFinish (snapshot), 7331 result.content and 7363
// result.finalStep.content.
//
// The TS prepareStepCalls snapshot shows both steps under each call because
// it records the SDK's live steps array (the callback itself asserts [] and
// length 1); Go hands each call a copy, so the port checks the steps each
// call saw.
func TestR87cgenStopWhenTwoStepsWithPrepareStep(t *testing.T) {
	trueModel := r87cgenModel(func(n int, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenTwoStepResponse(n)
	})
	type prepareCall struct {
		ModelID        string        `json:"modelId"`
		StepNumber     int           `json:"stepNumber"`
		Steps          []interface{} `json:"steps"`
		Messages       []interface{} `json:"messages"`
		RuntimeContext interface{}   `json:"runtimeContext"`
	}
	var prepareCalls []prepareCall
	var onStepFinish []types.StepResult
	toolChoice := types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "tool1"}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModelWithFiles(),
		Tools: []types.Tool{r87cgenTool("tool1", func(in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			if !reflect.DeepEqual(in, map[string]interface{}{"value": "value"}) {
				t.Errorf("args = %v", in)
			}
			r87cgenCheck(t, "tool messages", r87cgenMessagesView(t, o.Messages), `[{"role":"user","content":[{"type":"text","text":"new input from prepareStep"}]}]`)
			return "result1", nil
		}, "value")},
		RuntimeContext: map[string]interface{}{"context": "state1"},
		Prompt:         "test-input",
		Internal:       &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		StopWhen:       []StopCondition{IsStepCount(3)},
		OnStepFinish:   func(_ context.Context, s types.StepResult, _ interface{}) { onStepFinish = append(onStepFinish, s) },
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			prepareCalls = append(prepareCalls, prepareCall{o.Model.ModelID(), o.StepNumber, r87cgenStepsView(t, o.Steps), r87cgenMessagesView(t, o.Messages), o.RuntimeContext})
			switch o.StepNumber {
			case 0:
				if len(o.Steps) != 0 {
					t.Errorf("step 0 steps = %d", len(o.Steps))
				}
				return PrepareStepOptions{Model: trueModel, ToolChoice: toolChoice, Instructions: r87cgenStr("system-message-0"),
					Messages:       []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "new input from prepareStep"}}}},
					RuntimeContext: map[string]interface{}{"context": "state2"}}
			case 1:
				if len(o.Steps) != 1 {
					t.Errorf("step 1 steps = %d", len(o.Steps))
				}
				return PrepareStepOptions{Model: trueModel, ActiveTools: []string{}, Instructions: r87cgenStr("system-message-1"),
					RuntimeContext: map[string]interface{}{"context": "state3"}}
			}
			return PrepareStepOptions{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	step0 := r87cgenSprintf(r87cgenTwoStepStep0, "", `{"context":"state2"}`)
	step1 := r87cgenSprintf(r87cgenTwoStepStep1, `{"context":"state3"}`)
	r87cgenCheck(t, "6709 prepareStep calls", r87cgenRaw(t, prepareCalls), `[{"messages":[{"content":[{"type":"text","text":"test-input"}],"role":"user"}],"modelId":"mock-model-id","runtimeContext":{"context":"state1"},"stepNumber":0,"steps":[]},`+
		`{"messages":[{"content":[{"type":"text","text":"new input from prepareStep"}],"role":"user"},{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","runtimeContext":{"context":"state2"},"stepNumber":1,"steps":[`+step0+`]}]`)
	if res.Text != "Hello, world!" {
		t.Errorf("7237 text = %q", res.Text)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call-1" {
		t.Errorf("7241 toolCalls = %+v", res.ToolCalls)
	}
	if len(res.FinalStep.ToolCalls) != 0 {
		t.Errorf("7247 finalStep.toolCalls = %+v", res.FinalStep.ToolCalls)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].ToolCallID != "call-1" {
		t.Errorf("7251 toolResults = %+v", res.ToolResults)
	}
	if len(res.FinalStep.ToolResults) != 0 {
		t.Errorf("7257 finalStep.toolResults = %+v", res.FinalStep.ToolResults)
	}
	r87cgenCheck(t, "7261 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), r87cgenTwoStepMessages)
	r87cgenCheck(t, "7265 totalUsage", r87cgenRaw(t, res.TotalUsage), r87cgenTotalUsage)
	r87cgenCheck(t, "7284 usage", r87cgenRaw(t, res.Usage), r87cgenTotalUsage)
	r87cgenCheck(t, "7303 finalStep.usage", r87cgenRaw(t, res.FinalStep.Usage), r87cgenFinalUsage)
	r87cgenCheck(t, "7323 steps", r87cgenStepsView(t, res.Steps), "["+step0+","+step1+"]")
	r87cgenCheck(t, "7327 onStepFinish", r87cgenStepsView(t, onStepFinish), "["+step0+","+step1+"]")
	r87cgenCheck(t, "7331 content", r87cgenContentView(t, res.Content), `[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},{"text":"Hello, world!","type":"text"}]`)
	r87cgenCheck(t, "7363 finalStep.content", r87cgenContentView(t, res.FinalStep.Content), `[{"text":"Hello, world!","type":"text"}]`)
}

// generate-text.test.ts 7693 'options.stopWhen' > 'should complete tool loop
// with isLoopFinished()'.
func TestR87cgenIsLoopFinished(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(n int, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			switch n {
			case 0:
				return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
			case 1:
				return r87cgenResult(r87cgenText("Done!")), nil
			}
			return nil, errors.New("Unexpected response count")
		}),
		Tools:    []types.Tool{r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }, "value")},
		Prompt:   "test-input",
		StopWhen: []StopCondition{IsLoopFinished()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Done!" || len(res.Steps) != 2 {
		t.Fatalf("text = %q, steps = %d", res.Text, len(res.Steps))
	}
}

// generate-text.test.ts 7763 'options.providerOptions' > 'should pass
// provider options to model'.
func TestR87cgenProviderOptionsToModel(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(_ int, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			if !reflect.DeepEqual(o.ProviderOptions, map[string]interface{}{"aProvider": map[string]interface{}{"someKey": "someValue"}}) {
				t.Errorf("providerOptions = %#v", o.ProviderOptions)
			}
			return r87cgenResult(r87cgenText("provider metadata test")), nil
		}),
		Prompt:          "test-input",
		ProviderOptions: map[string]interface{}{"aProvider": map[string]interface{}{"someKey": "someValue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "provider metadata test" {
		t.Fatalf("text = %q", res.Text)
	}
}

// generate-text.test.ts 7805 'options.reasoning' > 'should pass through
// provider-default'.
func TestR87cgenReasoningProviderDefault(t *testing.T) {
	model := r87cgenHello("test")
	level := types.ReasoningDefault
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Reasoning: &level}); err != nil {
		t.Fatal(err)
	}
	if got := model.GenerateCalls[0].Reasoning; got == nil || *got != "provider-default" {
		t.Fatalf("reasoning = %v, want provider-default", got)
	}
}

// generate-text.test.ts 7824 'options.abortSignal' > 'should forward abort
// signal to tool execution': the caller aborts after the call started (in Go,
// while the model call runs); the tool still runs with the call's abort
// signal (its ctx is cancelled), call-1, the messages and no context.
func TestR87cgenAbortSignalForwardedToTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type call struct {
		input     map[string]interface{}
		cancelled bool
		id        string
		messages  int
		toolCtx   interface{}
	}
	var calls []call
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			cancel()
			return r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
		}),
		Tools: []types.Tool{{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(toolCtx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			calls = append(calls, call{in, toolCtx.Err() != nil, o.ToolCallID, len(o.Messages), o.ToolContext})
			return "tool result", nil
		}}},
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatalf("err = %v, want the call to finish", err)
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].input, map[string]interface{}{"value": "value"}) || !calls[0].cancelled || calls[0].id != "call-1" || calls[0].messages == 0 || calls[0].toolCtx != nil {
		t.Fatalf("tool calls = %+v", calls)
	}
}

func r87cgenDur(ms int) *time.Duration { d := time.Duration(ms) * time.Millisecond; return &d }

func r87cgenHasDeadline(ctx context.Context) bool {
	_, ok := ctx.Deadline()
	return ok
}

// generate-text.test.ts 'options.timeout' (TS abort signals are Go context
// deadlines: "an AbortSignal is passed" is a ctx with a deadline):
// generate-text.test.ts 8016 should forward timeout abort signal to tool
// execution, 8055 should support timeout object with totalMs, 8119 should
// forward stepMs as abort signal to each step, 8187 should forward stepMs
// abort signal to tool execution, 8221 should support both totalMs and
// stepMs together.
func TestR87cgenTimeoutSignals(t *testing.T) {
	toolCallModel := func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
	}
	t.Run("8016 timeout to tool execution", func(t *testing.T) {
		type call struct {
			input       map[string]interface{}
			hasDeadline bool
			id          string
			messages    int
			toolCtx     interface{}
		}
		var calls []call
		if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r87cgenModel(toolCallModel),
			Tools: []types.Tool{{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(ctx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
				calls = append(calls, call{in, r87cgenHasDeadline(ctx), o.ToolCallID, len(o.Messages), o.ToolContext})
				return "tool result", nil
			}}},
			Prompt:  "test-input",
			Timeout: &TimeoutConfig{Total: r87cgenDur(5000)},
		}); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || !reflect.DeepEqual(calls[0].input, map[string]interface{}{"value": "value"}) || !calls[0].hasDeadline || calls[0].id != "call-1" || calls[0].messages == 0 || calls[0].toolCtx != nil {
			t.Fatalf("tool calls = %+v", calls)
		}
	})
	modelSignals := func(t *testing.T, timeout *TimeoutConfig) []bool {
		var got []bool
		if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				got = append(got, r87cgenHasDeadline(ctx))
				return r87cgenResult(r87cgenText("Hello, world!")), nil
			}},
			Prompt:  "test-input",
			Timeout: timeout,
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Run("8055 totalMs", func(t *testing.T) {
		if got := modelSignals(t, &TimeoutConfig{Total: r87cgenDur(5000)}); !reflect.DeepEqual(got, []bool{true}) {
			t.Fatalf("model signals = %v", got)
		}
	})
	t.Run("8119 stepMs to each step", func(t *testing.T) {
		if got := modelSignals(t, &TimeoutConfig{PerStep: r87cgenDur(5000)}); !reflect.DeepEqual(got, []bool{true}) {
			t.Fatalf("model signals = %v", got)
		}
	})
	t.Run("8221 totalMs and stepMs", func(t *testing.T) {
		if got := modelSignals(t, &TimeoutConfig{Total: r87cgenDur(10000), PerStep: r87cgenDur(5000)}); !reflect.DeepEqual(got, []bool{true}) {
			t.Fatalf("model signals = %v", got)
		}
	})
	t.Run("8187 stepMs to tool execution", func(t *testing.T) {
		hasDeadline := false
		if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r87cgenModel(toolCallModel),
			Tools: []types.Tool{{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				hasDeadline = r87cgenHasDeadline(ctx)
				return "tool result", nil
			}}},
			Prompt:  "test-input",
			Timeout: &TimeoutConfig{PerStep: r87cgenDur(5000)},
		}); err != nil {
			t.Fatal(err)
		}
		if !hasDeadline {
			t.Fatal("tool ctx has no deadline")
		}
	})
}

// generate-text.test.ts 8241 'options.timeout' > 'should abort when step
// timeout expires': the model call's signal is aborted with a TimeoutError
// reason (Go: the ctx is done and its cause is a *TimeoutError named
// "TimeoutError").
func TestR87cgenStepTimeoutAborts(t *testing.T) {
	zero := 0
	var aborted bool
	var cause error
	_, _ = GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			aborted, cause = ctx.Err() != nil, context.Cause(ctx)
			return r87cgenResult(r87cgenText("Hello, world!")), nil
		}},
		Prompt:     "test-input",
		Timeout:    &TimeoutConfig{PerStep: r87cgenDur(50)},
		MaxRetries: &zero,
	})
	var te *TimeoutError
	if !aborted || !errors.As(cause, &te) || te.ErrorName() != "TimeoutError" {
		t.Fatalf("aborted = %v, reason = %T %v", aborted, cause, cause)
	}
}

// The class of 8241 (TS util/set-abort-timeout.ts, merge-abort-signals.ts):
// every GenerateText timeout aborts with a TimeoutError reason. The total
// timeout's reason is AbortSignal.timeout's ("The operation was aborted due
// to timeout"), the step timeout's setAbortTimeout's ("Step timeout of 50ms
// exceeded"), a tool timeout's AbortSignal.timeout's, and a batch call's
// timeout is its total timeout.
func TestR87cgenTimeoutCauses(t *testing.T) {
	blockingModel := func(cause *error) *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			<-ctx.Done()
			*cause = context.Cause(ctx)
			return nil, ctx.Err()
		}}
	}
	check := func(t *testing.T, cause error, reason TimeoutReason, text string) {
		t.Helper()
		var te *TimeoutError
		if !errors.As(cause, &te) || te.Reason != reason || te.Error() != text || !errors.Is(cause, context.DeadlineExceeded) {
			t.Fatalf("cause = %T %v, want the %s TimeoutError %q", cause, cause, reason, text)
		}
	}
	zero := 0
	t.Run("total", func(t *testing.T) {
		var cause error
		_, _ = GenerateText(context.Background(), GenerateTextOptions{Model: blockingModel(&cause), Prompt: "x", MaxRetries: &zero, Timeout: &TimeoutConfig{Total: r87cgenDur(30)}})
		check(t, cause, TimeoutReasonTotal, "The operation was aborted due to timeout")
	})
	t.Run("step", func(t *testing.T) {
		var cause error
		_, _ = GenerateText(context.Background(), GenerateTextOptions{Model: blockingModel(&cause), Prompt: "x", MaxRetries: &zero, Timeout: &TimeoutConfig{PerStep: r87cgenDur(50)}})
		check(t, cause, TimeoutReasonStep, "Step timeout of 50ms exceeded")
	})
	t.Run("tool", func(t *testing.T) {
		var cause error
		_, _ = GenerateText(context.Background(), GenerateTextOptions{
			Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return r87cgenResult(r87cgenCall("call-1", "tool1", `{ "value": "value" }`)), nil
			}),
			Tools: []types.Tool{{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				<-ctx.Done()
				cause = context.Cause(ctx)
				return nil, ctx.Err()
			}}},
			Prompt: "x", Timeout: &TimeoutConfig{ToolMs: r87cgenDur(30)},
		})
		check(t, cause, TimeoutReasonTool, "The operation was aborted due to timeout")
	})
	t.Run("batch", func(t *testing.T) {
		ctx, cancel := withOptionalTimeout(context.Background(), r87cgenDur(10))
		defer cancel()
		<-ctx.Done()
		check(t, context.Cause(ctx), TimeoutReasonTotal, "The operation was aborted due to timeout")
	})
}

// generate-text.test.ts 8932 'options.output' > 'text output' > 'should set
// responseFormat to text and not change the prompt': the model call options
// snapshot (abortSignal undefined: no deadline; headers: the user agent;
// every sampling setting, providerOptions, reasoning, seed, stopSequences and
// tools undefined; toolChoice auto; responseFormat text; the plain user
// prompt). TS mocks VERSION as 0.0.0-test; Go sends its own version.
func TestR87cgenTextOutputCallOptions(t *testing.T) {
	var opts *provider.GenerateOptions
	var hasDeadline bool
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			opts, hasDeadline = o, r87cgenHasDeadline(ctx)
			return r87cgenResult(r87cgenText("Hello, world!")), nil
		}},
		Prompt: "prompt",
		Output: TextOutput(),
	}); err != nil {
		t.Fatal(err)
	}
	if hasDeadline {
		t.Error("abortSignal: the call has a deadline")
	}
	got := r87cgenObj{
		"headers":         opts.Headers,
		"prompt":          r87cgenPromptView(t, opts.Prompt),
		"responseFormat":  r87cgenObj{"type": opts.ResponseFormat.Type},
		"toolChoice":      r87cgenRaw(t, opts.ToolChoice),
		"tools":           r87cgenToolsView(t, opts.Tools),
		"providerOptions": opts.ProviderOptions,
	}
	unset := opts.FrequencyPenalty == nil && opts.MaxTokens == nil && opts.PresencePenalty == nil && opts.Reasoning == nil &&
		opts.Seed == nil && opts.StopSequences == nil && opts.Temperature == nil && opts.TopK == nil && opts.TopP == nil
	if !unset || opts.ResponseFormat.Schema != nil || opts.ResponseFormat.Name != "" || opts.ResponseFormat.Description != "" {
		t.Errorf("call options = %+v", opts)
	}
	r71genCheckJSON(t, "call options", r87cgenRaw(t, got), r87cgenWant(t, `{"headers":{"user-agent":"ai/`+version.Version+`"},"prompt":[{"content":[{"text":"prompt","type":"text"}],"role":"user"}],"providerOptions":null,"responseFormat":{"type":"text"},"toolChoice":{"type":"auto"},"tools":null}`))
}

// generate-text.test.ts 9281 'options.output' > 'provider-selected structured
// output' > 'should preserve all text for $name output' (default and text
// output): structuredOutputText does not narrow text output.
func TestR87cgenTextOutputKeepsAllText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output interface{}
	}{{"default", nil}, {"text", TextOutput()}} {
		t.Run(tc.name, func(t *testing.T) {
			selected := "Final answer."
			res, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
					r := r87cgenResult(r87cgenText("Commentary. "), r87cgenText("Final answer."))
					r.StructuredOutputText = &selected
					return r, nil
				}),
				Prompt: "prompt",
				Output: tc.output,
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Output != "Commentary. Final answer." || res.Text != "Commentary. Final answer." {
				t.Fatalf("output = %#v, text = %q", res.Output, res.Text)
			}
		})
	}
}

// generate-text.test.ts 9743 'programmatic tool calling' > 'should stop an
// interrupted response with a deferred provider tool result': one model
// call, one step, finish reason interrupted.
func TestR87cgenInterruptedDeferredProviderTool(t *testing.T) {
	model := r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
		r := r87cgenResult(types.ToolCallContent{ToolCallID: "call-1", ToolName: "program", Input: "{}", ProviderExecuted: true})
		r.FinishReason, r.RawFinishReason = types.FinishReasonInterrupted, ""
		return r, nil
	})
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "Run a program",
		Tools:    []types.Tool{{Name: "program", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, ProviderID: "test.program", Parameters: r87cgenSchema(), SupportsDeferredResults: true}},
		StopWhen: []StopCondition{IsStepCount(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.GenerateCalls) != 1 || len(res.Steps) != 1 || res.FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("model calls %d, steps %d, finish %s", len(model.GenerateCalls), len(res.Steps), res.FinishReason)
	}
}

// generate-text.test.ts 11062 'dynamic tools' > 'should execute dynamic
// tools': the client runs the tool; the content is the dynamic call and its
// dynamic result.
func TestR87cgenDynamicToolExecuted(t *testing.T) {
	executed := false
	tool := r87cgenTool("dynamicTool", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		executed = true
		return map[string]interface{}{"value": "test-result"}, nil
	}, "value")
	tool.Type = types.ToolTypeDynamic
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(r87cgenCall("call-1", "dynamicTool", `{ "value": "test" }`)), nil
		}),
		Tools: []types.Tool{tool}, Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Fatal("dynamic tool not executed")
	}
	r87cgenCheck(t, "content", r87cgenContentView(t, res.Content), `[{"dynamic":true,"input":{"value":"test"},"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-call"},{"dynamic":true,"input":{"value":"test"},"output":{"value":"test-result"},"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-result"}]`)
}

// generate-text.test.ts 11235 'context' > 'should pass sandbox to
// prepareStep' and 11346 'should send runtimeContext in onFinish callback'.
func TestR87cgenContextSandboxAndOnFinishRuntimeContext(t *testing.T) {
	sandbox := &r78genSandbox{name: "test sandbox"}
	var captured interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello, world!"), Prompt: "test",
		ExperimentalSandbox: sandbox,
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			captured = o.ExperimentalSandbox
			return PrepareStepOptions{}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if captured != sandbox {
		t.Errorf("prepareStep sandbox = %v", captured)
	}
	var recorded, recordedEvent interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("Hello, world!"), Prompt: "test-input",
		RuntimeContext: map[string]interface{}{"context": "test"},
		OnFinish:       func(_ context.Context, _ *GenerateTextResult, rc interface{}) { recorded = rc },
		OnFinishEvent:  func(_ context.Context, e OnFinishEvent) { recordedEvent = e.RuntimeContext },
	}); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"context": "test"}
	if !reflect.DeepEqual(recorded, want) || !reflect.DeepEqual(recordedEvent, want) {
		t.Errorf("onFinish runtimeContext = %v / %v", recorded, recordedEvent)
	}
}

// r87cgenContextIntegration records the runtime / tools context of the
// telemetry start, step start, step end and end events.
type r87cgenContextIntegration struct {
	telemetry.NoopTelemetryIntegration
	tools bool
	got   *[]interface{}
}

func (i r87cgenContextIntegration) pick(rc, tc map[string]interface{}) {
	v := rc
	if i.tools {
		v = tc
	}
	if v == nil {
		v = map[string]interface{}{}
	}
	*i.got = append(*i.got, v)
}

func (i r87cgenContextIntegration) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	i.pick(e.RuntimeContext, e.ToolsContext)
	return ctx
}

func (i r87cgenContextIntegration) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	i.pick(e.RuntimeContext, e.ToolsContext)
	return ctx
}

func (i r87cgenContextIntegration) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	i.pick(e.RuntimeContext, e.ToolsContext)
}

func (i r87cgenContextIntegration) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	i.pick(e.RuntimeContext, e.ToolsContext)
}

// generate-text.test.ts 11463 'context' > 'should include configured
// toolsContext properties in telemetry', 11527 'should exclude toolsContext
// properties from telemetry by default' and 11581 'should exclude
// runtimeContext from telemetry by default': the four integration events
// carry only the included keys ({} by default).
func TestR87cgenTelemetryContextFiltering(t *testing.T) {
	weather := r87cgenTool("weather", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "sunny", nil }, "city")
	toolsContext := map[string]interface{}{"weather": map[string]interface{}{"apiKey": "secret-api-key", "region": "eu"}}
	run := func(t *testing.T, opts GenerateTextOptions, tools bool, settings TelemetrySettings) []interface{} {
		t.Helper()
		var got []interface{}
		settings.Integrations = []telemetry.TelemetryIntegration{r87cgenContextIntegration{tools: tools, got: &got}}
		opts.Model, opts.Prompt, opts.Telemetry = r87cgenHello("Hello, world!"), "test-input", &settings
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		return r87cgenRaw(t, got).([]interface{})
	}
	t.Run("11463 included toolsContext", func(t *testing.T) {
		got := run(t, GenerateTextOptions{Tools: []types.Tool{weather}, ToolsContext: toolsContext}, true,
			TelemetrySettings{IncludeToolsContext: map[string]map[string]bool{"weather": {"region": true}}})
		r87cgenCheck(t, "toolsContext", got, `[{"weather":{"region":"eu"}},{"weather":{"region":"eu"}},{"weather":{"region":"eu"}},{"weather":{"region":"eu"}}]`)
	})
	t.Run("11527 toolsContext excluded by default", func(t *testing.T) {
		got := run(t, GenerateTextOptions{Tools: []types.Tool{weather}, ToolsContext: toolsContext}, true, TelemetrySettings{})
		r87cgenCheck(t, "toolsContext", got, `[{},{},{},{}]`)
	})
	t.Run("11581 runtimeContext excluded by default", func(t *testing.T) {
		got := run(t, GenerateTextOptions{RuntimeContext: map[string]interface{}{"userId": "user-123", "requestId": "request-123"}}, false, TelemetrySettings{})
		r87cgenCheck(t, "runtimeContext", got, `[{},{},{},{}]`)
	})
}

// generate-text.test.ts 11870 'tools with preliminary results' > 'single
// tool with preliminary results' > 'should only include final tool result in
// step content' (snapshot of result.steps).
func TestR87cgenPreliminaryFinalResultInStep(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			r := r87cgenToolCalls(r87cgenCall("call-1", "cityAttractions", `{ "city": "San Francisco" }`))
			r.Usage = r87cgenUsage(10, 20)
			return r, nil
		}),
		Prompt:   "test-input",
		Internal: &InternalOptions{GenerateID: func() string { return "test-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		Tools:    []types.Tool{cityAttractionsTool()},
	})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "steps", r87cgenStepsView(t, res.Steps), `[{"callId":"test-telemetry-call-id","content":[{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-call"},{"dynamic":false,"input":{"city":"San Francisco"},"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"id":"test-id","messages":[{"content":[{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"json","value":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"}},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":10},"inputTokens":10,"outputTokenDetails":{"textTokens":20},"outputTokens":20,"totalTokens":30},"warnings":[]}]`)
}

// r87cgenPromptsView is the TS prompts list a model recorded.
func r87cgenPromptsView(t *testing.T, prompts [][]types.Message) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, p := range prompts {
		out = append(out, r87cgenMessagesView(t, p))
	}
	return out
}

// r87cgenRecording answers every call with resp(n) and records the prompts.
func r87cgenRecording(prompts *[][]types.Message, resp func(n int) *types.GenerateResult) *testutil.MockLanguageModel {
	return r87cgenModel(func(n int, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		*prompts = append(*prompts, o.Prompt.Messages)
		return resp(n), nil
	})
}

const (
	r87cgenUserTestInput = `{"content":[{"text":"test-input","type":"text"}],"role":"user"}`
	r87cgenAssistantCall = `{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"}`
	r87cgenHelloMessage  = `{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}`
)

// generate-text.test.ts 'tool execution approval' > 'when a single tool has
// a needsApproval function' (beforeEach: call-1 needs approval, call-2 does
// not): 12433 should have tool-calls finish reason, 12437 should add a tool
// approval request to the content, 12491 should include tool approval
// request in response messages, 12543 should call the needsApproval function
// with the correct input and options.
func TestR87cgenNeedsApprovalFunction(t *testing.T) {
	type approvalCall struct {
		Input   map[string]interface{} `json:"input"`
		Options r87cgenObj             `json:"options"`
	}
	var calls []approvalCall
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(r87cgenCall("call-1", "tool1", `{ "value": "value-needs-approval" }`), r87cgenCall("call-2", "tool1", `{ "value": "value-no-approval" }`)), nil
		}),
		Tools: []types.Tool{r87cgenTool("tool1", func(in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "result for " + in["value"].(string), nil
		}, "value")},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.SingleToolApprovalFunc(func(in map[string]interface{}, o types.SingleToolApprovalOptions) types.ToolApprovalResult {
			opts := r87cgenObj{"messages": r87cgenMessagesView(t, o.Messages), "runtimeContext": r87cgenObj{}, "toolCallId": o.ToolCallID}
			if o.RuntimeContext != nil {
				opts["runtimeContext"] = o.RuntimeContext
			}
			if o.ToolContext != nil {
				opts["toolContext"] = o.ToolContext
			}
			calls = append(calls, approvalCall{in, opts})
			if in["value"] == "value-needs-approval" {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		})},
		StopWhen: []StopCondition{IsStepCount(3)},
		Prompt:   "test-input",
		Internal: r87cgenIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("12433 finishReason = %s", res.FinishReason)
	}
	r87cgenCheck(t, "12437 content", r87cgenContentView(t, res.Content), `[`+
		`{"input":{"value":"value-needs-approval"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},`+
		`{"input":{"value":"value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-call"},`+
		`{"dynamic":false,"input":{"value":"value-no-approval"},"output":"result for value-no-approval","toolCallId":"call-2","toolName":"tool1","type":"tool-result"},`+
		`{"approvalId":"id-1","toolCall":{"input":{"value":"value-needs-approval"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},"type":"tool-approval-request"}]`)
	r87cgenCheck(t, "12491 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[`+
		`{"input":{"value":"value-needs-approval"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},`+
		`{"input":{"value":"value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-call"},`+
		`{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"}],"role":"assistant"},`+
		`{"content":[{"output":{"type":"text","value":"result for value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-result"}],"role":"tool"}]`)
	r87cgenCheck(t, "12543 needsApproval calls", r87cgenRaw(t, calls), `[`+
		`{"input":{"value":"value-needs-approval"},"options":{"messages":[`+r87cgenUserTestInput+`],"runtimeContext":{},"toolCallId":"call-1"}},`+
		`{"input":{"value":"value-no-approval"},"options":{"messages":[`+r87cgenUserTestInput+`],"runtimeContext":{},"toolCallId":"call-2"}}]`)
}

// generate-text.test.ts 'when a call from a single tool that needs approval
// is approved': 12657 should execute the tool (input, no abort signal: no
// deadline, call-1, the messages), 12668 should call the model with a prompt
// that includes the tool result, 12719 should include the tool result in the
// response messages.
func TestR87cgenApprovedToolCall(t *testing.T) {
	var prompts [][]types.Message
	type exec struct {
		input       map[string]interface{}
		hasDeadline bool
		id          string
		messages    int
	}
	var execs []exec
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenRecording(&prompts, func(int) *types.GenerateResult { return r87cgenResult(r87cgenText("Hello, world!")) }),
		Tools: []types.Tool{{Name: "tool1", Parameters: r87cgenSchema("value"), Execute: func(ctx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			execs = append(execs, exec{in, r87cgenHasDeadline(ctx), o.ToolCallID, len(o.Messages)})
			return "result1", nil
		}}},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Internal:     r87cgenIDs(),
		Messages:     r71genApprovalHistory(true, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 1 || !reflect.DeepEqual(execs[0].input, map[string]interface{}{"value": "value"}) || execs[0].hasDeadline || execs[0].id != "call-1" || execs[0].messages == 0 {
		t.Errorf("12657 executions = %+v", execs)
	}
	toolResult := `{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}`
	r87cgenCheck(t, "12668 prompts", r87cgenPromptsView(t, prompts), `[[`+r87cgenUserTestInput+`,`+r87cgenAssistantCall+`,`+toolResult+`]]`)
	r87cgenCheck(t, "12719 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[`+toolResult+`,`+r87cgenHelloMessage+`]`)
}

// generate-text.test.ts 13160 'when experimental_toolApprovalSecret is
// configured' > 'should work without a secret (backward compatible)': an
// unsigned approval request is executed once when no secret is set.
func TestR87cgenApprovalWithoutSecret(t *testing.T) {
	executions := 0
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenHello("done"),
		Tools: []types.Tool{r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executions++
			return "result1", nil
		}, "value")},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}},
				types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
}

// generate-text.test.ts 'when a call from a single tool that needs approval
// is denied': 13405 should not execute the tool, 13409 should call the model
// with a prompt that includes the tool error (execution-denied, no reason),
// 13460 should include the tool error in the response messages.
func TestR87cgenDeniedToolCall(t *testing.T) {
	var prompts [][]types.Message
	executions := 0
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenRecording(&prompts, func(int) *types.GenerateResult { return r87cgenToolCalls(r87cgenText("Hello, world!")) }),
		Tools: []types.Tool{r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executions++
			return "result1", nil
		}, "value")},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Internal:     r87cgenIDs(),
		Messages:     r71genApprovalHistory(false, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if executions != 0 {
		t.Errorf("13405 executions = %d", executions)
	}
	denied := `{"content":[{"output":{"type":"execution-denied"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}`
	r87cgenCheck(t, "13409 prompts", r87cgenPromptsView(t, prompts), `[[`+r87cgenUserTestInput+`,`+r87cgenAssistantCall+`,`+denied+`]]`)
	r87cgenCheck(t, "13460 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[`+denied+`,`+r87cgenHelloMessage+`]`)
}

// generate-text.test.ts 14141 'when two calls from a single tool that needs
// approval are approved' > 'should include the tool results in the response
// messages'.
func TestR87cgenTwoApprovedToolCalls(t *testing.T) {
	var prompts [][]types.Message
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:        r87cgenRecording(&prompts, func(int) *types.GenerateResult { return r87cgenToolCalls(r87cgenText("Hello, world!")) }),
		Tools:        []types.Tool{r87cgenTool("tool1", func(map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }, "value")},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
		StopWhen:     []StopCondition{IsStepCount(3)},
		Internal:     r87cgenIDs(),
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value1"}},
				types.ToolApprovalRequestContent{ApprovalID: "id-1", ToolCallID: "call-1"},
				types.ToolCallContent{ToolCallID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value2"}},
				types.ToolApprovalRequestContent{ApprovalID: "id-2", ToolCallID: "call-2"},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolApprovalResponseContent{ApprovalID: "id-1", Approved: true},
				types.ToolApprovalResponseContent{ApprovalID: "id-2", Approved: true},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[`+
		`{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"},`+
		`{"output":{"type":"text","value":"result1"},"toolCallId":"call-2","toolName":"tool1","type":"tool-result"}],"role":"tool"},`+r87cgenHelloMessage+`]`)
}

var r87cgenMCPTool = types.Tool{Name: "mcp_tool", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, ProviderID: "test.mcp_tool", Parameters: r87cgenSchema("query")}

// generate-text.test.ts 'provider-executed tool (MCP) approval' > 'when a
// provider-executed tool emits tool-approval-request': 14226 should only
// execute 1 step when waiting for approval, 14230 should have tool-calls
// finish reason, 14234 should add tool approval request with providerExecuted
// tool call to content, 14267 should include tool approval request in
// response messages.
func TestR87cgenMCPApprovalRequest(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenModel(func(int, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r87cgenToolCalls(types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Input: `{ "query": "test" }`, ProviderExecuted: true},
				types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"}), nil
		}),
		Tools:    []types.Tool{r87cgenMCPTool},
		StopWhen: []StopCondition{IsStepCount(3)},
		Prompt:   "test-input",
		Internal: r87cgenIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Errorf("14226 steps = %d", len(res.Steps))
	}
	if res.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("14230 finishReason = %s", res.FinishReason)
	}
	call := `{"input":{"query":"test"},"providerExecuted":true,"toolCallId":"mcp-call-1","toolName":"mcp_tool","type":"tool-call"}`
	r87cgenCheck(t, "14234 content", r87cgenContentView(t, res.Content), `[`+call+`,{"approvalId":"mcp-approval-1","toolCall":`+call+`,"type":"tool-approval-request"}]`)
	r87cgenCheck(t, "14267 responseMessages", r87cgenMessagesView(t, res.ResponseMessages), `[{"content":[`+call+`,{"approvalId":"mcp-approval-1","toolCallId":"mcp-call-1","type":"tool-approval-request"}],"role":"assistant"}]`)
}

// generate-text.test.ts 14441 'when a provider-executed tool approval is
// approved' > 'should include provider-executed tool result in content'.
func TestR87cgenMCPApprovedContent(t *testing.T) {
	var prompts [][]types.Message
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenRecording(&prompts, func(n int) *types.GenerateResult {
			if n == 0 {
				return r87cgenResult(
					types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Input: `{ "query": "test" }`, ProviderExecuted: true},
					types.ToolResultContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Result: map[string]interface{}{"shortened_url": "https://short.url/abc"}, ProviderExecuted: true},
					r87cgenText("Here is your shortened URL: https://short.url/abc"))
			}
			return r87cgenResult(r87cgenText("Done"))
		}),
		Tools: []types.Tool{r87cgenMCPTool}, StopWhen: []StopCondition{IsStepCount(3)}, Internal: r87cgenIDs(),
		Messages: r71genMCPHistory(true, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	// TS prints "dynamic": undefined for the provider's result; the view's
	// default false is dropped to match.
	content := r87cgenContentView(t, res.Content)
	if r, ok := content[1].(r87cgenObj); ok && r["dynamic"] == false {
		delete(r, "dynamic")
	}
	r87cgenCheck(t, "content", content, `[{"input":{"query":"test"},"providerExecuted":true,"toolCallId":"mcp-call-1","toolName":"mcp_tool","type":"tool-call"},`+
		`{"input":{"query":"test"},"output":{"shortened_url":"https://short.url/abc"},"providerExecuted":true,"toolCallId":"mcp-call-1","toolName":"mcp_tool","type":"tool-result"},`+
		`{"text":"Here is your shortened URL: https://short.url/abc","type":"text"}]`)
}

// generate-text.test.ts 'when a provider-executed tool approval is denied':
// 14612 should include text response from model in content, 14623 should
// have stop finish reason.
func TestR87cgenMCPDenied(t *testing.T) {
	var prompts [][]types.Message
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r87cgenRecording(&prompts, func(int) *types.GenerateResult {
			return r87cgenResult(r87cgenText("I understand. The tool execution was not approved."))
		}),
		Tools: []types.Tool{r87cgenMCPTool}, StopWhen: []StopCondition{IsStepCount(3)}, Internal: r87cgenIDs(),
		Messages: r71genMCPHistory(false, "User denied the request"),
	})
	if err != nil {
		t.Fatal(err)
	}
	r87cgenCheck(t, "14612 content", r87cgenContentView(t, res.Content), `[{"text":"I understand. The tool execution was not approved.","type":"text"}]`)
	if res.FinishReason != types.FinishReasonStop {
		t.Errorf("14623 finishReason = %s", res.FinishReason)
	}
}
