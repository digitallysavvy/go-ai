package ai

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
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 72 (gen) ports of generate-text.test.ts, generate-object.test.ts and
// stream-object.test.ts cases (ts-test-port-coverage.md appendix), with
// field-by-field assertions.

// r72genSeqModel returns the results in order, one per DoGenerate call.
func r72genSeqModel(t *testing.T, results ...*types.GenerateResult) *testutil.MockLanguageModel {
	t.Helper()
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			if n >= len(results) {
				t.Errorf("unexpected model call %d", n)
				return nil, errors.New("unexpected model call")
			}
			r := *results[n]
			n++
			return &r, nil
		},
	}
}

func r72genText(text string) *types.GenerateResult {
	return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop, Usage: *usage(3, 10)}
}

func r72genCalls(calls ...types.ToolCall) *types.GenerateResult {
	return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls, Usage: *usage(3, 10)}
}

func r72genValueTool(name string) types.Tool {
	return types.Tool{
		Name: name,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required": []interface{}{"value"}, "additionalProperties": false},
		Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return args["value"].(string) + "-result", nil
		},
	}
}

// generate-text.test.ts 2219 'should expose a tool call serialized as text
// for opt-in recovery': the violation error carries the response content.
func TestR72Gen_ToolChoiceViolationExposesContent(t *testing.T) {
	serialized := `{"toolName":"tool1","input":{"value":"value"}}`
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genText(serialized)), Prompt: "test-input",
		Tools: []types.Tool{r72genValueTool("tool1")}, ToolChoice: types.RequiredToolChoice(),
	})
	var violation *ToolChoiceViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("err = %v, want ToolChoiceViolationError", err)
	}
	if want := []types.ContentPart{types.TextContent{Text: serialized}}; !reflect.DeepEqual(violation.Content, want) {
		t.Fatalf("content = %#v, want %#v", violation.Content, want)
	}
	var recovered struct {
		ToolName string            `json:"toolName"`
		Input    map[string]string `json:"input"`
	}
	if err := json.Unmarshal([]byte(violation.Content[0].(types.TextContent).Text), &recovered); err != nil || recovered.ToolName != "tool1" || recovered.Input["value"] != "value" {
		t.Fatalf("recovered = %+v, %v", recovered, err)
	}
}

// generate-text.test.ts 2302 'should enforce the tool choice returned by
// prepareStep' and 2326 'should allow prepareStep to replace a required tool
// choice'.
func TestR72Gen_PrepareStepToolChoiceIsEnforced(t *testing.T) {
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genText("No tool call.")), Prompt: "test-input",
		Tools: []types.Tool{r72genValueTool("tool1")}, ToolChoice: types.AutoToolChoice(),
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ToolChoice: types.RequiredToolChoice()}
		},
	})
	var violation *ToolChoiceViolationError
	if !errors.As(err, &violation) || violation.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("err = %v, want a required ToolChoiceViolationError", err)
	}

	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genText("No tool call.")), Prompt: "test-input",
		Tools: []types.Tool{r72genValueTool("tool1")}, ToolChoice: types.RequiredToolChoice(),
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ToolChoice: types.AutoToolChoice()}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "No tool call." {
		t.Fatalf("text = %q", res.Text)
	}
}

// generate-text.test.ts 4166 'should pass stepNumber 0 for a single step',
// 4187 multi-step stepNumbers and 4248 'should have matching stepNumber
// between onStepStart and onStepFinish'.
func TestR72Gen_StepNumbers(t *testing.T) {
	var single []int
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genText("Hello!")), Prompt: "test-input",
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) { single = append(single, e.StepNumber) },
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(single, []int{0}) {
		t.Fatalf("single step numbers = %v", single)
	}

	var starts, ends, stepEnds []int
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "test" }`}),
			r72genCalls(types.ToolCall{ID: "call-2", ToolName: "tool1", RawArguments: `{ "value": "test2" }`}),
			r72genText("Done.")),
		Prompt: "test-input", Tools: []types.Tool{r72genValueTool("tool1")},
		StopWhen:          []StopCondition{StepCountIs(4)},
		OnStepStart:       func(_ context.Context, e OnStepStartEvent) { starts = append(starts, e.StepNumber) },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) { ends = append(ends, e.StepNumber) },
		OnStepEnd:         func(_ context.Context, s types.StepResult, _ interface{}) { stepEnds = append(stepEnds, s.StepNumber) },
	}); err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(starts, want) || !reflect.DeepEqual(ends, want) || !reflect.DeepEqual(stepEnds, want) {
		t.Fatalf("step numbers start=%v end=%v stepEnd=%v, want %v", starts, ends, stepEnds, want)
	}
}

// generate-text.test.ts 1690 'should send files from all steps to onFinish'.
func TestR72Gen_OnEndFilesFromAllSteps(t *testing.T) {
	step0 := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{}`})
	step0.Content = []types.ContentPart{types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("step-0")}}
	step1 := r72genText("")
	step1.Content = []types.ContentPart{types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("step-1")}}
	tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}

	var end OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, step0, step1), Prompt: "prompt", Tools: []types.Tool{tool},
		StopWhen:   []StopCondition{StepCountIs(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	files := func(fs []types.GeneratedFileContent) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.MediaType+":"+string(f.Data))
		}
		return out
	}
	want := []string{"text/plain:step-0", "text/plain:step-1"}
	if got := files(end.Files); !reflect.DeepEqual(got, want) {
		t.Fatalf("onEnd files = %v, want %v", got, want)
	}
	if got := files(res.Files); !reflect.DeepEqual(got, want) {
		t.Fatalf("result files = %v, want %v", got, want)
	}
	if got := files(end.Steps[len(end.Steps)-1].Files); !reflect.DeepEqual(got, want[1:]) {
		t.Fatalf("last step files = %v", got)
	}
}

// generate-text.test.ts 8357 'should support the deprecated
// experimental_repairToolCall option'.
func TestR72Gen_DeprecatedRepairToolCall(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": 123 }`})),
		Prompt: "test-input", Tools: []types.Tool{r72genValueTool("tool1")},
		ExperimentalRepairToolCall: func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			c := o.ToolCall
			c.RawArguments = `{ "value": "repaired" }`
			return &c, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call-1" || res.ToolCalls[0].ToolName != "tool1" ||
		!reflect.DeepEqual(res.ToolCalls[0].Arguments, map[string]interface{}{"value": "repaired"}) || res.ToolCalls[0].Invalid {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].Result != "repaired-result" {
		t.Fatalf("tool results = %+v", res.ToolResults)
	}
}

// generate-text.test.ts 8321 'should not execute a repaired tool when aborted
// during input validation': the call is aborted while the repair runs, so the
// repaired call never executes and GenerateText fails with the abort.
func TestR72Gen_AbortDuringRepairDoesNotExecute(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cancelled := errors.New("cancelled")
	executed := false
	tool := r72genValueTool("tool1")
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		executed = true
		return "tool result", nil
	}
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": 123 }`})),
		Prompt: "test-input", Tools: []types.Tool{tool},
		RepairToolCall: func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			cancel(cancelled)
			c := o.ToolCall
			c.RawArguments = `{ "value": "repaired" }`
			return &c, nil
		},
	})
	// TS rejects with the abort reason (abortSignal.reason).
	if !errors.Is(err, cancelled) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if executed {
		t.Fatal("repaired tool executed after the abort")
	}

	// A step timeout that fires while the repair runs is the step
	// TimeoutError (TS: the abort reason of the step timeout signal).
	stepTimeout := 20 * time.Millisecond
	_, err = GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": 123 }`})),
		Prompt: "test-input", Tools: []types.Tool{tool}, Timeout: &TimeoutConfig{PerStep: &stepTimeout},
		RepairToolCall: func(ctx context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			<-ctx.Done()
			c := o.ToolCall
			c.RawArguments = `{ "value": "repaired" }`
			return &c, nil
		},
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonStep {
		t.Fatalf("err = %T %v, want the step TimeoutError", err, err)
	}
	if executed {
		t.Fatal("repaired tool executed after the step timeout")
	}
}

// generate-text.test.ts 8573 'tools with custom schema > should contain tool
// calls': a tool whose input schema is an *schema.OrderedSchema (TS
// jsonSchema()) is sent as its JSON schema and its calls are parsed and
// validated against it.
func TestR72Gen_OrderedSchemaToolIsValidated(t *testing.T) {
	ordered := func(raw string) *schema.OrderedSchema {
		s, err := schema.NewOrderedSchema([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	tools := []types.Tool{
		{Name: "tool1", Parameters: ordered(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)},
		{Name: "tool2", Parameters: ordered(`{"type":"object","properties":{"somethingElse":{"type":"string"}},"required":["somethingElse"],"additionalProperties":false}`)},
	}
	model := r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}))
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Tools: tools, ToolChoice: types.RequiredToolChoice()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Invalid || res.ToolCalls[0].Dynamic ||
		!reflect.DeepEqual(res.ToolCalls[0].Arguments, map[string]interface{}{"value": "value"}) {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if got := model.GenerateCalls[0].ToolChoice; got.Type != types.ToolChoiceRequired {
		t.Fatalf("tool choice = %+v", got)
	}

	// An input that breaks the schema is an invalid call (TS
	// InvalidToolInputError), as for a map schema.
	res, err = GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": 1 }`})),
		Prompt: "test-input", Tools: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].Invalid || !res.ToolCalls[0].Dynamic || !IsInvalidToolInputError(res.ToolCalls[0].Error) {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}

	// The same for a bare *schema.JSONSchemaValidator.
	res, err = GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool3", RawArguments: `{}`})),
		Prompt: "test-input",
		Tools:  []types.Tool{{Name: "tool3", Parameters: schema.NewJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"q"}})}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].Invalid {
		t.Fatalf("validator tool calls = %+v", res.ToolCalls)
	}
}

// generate-text.test.ts 9043 'should parse the output when the finish reason
// is missing' and 9504 'should not parse mixed text and tool calls as output'.
func TestR72Gen_OutputParsingByFinishReason(t *testing.T) {
	valueSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}})
	missing := r72genText(`{ "value": "test-value" }`)
	missing.FinishReason = ""
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, missing), Prompt: "prompt",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: valueSchema}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Output, map[string]interface{}{"value": "test-value"}) {
		t.Fatalf("output = %#v", res.Output)
	}

	mixed := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "testTool", RawArguments: `{ "value": "test" }`})
	mixed.Text = "I will use the test tool."
	res, err = GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, mixed), Prompt: "prompt",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []interface{}{"summary"}})}),
		Tools:  []types.Tool{r72genValueTool("testTool")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "I will use the test tool." || res.Output != nil || len(res.ToolCalls) != 1 || len(res.ToolResults) != 1 {
		t.Fatalf("text=%q output=%#v calls=%d results=%d", res.Text, res.Output, len(res.ToolCalls), len(res.ToolResults))
	}
}

// generate-text.test.ts 9304 'should not reuse an earlier step selection when
// the final selection is %j'.
func TestR72Gen_StructuredOutputTextNotReusedAcrossSteps(t *testing.T) {
	final := `{"value":"final"}`
	for _, last := range []*string{nil, &final} {
		earlierText := `{"value":"earlier"}`
		step0 := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "testTool", RawArguments: `{}`})
		step0.Text = earlierText
		step0.StructuredOutputText = &earlierText
		step1 := r72genText(final)
		step1.StructuredOutputText = last
		tool := types.Tool{Name: "testTool", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "tool result", nil
			}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r72genSeqModel(t, step0, step1), Prompt: "prompt", Tools: []types.Tool{tool},
			Output:   ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}),
			StopWhen: []StopCondition{StepCountIs(2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res.Output, map[string]interface{}{"value": "final"}) {
			t.Fatalf("last=%v output = %#v", last, res.Output)
		}
	}
}

// generate-text.test.ts 9007 'should expose parse diagnostics when output is
// truncated' and 9256 'should reject invalid selected text %j without
// falling back to valid content': the NoObjectGeneratedError carries the
// text, finish reason and usage.
func TestR72Gen_OutputParseDiagnostics(t *testing.T) {
	valueSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}})
	truncated := r72genText(`{"value":"test`)
	truncated.FinishReason = types.FinishReasonLength
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, truncated), Prompt: "prompt",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: valueSchema}),
	})
	var nog *NoObjectGeneratedError
	if !errors.As(err, &nog) {
		t.Fatalf("err = %v", err)
	}
	if nog.Message != "No object generated: could not parse the response." || nog.Text != `{"value":"test` ||
		nog.FinishReason != types.FinishReasonLength || nog.Usage == nil || nog.Usage.OutputTokens == nil || *nog.Usage.OutputTokens != 10 {
		t.Fatalf("error = %+v", nog)
	}
	// TS: error.cause instanceof JSONParseError (r72 gen CR-1, output.go).
	if !providererrors.IsJSONParseError(nog.Cause) {
		t.Errorf("cause = %T %v, want a JSONParseError", nog.Cause, nog.Cause)
	}

	for _, selected := range []string{"", `{"value":`} {
		res := r72genText(`{"value":"draft"}`)
		sel := selected
		res.StructuredOutputText = &sel
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: r72genSeqModel(t, res), Prompt: "prompt",
			Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: valueSchema}),
		})
		var nog *NoObjectGeneratedError
		if !errors.As(err, &nog) || nog.Text != selected {
			t.Fatalf("selected %q: err = %v", selected, err)
		}
		if !providererrors.IsJSONParseError(nog.Cause) {
			t.Errorf("selected %q: cause = %T, want a JSONParseError", selected, nog.Cause)
		}
	}
}

// generate-text.test.ts 11371 'should pass full runtimeContext to callbacks'
// (telemetry filtering does not reach the callbacks).
func TestR72Gen_FullRuntimeContextInCallbacks(t *testing.T) {
	rc := map[string]interface{}{"userId": "user-123", "requestId": "request-123"}
	var got []interface{}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genText("Hello, world!")), Prompt: "test-input",
		RuntimeContext:    rc,
		Telemetry:         &TelemetrySettings{IncludeRuntimeContext: map[string]bool{"requestId": true}},
		OnStart:           func(_ context.Context, e OnStartEvent) { got = append(got, e.RuntimeContext) },
		OnStepStart:       func(_ context.Context, e OnStepStartEvent) { got = append(got, e.RuntimeContext) },
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) { got = append(got, e.RuntimeContext) },
		OnEndEvent:        func(_ context.Context, e OnFinishEvent) { got = append(got, e.RuntimeContext) },
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []interface{}{rc, rc, rc, rc}) {
		t.Fatalf("runtime contexts = %#v", got)
	}
}

// generate-text.test.ts 12026 'should call logWarnings once for each step
// with warnings from that step' and 12091 (no warnings: nothing logged, the
// TS logWarnings returns early for an empty list).
func TestR72Gen_LogWarningsPerStep(t *testing.T) {
	var mu sync.Mutex
	var calls []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) {
		if o.Provider != "mock-provider" || o.Model != "r72gen-warn" {
			return
		}
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
	})
	defer SetLogWarnings(nil)

	w1 := types.Warning{Type: "other", Message: "Warning from step 1"}
	w2 := types.Warning{Type: "other", Message: "Warning from step 2"}
	step0 := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "testTool", RawArguments: `{ "value": "test" }`})
	step0.Warnings = []types.Warning{w1}
	step1 := r72genText("Final response")
	step1.Warnings = []types.Warning{w2}
	model := r72genSeqModel(t, step0, step1)
	model.ModelName = "r72gen-warn"
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "Hello", Tools: []types.Tool{r72genValueTool("testTool")},
		StopWhen: []StopCondition{StepCountIs(3)},
	}); err != nil {
		t.Fatal(err)
	}
	want := []LogWarningsOptions{
		{Warnings: []types.Warning{w1}, Provider: "mock-provider", Model: "r72gen-warn"},
		{Warnings: []types.Warning{w2}, Provider: "mock-provider", Model: "r72gen-warn"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("logWarnings calls = %+v, want %+v", calls, want)
	}

	calls = nil
	model = r72genSeqModel(t, r72genText("Hello, world!"))
	model.ModelName = "r72gen-warn"
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "Hello"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("logWarnings calls without warnings = %+v", calls)
	}
}

// generate-object.test.ts 853 / 874 'should throw NoObjectGeneratedError when
// parsing fails (with repairResponse)', options.repairText 630 'should be able
// to repair a JSONParseError' and stream-object.test.ts 1845 'should be able to
// repair JSON wrapped with markdown code blocks': the parse failure's cause,
// and the error RepairText receives, is a JSONParseError holding the text.
func TestR72Gen_ObjectParseFailureIsJSONParseError(t *testing.T) {
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: objectModel(`{ broken json`), Prompt: "prompt", Schema: contentSchema(),
	})
	var nog *NoObjectGeneratedError
	if !errors.As(err, &nog) || nog.Message != "No object generated: could not parse the response." {
		t.Fatalf("err = %v", err)
	}
	var parseErr *providererrors.JSONParseError
	if !errors.As(nog.Cause, &parseErr) || parseErr.Text != `{ broken json` {
		t.Fatalf("cause = %T %v, want a JSONParseError", nog.Cause, nog.Cause)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(nog.Cause, &syntaxErr) {
		t.Fatalf("the parser error is not reachable through the cause: %v", nog.Cause)
	}
	if nog.Text != `{ broken json` || nog.FinishReason != types.FinishReasonStop || nog.Usage == nil || *nog.Usage.InputTokens != 10 || *nog.Usage.OutputTokens != 20 || nog.Response == nil {
		t.Fatalf("error = %+v", nog)
	}

	// The repaired text still fails to parse: the second failure is reported.
	_, err = GenerateObject(context.Background(), GenerateObjectOptions{
		Model: objectModel(`{ broken json`), Prompt: "prompt", Schema: contentSchema(),
		RepairText: func(_ context.Context, text string, _ error) (*string, error) { s := text + "{"; return &s, nil },
	})
	if !errors.As(err, &nog) || nog.Message != "No object generated: could not parse the response." || !providererrors.IsJSONParseError(nog.Cause) || nog.Text != `{ broken json{` {
		t.Fatalf("repaired err = %v (%+v)", err, nog)
	}

	fenced := "```json\n{ \"content\": \"test message\" }\n```"
	var repairErr error
	var repairText string
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  streamObjectModel(append(textChunks(fenced), finishChunk())...),
		Prompt: "prompt", Schema: contentSchema(),
		RepairText: func(_ context.Context, text string, e error) (*string, error) {
			repairText, repairErr = text, e
			s := `{ "content": "test message" }`
			return &s, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !providererrors.IsJSONParseError(repairErr) || repairText != fenced {
		t.Fatalf("repair got %q, %T %v", repairText, repairErr, repairErr)
	}
	if !reflect.DeepEqual(res.Object, map[string]interface{}{"content": "test message"}) {
		t.Fatalf("object = %#v", res.Object)
	}
}

// stream-object.test.ts 2057 'should call logWarnings with the correct
// warnings': TS logs in the finish part's handler, also after an error part,
// so a stream that fails without a finish part logs nothing.
func TestR72Gen_StreamObjectLogsWarningsOnFinishPart(t *testing.T) {
	var mu sync.Mutex
	var calls []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) {
		if o.Model != "r72gen-so-warn" {
			return
		}
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
	})
	defer SetLogWarnings(nil)

	warning := types.Warning{Type: "other", Message: "w"}
	start := provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{warning}}
	errPart := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("boom")}
	run := func(chunks ...provider.StreamChunk) {
		t.Helper()
		model := streamObjectModel(chunks...)
		model.ModelName = "r72gen-so-warn"
		_, _ = StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "prompt", Schema: contentSchema()})
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := len(calls)
		calls = nil
		return n
	}

	run(append(append([]provider.StreamChunk{start}, textChunks(`{ "content": "x" }`)...), finishChunk())...)
	if n := count(); n != 1 {
		t.Fatalf("finished stream: %d logWarnings calls, want 1", n)
	}
	run(start, errPart, finishChunk())
	if n := count(); n != 1 {
		t.Fatalf("error part then finish: %d logWarnings calls, want 1", n)
	}
	run(start, errPart)
	if n := count(); n != 0 {
		t.Fatalf("error part without finish: %d logWarnings calls, want 0", n)
	}
}

// generate-object.test.ts callbacks: 1438 'should provide the raw objectText
// and usage', 1472 / 1559 reasoning in the step finish and finish events, 1526
// 'should provide the parsed object and metadata', 1586 ordering
// onStart -> onStepStart -> doGenerate -> onStepFinish -> onFinish and 1628
// one callId for all events; result.response 320 and the structured-output
// case 65 (reasoning, response body, raw objectText).
func TestR72Gen_GenerateObjectCallbackEvents(t *testing.T) {
	var order []string
	finalAnswer := `{"content":"final"}`
	draft := `{"content":"draft"}`
	body := map[string]interface{}{"output": []interface{}{draft, finalAnswer}}
	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider", ModelName: "test-model", StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			order = append(order, "doGenerate")
			return &types.GenerateResult{
				Content: []types.ContentPart{
					types.ReasoningContent{Text: "Checking both answers."},
					types.TextContent{Text: draft}, types.TextContent{Text: finalAnswer},
				},
				StructuredOutputText: &finalAnswer,
				FinishReason:         types.FinishReasonStop,
				Usage:                *usage(3, 10),
				ProviderMetadata:     map[string]interface{}{"test": map[string]interface{}{"key": "value"}},
				ResponseMetadata:     &types.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)},
				ResponseHeaders:      map[string]string{"x-h": "v"},
				RawResponse:          body,
			}, nil
		},
	}
	var callIDs []string
	var step ObjectOnStepFinishEvent
	var end ObjectOnFinishEvent
	res, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnStart: func(_ context.Context, e ObjectOnStartEvent) {
			order = append(order, "onStart")
			callIDs = append(callIDs, e.CallID)
		},
		OnStepStart: func(_ context.Context, e ObjectOnStepStartEvent) {
			order = append(order, "onStepStart")
			callIDs = append(callIDs, e.CallID)
		},
		OnStepFinish: func(_ context.Context, e ObjectOnStepFinishEvent) {
			order = append(order, "onStepFinish")
			callIDs = append(callIDs, e.CallID)
			step = e
		},
		OnFinishEvent: func(_ context.Context, e ObjectOnFinishEvent) {
			order = append(order, "onFinish")
			callIDs = append(callIDs, e.CallID)
			end = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"onStart", "onStepStart", "doGenerate", "onStepFinish", "onFinish"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if len(callIDs) != 4 || callIDs[0] == "" || callIDs[1] != callIDs[0] || callIDs[2] != callIDs[0] || callIDs[3] != callIDs[0] {
		t.Fatalf("call ids = %v", callIDs)
	}
	wantObject := map[string]interface{}{"content": "final"}
	if !reflect.DeepEqual(res.Object, wantObject) || res.Reasoning != "Checking both answers." || !reflect.DeepEqual(res.Response.Body, body) ||
		res.Response.ID != "id-0" || res.Response.ModelID != "mock-model-id" || !res.Response.Timestamp.Equal(time.Unix(0, 0)) ||
		!reflect.DeepEqual(res.Response.Headers, map[string]string{"x-h": "v"}) || *res.Usage.InputTokens != 3 || *res.Usage.OutputTokens != 10 {
		t.Fatalf("result = %+v", res)
	}
	if step.StepNumber != 0 || step.Provider != "test-provider" || step.ModelID != "test-model" || step.ObjectText != draft+finalAnswer ||
		step.FinishReason != types.FinishReasonStop || step.Reasoning != "Checking both answers." || !reflect.DeepEqual(step.Usage, res.Usage) ||
		!reflect.DeepEqual(step.Response, res.Response) {
		t.Fatalf("step finish = %+v", step)
	}
	if !reflect.DeepEqual(end.Object, wantObject) || end.Error != nil || end.FinishReason != types.FinishReasonStop || end.Reasoning != "Checking both answers." ||
		!reflect.DeepEqual(end.Usage, res.Usage) || !reflect.DeepEqual(end.ProviderMetadata, map[string]interface{}{"test": map[string]interface{}{"key": "value"}}) ||
		!reflect.DeepEqual(end.Response, res.Response) {
		t.Fatalf("finish = %+v", end)
	}
}

// stream-object.test.ts options.onFinish 643 (valid object) and 692 (object
// that does not match the schema) snapshots, 2410 callback ordering and 2462
// one callId.
func TestR72Gen_StreamObjectOnFinishSnapshots(t *testing.T) {
	meta := provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}}
	finish := finishChunk()
	finish.ProviderMetadata = json.RawMessage(`{"testProvider":{"testKey":"testValue"}}`)

	var order, callIDs []string
	var end ObjectOnFinishEvent
	model := streamObjectModel(append(append([]provider.StreamChunk{meta}, textChunks(`{ "content": "Hello, world!" }`)...), finish)...)
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnStart: func(_ context.Context, e ObjectOnStartEvent) {
			order = append(order, "onStart")
			callIDs = append(callIDs, e.CallID)
		},
		OnStepStart: func(_ context.Context, e ObjectOnStepStartEvent) {
			order = append(order, "onStepStart")
			callIDs = append(callIDs, e.CallID)
		},
		OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
			order = append(order, "onStepFinish")
			callIDs = append(callIDs, e.CallID)
		},
		OnEnd: func(_ context.Context, e ObjectOnFinishEvent) {
			order = append(order, "onFinish")
			callIDs = append(callIDs, e.CallID)
			end = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"onStart", "onStepStart", "onStepFinish", "onFinish"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v", order)
	}
	if len(callIDs) != 4 || callIDs[0] == "" || callIDs[3] != callIDs[0] || callIDs[1] != callIDs[0] || callIDs[2] != callIDs[0] {
		t.Fatalf("call ids = %v", callIDs)
	}
	wantResponse := GenerateStepResponse{ID: "id-0", ModelID: "mock-model-id", Timestamp: time.Unix(0, 0)}
	if !reflect.DeepEqual(end.Object, map[string]interface{}{"content": "Hello, world!"}) || end.Error != nil || end.FinishReason != types.FinishReasonStop ||
		!reflect.DeepEqual(end.ProviderMetadata, map[string]interface{}{"testProvider": map[string]interface{}{"testKey": "testValue"}}) ||
		end.Reasoning != "" || !reflect.DeepEqual(end.Request, GenerateStepRequest{}) || !reflect.DeepEqual(end.Response, wantResponse) ||
		*end.Usage.InputTokens != 3 || *end.Usage.OutputTokens != 10 || *end.Usage.TotalTokens != 13 || end.Warnings != nil {
		t.Fatalf("valid onFinish = %+v", end)
	}

	end = ObjectOnFinishEvent{}
	model = streamObjectModel(append(append([]provider.StreamChunk{meta}, textChunks(`{ `, `"invalid": `, `"Hello, `, `world`, `!"`, ` }`)...), finishChunk())...)
	_, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnEnd: func(_ context.Context, e ObjectOnFinishEvent) { end = e },
	})
	var nog *NoObjectGeneratedError
	if !errors.As(err, &nog) || nog.Message != "No object generated: response did not match schema." {
		t.Fatalf("err = %v", err)
	}
	if end.Object != nil || end.Error == nil || end.Error.Error() != "No object generated: response did not match schema." || end.FinishReason != types.FinishReasonStop ||
		end.ProviderMetadata != nil || !reflect.DeepEqual(end.Response, wantResponse) || *end.Usage.TotalTokens != 13 || end.Warnings != nil {
		t.Fatalf("invalid onFinish = %+v", end)
	}
}

// stream-object.test.ts output = "enum": 1501 'should not stream incorrect
// values', 1534 'should handle ambiguous values', 1568 'should handle
// non-ambiguous values'; output = "array" 1355 two elements in one chunk.
func TestR72Gen_StreamObjectEnumAndArrayPartials(t *testing.T) {
	enumDeltas := textChunks(`{ `, `"result": `, `"foo`, `bar`, `"`, ` }`)
	cases := []struct {
		name    string
		values  []string
		want    []interface{}
		wantErr bool
	}{
		{"incorrect", []string{"sunny", "rainy", "snowy"}, nil, true},
		{"ambiguous", []string{"foobar", "foobar2"}, []interface{}{"foo", "foobar"}, false},
		{"non-ambiguous", []string{"foobar", "barfoo"}, []interface{}{"foobar"}, false},
	}
	for _, c := range cases {
		var partials []interface{}
		res, err := StreamObject(context.Background(), StreamObjectOptions{
			Model: streamObjectModel(append(append([]provider.StreamChunk(nil), enumDeltas...), finishChunk())...), Prompt: "prompt",
			OutputMode: ObjectModeEnum, EnumValues: c.values,
			OnChunk: func(p interface{}) { partials = append(partials, p) },
		})
		if !reflect.DeepEqual(partials, c.want) {
			t.Fatalf("%s: partials = %#v, want %#v", c.name, partials, c.want)
		}
		if c.wantErr {
			if err == nil {
				t.Fatalf("%s: want an error for a value outside the enum", c.name)
			}
			continue
		}
		if err != nil || res.EnumValue != "foobar" || res.Object != "foobar" {
			t.Fatalf("%s: result = %+v, %v", c.name, res, err)
		}
	}

	var partials []interface{}
	var end ObjectOnFinishEvent
	elementSchema := contentSchema()
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  streamObjectModel(append(textChunks(`{"elements":[{"content":"element 1"},{"content":"element 2"}]}`), finishChunk())...),
		Prompt: "prompt", Schema: elementSchema, OutputMode: ObjectModeArray,
		OnChunk: func(p interface{}) { partials = append(partials, p) },
		OnEnd:   func(_ context.Context, e ObjectOnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{map[string]interface{}{"content": "element 1"}, map[string]interface{}{"content": "element 2"}}
	if !reflect.DeepEqual(partials, []interface{}{want}) {
		t.Fatalf("array partials = %#v", partials)
	}
	if !reflect.DeepEqual(res.Array, want) || !reflect.DeepEqual(res.Object, want) || !reflect.DeepEqual(end.Object, want) {
		t.Fatalf("array result = %#v / %#v, onFinish = %#v", res.Array, res.Object, end.Object)
	}
}

// TS step and result response.headers are the model response's headers. A
// provider that reports them only in GenerateResult.ResponseHeaders next to a
// ResponseMetadata without Headers (gmicloud, zai) still has them on the
// response (generate-text.test.ts 2940 'should contain response body and
// headers').
func TestR72Gen_ResponseHeadersFromResponseHeadersField(t *testing.T) {
	headers := map[string]string{"custom-response-header": "response-header-value"}
	full := r72genText("Hello")
	full.ResponseMetadata = &types.ResponseMetadata{ID: "id-0", ModelID: "m", Timestamp: time.Unix(0, 0)}
	full.ResponseHeaders = headers
	partial := r72genText("Hello")
	partial.ResponseMetadata = &types.ResponseMetadata{ID: "id-0", ModelID: "m"}
	partial.ResponseHeaders = headers
	for name, r := range map[string]*types.GenerateResult{"complete metadata": full, "partial metadata": partial} {
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, r), Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res.Response.Headers, headers) || !reflect.DeepEqual(res.FinalStep.Response.Headers, headers) || res.Response.ID != "id-0" {
			t.Fatalf("%s: response = %+v", name, res.Response)
		}
		obj, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model:  r72genSeqModel(t, &types.GenerateResult{Text: `{"content":"x"}`, FinishReason: types.FinishReasonStop, ResponseMetadata: r.ResponseMetadata, ResponseHeaders: headers}),
			Prompt: "p", Schema: contentSchema(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(obj.Response.Headers, headers) {
			t.Fatalf("%s: object response = %+v", name, obj.Response)
		}
	}
}

// Round 72 (gen) ports of ui/convert-to-model-messages.test.ts. Each result
// is rendered in the TS ModelMessage shape (r72genTSMessages) and compared
// with the TS inline snapshot (or the shape its snapshot file records).

func r72genTSPart(t *testing.T, part types.ContentPart) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	set := func(k string, v interface{}, ok bool) {
		if ok {
			out[k] = v
		}
	}
	switch p := part.(type) {
	case types.TextContent:
		out["type"], out["text"] = "text", p.Text
		set("providerOptions", p.ProviderOptions, p.ProviderOptions != nil)
	case types.ReasoningContent:
		out["type"], out["text"] = "reasoning", p.Text
		set("providerOptions", p.ProviderOptions, p.ProviderOptions != nil)
	case types.FileContent:
		out["type"], out["mediaType"] = "file", p.MediaType
		set("filename", p.Filename, p.Filename != "")
		switch p.FileData.Type {
		case types.FileDataTypeReference:
			out["data"] = map[string]interface{}{"type": "reference", "reference": map[string]string(p.FileData.Reference)}
		default:
			out["data"] = map[string]interface{}{"type": "url", "url": p.FileData.URL}
		}
		set("providerOptions", p.ProviderOptions, p.ProviderOptions != nil)
	case types.ToolCallContent:
		out["type"], out["toolCallId"], out["toolName"] = "tool-call", p.ToolCallID, p.ToolName
		var input interface{}
		if p.Arguments != nil {
			input = p.Arguments
		} else if p.Input != "" {
			if err := json.Unmarshal([]byte(p.Input), &input); err != nil {
				input = p.Input
			}
		}
		out["input"] = input
		set("providerExecuted", true, p.ProviderExecuted)
		set("providerOptions", p.ProviderOptions, p.ProviderOptions != nil)
	case types.ToolApprovalRequestContent:
		out["type"], out["approvalId"], out["toolCallId"] = "tool-approval-request", p.ApprovalID, p.ToolCallID
		set("isAutomatic", true, p.IsAutomatic)
		set("reason", p.Reason, p.Reason != "")
		set("signature", p.Signature, p.Signature != "")
	case types.ToolApprovalResponseContent:
		out["type"], out["approvalId"], out["approved"] = "tool-approval-response", p.ApprovalID, p.Approved
		set("reason", p.Reason, p.Reason != "")
		set("providerExecuted", true, p.ProviderExecuted)
	case types.ToolResultContent:
		out["type"], out["toolCallId"], out["toolName"] = "tool-result", p.ToolCallID, p.ToolName
		if p.Output != nil {
			o := map[string]interface{}{"type": string(p.Output.Type)}
			set2 := func(k string, v interface{}, ok bool) {
				if ok {
					o[k] = v
				}
			}
			set2("value", p.Output.Value, p.Output.Type != types.ToolResultOutputExecutionDenied)
			set2("reason", p.Output.Reason, p.Output.Reason != "")
			out["output"] = o
		}
		set("providerExecuted", true, p.ProviderExecuted)
		set("providerOptions", p.ProviderOptions, p.ProviderOptions != nil)
	default:
		t.Fatalf("unexpected part %T", part)
	}
	return out
}

func r72genTSMessages(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	var out []interface{}
	for _, m := range msgs {
		var content []interface{}
		for _, p := range m.Content {
			content = append(content, r72genTSPart(t, p))
		}
		msg := map[string]interface{}{"role": string(m.Role), "content": content}
		if m.ProviderOptions != nil {
			msg["providerOptions"] = m.ProviderOptions
		}
		out = append(out, msg)
	}
	return r72genJSON(t, out)
}

func r72genJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func r72genCheckMessages(t *testing.T, ui []UIMessage, wantJSON string, opts ...ConvertToModelMessagesOptions) {
	t.Helper()
	got, err := ConvertToModelMessages(context.Background(), ui, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var want interface{}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if g := r72genTSMessages(t, got); !reflect.DeepEqual(g, want) {
		gj, _ := json.Marshal(g)
		t.Fatalf("messages =\n%s\nwant\n%s", gj, wantJSON)
	}
}

func r72genScreenshot(id, value, output string) *ToolUIPart {
	return &ToolUIPart{Type: "tool-screenshot", State: ToolStateOutputAvailable, ToolCallID: id,
		Input: map[string]interface{}{"value": value}, Output: output}
}

func r72genScreenshotStep(ids ...string) string {
	calls, results := "", ""
	for i, id := range ids {
		if i > 0 {
			calls += ","
			results += ","
		}
		n := id[len("call-"):]
		calls += `{"type":"tool-call","toolCallId":"` + id + `","toolName":"screenshot","input":{"value":"value-` + n + `"}}`
		results += `{"type":"tool-result","toolCallId":"` + id + `","toolName":"screenshot","output":{"type":"text","value":"result-` + n + `"}}`
	}
	return calls + "|" + results
}

// 1399 'should handle conversation with multiple tool invocations that have
// step information', 1443 'mix of tool invocations and text' and 1547
// 'multiple tool invocations and user message at the end' (snapshot file):
// every step-start closes a block of one assistant and one tool message.
func TestR72GenConvert_StepBlocks(t *testing.T) {
	split := func(s string) (string, string) {
		for i := range s {
			if s[i] == '|' {
				return s[:i], s[i+1:]
			}
		}
		return s, ""
	}
	c1, r1 := split(r72genScreenshotStep("call-1"))
	c23, r23 := split(r72genScreenshotStep("call-2", "call-3"))
	c4, r4 := split(r72genScreenshotStep("call-4"))

	r72genCheckMessages(t, []UIMessage{{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		StepStartUIPart{}, TextUIPart{Text: "response"}, r72genScreenshot("call-1", "value-1", "result-1"),
		StepStartUIPart{}, r72genScreenshot("call-2", "value-2", "result-2"), r72genScreenshot("call-3", "value-3", "result-3"),
		StepStartUIPart{}, r72genScreenshot("call-4", "value-4", "result-4"),
	}}}, `[
		{"role":"assistant","content":[{"type":"text","text":"response"},`+c1+`]},
		{"role":"tool","content":[`+r1+`]},
		{"role":"assistant","content":[`+c23+`]},
		{"role":"tool","content":[`+r23+`]},
		{"role":"assistant","content":[`+c4+`]},
		{"role":"tool","content":[`+r4+`]}]`)

	r72genCheckMessages(t, []UIMessage{{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		StepStartUIPart{}, TextUIPart{Text: "i am gonna use tool1"}, r72genScreenshot("call-1", "value-1", "result-1"),
		StepStartUIPart{}, TextUIPart{Text: "i am gonna use tool2 and tool3"}, r72genScreenshot("call-2", "value-2", "result-2"), r72genScreenshot("call-3", "value-3", "result-3"),
		StepStartUIPart{}, r72genScreenshot("call-4", "value-4", "result-4"),
		StepStartUIPart{}, TextUIPart{Text: "final response"},
	}}}, `[
		{"role":"assistant","content":[{"type":"text","text":"i am gonna use tool1"},`+c1+`]},
		{"role":"tool","content":[`+r1+`]},
		{"role":"assistant","content":[{"type":"text","text":"i am gonna use tool2 and tool3"},`+c23+`]},
		{"role":"tool","content":[`+r23+`]},
		{"role":"assistant","content":[`+c4+`]},
		{"role":"tool","content":[`+r4+`]},
		{"role":"assistant","content":[{"type":"text","text":"final response"}]}]`)

	r72genCheckMessages(t, []UIMessage{
		{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
			StepStartUIPart{}, r72genScreenshot("call-1", "value-1", "result-1"),
			StepStartUIPart{}, r72genScreenshot("call-2", "value-2", "result-2"), r72genScreenshot("call-3", "value-3", "result-3"),
			StepStartUIPart{}, r72genScreenshot("call-4", "value-4", "result-4"),
			StepStartUIPart{}, TextUIPart{Text: "response"},
		}},
		{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Thanks!"}}},
	}, `[
		{"role":"assistant","content":[`+c1+`]},
		{"role":"tool","content":[`+r1+`]},
		{"role":"assistant","content":[`+c23+`]},
		{"role":"tool","content":[`+r23+`]},
		{"role":"assistant","content":[`+c4+`]},
		{"role":"tool","content":[`+r4+`]},
		{"role":"assistant","content":[{"type":"text","text":"response"}]},
		{"role":"user","content":[{"type":"text","text":"Thanks!"}]}]`)
}

var r72genWeatherUser = UIMessage{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "What is the weather in Tokyo?"}}}

const r72genWeatherUserJSON = `{"role":"user","content":[{"type":"text","text":"What is the weather in Tokyo?"}]}`
const r72genWeatherCallJSON = `{"type":"tool-call","toolCallId":"call-1","toolName":"weather","input":{"city":"Tokyo"}}`

// Approval round trips: 2381 'should preserve automatic approval metadata
// for approved tool results', 2475 'should convert a denied tool approval
// request and follow up text (static tool)', 2936 'tool output result with
// approval and follow up text', 3245 'tool error result with approval and
// follow up text' (inline snapshots).
func TestR72GenConvert_ApprovalRoundTrips(t *testing.T) {
	tokyo := map[string]interface{}{"city": "Tokyo"}
	followUp := func(text string) []UIMessagePart {
		return []UIMessagePart{StepStartUIPart{}, TextUIPart{Text: text}}
	}

	r72genCheckMessages(t, []UIMessage{r72genWeatherUser, {Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		StepStartUIPart{},
		&ToolUIPart{Type: "tool-weather", State: ToolStateOutputAvailable, ToolCallID: "call-1", Input: tokyo,
			Output:   map[string]interface{}{"weather": "Sunny"},
			Approval: &ToolUIPartApproval{ID: "approval-1", Approved: boolPtr(true), IsAutomatic: boolPtr(true), Reason: "trusted internal tool"}},
	}}}, `[`+r72genWeatherUserJSON+`,
		{"role":"assistant","content":[`+r72genWeatherCallJSON+`,{"type":"tool-approval-request","approvalId":"approval-1","isAutomatic":true,"toolCallId":"call-1"}]},
		{"role":"tool","content":[{"type":"tool-approval-response","approvalId":"approval-1","approved":true,"reason":"trusted internal tool"},
			{"type":"tool-result","toolCallId":"call-1","toolName":"weather","output":{"type":"json","value":{"weather":"Sunny"}}}]}]`)

	r72genCheckMessages(t, []UIMessage{r72genWeatherUser, {Role: UIMessageRoleAssistant, Parts: append([]UIMessagePart{
		StepStartUIPart{},
		&ToolUIPart{Type: "tool-weather", State: ToolStateApprovalResponded, ToolCallID: "call-1", Input: tokyo,
			Approval: &ToolUIPartApproval{ID: "approval-1", Approved: boolPtr(false), Reason: "I don't want to approve this"}},
	}, followUp("I was not able to retrieve the weather.")...)}}, `[`+r72genWeatherUserJSON+`,
		{"role":"assistant","content":[`+r72genWeatherCallJSON+`,{"type":"tool-approval-request","approvalId":"approval-1","toolCallId":"call-1"}]},
		{"role":"tool","content":[{"type":"tool-approval-response","approvalId":"approval-1","approved":false,"reason":"I don't want to approve this"},
			{"type":"tool-result","toolCallId":"call-1","toolName":"weather","output":{"type":"execution-denied","reason":"I don't want to approve this"}}]},
		{"role":"assistant","content":[{"type":"text","text":"I was not able to retrieve the weather."}]}]`)

	r72genCheckMessages(t, []UIMessage{r72genWeatherUser, {Role: UIMessageRoleAssistant, Parts: append([]UIMessagePart{
		StepStartUIPart{},
		&ToolUIPart{Type: "tool-weather", State: ToolStateOutputAvailable, ToolCallID: "call-1", Input: tokyo,
			Output:   map[string]interface{}{"weather": "Sunny", "temperature": "20°C"},
			Approval: &ToolUIPartApproval{ID: "approval-1", Approved: boolPtr(true)}},
	}, followUp("The weather in Tokyo is sunny.")...)}}, `[`+r72genWeatherUserJSON+`,
		{"role":"assistant","content":[`+r72genWeatherCallJSON+`,{"type":"tool-approval-request","approvalId":"approval-1","toolCallId":"call-1"}]},
		{"role":"tool","content":[{"type":"tool-approval-response","approvalId":"approval-1","approved":true},
			{"type":"tool-result","toolCallId":"call-1","toolName":"weather","output":{"type":"json","value":{"temperature":"20°C","weather":"Sunny"}}}]},
		{"role":"assistant","content":[{"type":"text","text":"The weather in Tokyo is sunny."}]}]`)

	r72genCheckMessages(t, []UIMessage{r72genWeatherUser, {Role: UIMessageRoleAssistant, Parts: append([]UIMessagePart{
		StepStartUIPart{},
		&ToolUIPart{Type: "tool-weather", State: ToolStateOutputError, ToolCallID: "call-1", Input: tokyo,
			ErrorText: "Error: Fetching weather data failed",
			Approval:  &ToolUIPartApproval{ID: "approval-1", Approved: boolPtr(true)}},
	}, followUp("The weather in Tokyo is sunny.")...)}}, `[`+r72genWeatherUserJSON+`,
		{"role":"assistant","content":[`+r72genWeatherCallJSON+`,{"type":"tool-approval-request","approvalId":"approval-1","toolCallId":"call-1"}]},
		{"role":"tool","content":[{"type":"tool-approval-response","approvalId":"approval-1","approved":true},
			{"type":"tool-result","toolCallId":"call-1","toolName":"weather","output":{"type":"error-text","value":"Error: Fetching weather data failed"}}]},
		{"role":"assistant","content":[{"type":"text","text":"The weather in Tokyo is sunny."}]}]`)
}

// Provider metadata and file parts: 175 user text with provider metadata,
// 242 user file with provider metadata, 347 no filename when not provided,
// 431 assistant text with provider metadata, 569 / 600 / 633 / 670 assistant
// file parts (plain, filename, provider metadata, providerReference) and 1227
// 'should propagate provider metadata to provider-executed tool-result'.
func TestR72GenConvert_ProviderMetadataAndFiles(t *testing.T) {
	md := map[string]interface{}{"testProvider": map[string]interface{}{"signature": "1234567890"}}
	r72genCheckMessages(t, []UIMessage{{Role: UIMessageRoleUser, Parts: []UIMessagePart{
		TextUIPart{Text: "Hello, AI!", ProviderMetadata: md},
		FileUIPart{MediaType: "image/png", URL: "https://example.com/image.png", ProviderMetadata: md},
		FileUIPart{MediaType: "image/jpeg", URL: "https://example.com/image.jpg"},
	}}}, `[{"role":"user","content":[
		{"type":"text","text":"Hello, AI!","providerOptions":{"testProvider":{"signature":"1234567890"}}},
		{"type":"file","mediaType":"image/png","data":{"type":"url","url":"https://example.com/image.png"},"providerOptions":{"testProvider":{"signature":"1234567890"}}},
		{"type":"file","mediaType":"image/jpeg","data":{"type":"url","url":"https://example.com/image.jpg"}}]}]`)

	r72genCheckMessages(t, []UIMessage{{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		TextUIPart{Text: "Hello, human!", ProviderMetadata: md},
		FileUIPart{MediaType: "image/png", URL: "https://example.com/image.png"},
		FileUIPart{MediaType: "image/png", URL: "https://example.com/image.png", Filename: "image.png", ProviderMetadata: md},
		FileUIPart{MediaType: "image/png", ProviderReference: map[string]string{"openai": "file-abc"}},
	}}}, `[{"role":"assistant","content":[
		{"type":"text","text":"Hello, human!","providerOptions":{"testProvider":{"signature":"1234567890"}}},
		{"type":"file","mediaType":"image/png","data":{"type":"url","url":"https://example.com/image.png"}},
		{"type":"file","mediaType":"image/png","filename":"image.png","data":{"type":"url","url":"https://example.com/image.png"},"providerOptions":{"testProvider":{"signature":"1234567890"}}},
		{"type":"file","mediaType":"image/png","data":{"type":"reference","reference":{"openai":"file-abc"}}}]}]`)

	r72genCheckMessages(t, []UIMessage{{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		&ToolUIPart{Type: "tool-web_search", State: ToolStateOutputAvailable, ToolCallID: "call-1",
			Input: map[string]interface{}{"query": "q"}, Output: map[string]interface{}{"r": 1},
			ProviderExecuted: boolPtr(true), CallProviderMetadata: md},
	}}}, `[{"role":"assistant","content":[
		{"type":"tool-call","toolCallId":"call-1","toolName":"web_search","input":{"query":"q"},"providerExecuted":true,"providerOptions":{"testProvider":{"signature":"1234567890"}}},
		{"type":"tool-result","toolCallId":"call-1","toolName":"web_search","output":{"type":"json","value":{"r":1}},"providerExecuted":true,"providerOptions":{"testProvider":{"signature":"1234567890"}}}]}]`)
}

// generate-text.test.ts 7135 '2 steps: initial, tool-result with prepareStep
// > doGenerate should be called with the correct arguments': prepareStep's
// model, tool choice, instructions, messages and active tools apply to their
// step only; step 2 gets the outer (unset, so auto) tool choice and no tools.
func TestR72Gen_PrepareStepDoGenerateArguments(t *testing.T) {
	trueModel := r72genSeqModel(t,
		&types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}}, FinishReason: types.FinishReasonToolCalls, Usage: *usage(10, 5)},
		r72genText("Hello, world!"))
	newInput := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "new input from prepareStep"}}}}
	tool := r72genValueTool("tool1")
	var toolMessages []types.Message
	tool.Execute = func(_ context.Context, args map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		toolMessages = o.Messages
		return "result1", nil
	}
	var runtimeContexts []interface{}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t), Prompt: "test-input", Tools: []types.Tool{tool},
		RuntimeContext: map[string]interface{}{"context": "state1"},
		StopWhen:       []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			runtimeContexts = append(runtimeContexts, s.RuntimeContext)
			switch s.StepNumber {
			case 0:
				if len(s.Steps) != 0 {
					t.Errorf("step 0 steps = %d", len(s.Steps))
				}
				sys := "system-message-0"
				return PrepareStepOptions{Model: trueModel, ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "tool1"},
					Instructions: &sys, Messages: newInput, RuntimeContext: map[string]interface{}{"context": "state2"}}
			case 1:
				if len(s.Steps) != 1 {
					t.Errorf("step 1 steps = %d", len(s.Steps))
				}
				sys := "system-message-1"
				return PrepareStepOptions{Model: trueModel, ActiveTools: []string{}, Instructions: &sys, RuntimeContext: map[string]interface{}{"context": "state3"}}
			}
			return PrepareStepOptions{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hello, world!" {
		t.Fatalf("text = %q", res.Text)
	}
	if !reflect.DeepEqual(runtimeContexts, []interface{}{map[string]interface{}{"context": "state1"}, map[string]interface{}{"context": "state2"}}) {
		t.Fatalf("prepareStep runtime contexts = %#v", runtimeContexts)
	}
	if !reflect.DeepEqual(r72genJSON(t, toolMessages), r72genJSON(t, newInput)) {
		t.Fatalf("tool messages = %#v", toolMessages)
	}
	calls := trueModel.GenerateCalls
	if len(calls) != 2 {
		t.Fatalf("model calls = %d", len(calls))
	}
	first, second := calls[0], calls[1]
	if first.Prompt.System != "system-message-0" || !reflect.DeepEqual(r72genJSON(t, first.Prompt.Messages), r72genJSON(t, newInput)) {
		t.Fatalf("step 1 prompt = %+v", first.Prompt)
	}
	if first.ToolChoice.Type != types.ToolChoiceTool || first.ToolChoice.ToolName != "tool1" || len(first.Tools) != 1 || first.Tools[0].Name != "tool1" {
		t.Fatalf("step 1 tools = %+v, choice = %+v", first.Tools, first.ToolChoice)
	}
	if second.Prompt.System != "system-message-1" || second.ToolChoice.Type != types.ToolChoiceAuto || len(second.Tools) != 0 {
		t.Fatalf("step 2 system = %q, choice = %+v, tools = %+v", second.Prompt.System, second.ToolChoice, second.Tools)
	}
	msgs := second.Prompt.Messages
	if len(msgs) != 3 || msgs[0].Role != types.RoleUser || msgs[1].Role != types.RoleAssistant || msgs[2].Role != types.RoleTool {
		t.Fatalf("step 2 messages = %+v", msgs)
	}
	call, ok := msgs[1].Content[0].(types.ToolCallContent)
	if !ok || call.ToolCallID != "call-1" || call.ToolName != "tool1" || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"value": "value"}) || call.ProviderExecuted {
		t.Fatalf("step 2 assistant = %#v", msgs[1].Content)
	}
	result, ok := msgs[2].Content[0].(types.ToolResultContent)
	if !ok || result.ToolCallID != "call-1" || result.ToolName != "tool1" || result.Output == nil || result.Output.Type != types.ToolResultOutputText || result.Output.Value != "result1" {
		t.Fatalf("step 2 tool = %#v", msgs[2].Content)
	}
}

func r72genNoopTool(name string) types.Tool {
	return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, nil
		}}
}

func r72genToolNames(tools []types.Tool) []string {
	var out []string
	for _, tl := range tools {
		out = append(out, tl.Name)
	}
	return out
}

// generate-text.test.ts tool callers: 580 're-announces a caller message when
// the catalog reverts', 714 'distinguishes the direct marker from a caller
// named direct' and 771 'rejects caller names that are not caller-capable
// tools'.
func TestR72Gen_ToolCallers(t *testing.T) {
	// 580: the announcement follows the step's active tools; a catalog that
	// changes back is announced again.
	codeMode := r72genNoopTool("code_mode")
	codeMode.ExperimentalToolCaller = &types.ToolCallerDefinition{
		Type: types.ToolCallerTypeLocal,
		Bind: func([]types.Tool) types.Tool { return r72genNoopTool("bound") },
		PrepareModelMessage: func(tools []types.Tool) *string {
			names := r72genToolNames(tools)
			s := "Available caller tools: "
			for i, n := range names {
				if i > 0 {
					s += ", "
				}
				s += n
			}
			s += "."
			return &s
		},
	}
	var mu sync.Mutex
	var lastUser []string
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		last := ""
		for _, m := range o.Prompt.Messages {
			if m.Role == types.RoleUser {
				last = m.Content[0].(types.TextContent).Text
			}
		}
		lastUser = append(lastUser, last)
		if n := len(lastUser); n < 3 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-" + string(rune('0'+n)), ToolName: "code_mode", RawArguments: `{}`}}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "Done.", FinishReason: types.FinishReasonStop}, nil
	}}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "Use the available tool.",
		Tools:                   []types.Tool{codeMode, r72genNoopTool("toolA"), r72genNoopTool("toolB")},
		ExperimentalToolCallers: ExperimentalToolCallers{"toolA": {"code_mode"}, "toolB": {"code_mode"}},
		StopWhen:                []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			if s.StepNumber == 1 {
				return PrepareStepOptions{ActiveTools: []string{"code_mode", "toolB"}}
			}
			return PrepareStepOptions{ActiveTools: []string{"code_mode", "toolA"}}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Available caller tools: toolA.", "Available caller tools: toolB.", "Available caller tools: toolA."}; !reflect.DeepEqual(lastUser, want) {
		t.Fatalf("last user messages = %q, want %q", lastUser, want)
	}

	// 714: "direct" is a caller tool name; DirectToolCall is the marker.
	direct := r72genNoopTool("direct")
	direct.ExperimentalToolCaller = &types.ToolCallerDefinition{
		Type: types.ToolCallerTypeLocal,
		Bind: func(tools []types.Tool) types.Tool {
			names := r72genToolNames(tools)
			return types.Tool{Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return names, nil
			}}
		},
	}
	var modelTools []string
	dmodel := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		for _, tl := range o.Tools {
			modelTools = append(modelTools, tl.Name)
		}
		return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "direct", RawArguments: `{}`}}, FinishReason: types.FinishReasonToolCalls}, nil
	}}
	inventory := types.Tool{Name: "getInventory", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sku": map[string]interface{}{"type": "string"}}, "required": []interface{}{"sku"}},
		Execute: func(_ context.Context, a map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return a, nil
		}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: dmodel, Prompt: "Check inventory.", Tools: []types.Tool{direct, inventory},
		ExperimentalToolCallers: ExperimentalToolCallers{"getInventory": {"direct", DirectToolCall}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(modelTools, []string{"direct", "getInventory"}) {
		t.Fatalf("model tools = %v", modelTools)
	}
	if len(res.ToolResults) != 1 || !reflect.DeepEqual(res.ToolResults[0].Result, []string{"getInventory"}) {
		t.Fatalf("tool results = %+v", res.ToolResults)
	}

	// 771
	_, err = GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t), Prompt: "Check inventory.", Tools: []types.Tool{r72genNoopTool("getInventory")},
		ExperimentalToolCallers: ExperimentalToolCallers{"getInventory": {"getInventory"}},
	})
	if err == nil || !strings.Contains(err.Error(), `tool "getInventory" contains an invalid caller.`) {
		t.Fatalf("err = %v", err)
	}
}
