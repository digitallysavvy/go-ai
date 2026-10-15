package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays stream-text.test.ts 'options.output': structured output after an
// earlier tool step emitted text (only the final step's text is parsed), onEnd
// with a failed parse, and choice/json/array outputs.

type valueObj struct {
	Value string `json:"value"`
}

func toolThenAnswerModel(introID, answer string) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		ToolSupport:       true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: introID},
					{Type: provider.ChunkTypeText, ID: introID, Text: "Checking the value."},
					{Type: provider.ChunkTypeTextEnd, ID: introID},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "answer"},
				{Type: provider.ChunkTypeText, ID: "answer", Text: answer},
				{Type: provider.ChunkTypeTextEnd, ID: "answer"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
}

func lookupTool() types.Tool {
	return types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		},
	}
}

func TestTSReplay_StreamText_ObjectOutputAfterToolStep(t *testing.T) {
	var ended *OnFinishEvent
	var mu sync.Mutex
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  toolThenAnswerModel("intro", `{"value":"done"}`),
		Prompt: "Look up the value and return it.",
		Tools:  []types.Tool{lookupTool()},
		Output: ObjectOutput[valueObj](ObjectOutputOptions{
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}),
		}),
		StopWhen:   []StopCondition{IsStepCount(2)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ended = &e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.Output(); !reflect.DeepEqual(got, valueObj{Value: "done"}) {
		t.Fatalf("output = %#v (err %v)", got, res.OutputErr())
	}
	if got := res.PartialOutput(); !reflect.DeepEqual(got, valueObj{Value: "done"}) && !reflect.DeepEqual(got, map[string]interface{}{"value": "done"}) {
		t.Fatalf("partial output = %#v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if ended == nil || !reflect.DeepEqual(ended.Output, valueObj{Value: "done"}) {
		t.Fatalf("onEnd output = %#v", ended)
	}
}

func TestTSReplay_StreamText_ArrayOutputAfterToolStep(t *testing.T) {
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  toolThenAnswerModel("answer", `{"elements":[{"value":"done"}]}`),
		Prompt: "Look up the value and return it.",
		Tools:  []types.Tool{lookupTool()},
		Output: ArrayOutput[valueObj](ArrayOutputOptions[valueObj]{
			ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}),
		}),
		StopWhen: []StopCondition{IsStepCount(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.Output(); !reflect.DeepEqual(got, []valueObj{{Value: "done"}}) {
		t.Fatalf("output = %#v (err %v)", got, res.OutputErr())
	}
}

func TestTSReplay_StreamText_OnEndOutputUndefinedWhenParseFails(t *testing.T) {
	var ended *OnFinishEvent
	var mu sync.Mutex
	model := toolThenAnswerModel("intro", `not json`)
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "p", Tools: []types.Tool{lookupTool()},
		Output: ObjectOutput[valueObj](ObjectOutputOptions{
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}}),
		}),
		StopWhen:   []StopCondition{IsStepCount(2)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ended = &e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.ReadAll()
	if res.OutputErr() == nil {
		t.Fatal("expected a parse error from OutputErr")
	}
	mu.Lock()
	defer mu.Unlock()
	if ended == nil || ended.Output != nil {
		t.Fatalf("onEnd = %#v, want output nil", ended)
	}
}

func collectElements[E any](ch <-chan ElementStreamResult[E]) []E {
	var out []E
	for e := range ch {
		out = append(out, e.Element)
	}
	return out
}

// stream-text.test.ts 'array output': elements stream after an earlier tool
// step emitted text (output state is per step).
func TestTSReplay_ElementStream_AfterToolStep(t *testing.T) {
	elemSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}})
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:    toolThenAnswerModel("answer", `{"elements":[{"value":"done"}]}`),
		Prompt:   "Look up the value and return it.",
		Tools:    []types.Tool{lookupTool()},
		Output:   ArrayOutput[valueObj](ArrayOutputOptions[valueObj]{ElementSchema: elemSchema}),
		StopWhen: []StopCondition{IsStepCount(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectElements(ElementStream[valueObj](res, ElementStreamOptions[valueObj]{ElementSchema: elemSchema}))
	if !reflect.DeepEqual(got, []valueObj{{Value: "done"}}) {
		t.Fatalf("elements = %#v", got)
	}
}

// An element is published only once it is complete: a string value that is
// still streaming must not appear as a truncated element.
func TestElementStream_HoldsBackTheElementStillStreaming(t *testing.T) {
	elemSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}})
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(append(textChunks(`{"elements":[{"value":"do`, `ne"}`, `,{"value":"second"}]}`), finishChunk())), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectElements(ElementStream[valueObj](res, ElementStreamOptions[valueObj]{ElementSchema: elemSchema}))
	if !reflect.DeepEqual(got, []valueObj{{Value: "done"}, {Value: "second"}}) {
		t.Fatalf("elements = %#v, want complete elements only", got)
	}
}

// stream-text.test.ts 'should error elementStream when the model exceeds maxItems'.
func TestTSReplay_ElementStream_MaxItems(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(append(textChunks(`{"elements":[`, `"element 1",`, `"element 2",`, `"element 3"]}`), finishChunk())), nil
		},
	}
	maxItems := 2
	output := ArrayOutput[string](ArrayOutputOptions[string]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"}),
		MaxItems:      &maxItems,
	})
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	got := collectElements(ElementStreamWithOutput(res, output))
	if !reflect.DeepEqual(got, []string{"element 1", "element 2"}) {
		t.Fatalf("elements = %#v", got)
	}
	var streamErr error
	got = collectElements(ElementStream[string](mustStream(t, model, output), ElementStreamOptions[string]{
		ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"}),
		MaxItems:      &maxItems,
		OnError:       func(e error) { streamErr = e },
	}))
	if !reflect.DeepEqual(got, []string{"element 1", "element 2"}) {
		t.Fatalf("elements = %#v", got)
	}
	if streamErr == nil || streamErr.Error() != "elements array must contain at most 2 items" {
		t.Fatalf("error = %v", streamErr)
	}
}

func mustStream(t *testing.T, model provider.LanguageModel, output interface{}) *StreamTextResult {
	t.Helper()
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// stream-text.test.ts 'mixed multi content streaming with interleaving parts'
// ('mixed text and reasoning blocks'): blocks are recorded per id, in start order.
func TestTSReplay_StreamText_InterleavedTextAndReasoningBlocks(t *testing.T) {
	ch := func(ty provider.ChunkType, id, text string) provider.StreamChunk {
		return provider.StreamChunk{Type: ty, ID: id, Text: text}
	}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				ch(provider.ChunkTypeReasoningStart, "0", ""),
				ch(provider.ChunkTypeTextStart, "1", ""),
				ch(provider.ChunkTypeReasoning, "0", "Thinking..."),
				ch(provider.ChunkTypeText, "1", "Hello"),
				ch(provider.ChunkTypeText, "1", ", "),
				ch(provider.ChunkTypeTextStart, "2", ""),
				ch(provider.ChunkTypeText, "2", "This "),
				ch(provider.ChunkTypeText, "2", "is "),
				ch(provider.ChunkTypeReasoningStart, "3", ""),
				ch(provider.ChunkTypeReasoning, "0", "I'm thinking..."),
				ch(provider.ChunkTypeReasoning, "3", "Separate thoughts"),
				ch(provider.ChunkTypeText, "2", "a"),
				ch(provider.ChunkTypeText, "1", "world!"),
				ch(provider.ChunkTypeReasoningEnd, "0", ""),
				ch(provider.ChunkTypeText, "2", " test."),
				ch(provider.ChunkTypeTextEnd, "2", ""),
				ch(provider.ChunkTypeReasoningEnd, "3", ""),
				ch(provider.ChunkTypeTextEnd, "1", ""),
				finishChunk(),
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	s := res.Stream()
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeTextStart, provider.ChunkTypeReasoningEnd, provider.ChunkTypeTextEnd, provider.ChunkTypeText, provider.ChunkTypeReasoning:
			ids = append(ids, string(c.Type)+":"+c.ID+":"+c.Text)
		}
	}
	wantIDs := []string{
		"reasoning-start:0:", "text-start:1:", "reasoning:0:Thinking...", "text:1:Hello", "text:1:, ",
		"text-start:2:", "text:2:This ", "text:2:is ", "reasoning-start:3:", "reasoning:0:I'm thinking...",
		"reasoning:3:Separate thoughts", "text:2:a", "text:1:world!", "reasoning-end:0:", "text:2: test.",
		"text-end:2:", "reasoning-end:3:", "text-end:1:",
	}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("full stream block parts:\n got %q\nwant %q", ids, wantIDs)
	}

	type part struct{ Kind, Text string }
	var got []part
	for _, c := range res.Content() {
		switch p := c.(type) {
		case types.TextContent:
			got = append(got, part{"text", p.Text})
		case types.ReasoningContent:
			got = append(got, part{"reasoning", p.Text})
		}
	}
	want := []part{{"reasoning", "Thinking...I'm thinking..."}, {"text", "Hello, world!"}, {"text", "This is a test."}, {"reasoning", "Separate thoughts"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("content = %#v, want %#v", got, want)
	}
	if res.Text() != "Hello, world!This is a test." {
		t.Fatalf("text = %q", res.Text())
	}
}

// stream-text.test.ts 'dynamic tools': tool-input-start, tool-call and
// tool-result carry dynamic: true, and the result lands in dynamicToolResults.
func TestTSReplay_StreamText_DynamicTool(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool"}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`, ToolCall: &types.ToolCall{ID: "call-1"}},
				{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool", Arguments: map[string]interface{}{"value": "value"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
			}), nil
		},
	}
	tool := types.Tool{
		Name: "dynamicTool", Type: types.ToolTypeDynamic,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"value": "test-result"}, nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	dyn := map[provider.ChunkType]bool{}
	s := res.Stream()
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolCall:
			dyn[c.Type] = c.ToolCall != nil && c.ToolCall.Dynamic
		case provider.ChunkTypeToolResult:
			dyn[c.Type] = c.ToolResult != nil && c.ToolResult.Dynamic
		}
	}
	for _, ty := range []provider.ChunkType{provider.ChunkTypeToolInputStart, provider.ChunkTypeToolCall, provider.ChunkTypeToolResult} {
		if !dyn[ty] {
			t.Errorf("%s: dynamic = false, want true", ty)
		}
	}
	if got := res.DynamicToolResults(); len(got) != 1 {
		t.Fatalf("dynamic tool results = %d, want 1", len(got))
	}
	if got := res.StaticToolResults(); len(got) != 0 {
		t.Fatalf("static tool results = %d, want 0", len(got))
	}
	var contentDynamic bool
	for _, p := range res.Content() {
		if tr, ok := p.(types.ToolResultContent); ok {
			contentDynamic = tr.Dynamic
		}
	}
	if !contentDynamic {
		t.Fatal("tool-result content part is not dynamic")
	}

	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "dynamicTool", Arguments: map[string]interface{}{"value": "value"}}}}, nil
		}},
		Prompt: "p", Tools: []types.Tool{tool}, MaxSteps: intPtr(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gen.DynamicToolResults) != 1 || len(gen.StaticToolResults) != 0 {
		t.Fatalf("generateText dynamic=%d static=%d", len(gen.DynamicToolResults), len(gen.StaticToolResults))
	}
}

// stream-text.test.ts 'provider-executed tools': single step, content in call
// order (call, result, call, error).
func TestTSReplay_StreamText_ProviderExecutedToolCallAndResult(t *testing.T) {
	args := map[string]interface{}{"value": "value"}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`, ToolCall: &types.ToolCall{ID: "call-1"}},
				{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", Arguments: args, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web_search", Result: `{ "value": "result1" }`, ProviderExecuted: true, ProviderMetadata: map[string]interface{}{"provider": map[string]interface{}{"itemId": "result-1"}}}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "web_search", Arguments: args, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-2", ToolName: "web_search", Error: errors.New("ERROR"), ProviderExecuted: true, ProviderMetadata: map[string]interface{}{"provider": map[string]interface{}{"itemId": "error-1"}}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
	tool := types.Tool{
		Name: "web_search", Type: "provider", ProviderExecuted: true,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(4)}})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, c := range res.Content() {
		switch p := c.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "tool-call:"+p.ToolCallID)
			if !p.ProviderExecuted {
				t.Errorf("tool-call %s not providerExecuted", p.ToolCallID)
			}
		case types.ToolResultContent:
			kinds = append(kinds, "tool-result:"+p.ToolCallID)
			if !p.ProviderExecuted || p.Input["value"] != "value" || len(p.ProviderMetadata) == 0 {
				t.Errorf("tool-result %+v lacks providerExecuted/input/metadata", p)
			}
		case types.ToolErrorContent:
			kinds = append(kinds, "tool-error:"+p.ToolCallID)
			if !p.ProviderExecuted || p.Input["value"] != "value" {
				t.Errorf("tool-error %+v lacks providerExecuted/input", p)
			}
		default:
			kinds = append(kinds, "other")
		}
	}
	want := []string{"tool-call:call-1", "tool-result:call-1", "tool-call:call-2", "tool-error:call-2"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("content = %v, want %v", kinds, want)
	}
	if n := len(res.Steps()); n != 1 {
		t.Fatalf("steps = %d, want 1 (provider-executed results do not continue the loop)", n)
	}
}

// stream-text.test.ts 'provider-executed tool error with structured object
// preserves error in ui message stream'.
func TestTSReplay_StreamText_ProviderExecutedStructuredErrorInUIStream(t *testing.T) {
	structured := map[string]interface{}{"type": "web_fetch_tool_result_error", "errorCode": "url_not_accessible"}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_fetch", Arguments: map[string]interface{}{"url": "https://example.com"}, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web_fetch", Result: structured, Error: errors.New("url_not_accessible"), ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
	tool := types.Tool{Name: "web_fetch", Type: "provider", ProviderExecuted: true, Parameters: map[string]interface{}{"type": "object"}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "p", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := res.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{OnError: func(error) string { return "Oops, an error occurred!" }})
	var errorText string
	for c := range chunks {
		if c["type"] == "tool-output-error" {
			errorText, _ = c["errorText"].(string)
		}
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(errorText), &parsed); err != nil || !reflect.DeepEqual(parsed, structured) {
		t.Fatalf("errorText = %q (%v), want the structured error as JSON", errorText, err)
	}
	// The assistant content keeps the structured error value for replay.
	var kept interface{}
	for _, p := range res.Content() {
		if te, ok := p.(types.ToolErrorContent); ok {
			kept = te.Error
		}
	}
	if !reflect.DeepEqual(kept, structured) {
		t.Fatalf("tool-error content error = %#v, want the structured error", kept)
	}
}

// stream-text.test.ts 'provider-executed tool result replay across steps': the
// next step's prompt keeps the provider call and result (with their provider
// metadata) in the assistant message and the local result in the tool message.
func TestTSReplay_StreamText_ProviderExecutedResultReplayedInNextPrompt(t *testing.T) {
	var prompts []types.Prompt
	var mu sync.Mutex
	calls := 0
	toolSearchResult := map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "function", "name": "get_weather"}}}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			prompts = append(prompts, o.Prompt)
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "tool-search-call-1", ToolName: "toolSearch", Arguments: map[string]interface{}{"arguments": map[string]interface{}{"paths": []interface{}{"get_weather"}}, "call_id": nil}, ProviderExecuted: true, ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "tsc_123"}}}},
					{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "tool-search-call-1", ToolName: "toolSearch", Result: toolSearchResult, ProviderExecuted: true, ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "tso_123"}}}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "get_weather", Arguments: map[string]interface{}{"location": "San Francisco, CA"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			}
			return testutil.NewMockTextStream(append(textChunks("Sunny."), finishChunk())), nil
		},
	}
	tools := []types.Tool{
		{Name: "toolSearch", Type: "provider", ProviderExecuted: true, Parameters: map[string]interface{}{"type": "object"}},
		{Name: "get_weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "sunny", nil
			}},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", Tools: tools, StopWhen: []StopCondition{IsStepCount(3)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("model calls = %d, want 2", len(prompts))
	}
	msgs := prompts[1].Messages
	if len(msgs) != 3 || msgs[1].Role != types.RoleAssistant || msgs[2].Role != types.RoleTool {
		t.Fatalf("second prompt roles = %v", func() []types.MessageRole {
			var r []types.MessageRole
			for _, m := range msgs {
				r = append(r, m.Role)
			}
			return r
		}())
	}
	var sawCall, sawResult, sawLocalCall bool
	for _, p := range msgs[1].Content {
		switch c := p.(type) {
		case types.ToolCallContent:
			if c.ToolCallID == "tool-search-call-1" && c.ProviderExecuted && c.ProviderOptions["openai"] != nil {
				sawCall = true
			}
			if c.ToolCallID == "call-2" && !c.ProviderExecuted {
				sawLocalCall = true
			}
		case types.ToolResultContent:
			if c.ToolCallID == "tool-search-call-1" && c.Output != nil && c.ProviderOptions["openai"] != nil {
				sawResult = true
			}
		}
	}
	if !sawCall || !sawResult || !sawLocalCall {
		t.Fatalf("assistant message lost provider call/result metadata: call=%v result=%v local=%v: %#v", sawCall, sawResult, sawLocalCall, msgs[1].Content)
	}
}
