package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Reviewer item 5 (r80): TS getToolPartInputSchemaInput uses hasOwnProperty,
// so `"inputSchemaInput": null` is validated (null against the tool input
// schema) instead of the part input, and it survives serialization.
func TestR80StreamApprovalInputSchemaInputNull(t *testing.T) {
	countTool := types.Tool{
		Name: "count",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"count"},
		},
	}
	_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true,"inputSchemaInput":null}}
		]}]`),
		Tools: []types.Tool{countTool},
	})
	if err == nil {
		t.Fatal("null inputSchemaInput validated the part input instead of null")
	}
	if !strings.Contains(err.Error(), "messages[0].parts[0].input") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Absent key: the part input is validated (and passes).
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true}}
		]}]`),
		Tools: []types.Tool{countTool},
	}); err != nil {
		t.Fatalf("absent inputSchemaInput: %v", err)
	}

	// Round trip keeps the null key; an absent key stays absent.
	for _, tc := range []struct{ in, want string }{
		{`{"id":"a","approved":true,"inputSchemaInput":null}`, `{"id":"a","approved":true,"inputSchemaInput":null}`},
		{`{"id":"a","approved":true}`, `{"id":"a","approved":true}`},
		{`{"id":"a","inputSchemaInput":{"x":1}}`, `{"id":"a","inputSchemaInput":{"x":1}}`},
	} {
		var a ToolUIPartApproval
		if err := json.Unmarshal([]byte(tc.in), &a); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("round trip %s: got %s, want %s", tc.in, got, tc.want)
		}
	}
}

// r80streamTextStream runs StreamText and returns ToTextStream over its
// full stream (TS result.textStream).
func r80streamTextStream(t *testing.T, steps []provider.StreamChunk) []string {
	t.Helper()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	out, errs := ToTextStream(context.Background(), r.FullStream())
	got := []string{}
	for s := range out {
		got = append(got, s)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	return got
}

// stream-text.test.ts 555 / 591 / 619: result.textStream sends the text
// deltas, drops empty ones, and leaves reasoning out.
func TestR80StreamTextStream(t *testing.T) {
	if got := r80streamTextStream(t, r71streamHelloWorld); !reflect.DeepEqual(got, []string{"Hello", ", ", "world!"}) {
		t.Fatalf("555 textStream = %q", got)
	}
	var withEmpty []provider.StreamChunk
	withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"})
	for _, d := range []string{"", "Hello", "", ", ", "", "world!", ""} {
		withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}, r77streamFinishStop())
	if got := r80streamTextStream(t, withEmpty); !reflect.DeepEqual(got, []string{"Hello", ", ", "world!"}) {
		t.Fatalf("591 textStream = %q", got)
	}
	if got := r80streamTextStream(t, r74streamReasoningSteps()); !reflect.DeepEqual(got, []string{"Hi", " there!"}) {
		t.Fatalf("619 textStream = %q", got)
	}
}

// r80streamPartsJSON extends r77streamPartsJSON with the reasoning and
// tool-call fields and the text/reasoning providerMetadata of the TS
// snapshots.
func r80streamPartsJSON(parts []provider.StreamChunk) []map[string]interface{} {
	out := r77streamPartsJSON(parts, false)
	for i, p := range parts {
		m := out[i]
		switch p.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
			m["id"] = p.ID
		case provider.ChunkTypeReasoning:
			m["id"], m["text"] = p.ID, p.Reasoning
		case provider.ChunkTypeToolCall:
			c := p.ToolCall
			m["toolCallId"], m["toolName"], m["input"], m["title"] = c.ID, c.ToolName, c.Arguments, c.Title
			if c.ProviderExecuted {
				m["providerExecuted"] = true
			}
			if len(c.ProviderMetadata) > 0 {
				m["providerMetadata"] = c.ProviderMetadata
			}
		}
		switch p.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd, provider.ChunkTypeReasoning,
			provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd, provider.ChunkTypeText:
			if len(p.ProviderMetadata) > 0 {
				m["providerMetadata"] = json.RawMessage(p.ProviderMetadata)
			}
		}
	}
	return out
}

const r80streamFinishParts = `{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id"},"usage":{"inputTokens":3,"outputTokens":10,"totalTokens":13},"hasTimeBetweenOutputChunks":%s},
	{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokens":3,"outputTokens":10,"totalTokens":13}}`

// stream-text.test.ts 814: result.stream > should send reasoning deltas
// (modelWithReasoning inline snapshot: every reasoning block in order, the
// empty delta that carries a signature kept, interleaved blocks 4/5 ended
// with their own metadata, timeBetweenOutputChunksMs set).
func TestR80StreamFullStreamReasoningDeltas(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, r74streamReasoningSteps()), Prompt: "prompt"})
	sig1 := `{"testProvider":{"signature":"1234567890"}}`
	sig2 := `{"testProvider":{"signature":"0987654321"}}`
	r75streamAssertJSON(t, "parts", r80streamPartsJSON(parts), `[{"type":"start"},{"type":"start-step"},
		{"type":"reasoning-start","id":"1"},
		{"type":"reasoning-delta","id":"1","text":"I will open the conversation"},
		{"type":"reasoning-delta","id":"1","text":" with witty banter."},
		{"type":"reasoning-delta","id":"1","text":"","providerMetadata":`+sig1+`},
		{"type":"reasoning-end","id":"1"},
		{"type":"reasoning-start","id":"2","providerMetadata":{"testProvider":{"redactedData":"redacted-reasoning-data"}}},
		{"type":"reasoning-end","id":"2"},
		{"type":"reasoning-start","id":"3"},
		{"type":"reasoning-delta","id":"3","text":" Once the user has relaxed,"},
		{"type":"reasoning-delta","id":"3","text":" I will pry for valuable information."},
		{"type":"reasoning-end","id":"3","providerMetadata":`+sig1+`},
		{"type":"reasoning-start","id":"4","providerMetadata":`+sig1+`},
		{"type":"reasoning-delta","id":"4","text":" I need to think about"},
		{"type":"reasoning-delta","id":"4","text":" this problem carefully."},
		{"type":"reasoning-start","id":"5","providerMetadata":`+sig1+`},
		{"type":"reasoning-delta","id":"5","text":" The best solution"},
		{"type":"reasoning-delta","id":"5","text":" requires careful"},
		{"type":"reasoning-delta","id":"5","text":" consideration of all factors."},
		{"type":"reasoning-end","id":"4","providerMetadata":`+sig2+`},
		{"type":"reasoning-end","id":"5","providerMetadata":`+sig2+`},
		{"type":"text-start","id":"1"},
		{"type":"text-delta","id":"1","text":"Hi"},
		{"type":"text-delta","id":"1","text":" there!"},
		{"type":"text-end","id":"1","providerMetadata":`+sig2+`},
		`+strings.Replace(r80streamFinishParts, "%s", "true", 1)+`]`)
}

// stream-text.test.ts 1734: result.stream > should send tool calls (the
// wire tools and tool choice, the parsed input, the call's provider
// metadata and the tool title; a tool without execute ends the run).
func TestR80StreamFullStreamToolCalls(t *testing.T) {
	var got *provider.GenerateOptions
	model := r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`, ProviderMetadata: map[string]interface{}{"testProvider": map[string]interface{}{"signature": "sig"}}}},
		r77streamFinishStop(),
	})
	inner := model.DoStreamFunc
	model.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		got = o
		return inner(ctx, o)
	}
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: model, Prompt: "test-input",
		Tools:      []types.Tool{{Name: "tool1", Title: "Tool 1", Parameters: r71streamValueSchema}},
		ToolChoice: types.RequiredToolChoice(),
	})
	if got == nil || len(got.Tools) != 1 || got.Tools[0].Name != "tool1" || !reflect.DeepEqual(got.Tools[0].Parameters, r71streamValueSchema) {
		t.Fatalf("tools = %+v", got)
	}
	if got.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("tool choice = %+v", got.ToolChoice)
	}
	r75streamAssertJSON(t, "parts", r80streamPartsJSON(parts), `[{"type":"start"},{"type":"start-step"},
		{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"},"title":"Tool 1","providerMetadata":{"testProvider":{"signature":"sig"}}},
		`+strings.Replace(r80streamFinishParts, "%s", "false", 1)+`]`)
}

// stream-text.test.ts 2281: result.stream > should refine tool input before
// tool execution, stream parts, and callbacks (the refined input reaches
// Execute, the tool-call and tool-result parts, the model-call-end content
// and the tool execution start event).
func TestR80StreamRefineToolInputEverywhere(t *testing.T) {
	var (
		mu        sync.Mutex
		execInput map[string]interface{}
		endEvents []LanguageModelCallEndEvent
		starts    []OnToolCallStartEvent
	)
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		mu.Lock()
		execInput = args
		mu.Unlock()
		return "result:" + args["value"].(string), nil
	}}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": " raw " }`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}, r71streamHelloWorld),
		Prompt: "test-input",
		Tools:  []types.Tool{tool},
		ExperimentalRefineToolInput: map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
			return map[string]interface{}{"value": strings.TrimSpace(o.ToolCall.Arguments["value"].(string))}, nil
		}},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			endEvents = append(endEvents, e)
			mu.Unlock()
		},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			starts = append(starts, e)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	want := map[string]interface{}{"value": "raw"}
	if !reflect.DeepEqual(execInput, want) {
		t.Fatalf("execute input = %v", execInput)
	}
	var sawCall, sawResult bool
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeToolCall:
			sawCall = p.ToolCall.ID == "call-1" && p.ToolCall.ToolName == "tool1" && reflect.DeepEqual(p.ToolCall.Arguments, want)
		case provider.ChunkTypeToolResult:
			sawResult = p.ToolResult.ToolCallID == "call-1" && p.ToolResult.ToolName == "tool1" && reflect.DeepEqual(p.ToolResult.Input, want) && p.ToolResult.Result == "result:raw"
		}
	}
	if !sawCall || !sawResult {
		t.Fatalf("tool-call refined = %v, tool-result refined = %v; parts = %v", sawCall, sawResult, r73streamNames(parts))
	}
	if len(endEvents) == 0 || len(endEvents[0].Content) == 0 {
		t.Fatalf("model call end events = %+v", endEvents)
	}
	call, ok := endEvents[0].Content[0].(types.ToolCallContent)
	if !ok {
		if p, isPtr := endEvents[0].Content[0].(*types.ToolCallContent); isPtr {
			call, ok = *p, true
		}
	}
	if !ok || !reflect.DeepEqual(call.Arguments, want) {
		t.Fatalf("model call end content[0] = %#v", endEvents[0].Content[0])
	}
	if len(starts) != 1 || !reflect.DeepEqual(starts[0].ToolCall.Arguments, want) {
		t.Fatalf("tool execution start = %+v", starts)
	}
	// The next prompt's assistant tool call carries the refined input.
	msgs := r.ResponseMessages()
	if len(msgs) == 0 || len(msgs[0].Content) == 0 {
		t.Fatalf("response messages = %+v", msgs)
	}
	if rc, ok := msgs[0].Content[0].(types.ToolCallContent); !ok || !reflect.DeepEqual(rc.Arguments, want) {
		t.Fatalf("response tool call = %#v", msgs[0].Content[0])
	}
}

// stream-text.test.ts 2629: result.stream > should send tool results (the
// execute options carry the prompt messages; tool-result has the input and
// output and dynamic false; finish-step reports the call's tool
// execution time; finish reason stop ends the run after one step).
func TestR80StreamFullStreamToolResults(t *testing.T) {
	var (
		mu       sync.Mutex
		execMsgs []types.Message
	)
	tool := types.Tool{Name: "tool1", Title: "Tool 1", Parameters: r71streamValueSchema, Execute: func(_ context.Context, args map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		mu.Lock()
		execMsgs = o.Messages
		mu.Unlock()
		return args["value"].(string) + "-result", nil
	}}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
			r77streamFinishStop(),
		}),
		Prompt: "test-input",
		Tools:  []types.Tool{tool},
	})
	mu.Lock()
	r75streamAssertJSON(t, "execute messages", r76streamRoleText(execMsgs), `[{"role":"user","content":[{"type":"text","text":"test-input"}]}]`)
	mu.Unlock()
	out := r80streamPartsJSON(parts)
	var perf *types.StepPerformance
	for i, p := range parts {
		switch p.Type {
		case provider.ChunkTypeToolResult:
			res := p.ToolResult
			// Title is not compared: TS executeToolCall builds the result
			// without the call's title, while Go's executor copies it
			// (gen's executeTools; cr-r80-stream-to-gen.md).
			out[i]["toolCallId"], out[i]["toolName"], out[i]["input"], out[i]["output"], out[i]["dynamic"] = res.ToolCallID, res.ToolName, res.Input, res.Result, res.Dynamic
		case provider.ChunkTypeFinishStep:
			perf = p.Performance
		}
	}
	r75streamAssertJSON(t, "parts", out, `[{"type":"start"},{"type":"start-step"},
		{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"},"title":"Tool 1"},
		{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"},"output":"value-result","dynamic":false},
		`+strings.Replace(r80streamFinishParts, "%s", "false", 1)+`]`)
	if perf == nil {
		t.Fatal("finish-step has no performance")
	}
	if _, ok := perf.ToolExecutionMs["call-1"]; !ok || len(perf.ToolExecutionMs) != 1 {
		t.Fatalf("toolExecutionMs = %v", perf.ToolExecutionMs)
	}
	if n := len(r.Steps()); n != 1 {
		t.Fatalf("steps = %d", n)
	}
}

// validate-ui-messages.ts: an output-error part with `"input": null` has an
// input (TS `toolPart.input !== undefined`), so null is validated against
// the tool input schema; the failed check keeps the part loadable as a
// dynamic tool part. The null input survives serialization; an absent input
// stays absent and is not validated.
func TestR80StreamOutputErrorNullInput(t *testing.T) {
	countTool := types.Tool{Name: "count", Parameters: map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"count"},
	}}
	run := func(input string) UIMessagePart {
		t.Helper()
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-count","toolCallId":"call-1","state":"output-error"` + input + `,"errorText":"boom"}]}]`),
			Tools: []types.Tool{countTool},
		})
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		return msgs[0].Parts[0]
	}
	part := run(`,"input":null`)
	if part.UIPartType() != "dynamic-tool" {
		t.Fatalf("null input: part type = %s, want dynamic-tool", part.UIPartType())
	}
	b, _ := json.Marshal(part)
	if !strings.Contains(string(b), `"input":null`) {
		t.Fatalf("null input dropped: %s", b)
	}
	if part = run(``); part.UIPartType() != "tool-count" {
		t.Fatalf("absent input: part type = %s", part.UIPartType())
	}
	if b, _ = json.Marshal(part); strings.Contains(string(b), `"input"`) {
		t.Fatalf("absent input serialized: %s", b)
	}
	// input-streaming keeps a null input too.
	var p ToolUIPart
	if err := json.Unmarshal([]byte(`{"type":"tool-count","toolCallId":"c","state":"input-streaming","input":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if b, _ = json.Marshal(p); string(b) != `{"type":"tool-count","toolCallId":"c","state":"input-streaming","input":null}` {
		t.Fatalf("input-streaming round trip: %s", b)
	}
}
