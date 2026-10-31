package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 74 (stream): ports of TS stream-text.test.ts cases with exact
// assertions. Package-level identifiers are prefixed r74stream.

// r74streamAssertJSON compares got and want by JSON value (object key order
// ignored). A content tool-result's raw "result" (kept next to the model
// output once content parts carry the raw tool output, gen r74 CR-1) must
// equal the text model output's value and is then left out of the
// comparison, so the assertions hold with and without that change.
func r74streamAssertJSON(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal: %v", label, err)
	}
	var g, w interface{}
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	r74streamDropRawResults(t, label, g)
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want JSON: %v", label, err)
	}
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		t.Fatalf("%s:\n got  %s\n want %s", label, gb, wb)
	}
}

func r74streamReasoningSteps() []provider.StreamChunk {
	sig1 := json.RawMessage(`{"testProvider":{"signature":"1234567890"}}`)
	sig2 := json.RawMessage(`{"testProvider":{"signature":"0987654321"}}`)
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeReasoningStart, ID: "1"},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "I will open the conversation"},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " with witty banter."},
		{Type: provider.ChunkTypeReasoning, ID: "1", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
		{Type: provider.ChunkTypeReasoningStart, ID: "2", ProviderMetadata: json.RawMessage(`{"testProvider":{"redactedData":"redacted-reasoning-data"}}`)},
		{Type: provider.ChunkTypeReasoningEnd, ID: "2"},
		{Type: provider.ChunkTypeReasoningStart, ID: "3"},
		{Type: provider.ChunkTypeReasoning, ID: "3", Reasoning: " Once the user has relaxed,"},
		{Type: provider.ChunkTypeReasoning, ID: "3", Reasoning: " I will pry for valuable information."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "3", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoningStart, ID: "4", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoning, ID: "4", Reasoning: " I need to think about"},
		{Type: provider.ChunkTypeReasoning, ID: "4", Reasoning: " this problem carefully."},
		{Type: provider.ChunkTypeReasoningStart, ID: "5", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " The best solution"},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " requires careful"},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " consideration of all factors."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "4", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeReasoningEnd, ID: "5", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hi"},
		{Type: provider.ChunkTypeText, ID: "1", Text: " there!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
}

// TS 8724: result.steps > should add the reasoning from the model response
// to the step result (modelWithReasoning): each reasoning block is one
// content part in start order with its latest provider metadata, and the
// response message keeps every reasoning part (metadata as providerOptions).
func r74streamDropRawResults(t *testing.T, label string, v interface{}) {
	t.Helper()
	switch x := v.(type) {
	case []interface{}:
		for _, e := range x {
			r74streamDropRawResults(t, label, e)
		}
	case map[string]interface{}:
		if x["type"] == "tool-result" {
			if raw, ok := x["result"]; ok {
				out, _ := x["output"].(map[string]interface{})
				if out == nil || out["type"] != "text" || !reflect.DeepEqual(out["value"], raw) {
					t.Fatalf("%s: tool-result raw result %v does not match its output %v", label, raw, x["output"])
				}
				delete(x, "result")
			}
		}
		for _, e := range x {
			r74streamDropRawResults(t, label, e)
		}
	}
}

func TestR74StreamStepReasoningContent(t *testing.T) {
	r, _ := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, r74streamReasoningSteps()), Prompt: "test-input", Internal: r72streamIDs()})
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	sig1, sig2 := `{"testProvider":{"signature":"1234567890"}}`, `{"testProvider":{"signature":"0987654321"}}`
	part := func(typ, text, md, key string) string {
		return `{"type":"` + typ + `","text":` + strconv.Quote(text) + `,"` + key + `":` + md + `}`
	}
	parts := func(key string) string {
		return `[` + strings.Join([]string{
			part("reasoning", "I will open the conversation with witty banter.", sig1, key),
			part("reasoning", "", `{"testProvider":{"redactedData":"redacted-reasoning-data"}}`, key),
			part("reasoning", " Once the user has relaxed, I will pry for valuable information.", sig1, key),
			part("reasoning", " I need to think about this problem carefully.", sig2, key),
			part("reasoning", " The best solution requires careful consideration of all factors.", sig2, key),
			part("text", "Hi there!", sig2, key),
		}, ",") + `]`
	}
	r74streamAssertJSON(t, "step content", steps[0].Content, parts("providerMetadata"))
	r74streamAssertJSON(t, "step response messages", steps[0].Response.Messages, `[{"role":"assistant","content":`+parts("providerOptions")+`}]`)
	st := steps[0]
	if st.FinishReason != types.FinishReasonStop || st.RawFinishReason != "stop" || st.StepNumber != 0 || st.CallID != "test-telemetry-call-id" ||
		st.Model.Provider != "mock-provider" || st.Model.ModelID != "mock-model-id" || st.Response.ID != "id-0" || st.Response.ModelID != "mock-model-id" ||
		st.ProviderMetadata != nil || len(st.Warnings) != 0 {
		t.Fatalf("step = %+v", st)
	}
	// TS: timeBetweenOutputChunksMs is set (several output chunks).
	if st.Performance.TimeBetweenOutputChunksMs == nil {
		t.Fatalf("performance = %+v, want timeBetweenOutputChunksMs", st.Performance)
	}
}

// TS 8910: result.steps > should add the sources from the model response to
// the step result (modelWithSources): sources stay in the step content in
// stream order, the response message holds only the text, and a step with
// a single output chunk has no timeBetweenOutputChunksMs.
func TestR74StreamStepSourcesContent(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value"}}`)}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "456", URL: "https://example.com/2", Title: "Example 2", ProviderMetadata: json.RawMessage(`{"provider":{"custom":"value2"}}`)}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, _ := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()})
	got := r.Steps()
	if len(got) != 1 {
		t.Fatalf("steps = %d", len(got))
	}
	src1 := `{"type":"source","sourceType":"url","id":"123","url":"https://example.com","title":"Example","providerMetadata":{"provider":{"custom":"value"}}}`
	src2 := `{"type":"source","sourceType":"url","id":"456","url":"https://example.com/2","title":"Example 2","providerMetadata":{"provider":{"custom":"value2"}}}`
	r74streamAssertJSON(t, "step content", got[0].Content, `[`+src1+`,{"type":"text","text":"Hello!"},`+src2+`]`)
	r74streamAssertJSON(t, "step sources", got[0].Sources, `[`+src1+`,`+src2+`]`)
	r74streamAssertJSON(t, "step response messages", got[0].Response.Messages, `[{"role":"assistant","content":[{"type":"text","text":"Hello!"}]}]`)
	if got[0].Performance.TimeBetweenOutputChunksMs != nil {
		t.Fatalf("timeBetweenOutputChunksMs = %+v, want unset", got[0].Performance.TimeBetweenOutputChunksMs)
	}
}

// TS 2354: result.stream > should send tool call deltas (snapshot): the
// tool-input-start (dynamic false), every delta as given, tool-input-end,
// the parsed tool-call, and the finish-step/finish parts with tool-calls.
func TestR74StreamToolCallDeltas(t *testing.T) {
	const id = "call_O17Uplv4lJvD6DVdIvFFeRMw"
	deltas := []string{`{"`, "value", `":"`, "Spark", "le", " Day", `"}`}
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: id, ToolName: "test-tool"}},
	}
	for _, d := range deltas {
		step = append(step, provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: id, Text: d})
	}
	step = append(step,
		provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: id}},
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "test-tool", RawArguments: `{"value":"Sparkle Day"}`}},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	)
	tool := types.Tool{Name: "test-tool", Parameters: r71streamValueSchema}
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input", Tools: []types.Tool{tool}, ToolChoice: types.RequiredToolChoice()})
	var kinds []provider.ChunkType
	var gotDeltas []string
	for _, p := range parts {
		kinds = append(kinds, p.Type)
		switch p.Type {
		case provider.ChunkTypeToolInputStart:
			if p.ToolCall == nil || p.ToolCall.ID != id || p.ToolCall.ToolName != "test-tool" || p.ToolCall.Dynamic || p.ToolCall.Title != "" {
				t.Errorf("tool-input-start = %+v", p.ToolCall)
			}
		case provider.ChunkTypeToolInputDelta:
			if p.ID != id {
				t.Errorf("tool-input-delta id = %q", p.ID)
			}
			gotDeltas = append(gotDeltas, p.Text)
		case provider.ChunkTypeToolInputEnd:
			if p.ToolCall == nil || p.ToolCall.ID != id {
				t.Errorf("tool-input-end = %+v", p.ToolCall)
			}
		case provider.ChunkTypeToolCall:
			if p.ToolCall.ID != id || p.ToolCall.ToolName != "test-tool" || !reflect.DeepEqual(p.ToolCall.Arguments, map[string]interface{}{"value": "Sparkle Day"}) ||
				p.ToolCall.ProviderExecuted || p.ToolCall.ProviderMetadata != nil || p.ToolCall.Title != "" || p.ToolCall.Dynamic || p.ToolCall.Invalid {
				t.Errorf("tool-call = %+v", p.ToolCall)
			}
		case provider.ChunkTypeFinishStep:
			if p.FinishReason != types.FinishReasonToolCalls || p.RawFinishReason != "" || p.Response == nil || p.Response.ID != "id-0" || p.Response.ModelID != "mock-model-id" || p.ProviderMetadata != nil {
				t.Errorf("finish-step = %+v", p)
			}
		case provider.ChunkTypeFinish:
			if p.FinishReason != types.FinishReasonToolCalls || p.RawFinishReason != "" || p.Usage == nil || p.Usage.GetTotalTokens() != 13 {
				t.Errorf("finish = %+v", p)
			}
		}
	}
	want := []provider.ChunkType{provider.ChunkTypeStart, provider.ChunkTypeStartStep, provider.ChunkTypeToolInputStart}
	for range deltas {
		want = append(want, provider.ChunkTypeToolInputDelta)
	}
	want = append(want, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall, provider.ChunkTypeFinishStep, provider.ChunkTypeFinish)
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("parts = %v\nwant %v", kinds, want)
	}
	if !reflect.DeepEqual(gotDeltas, deltas) {
		t.Fatalf("deltas = %q", gotDeltas)
	}
}

// r74streamTransformedRun is the TS "2 steps: initial, tool-result with
// transformed tool results" setup (the r72 port's model and transform), with
// onEnd and onStepFinish recorded.
func r74streamTransformedRun(t *testing.T) (*StreamTextResult, OnFinishEvent, []OnStepFinishEvent, []types.StepResult) {
	t.Helper()
	var mu sync.Mutex
	call := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			n := call
			call++
			mu.Unlock()
			if n == 0 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
					{Type: provider.ChunkTypeReasoningStart, ID: "id-0"},
					{Type: provider.ChunkTypeReasoning, ID: "id-0", Reasoning: "thinking"},
					{Type: provider.ChunkTypeReasoningEnd, ID: "id-0"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
			}), nil
		}}
	upper := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && !c.ToolResult.Dynamic {
			tr := *c.ToolResult
			if s, ok := tr.Result.(string); ok {
				tr.Result = strings.ToUpper(s)
			}
			input := map[string]interface{}{}
			for k, v := range tr.Input {
				input[k] = v
			}
			if s, ok := input["value"].(string); ok {
				input["value"] = strings.ToUpper(s)
			}
			tr.Input = input
			c.ToolResult = &tr
		}
		return []provider.StreamChunk{c}
	}
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}}
	var end OnFinishEvent
	ends := 0
	var stepEvents []OnStepFinishEvent
	var stepResults []types.StepResult
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, ExperimentalTransform: []StreamTransformFunc{upper},
		StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(),
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; ends++; mu.Unlock() },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) {
			mu.Lock()
			stepResults = append(stepResults, s)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if ends != 1 {
		t.Fatalf("onEnd calls = %d", ends)
	}
	return r, end, stepEvents, stepResults
}

// The step content's tool result holds the model output in Output (and,
// after gen r74 CR-1, the raw output in Result, which r74streamAssertJSON
// checks and drops); TS content has `output: 'RESULT1', dynamic: false`.
// Assistant messages also carry Go's legacy tool_calls field.
const (
	r74streamTransformedStep0Content = `[{"type":"reasoning","text":"thinking"},` +
		`{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}},` +
		`{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","input":{"value":"VALUE"},"output":{"type":"text","value":"RESULT1"}}]`
	r74streamTransformedStep0Messages = `[{"role":"assistant","content":[{"type":"reasoning","text":"thinking"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}],` +
		`"tool_calls":[{"arguments":{"value":"value"},"id":"call-1","toolName":"tool1"}]},` +
		`{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"RESULT1"}}]}]`
	r74streamTransformedStep1Messages = `[{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}]`
)

// TS 16693: ... with transformed tool results > callbacks > onFinish should
// send correct information: the content of every step with the transformed
// tool result, the final step's text/reasoning/finish reason, the
// transformed static tool results, and response messages of all steps.
func TestR74StreamTransformedToolResultOnEnd(t *testing.T) {
	_, end, _, _ := r74streamTransformedRun(t)
	content := strings.TrimSuffix(r74streamTransformedStep0Content, "]") + `,{"type":"text","text":"Hello, world!"}]`
	r74streamAssertJSON(t, "onEnd content", end.Content, content)
	if end.CallID != "test-telemetry-call-id" || end.Text != "Hello, world!" || end.FinishReason != types.FinishReasonStop || end.RawFinishReason != "stop" || end.StepNumber != 1 {
		t.Fatalf("onEnd = callId %q text %q finish %q/%q step %d", end.CallID, end.Text, end.FinishReason, end.RawFinishReason, end.StepNumber)
	}
	// TS onEnd reasoning/reasoningText are the final step's: [] and undefined.
	if len(end.Reasoning) != 0 || end.ReasoningText != "" {
		t.Fatalf("onEnd reasoning = %#v / %q, want the final step's (none)", end.Reasoning, end.ReasoningText)
	}
	for label, results := range map[string][]types.ToolResult{"toolResults": end.ToolResults, "staticToolResults": end.StaticToolResults} {
		if len(results) != 1 || results[0].ToolCallID != "call-1" || results[0].Result != "RESULT1" || !reflect.DeepEqual(results[0].Input, map[string]interface{}{"value": "VALUE"}) || results[0].Dynamic {
			t.Fatalf("onEnd %s = %#v", label, results)
		}
	}
	if len(end.DynamicToolResults) != 0 || len(end.DynamicToolCalls) != 0 || len(end.Sources) != 0 || len(end.Files) != 0 {
		t.Fatalf("onEnd dynamic/sources/files = %v %v %v %v", end.DynamicToolResults, end.DynamicToolCalls, end.Sources, end.Files)
	}
	if len(end.ToolCalls) != 1 || !reflect.DeepEqual(end.ToolCalls[0].Arguments, map[string]interface{}{"value": "value"}) {
		t.Fatalf("onEnd toolCalls = %#v", end.ToolCalls)
	}
	msgs := strings.TrimSuffix(r74streamTransformedStep0Messages, "]") + "," + strings.TrimPrefix(r74streamTransformedStep1Messages, "[")
	r74streamAssertJSON(t, "onEnd responseMessages", end.ResponseMessages, msgs)
	if len(end.Steps) != 2 || end.FinalStep.StepNumber != 1 || end.FinalStep.Text != "Hello, world!" {
		t.Fatalf("onEnd steps = %d, finalStep = %+v", len(end.Steps), end.FinalStep)
	}
	if end.TotalUsage.GetTotalTokens() != 26 || end.Usage.GetTotalTokens() != 26 {
		t.Fatalf("onEnd usage = %+v / %+v", end.Usage, end.TotalUsage)
	}
}

// TS 17183: ... with transformed tool results > callbacks > onStepFinish
// should send correct information: step 0 carries the transformed tool
// result in its content, tool results and response messages.
func TestR74StreamTransformedToolResultOnStepFinish(t *testing.T) {
	r, _, events, results := r74streamTransformedRun(t)
	if len(events) != 2 || len(results) != 2 {
		t.Fatalf("step events = %d, step results = %d", len(events), len(results))
	}
	for i, s := range results {
		if !reflect.DeepEqual(s.Content, r.Steps()[i].Content) {
			t.Fatalf("onStepFinish %d content = %#v", i, s.Content)
		}
	}
	r74streamAssertJSON(t, "step 0 content", events[0].Content, r74streamTransformedStep0Content)
	r74streamAssertJSON(t, "step 0 response messages", events[0].Response.Messages, r74streamTransformedStep0Messages)
	r74streamAssertJSON(t, "step 1 content", events[1].Content, `[{"type":"text","text":"Hello, world!"}]`)
	r74streamAssertJSON(t, "step 1 response messages", events[1].Response.Messages, r74streamTransformedStep1Messages)
	e0, e1 := events[0], events[1]
	if e0.FinishReason != types.FinishReasonToolCalls || e0.StepNumber != 0 || e0.Response.ID != "id-0" || e1.FinishReason != types.FinishReasonStop || e1.StepNumber != 1 || e1.Response.ID != "id-1" {
		t.Fatalf("step events = %+v / %+v", e0, e1)
	}
	if len(e0.ToolResults) != 1 || e0.ToolResults[0].Result != "RESULT1" || !reflect.DeepEqual(e0.ToolResults[0].Input, map[string]interface{}{"value": "VALUE"}) ||
		len(e0.StaticToolResults) != 1 || e0.StaticToolResults[0].Result != "RESULT1" {
		t.Fatalf("step 0 tool results = %#v / %#v", e0.ToolResults, e0.StaticToolResults)
	}
	if len(e0.Reasoning) != 1 || e0.Reasoning[0].Text != "thinking" || e0.Text != "" || e1.Text != "Hello, world!" {
		t.Fatalf("step 0 reasoning %#v, texts %q %q", e0.Reasoning, e0.Text, e1.Text)
	}
}

// r74streamUpperRun is the TS "options.transform > with base transformation"
// onFinish / onStepFinish setup: text interrupted by a tool call, the
// finish part's testProvider metadata, and upperCaseTransform.
func r74streamUpperRun(t *testing.T) (OnFinishEvent, []OnStepFinishEvent, *StreamTextResult) {
	t.Helper()
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage, ProviderMetadata: json.RawMessage(`{"testProvider":{"testKey":"testValue"}}`)},
	}
	var mu sync.Mutex
	var end OnFinishEvent
	var stepEvents []OnStepFinishEvent
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, steps), Prompt: "test-input",
		Tools:                 []types.Tool{r71streamTool1(true, nil, nil)},
		ExperimentalTransform: []StreamTransformFunc{r72streamUpperTransform},
		Internal:              &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		OnEndEvent:            func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	return end, stepEvents, r
}

const (
	// One text part at its text-start position (the tool call arrived in the
	// middle of the text block); the transformed tool call and result. The
	// tool result is in Go's content shape (see r74streamAssertJSON).
	r74streamUpperContent = `[{"type":"text","text":"HELLO, WORLD!"},` +
		`{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"VALUE"}},` +
		`{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","input":{"value":"VALUE"},"output":{"type":"text","value":"VALUE-RESULT"}}]`
	r74streamUpperMessages = `[{"role":"assistant","content":[{"type":"text","text":"HELLO, WORLD!"},{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"VALUE"}}],` +
		`"tool_calls":[{"arguments":{"value":"VALUE"},"id":"call-1","toolName":"tool1"}]},` +
		`{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"VALUE-RESULT"}}]}]`
)

// TS 21473: options.transform > with base transformation > options.onFinish
// should receive transformed data (snapshot).
func TestR74StreamTransformOnEndReceivesTransformedData(t *testing.T) {
	end, _, _ := r74streamUpperRun(t)
	r74streamAssertJSON(t, "onEnd content", end.Content, r74streamUpperContent)
	r74streamAssertJSON(t, "onEnd responseMessages", end.ResponseMessages, r74streamUpperMessages)
	r74streamAssertJSON(t, "onEnd providerMetadata", end.ProviderMetadata, `{"testProvider":{"testKey":"TEST VALUE"}}`)
	if end.CallID != "test-telemetry-call-id" || end.Text != "HELLO, WORLD!" || end.FinishReason != types.FinishReasonStop || end.StepNumber != 0 || len(end.Reasoning) != 0 || end.ReasoningText != "" {
		t.Fatalf("onEnd = callId %q text %q finish %q step %d reasoning %v", end.CallID, end.Text, end.FinishReason, end.StepNumber, end.Reasoning)
	}
	if len(end.ToolCalls) != 1 || end.ToolCalls[0].Arguments["value"] != "VALUE" || len(end.StaticToolCalls) != 1 || len(end.DynamicToolCalls) != 0 {
		t.Fatalf("onEnd tool calls = %#v / %#v", end.ToolCalls, end.StaticToolCalls)
	}
	if len(end.ToolResults) != 1 || end.ToolResults[0].Result != "VALUE-RESULT" || end.ToolResults[0].Input["value"] != "VALUE" || len(end.StaticToolResults) != 1 {
		t.Fatalf("onEnd tool results = %#v", end.ToolResults)
	}
	if len(end.Steps) != 1 || end.FinalStep.Text != "HELLO, WORLD!" {
		t.Fatalf("onEnd steps = %#v", end.Steps)
	}
	r74streamAssertJSON(t, "onEnd finalStep providerMetadata", end.FinalStep.ProviderMetadata, `{"testProvider":{"testKey":"TEST VALUE"}}`)
	r74streamAssertJSON(t, "onEnd finalStep content", end.FinalStep.Content, r74streamUpperContent)
	r74streamAssertJSON(t, "onEnd finalStep response messages", end.FinalStep.Response.Messages, r74streamUpperMessages)
}

// TS 22007: options.transform > with base transformation >
// options.onStepFinish should receive transformed data (snapshot).
func TestR74StreamTransformOnStepFinishReceivesTransformedData(t *testing.T) {
	_, events, _ := r74streamUpperRun(t)
	if len(events) != 1 {
		t.Fatalf("step events = %d", len(events))
	}
	e := events[0]
	r74streamAssertJSON(t, "step content", e.Content, r74streamUpperContent)
	r74streamAssertJSON(t, "step response messages", e.Response.Messages, r74streamUpperMessages)
	r74streamAssertJSON(t, "step providerMetadata", e.ProviderMetadata, `{"testProvider":{"testKey":"TEST VALUE"}}`)
	if e.Text != "HELLO, WORLD!" || e.FinishReason != types.FinishReasonStop || e.RawFinishReason != "stop" || e.StepNumber != 0 || e.Response.ID != "id-0" || e.Response.ModelID != "mock-model-id" {
		t.Fatalf("step event = %+v", e)
	}
	if len(e.ToolResults) != 1 || e.ToolResults[0].Result != "VALUE-RESULT" || len(e.ToolCalls) != 1 || e.ToolCalls[0].Arguments["value"] != "VALUE" {
		t.Fatalf("step tool calls/results = %#v / %#v", e.ToolCalls, e.ToolResults)
	}
}

// TS 22699: options.transform > with transformation that aborts stream >
// options.onStepFinish should be called: the step comes from the
// transform's own finish-step part (its response, null usage, performance
// without the per-second and first-output figures), with the text produced
// before the stop.
func TestR74StreamTransformStopOnStepFinish(t *testing.T) {
	stopWord := func(ctx context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type != provider.ChunkTypeText || !strings.Contains(c.Text, "STOP") {
			return []provider.StreamChunk{c}
		}
		if stop, ok := StreamTransformStopFromContext(ctx); ok {
			stop()
		}
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: &types.Usage{},
				Performance: &types.StepPerformance{ToolExecutionMs: map[string]int64{}},
				Response:    &types.StepResponse{ID: "response-id", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}
	}
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
		{Type: provider.ChunkTypeText, ID: "1", Text: "STOP"},
		{Type: provider.ChunkTypeText, ID: "1", Text: " World"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	var mu sync.Mutex
	var got []types.StepResult
	var events []OnStepFinishEvent
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, steps), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{stopWord},
		Internal:              &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) {
			mu.Lock()
			got = append(got, s)
			mu.Unlock()
		},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || len(events) != 1 {
		t.Fatalf("onStepFinish calls = %d, step events = %d", len(got), len(events))
	}
	s := got[0]
	r74streamAssertJSON(t, "step content", s.Content, `[{"type":"text","text":"Hello, "}]`)
	r74streamAssertJSON(t, "step response messages", s.Response.Messages, `[{"role":"assistant","content":[{"type":"text","text":"Hello, "}]}]`)
	r74streamAssertJSON(t, "step performance", s.Performance, `{"stepTimeMs":0,"responseTimeMs":0,"toolExecutionMs":{},"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0}`)
	if s.CallID != "test-telemetry-call-id" || s.FinishReason != types.FinishReasonStop || s.RawFinishReason != "" || s.ProviderMetadata != nil || s.StepNumber != 0 ||
		s.Model.Provider != "mock-provider" || s.Model.ModelID != "mock-model-id" || s.Response.ID != "response-id" || s.Response.ModelID != "mock-model-id" || !s.Response.Timestamp.Equal(time.Unix(0, 0)) || len(s.Warnings) != 0 {
		t.Fatalf("step = %+v", s)
	}
	if s.Usage.InputTokens != nil || s.Usage.OutputTokens != nil || s.Usage.TotalTokens != nil {
		t.Fatalf("step usage = %+v, want the transform's null usage", s.Usage)
	}
	if e := events[0]; e.Text != "Hello, " || e.Response.ID != "response-id" || !reflect.DeepEqual(e.Content, s.Content) {
		t.Fatalf("step event = %+v", e)
	}
}

// TS 30466 / 30472 / 30535 (automatically rejected) and 30737 / 30750 /
// 30813 (automatically approved): the run continues to a second step; the
// second model prompt holds the tool call and the tool result only (the
// execution-denied output, or the tool's output), while the response
// messages also keep the automatic approval request and response.
func TestR74StreamAutomaticApprovalPromptAndResponseMessages(t *testing.T) {
	for _, tc := range []struct {
		status   types.ToolApprovalStatus
		reason   string
		approved bool
		output   string
		executed int
	}{
		{types.ToolApprovalStatusDenied, "blocked by policy", false, `{"type":"execution-denied","reason":"blocked by policy"}`, 0},
		{types.ToolApprovalStatusApproved, "trusted internal tool", true, `{"type":"text","value":"result1"}`, 1},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			var mu sync.Mutex
			executed := 0
			tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				executed++
				mu.Unlock()
				return "result1", nil
			}}
			var prompts [][]types.Message
			call := []provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}
			hello := []provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
			}
			reason := tc.reason
			r := r71streamRun(t, StreamTextOptions{
				Model: r73streamPromptModel(&prompts, call, hello), Prompt: "test-input",
				StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool},
				ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalResult{Status: tc.status, Reason: &reason}},
			})
			mu.Lock()
			if executed != tc.executed {
				t.Errorf("execute calls = %d, want %d", executed, tc.executed)
			}
			mu.Unlock()
			if r.Text() != "Hello, world!" || len(r.Steps()) != 2 {
				t.Fatalf("text = %q, steps = %d", r.Text(), len(r.Steps()))
			}
			if len(prompts) != 2 {
				t.Fatalf("model calls = %d", len(prompts))
			}
			user := `{"role":"user","content":[{"type":"text","text":"test-input"}]}`
			r74streamAssertJSON(t, "step 1 prompt", prompts[0], `[`+user+`]`)
			r74streamAssertJSON(t, "step 2 prompt", prompts[1], `[`+user+`,`+
				`{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}],"tool_calls":[{"arguments":{"value":"value"},"id":"call-1","toolName":"tool1"}]},`+
				`{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":`+tc.output+`}]}]`)
			r74streamAssertJSON(t, "response messages", r.ResponseMessages(), `[`+
				`{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}},{"type":"tool-approval-request","approvalId":"id-1","toolCallId":"call-1","isAutomatic":true}],"tool_calls":[{"arguments":{"value":"value"},"id":"call-1","toolName":"tool1"}]},`+
				`{"role":"tool","content":[{"type":"tool-approval-response","approvalId":"id-1","approved":`+strconv.FormatBool(tc.approved)+`,"reason":"`+tc.reason+`"},{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":`+tc.output+`}]},`+
				`{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}]`)
		})
	}
}

// With experimental_transform the recorded step follows the transformed
// stream (TS eventProcessor records after the transforms), its files
// included: a transform that drops a file part leaves it out of the step's
// Files as well as its content, the result's Files() and the end event.
func TestR74StreamTransformedStepFiles(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "SGVsbG8="}}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "QkFVRw=="}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	dropText := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeFile && c.GeneratedFileContent != nil && c.GeneratedFileContent.MediaType == "text/plain" {
			return nil
		}
		return []provider.StreamChunk{c}
	}
	var mu sync.Mutex
	var end OnFinishEvent
	var stepEvents []OnStepFinishEvent
	var stepResults []types.StepResult
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, steps), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{dropText},
		OnEndEvent:            func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
		OnStepFinish: func(_ context.Context, s types.StepResult, _ interface{}) {
			mu.Lock()
			stepResults = append(stepResults, s)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	only := func(label string, files []types.GeneratedFileContent) {
		t.Helper()
		if len(files) != 1 || files[0].MediaType != "image/jpeg" {
			t.Fatalf("%s = %+v, want only the image/jpeg file", label, files)
		}
	}
	only("Steps()[0].Files", r.Steps()[0].Files)
	only("FinalStep().Files", r.FinalStep().Files)
	only("Files()", r.Files())
	only("onStepFinish step Files", stepResults[0].Files)
	only("step event Files", stepEvents[0].Files)
	only("onEnd Files", end.Files)
	only("onEnd Steps[0].Files", end.Steps[0].Files)
	if kinds := len(r.Steps()[0].Content); kinds != 2 {
		t.Fatalf("step content = %#v", r.Steps()[0].Content)
	}
}

// TS records a step's warnings and request from the start-step part that
// leaves the experimental_transform chain (recordedWarnings = part.warnings,
// recordedRequest = part.request): a transform that rewrites the start-step
// warnings changes the step's warnings, the result's warnings, the step and
// end events and the logged warnings.
func TestR74StreamTransformedStartStepWarnings(t *testing.T) {
	setupLogWarnings(t)
	var logMu sync.Mutex
	var logged []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) {
		logMu.Lock()
		logged = append(logged, o)
		logMu.Unlock()
	})
	raw := types.Warning{Type: "other", Message: "raw warning"}
	replaced := types.Warning{Type: "other", Message: "transformed warning"}
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{raw}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	rewrite := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeStartStep {
			c.Warnings = []types.Warning{replaced}
			c.Request = &types.StepRequest{Body: "transformed body"}
		}
		return []provider.StreamChunk{c}
	}
	var mu sync.Mutex
	var end OnFinishEvent
	var stepEvents []OnStepFinishEvent
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, steps), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{rewrite},
		OnEndEvent:            func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			stepEvents = append(stepEvents, e)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	want := []types.Warning{replaced}
	for label, got := range map[string][]types.Warning{
		"Steps()[0].Warnings": r.Steps()[0].Warnings,
		"Warnings()":          r.Warnings(),
		"step event Warnings": stepEvents[0].Warnings,
		"onEnd Warnings":      end.Warnings,
	} {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", label, got, want)
		}
	}
	if body := r.Steps()[0].Request.Body; body != "transformed body" {
		t.Errorf("step request body = %#v, want the transformed start-step's", body)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if len(logged) != 1 || !reflect.DeepEqual(logged[0].Warnings, want) {
		t.Errorf("logged warnings = %+v", logged)
	}
}

// r74streamEndTelemetry records the telemetry end event.
type r74streamEndTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	ends []telemetry.TelemetryFinishEvent
}

func (r *r74streamEndTelemetry) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	r.ends = append(r.ends, e)
	r.mu.Unlock()
}

// TS onEnd (and the telemetry onEnd, the same event) reports the final
// step's finish reason (`finishReason: finalStep.finishReason`), while
// result.finishReason is the recorded call-level one (`recordedFinishReason
// ?? 'other'`). After a continuation step that fails to open (TS 4358: no
// `finish` part) the result says "other" and onEnd says the recorded step's
// "tool-calls".
func TestR74StreamOnEndFinishReasonIsFinalStepAfterContinuationFailure(t *testing.T) {
	tel := &r74streamEndTelemetry{}
	var mu sync.Mutex
	var ends []OnFinishEvent
	zero := 0
	r, err := StreamText(context.Background(), StreamTextOptions{
		OnError: func(context.Context, error) {},
		Model:   r70ToolCallModelThen(func() (provider.TextStream, error) { return nil, errors.New("provider failure") }),
		Prompt:  "test-input", Tools: []types.Tool{r70Tool("tool1", nil)}, MaxRetries: &zero, StopWhen: []StopCondition{StepCountIs(3)},
		Telemetry:  &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if r.FinishReason() != types.FinishReasonOther {
		t.Fatalf("FinishReason() = %q, want other", r.FinishReason())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 || ends[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("onEnd = %+v, want finish reason tool-calls", ends)
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.ends) != 1 || tel.ends[0].FinishReason != string(types.FinishReasonToolCalls) {
		t.Fatalf("telemetry onEnd = %+v, want finish reason tool-calls", tel.ends)
	}
}

// TS onEnd takes runtimeContext and toolsContext from the final recorded step
// (finalStep.runtimeContext / finalStep.toolsContext): a PrepareStep that set
// new contexts for a continuation step that then failed to open does not
// reach onEnd or the telemetry end.
func TestR74StreamOnEndContextsAreFinalStepsAfterContinuationFailure(t *testing.T) {
	tel := &r74streamEndTelemetry{}
	var mu sync.Mutex
	var ends []OnFinishEvent
	zero := 0
	r, err := StreamText(context.Background(), StreamTextOptions{
		OnError: func(context.Context, error) {},
		Model:   r70ToolCallModelThen(func() (provider.TextStream, error) { return nil, errors.New("provider failure") }),
		Prompt:  "test-input", Tools: []types.Tool{r70Tool("tool1", nil)}, MaxRetries: &zero, StopWhen: []StopCondition{StepCountIs(3)},
		RuntimeContext: map[string]interface{}{"state": "initial"},
		ToolsContext:   map[string]interface{}{"tool1": map[string]interface{}{"k": "initial"}},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			if s.StepNumber == 1 {
				s.RuntimeContext = map[string]interface{}{"state": "step-2"}
				s.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"k": "step-2"}}
			}
			return s
		},
		Telemetry:  &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{tel}, IncludeRuntimeContext: map[string]bool{"state": true}},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	mu.Lock()
	defer mu.Unlock()
	want := map[string]interface{}{"state": "initial"}
	wantTools := map[string]interface{}{"tool1": map[string]interface{}{"k": "initial"}}
	if len(ends) != 1 || !reflect.DeepEqual(ends[0].RuntimeContext, want) || !reflect.DeepEqual(ends[0].ExperimentalContext, want) || !reflect.DeepEqual(ends[0].ToolsContext, wantTools) {
		t.Fatalf("onEnd contexts = %+v", ends)
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.ends) != 1 || !reflect.DeepEqual(tel.ends[0].RuntimeContext, want) {
		t.Fatalf("telemetry onEnd runtime context = %+v", tel.ends)
	}
}

// The deprecated ExperimentalContext alias carries the same runtime context
// as RuntimeContext on every StreamText event, also after a PrepareStep
// replaced it (as GenerateText does): step start, tool execution, step end
// and end.
func TestR74StreamExperimentalContextFollowsPreparedRuntimeContext(t *testing.T) {
	var mu sync.Mutex
	var pairs [][2]interface{}
	record := func(exp, rt interface{}) { mu.Lock(); pairs = append(pairs, [2]interface{}{exp, rt}); mu.Unlock() }
	r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, r72streamToolCallStep, r72streamToolCallStep, r72streamHelloStep), Prompt: "test-input",
		Tools: []types.Tool{r71streamTool1(true, nil, nil)}, StopWhen: []StopCondition{StepCountIs(3)},
		RuntimeContext: map[string]interface{}{"state": "initial"},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			s.RuntimeContext = map[string]interface{}{"state": fmt.Sprintf("step-%d", s.StepNumber)}
			return s
		},
		OnStepStart:          func(_ context.Context, e OnStepStartEvent) { record(e.ExperimentalContext, e.RuntimeContext) },
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) { record(e.ExperimentalContext, e.RuntimeContext) },
		OnStepFinishEvent:    func(_ context.Context, e OnStepFinishEvent) { record(e.ExperimentalContext, e.RuntimeContext) },
		OnEndEvent:           func(_ context.Context, e OnFinishEvent) { record(e.ExperimentalContext, e.RuntimeContext) },
	})
	mu.Lock()
	defer mu.Unlock()
	if len(pairs) < 9 {
		t.Fatalf("events = %d", len(pairs))
	}
	for i, p := range pairs {
		if !reflect.DeepEqual(p[0], p[1]) {
			t.Errorf("event %d: ExperimentalContext %v, RuntimeContext %v", i, p[0], p[1])
		}
	}
}
