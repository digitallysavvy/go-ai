package ai

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

// Ports of the generate-text.test.ts callback-payload cases (options.onStart,
// onStepStart, onToolExecutionStart/End, onEnd) at e64ed82f27 with
// field-by-field assertions.

func r70Int64(v int64) *int64 { return &v }

// r70TestUsage is TS testUsage (3 input / 10 output / 13 total).
func r70TestUsage() types.Usage {
	return types.Usage{InputTokens: r70Int64(3), OutputTokens: r70Int64(10), TotalTokens: r70Int64(13)}
}

// r70Model is TS MockLanguageModelV4 (provider mock-provider, model
// mock-model-id) answering each doGenerate call from responses in order (the
// last response repeats).
func r70Model(responses ...func() *types.GenerateResult) *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider",
		ModelName:    "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			i := n
			n++
			mu.Unlock()
			if i >= len(responses) {
				i = len(responses) - 1
			}
			return responses[i](), nil
		},
	}
}

func r70Text(text string) func() *types.GenerateResult {
	return func() *types.GenerateResult {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.TextContent{Text: text}},
			FinishReason: types.FinishReasonStop,
			Usage:        r70TestUsage(),
		}
	}
}

func r70ToolCalls(calls ...types.ToolCall) func() *types.GenerateResult {
	return func() *types.GenerateResult {
		content := make([]types.ContentPart, 0, len(calls))
		for _, c := range calls {
			in, _ := json.Marshal(c.Arguments)
			content = append(content, types.ToolCallContent{ToolCallID: c.ID, ToolName: c.ToolName, Input: string(in), Arguments: c.Arguments})
		}
		return &types.GenerateResult{
			Content:      content,
			ToolCalls:    calls,
			FinishReason: types.FinishReasonToolCalls,
			Usage:        r70TestUsage(),
		}
	}
}

func r70Call(id, value string) types.ToolCall {
	return types.ToolCall{ID: id, ToolName: "tool1", Arguments: map[string]interface{}{"value": value}}
}

// r70Tool1 is TS tool1: input {value: string}, execute `${value}-result`.
func r70Tool1() types.Tool {
	return types.Tool{
		Name: "tool1",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required":   []string{"value"},
		},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return input["value"].(string) + "-result", nil
		},
	}
}

// r70Settings is TS defaultSettings(): prompt "prompt" and the fixed call ID.
func r70Settings(opts GenerateTextOptions) GenerateTextOptions {
	if opts.Prompt == "" && len(opts.Messages) == 0 {
		opts.Prompt = "prompt"
	}
	opts.Internal = &InternalOptions{
		GenerateID:     func() string { return "test-call-id" },
		GenerateCallID: func() string { return "test-telemetry-call-id" },
	}
	return opts
}

func r70UserMessage(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

func r70MessageTexts(t *testing.T, label string, msgs []types.Message, want []types.Message) {
	t.Helper()
	if len(msgs) != len(want) {
		t.Fatalf("%s: %d messages, want %d: %#v", label, len(msgs), len(want), msgs)
	}
	for i := range want {
		if msgs[i].Role != want[i].Role {
			t.Errorf("%s[%d].Role = %q, want %q", label, i, msgs[i].Role, want[i].Role)
		}
		if !reflect.DeepEqual(msgs[i].Content, want[i].Content) {
			t.Errorf("%s[%d].Content = %#v, want %#v", label, i, msgs[i].Content, want[i].Content)
		}
	}
}

// options.onStart > should send correct information with system and messages
// (and should pass runtimeContext, should be called before doGenerate).
func TestR70Gen_OnStart_SystemAndMessages(t *testing.T) {
	var order []string
	var mu sync.Mutex
	model := r70Model(func() *types.GenerateResult {
		mu.Lock()
		order = append(order, "doGenerate")
		mu.Unlock()
		return r70Text("Hello!")()
	})
	maxTokens, temp := 100, 0.5
	instructions := "you are a helpful assistant"
	var ev OnStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:          model,
		Instructions:   &instructions,
		Messages:       []types.Message{r70UserMessage("test-message")},
		MaxTokens:      &maxTokens,
		Temperature:    &temp,
		RuntimeContext: map[string]interface{}{"userId": "test-user", "sessionId": "123"},
		OnStart: func(_ context.Context, e OnStartEvent) {
			mu.Lock()
			order = append(order, "onStart")
			mu.Unlock()
			ev = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Provider != "mock-provider" || ev.ModelID != "mock-model-id" {
		t.Errorf("provider/modelId = %q/%q", ev.Provider, ev.ModelID)
	}
	if ev.Instructions != instructions {
		t.Errorf("Instructions = %q", ev.Instructions)
	}
	r70MessageTexts(t, "Messages", ev.Messages, []types.Message{r70UserMessage("test-message")})
	if ev.MaxTokens == nil || *ev.MaxTokens != 100 {
		t.Errorf("MaxTokens = %v, want 100", ev.MaxTokens)
	}
	if ev.Temperature == nil || *ev.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", ev.Temperature)
	}
	if ev.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", ev.MaxRetries)
	}
	if !reflect.DeepEqual(ev.RuntimeContext, map[string]interface{}{"userId": "test-user", "sessionId": "123"}) {
		t.Errorf("RuntimeContext = %#v", ev.RuntimeContext)
	}
	if !reflect.DeepEqual(order, []string{"onStart", "doGenerate"}) {
		t.Errorf("order = %v", order)
	}
}

// options.onStepStart > should be called with correct data for a single step;
// should provide empty steps array on first step; should pass runtimeContext;
// should be called before doGenerate on each step.
func TestR70Gen_OnStepStart_SingleStep(t *testing.T) {
	var order []string
	var mu sync.Mutex
	model := r70Model(func() *types.GenerateResult {
		mu.Lock()
		order = append(order, "doGenerate")
		mu.Unlock()
		return r70Text("Hello!")()
	})
	var events []OnStepStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:          model,
		Prompt:         "test-input",
		RuntimeContext: map[string]interface{}{"userId": "test-user", "requestId": "req-123"},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			order = append(order, "onStepStart")
			mu.Unlock()
			events = append(events, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d step start events, want 1", len(events))
	}
	e := events[0]
	if e.StepNumber != 0 || len(e.Steps) != 0 {
		t.Errorf("StepNumber=%d len(Steps)=%d, want 0/0", e.StepNumber, len(e.Steps))
	}
	if e.Provider != "mock-provider" || e.ModelID != "mock-model-id" {
		t.Errorf("provider/modelId = %q/%q", e.Provider, e.ModelID)
	}
	r70MessageTexts(t, "Messages", e.Messages, []types.Message{r70UserMessage("test-input")})
	if !reflect.DeepEqual(e.RuntimeContext, map[string]interface{}{"userId": "test-user", "requestId": "req-123"}) {
		t.Errorf("RuntimeContext = %#v", e.RuntimeContext)
	}
	if !reflect.DeepEqual(order, []string{"onStepStart", "doGenerate"}) {
		t.Errorf("order = %v", order)
	}
}

// options.onStepStart > should provide previous step results in steps array
// for subsequent steps; should be called once per step in a multi-step tool
// loop; should provide steps with correct text content from prior steps.
func TestR70Gen_OnStepStart_PreviousSteps(t *testing.T) {
	step0 := func() *types.GenerateResult {
		r := r70ToolCalls(r70Call("call-1", "step0"))()
		r.Content = append([]types.ContentPart{types.TextContent{Text: "Thinking..."}}, r.Content...)
		return r
	}
	model := r70Model(step0, r70ToolCalls(r70Call("call-2", "step1")), r70Text("Final answer."))
	var events []OnStepStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{r70Tool1()},
		StopWhen:    []StopCondition{IsStepCount(4)},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("%d step start events, want 3", len(events))
	}
	for i, e := range events {
		if e.StepNumber != i || len(e.Steps) != i {
			t.Errorf("event %d: StepNumber=%d len(Steps)=%d", i, e.StepNumber, len(e.Steps))
		}
	}
	s := events[1].Steps[0]
	if s.FinishReason != types.FinishReasonToolCalls || len(s.ToolCalls) != 1 || s.ToolCalls[0].ToolName != "tool1" {
		t.Errorf("step 0 = %+v", s)
	}
	if len(s.ToolResults) != 1 || s.ToolResults[0].Result != "step0-result" {
		t.Errorf("step 0 tool results = %+v", s.ToolResults)
	}
	if s.Text != "Thinking..." {
		t.Errorf("step 0 text = %q, want Thinking...", s.Text)
	}
	if events[2].Steps[0].FinishReason != types.FinishReasonToolCalls || events[2].Steps[1].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("step 2 previous finish reasons = %q/%q", events[2].Steps[0].FinishReason, events[2].Steps[1].FinishReason)
	}
	if r := events[2].Steps[1].ToolResults; len(r) != 1 || r[0].Result != "step1-result" {
		t.Errorf("step 1 tool results = %+v", r)
	}
}

// options.onStepStart > should pass updated runtimeContext from prepareStep.
func TestR70Gen_OnStepStart_PrepareStepRuntimeContext(t *testing.T) {
	model := r70Model(r70ToolCalls(r70Call("call-1", "test")), r70Text("Done."))
	var events []OnStepStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{r70Tool1()},
		RuntimeContext: map[string]interface{}{"counter": 0},
		StopWhen:       []StopCondition{IsStepCount(3)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			rc := step.RuntimeContext.(map[string]interface{})
			step.RuntimeContext = map[string]interface{}{"counter": rc["counter"].(int) + 1, "stepNumber": step.StepNumber}
			return step
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("%d events", len(events))
	}
	for i, want := range []map[string]interface{}{{"counter": 1, "stepNumber": 0}, {"counter": 2, "stepNumber": 1}} {
		if !reflect.DeepEqual(events[i].RuntimeContext, want) {
			t.Errorf("event %d RuntimeContext = %#v, want %#v", i, events[i].RuntimeContext, want)
		}
	}
}

// options.onStepStart > should pass updated toolsContext from prepareStep.
func TestR70Gen_OnStepStart_PrepareStepToolsContext(t *testing.T) {
	model := r70Model(r70ToolCalls(r70Call("call-1", "test")), r70Text("Done."))
	var recorded interface{}
	tool := r70Tool1()
	tool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"label": map[string]interface{}{"type": "string"}}, "required": []string{"label"},
	})
	tool.Execute = func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		recorded = o.ToolContext
		return "test-result", nil
	}
	var prepared []map[string]interface{}
	var events []OnStepStartEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{tool},
		ToolsContext: map[string]interface{}{"tool1": map[string]interface{}{"label": "initial"}},
		StopWhen:     []StopCondition{IsStepCount(3)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			prepared = append(prepared, step.ToolsContext)
			if step.StepNumber == 0 {
				step.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"label": "updated"}}
			}
			return step
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := map[string]interface{}{"tool1": map[string]interface{}{"label": "initial"}}
	updated := map[string]interface{}{"tool1": map[string]interface{}{"label": "updated"}}
	if !reflect.DeepEqual(prepared, []map[string]interface{}{initial, updated}) {
		t.Errorf("prepareStep toolsContexts = %#v", prepared)
	}
	if len(events) != 2 || !reflect.DeepEqual(events[0].ToolsContext, updated) || !reflect.DeepEqual(events[1].ToolsContext, updated) {
		t.Errorf("step start toolsContexts = %#v", events)
	}
	if !reflect.DeepEqual(recorded, map[string]interface{}{"label": "updated"}) {
		t.Errorf("tool context = %#v", recorded)
	}
}

// options.onToolExecutionStart > should use experimental_onToolCallStart as a
// fallback; should prefer onToolExecutionStart over experimental_onToolCallStart.
// options.onToolExecutionEnd: the same for experimental_onToolCallFinish.
// TS resolves `onToolExecutionStart ?? experimental_onToolCallStart`: only one
// of the two runs.
func TestR70Gen_ToolExecutionCallbacks_DeprecatedFallback(t *testing.T) {
	run := func(t *testing.T, setNew bool) []string {
		var mu sync.Mutex
		var calls []string
		record := func(name string) {
			mu.Lock()
			calls = append(calls, name)
			mu.Unlock()
		}
		opts := GenerateTextOptions{
			Model: r70Model(r70ToolCalls(r70Call("call-1", "test"))),
			Tools: []types.Tool{r70Tool1()},
			OnToolCallStart: func(context.Context, OnToolCallStartEvent) {
				record("experimental_onToolCallStart")
			},
			OnToolCallFinish: func(context.Context, OnToolCallFinishEvent) {
				record("experimental_onToolCallFinish")
			},
		}
		if setNew {
			opts.OnToolExecutionStart = func(context.Context, OnToolCallStartEvent) { record("onToolExecutionStart") }
			opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { record("onToolExecutionEnd") }
		}
		if _, err := GenerateText(context.Background(), r70Settings(opts)); err != nil {
			t.Fatal(err)
		}
		return calls
	}
	if got := run(t, false); !reflect.DeepEqual(got, []string{"experimental_onToolCallStart", "experimental_onToolCallFinish"}) {
		t.Errorf("fallback only: calls = %v", got)
	}
	if got := run(t, true); !reflect.DeepEqual(got, []string{"onToolExecutionStart", "onToolExecutionEnd"}) {
		t.Errorf("both set: calls = %v, want only the new callbacks", got)
	}
}

func r70CheckToolCall(t *testing.T, label string, got types.ToolCall, id, value string) {
	t.Helper()
	if got.ID != id || got.ToolName != "tool1" || !reflect.DeepEqual(got.Arguments, map[string]interface{}{"value": value}) {
		t.Errorf("%s: ToolCall = %+v, want %s tool1 {value: %s}", label, got, id, value)
	}
	if got.ProviderExecuted || got.ProviderMetadata != nil || got.Title != "" {
		t.Errorf("%s: ToolCall providerExecuted/providerMetadata/title set: %+v", label, got)
	}
}

// options.onToolExecutionStart > should be called with correct tool name, id,
// and input; should be called once per tool call in a multi-tool step; should
// pass context; should not fire for tools without execute; should be called
// before tool execution.
func TestR70Gen_OnToolExecutionStart_Payload(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		var events []OnToolCallStartEvent
		var order []string
		var mu sync.Mutex
		tool := r70Tool1()
		exec := tool.Execute
		tool.Execute = func(ctx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			order = append(order, "execute")
			mu.Unlock()
			return exec(ctx, in, o)
		}
		_, err := GenerateText(context.Background(), r70Settings(GenerateTextOptions{
			Model: r70Model(r70ToolCalls(r70Call("call-1", "test-arg"))), Prompt: "test-input",
			Tools: []types.Tool{tool},
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
				mu.Lock()
				order = append(order, "onToolExecutionStart")
				events = append(events, e)
				mu.Unlock()
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Fatalf("%d events", len(events))
		}
		e := events[0]
		if e.CallID != "test-telemetry-call-id" {
			t.Errorf("CallID = %q", e.CallID)
		}
		r70MessageTexts(t, "Messages", e.Messages, []types.Message{r70UserMessage("test-input")})
		r70CheckToolCall(t, "start", e.ToolCall, "call-1", "test-arg")
		if e.ToolContext != nil {
			t.Errorf("ToolContext = %#v, want nil", e.ToolContext)
		}
		if !reflect.DeepEqual(order, []string{"onToolExecutionStart", "execute"}) {
			t.Errorf("order = %v", order)
		}
	})
	t.Run("multi-tool step", func(t *testing.T) {
		var mu sync.Mutex
		var events []OnToolCallStartEvent
		_, err := GenerateText(context.Background(), r70Settings(GenerateTextOptions{
			Model: r70Model(r70ToolCalls(r70Call("call-1", "a"), r70Call("call-2", "b"))),
			Tools: []types.Tool{r70Tool1()},
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) {
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Fatalf("%d events", len(events))
		}
		// Go runs the step's tool calls concurrently (no ToolCallConcurrency),
		// so the event order is not fixed.
		sort.Slice(events, func(i, j int) bool { return events[i].ToolCall.ID < events[j].ToolCall.ID })
		for i, want := range []struct{ id, value string }{{"call-1", "a"}, {"call-2", "b"}} {
			if events[i].CallID != "test-telemetry-call-id" {
				t.Errorf("event %d CallID = %q", i, events[i].CallID)
			}
			r70MessageTexts(t, "Messages", events[i].Messages, []types.Message{r70UserMessage("prompt")})
			r70CheckToolCall(t, "start", events[i].ToolCall, want.id, want.value)
			if events[i].ToolContext != nil {
				t.Errorf("event %d ToolContext = %#v", i, events[i].ToolContext)
			}
		}
	})
	t.Run("context", func(t *testing.T) {
		tool := r70Tool1()
		tool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []string{"context"},
		})
		var events []OnToolCallStartEvent
		_, err := GenerateText(context.Background(), r70Settings(GenerateTextOptions{
			Model: r70Model(r70ToolCalls(r70Call("call-1", "test"))), Tools: []types.Tool{tool},
			ToolsContext:         map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}},
			OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) { events = append(events, e) },
		}))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || !reflect.DeepEqual(events[0].ToolContext, map[string]interface{}{"context": "test"}) {
			t.Fatalf("events = %#v", events)
		}
		r70CheckToolCall(t, "start", events[0].ToolCall, "call-1", "test")
	})
	t.Run("no execute", func(t *testing.T) {
		tool := r70Tool1()
		tool.Execute = nil
		var starts, ends int
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r70Model(r70ToolCalls(r70Call("call-1", "test"))), Prompt: "test-input", Tools: []types.Tool{tool},
			OnToolExecutionStart: func(context.Context, OnToolCallStartEvent) { starts++ },
			OnToolExecutionEnd:   func(context.Context, OnToolCallFinishEvent) { ends++ },
		})
		if err != nil {
			t.Fatal(err)
		}
		if starts != 0 || ends != 0 {
			t.Errorf("starts=%d ends=%d, want 0/0", starts, ends)
		}
	})
}

func r70CheckToolOutput(t *testing.T, label string, got types.ToolResult, id, value string, result interface{}, errText string) {
	t.Helper()
	if got.ToolCallID != id || got.ToolName != "tool1" || !reflect.DeepEqual(got.Input, map[string]interface{}{"value": value}) {
		t.Errorf("%s: ToolOutput = %+v", label, got)
	}
	if got.Dynamic {
		t.Errorf("%s: ToolOutput.Dynamic = true, want false", label)
	}
	if errText == "" {
		if got.Error != nil || got.Result != result {
			t.Errorf("%s: ToolOutput result/error = %#v/%v, want %#v", label, got.Result, got.Error, result)
		}
		return
	}
	if got.Error == nil || got.Error.Error() != errText || got.Result != nil {
		t.Errorf("%s: ToolOutput error = %v (result %#v), want %q", label, got.Error, got.Result, errText)
	}
}

// options.onToolExecutionEnd > should be called with correct data on success;
// with error data when tool execution fails; should pass context on success /
// on error; should have a positive toolExecutionMs.
func TestR70Gen_OnToolExecutionEnd_Payload(t *testing.T) {
	ctxSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []string{"context"},
	})
	cases := []struct {
		name     string
		value    string
		fail     bool
		withCtx  bool
		wantOut  interface{}
		wantErr  string
		wantTCtx interface{}
	}{
		{name: "success", value: "test-arg", wantOut: "test-arg-result"},
		{name: "error", value: "test", fail: true, wantErr: "tool execution failed"},
		{name: "context on success", value: "test", withCtx: true, wantOut: "test-result", wantTCtx: map[string]interface{}{"context": "test"}},
		{name: "context on error", value: "test", fail: true, withCtx: true, wantErr: "tool execution failed", wantTCtx: map[string]interface{}{"context": "test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := r70Tool1()
			if tc.fail {
				tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return nil, errors.New("tool execution failed")
				}
			}
			opts := GenerateTextOptions{
				Model: r70Model(r70ToolCalls(r70Call("call-1", tc.value))),
			}
			if tc.withCtx {
				tool.ContextSchema = ctxSchema
				opts.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}}
			}
			opts.Tools = []types.Tool{tool}
			var events []OnToolCallFinishEvent
			opts.OnToolExecutionEnd = func(_ context.Context, e OnToolCallFinishEvent) { events = append(events, e) }
			if _, err := GenerateText(context.Background(), r70Settings(opts)); err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatalf("%d events", len(events))
			}
			e := events[0]
			if e.CallID != "test-telemetry-call-id" {
				t.Errorf("CallID = %q", e.CallID)
			}
			r70MessageTexts(t, "Messages", e.Messages, []types.Message{r70UserMessage("prompt")})
			r70CheckToolCall(t, "end", e.ToolCall, "call-1", tc.value)
			if !reflect.DeepEqual(e.ToolContext, tc.wantTCtx) {
				t.Errorf("ToolContext = %#v, want %#v", e.ToolContext, tc.wantTCtx)
			}
			if e.ToolExecutionMs < 0 {
				t.Errorf("ToolExecutionMs = %d", e.ToolExecutionMs)
			}
			r70CheckToolOutput(t, "end", e.ToolOutput, "call-1", tc.value, tc.wantOut, tc.wantErr)
		})
	}
}

// options.onToolExecutionStart and onToolExecutionEnd ordering > should fire
// tool call callbacks for each tool in a multi-step loop (the second step's
// events carry the step's prompt with the first step's response messages).
func TestR70Gen_ToolExecutionCallbacks_MultiStepMessages(t *testing.T) {
	model := r70Model(r70ToolCalls(r70Call("call-1", "step0")), r70ToolCalls(r70Call("call-2", "step1")), r70Text("Done."))
	var starts []OnToolCallStartEvent
	var ends []OnToolCallFinishEvent
	_, err := GenerateText(context.Background(), r70Settings(GenerateTextOptions{
		Model: model, Tools: []types.Tool{r70Tool1()}, StopWhen: []StopCondition{IsStepCount(4)},
		OnToolExecutionStart: func(_ context.Context, e OnToolCallStartEvent) { starts = append(starts, e) },
		OnToolExecutionEnd:   func(_ context.Context, e OnToolCallFinishEvent) { ends = append(ends, e) },
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 || len(ends) != 2 {
		t.Fatalf("starts=%d ends=%d, want 2/2", len(starts), len(ends))
	}
	step1Prompt := func(t *testing.T, label string, msgs []types.Message) {
		t.Helper()
		if len(msgs) != 3 {
			t.Fatalf("%s: %d messages, want 3: %#v", label, len(msgs), msgs)
		}
		r70MessageTexts(t, label, msgs[:1], []types.Message{r70UserMessage("prompt")})
		if msgs[1].Role != types.RoleAssistant || len(msgs[1].Content) != 1 {
			t.Fatalf("%s[1] = %#v", label, msgs[1])
		}
		if tc, ok := msgs[1].Content[0].(types.ToolCallContent); !ok || tc.ToolCallID != "call-1" || tc.ToolName != "tool1" || !reflect.DeepEqual(tc.Arguments, map[string]interface{}{"value": "step0"}) {
			t.Errorf("%s[1] content = %#v", label, msgs[1].Content[0])
		}
		if msgs[2].Role != types.RoleTool || len(msgs[2].Content) != 1 {
			t.Fatalf("%s[2] = %#v", label, msgs[2])
		}
		tr, ok := msgs[2].Content[0].(types.ToolResultContent)
		if !ok || tr.ToolCallID != "call-1" || tr.ToolName != "tool1" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "step0-result" {
			t.Errorf("%s[2] content = %#v", label, msgs[2].Content[0])
		}
	}
	for i, value := range []string{"step0", "step1"} {
		id := []string{"call-1", "call-2"}[i]
		r70CheckToolCall(t, "start", starts[i].ToolCall, id, value)
		r70CheckToolCall(t, "end", ends[i].ToolCall, id, value)
		r70CheckToolOutput(t, "end", ends[i].ToolOutput, id, value, value+"-result", "")
		if starts[i].CallID != "test-telemetry-call-id" || ends[i].CallID != "test-telemetry-call-id" {
			t.Errorf("step %d call IDs = %q/%q", i, starts[i].CallID, ends[i].CallID)
		}
	}
	r70MessageTexts(t, "start[0].Messages", starts[0].Messages, []types.Message{r70UserMessage("prompt")})
	r70MessageTexts(t, "end[0].Messages", ends[0].Messages, []types.Message{r70UserMessage("prompt")})
	step1Prompt(t, "start[1].Messages", starts[1].Messages)
	step1Prompt(t, "end[1].Messages", ends[1].Messages)
}

// options.onEnd > should send correct information (inline snapshot, field by
// field).
func TestR70Gen_OnEnd_Payload(t *testing.T) {
	model := r70Model(func() *types.GenerateResult {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello, World!"},
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Input: `{ "value": "value" }`, Arguments: map[string]interface{}{"value": "value"}},
			},
			ToolCalls:       []types.ToolCall{r70Call("call-1", "value")},
			FinishReason:    types.FinishReasonStop,
			RawFinishReason: "stop",
			Usage:           r70TestUsage(),
			ResponseMetadata: &types.ResponseMetadata{
				ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0).UTC(),
				Headers: map[string]string{"call": "2"},
			},
		}
	})
	var ev OnFinishEvent
	n := 0
	_, err := GenerateText(context.Background(), r70Settings(GenerateTextOptions{
		Model: model, Prompt: "irrelevant", Tools: []types.Tool{r70Tool1()},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { ev = e; n++ },
	}))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("onEnd called %d times", n)
	}
	if ev.CallID != "test-telemetry-call-id" || ev.StepNumber != 0 {
		t.Errorf("CallID/StepNumber = %q/%d", ev.CallID, ev.StepNumber)
	}
	contentTypes := func(parts []types.ContentPart) []string {
		out := make([]string, len(parts))
		for i, p := range parts {
			out[i] = p.ContentType()
		}
		return out
	}
	wantContent := []string{"text", "tool-call", "tool-result"}
	if got := contentTypes(ev.Content); !reflect.DeepEqual(got, wantContent) {
		t.Errorf("Content types = %v, want %v", got, wantContent)
	}
	if len(ev.DynamicToolCalls) != 0 || len(ev.DynamicToolResults) != 0 || len(ev.Files) != 0 || len(ev.Sources) != 0 || len(ev.Warnings) != 0 || len(ev.Reasoning) != 0 {
		t.Errorf("dynamic/files/sources/warnings/reasoning not empty: %+v", ev)
	}
	if ev.ReasoningText != "" || ev.ProviderMetadata != nil {
		t.Errorf("ReasoningText/ProviderMetadata = %q/%v", ev.ReasoningText, ev.ProviderMetadata)
	}
	if ev.FinishReason != types.FinishReasonStop || ev.RawFinishReason != "stop" {
		t.Errorf("finish = %q/%q", ev.FinishReason, ev.RawFinishReason)
	}
	if ev.Model.Provider != "mock-provider" || ev.Model.ModelID != "mock-model-id" {
		t.Errorf("Model = %+v", ev.Model)
	}
	if ev.Text != "Hello, World!" {
		t.Errorf("Text = %q", ev.Text)
	}
	if len(ev.ToolCalls) != 1 || len(ev.StaticToolCalls) != 1 {
		t.Fatalf("tool calls = %d static %d", len(ev.ToolCalls), len(ev.StaticToolCalls))
	}
	r70CheckToolCall(t, "onEnd toolCalls", ev.ToolCalls[0], "call-1", "value")
	r70CheckToolCall(t, "onEnd staticToolCalls", ev.StaticToolCalls[0], "call-1", "value")
	if len(ev.ToolResults) != 1 || len(ev.StaticToolResults) != 1 {
		t.Fatalf("tool results = %d static %d", len(ev.ToolResults), len(ev.StaticToolResults))
	}
	r70CheckToolOutput(t, "onEnd toolResults", ev.ToolResults[0], "call-1", "value", "value-result", "")
	r70CheckToolOutput(t, "onEnd staticToolResults", ev.StaticToolResults[0], "call-1", "value", "value-result", "")
	if ev.Response.ID != "id-0" || ev.Response.ModelID != "mock-model-id" || !ev.Response.Timestamp.Equal(time.Unix(0, 0)) ||
		!reflect.DeepEqual(ev.Response.Headers, map[string]string{"call": "2"}) || ev.Response.Body != nil {
		t.Errorf("Response = %+v", ev.Response)
	}
	if ev.Request.Body != nil {
		t.Errorf("Request.Body = %#v", ev.Request.Body)
	}
	checkMessages := func(label string, msgs []types.Message) {
		t.Helper()
		if len(msgs) != 2 || msgs[0].Role != types.RoleAssistant || msgs[1].Role != types.RoleTool {
			t.Fatalf("%s = %#v", label, msgs)
		}
		if got := contentTypes(msgs[0].Content); !reflect.DeepEqual(got, []string{"text", "tool-call"}) {
			t.Errorf("%s[0] content types = %v", label, got)
		}
		tr, ok := msgs[1].Content[0].(types.ToolResultContent)
		if !ok || tr.ToolCallID != "call-1" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "value-result" {
			t.Errorf("%s[1] content = %#v", label, msgs[1].Content)
		}
	}
	checkMessages("Response.Messages", ev.Response.Messages)
	checkMessages("ResponseMessages", ev.ResponseMessages)
	if len(ev.Steps) != 1 {
		t.Fatalf("Steps = %d", len(ev.Steps))
	}
	if !reflect.DeepEqual(ev.FinalStep, ev.Steps[0]) {
		t.Errorf("FinalStep differs from Steps[0]")
	}
	s := ev.FinalStep
	if s.CallID != "test-telemetry-call-id" || s.StepNumber != 0 || s.FinishReason != types.FinishReasonStop || s.RawFinishReason != "stop" {
		t.Errorf("FinalStep = %+v", s)
	}
	if got := contentTypes(s.Content); !reflect.DeepEqual(got, wantContent) {
		t.Errorf("FinalStep content types = %v", got)
	}
	if _, ok := s.Performance.ToolExecutionMs["call-1"]; !ok {
		t.Errorf("FinalStep.Performance.ToolExecutionMs = %v, want call-1", s.Performance.ToolExecutionMs)
	}
	checkMessages("FinalStep.Response.Messages", s.Response.Messages)
	for label, u := range map[string]types.Usage{"Usage": ev.Usage, "TotalUsage": ev.TotalUsage, "FinalStep.Usage": s.Usage} {
		if u.InputTokens == nil || *u.InputTokens != 3 || u.OutputTokens == nil || *u.OutputTokens != 10 || u.TotalTokens == nil || *u.TotalTokens != 13 {
			t.Errorf("%s = %+v, want 3/10/13", label, u)
		}
	}
	if ev.Output != nil {
		t.Errorf("Output = %#v, want nil without an Output spec", ev.Output)
	}
}

// options.onEnd > should support onFinish as a deprecated alias; should
// prefer onEnd over onFinish (both the structured event and the result
// callbacks).
func TestR70Gen_OnEnd_DeprecatedAlias(t *testing.T) {
	run := func(setNew bool) []string {
		var calls []string
		opts := GenerateTextOptions{
			Model: r70Model(r70Text("Hello, World!")), Prompt: "irrelevant",
			OnFinishEvent: func(context.Context, OnFinishEvent) { calls = append(calls, "onFinishEvent") },
			OnFinish:      func(context.Context, *GenerateTextResult, interface{}) { calls = append(calls, "onFinish") },
		}
		if setNew {
			opts.OnEndEvent = func(context.Context, OnFinishEvent) { calls = append(calls, "onEndEvent") }
			opts.OnEnd = func(context.Context, *GenerateTextResult, interface{}) { calls = append(calls, "onEnd") }
		}
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		sort.Strings(calls)
		return calls
	}
	if got := run(false); !reflect.DeepEqual(got, []string{"onFinish", "onFinishEvent"}) {
		t.Errorf("alias only: %v", got)
	}
	if got := run(true); !reflect.DeepEqual(got, []string{"onEnd", "onEndEvent"}) {
		t.Errorf("both: %v, want only onEnd/onEndEvent", got)
	}
}

// StreamText shares executeTools: the deprecated OnToolCallStart /
// OnToolCallFinish run only when OnToolExecutionStart / OnToolExecutionEnd are
// unset (TS streamText `onToolExecutionStart ?? experimental_onToolCallStart`).
func TestR70Gen_StreamText_ToolExecutionCallbacks_DeprecatedFallback(t *testing.T) {
	run := func(t *testing.T, setNew bool) []string {
		var mu sync.Mutex
		var calls []string
		record := func(name string) {
			mu.Lock()
			calls = append(calls, name)
			mu.Unlock()
		}
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			},
		}
		opts := StreamTextOptions{
			Model: model, Prompt: "prompt", Tools: []types.Tool{r70Tool1()},
			OnToolCallStart:  func(context.Context, OnToolCallStartEvent) { record("experimental_onToolCallStart") },
			OnToolCallFinish: func(context.Context, OnToolCallFinishEvent) { record("experimental_onToolCallFinish") },
		}
		if setNew {
			opts.OnToolExecutionStart = func(context.Context, OnToolCallStartEvent) { record("onToolExecutionStart") }
			opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { record("onToolExecutionEnd") }
		}
		res, err := StreamText(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
	if got := run(t, false); !reflect.DeepEqual(got, []string{"experimental_onToolCallStart", "experimental_onToolCallFinish"}) {
		t.Errorf("fallback only: calls = %v", got)
	}
	if got := run(t, true); !reflect.DeepEqual(got, []string{"onToolExecutionStart", "onToolExecutionEnd"}) {
		t.Errorf("both set: calls = %v, want only the new callbacks", got)
	}
}

// A decision model whose DoDecide returns (nil, nil) fails the call with an
// error (TS reads result.answers and rejects) instead of panicking.
func TestR70Gen_Decide_NilModelResultIsAnError(t *testing.T) {
	model := &mockDecisionModel{providerName: "p", modelID: "m", doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
		return nil, nil
	}}
	var ended bool
	res, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, State: "message", Questions: decisionQuestionsFixture(),
		OnEnd: func(DecideOnEndEvent) { ended = true },
	})
	if err == nil || res != nil {
		t.Fatalf("ExperimentalDecide = %v, %v; want an error", res, err)
	}
	if err.Error() != "decision model p/m returned no result" {
		t.Errorf("error = %q", err)
	}
	if ended {
		t.Error("OnEnd called for a failed decision")
	}
}
