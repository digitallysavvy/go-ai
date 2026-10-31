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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 73 (gen): ToolErrorContent.Err (owner ruling 2026-10-31, option c)
// and generate-text.test.ts ports.

type r73genToolErr struct{ code int }

func (e *r73genToolErr) Error() string { return "tool failed" }

func r73genFailingTool(name string) types.Tool {
	return types.Tool{
		Name:       name,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, &r73genToolErr{code: 42}
		},
	}
}

func r73genToolErrors(content []types.ContentPart) []types.ToolErrorContent {
	var out []types.ToolErrorContent
	for _, p := range content {
		if te, ok := p.(types.ToolErrorContent); ok {
			out = append(out, te)
		}
	}
	return out
}

// TS tool-error part: error is the thrown value. Go keeps the text in Error
// and the typed error in Err; the JSON is unchanged.
func TestR73Gen_GenerateTextToolErrorContentKeepsTypedErr(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r73genFailingTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tes := r73genToolErrors(res.Steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", res.Steps[0].Content)
	}
	te := tes[0]
	var target *r73genToolErr
	if !errors.As(te.Err, &target) || target.code != 42 {
		t.Fatalf("Err = %#v, want the tool's *r73genToolErr", te.Err)
	}
	if te.Error != "tool failed" || te.ErrorText != "Error: tool failed" {
		t.Fatalf("Error = %#v, ErrorText = %q", te.Error, te.ErrorText)
	}
	b, err := json.Marshal(te)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"Err"`) || strings.Contains(string(b), `"err"`) {
		t.Fatalf("JSON = %s, Err must not be serialized", b)
	}
	// The result's content (TS steps.flatMap(step => step.content)) holds the
	// same typed error.
	if got := r73genToolErrors(res.Content); len(got) != 1 || got[0].Err != te.Err {
		t.Fatalf("result content tool errors = %#v", got)
	}
}

// The SDK's own errors (invalid input, unknown tool) are typed too.
func TestR73Gen_GenerateTextInvalidInputToolErrorErr(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": 1}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tes := r73genToolErrors(res.Steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", res.Steps[0].Content)
	}
	var target *InvalidToolInputError
	if !errors.As(tes[0].Err, &target) || target.ToolName != "tool1" {
		t.Fatalf("Err = %#v, want *InvalidToolInputError", tes[0].Err)
	}
	if tes[0].Error != target.Error() {
		t.Fatalf("Error = %#v, want %q", tes[0].Error, target.Error())
	}
}

// StreamText step content: local tool failures go through
// toolResultsToContentParts and keep the typed error.
func TestR73Gen_StreamTextToolErrorContentKeepsTypedErr(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			n++
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "test-input",
		Tools:    []types.Tool{r73genFailingTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	steps := res.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	tes := r73genToolErrors(steps[0].Content)
	if len(tes) != 1 {
		t.Fatalf("step 0 content = %#v, want one tool-error", steps[0].Content)
	}
	var target *r73genToolErr
	if !errors.As(tes[0].Err, &target) || target.code != 42 {
		t.Fatalf("Err = %#v, want the tool's *r73genToolErr", tes[0].Err)
	}
}

// toolResultContentFromToolResult: a provider-executed failure keeps a Go
// error, but a structured provider error value leaves Err nil.
func TestR73Gen_ProviderToolErrorErr(t *testing.T) {
	goErr := &r73genToolErr{code: 7}
	p := toolResultContentFromToolResult(types.ToolResult{ToolCallID: "c", ToolName: "web_search", Error: goErr, ProviderExecuted: true})
	te, ok := p.(types.ToolErrorContent)
	if !ok || te.Err != goErr || te.Error != "tool failed" {
		t.Fatalf("part = %#v, want Err = the provider's Go error", p)
	}
	structured := map[string]interface{}{"errorCode": "unavailable"}
	p = toolResultContentFromToolResult(types.ToolResult{ToolCallID: "c", ToolName: "web_search", Error: errors.New("x"), Result: structured, ProviderExecuted: true})
	te, ok = p.(types.ToolErrorContent)
	if !ok || te.Err != nil {
		t.Fatalf("part = %#v, want Err nil for a structured provider error", p)
	}
	// providerToolResultFromError carries a provider part's Err to the
	// tool result.
	tr := providerToolResultFromError(types.ToolErrorContent{ToolCallID: "c", Error: "tool failed", Err: goErr})
	if tr.Error != goErr {
		t.Fatalf("tool result error = %#v, want the part's Err", tr.Error)
	}
}

// TS StepResult.reasoning = content.filter(reasoning | reasoning-file): the
// reasoning file parts are StepResult.ReasoningFiles() in GenerateText and
// StreamText (carry-in item 2).
func TestR73Gen_StepReasoningFiles(t *testing.T) {
	f := types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1, 2}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, &types.GenerateResult{
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "think"},
				f,
				types.TextContent{Text: "done"},
			},
			Text: "done", FinishReason: types.FinishReasonStop,
		}),
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.FinalStep.ReasoningFiles()
	if len(got) != 1 || got[0].MediaType != "image/png" || string(got[0].Data) != "\x01\x02" {
		t.Fatalf("GenerateText ReasoningFiles = %#v", got)
	}
	if len(res.FinalStep.Reasoning) != 1 || res.FinalStep.Reasoning[0].Text != "think" {
		t.Fatalf("Reasoning = %#v", res.FinalStep.Reasoning)
	}

	sres, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				fc := f
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeReasoning, Reasoning: "think"},
					{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &fc},
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		},
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sres.ReadAll(); err != nil {
		t.Fatal(err)
	}
	got = sres.FinalStep().ReasoningFiles()
	if len(got) != 1 || got[0].MediaType != "image/png" {
		t.Fatalf("StreamText ReasoningFiles = %#v", got)
	}
	// Pointer parts count too; other parts do not.
	step := types.StepResult{Content: []types.ContentPart{&f, (*types.ReasoningFileContent)(nil), types.FileContent{MediaType: "image/png"}}}
	if got := step.ReasoningFiles(); len(got) != 1 {
		t.Fatalf("pointer ReasoningFiles = %#v", got)
	}
	if got := (types.StepResult{}).ReasoningFiles(); got != nil {
		t.Fatalf("empty ReasoningFiles = %#v, want nil", got)
	}
}

// TS stream-object.ts: providerMetadata is the finish part's
// (`providerMetadata = chunk.providerMetadata` in 'finish'); metadata on
// text parts (an OpenAI Responses itemId) is not the call's (carry-in
// item 3).
func TestR73Gen_StreamObjectProviderMetadataFromFinishOnly(t *testing.T) {
	itemMD := json.RawMessage(`{"openai":{"itemId":"msg-1"}}`)
	text := textChunks(`{ "content": "x" }`)
	for i := range text {
		text[i].ProviderMetadata = itemMD
	}
	finish := finishChunk()
	finish.ProviderMetadata = json.RawMessage(`{"testProvider":{"key":"value"}}`)
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(append([]provider.StreamChunk(nil), text...), finish)...), Prompt: "prompt", Schema: contentSchema(),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"testProvider": map[string]interface{}{"key": "value"}}
	if b, _ := json.Marshal(res.ProviderMetadata); string(b) != `{"testProvider":{"key":"value"}}` {
		t.Fatalf("ProviderMetadata = %#v, want %#v", res.ProviderMetadata, want)
	}

	// A finish part without metadata: TS providerMetadata is undefined.
	res, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(append([]provider.StreamChunk(nil), text...), finishChunk())...), Prompt: "prompt", Schema: contentSchema(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMetadata != nil {
		t.Fatalf("ProviderMetadata = %#v, want nil", res.ProviderMetadata)
	}
}

// generate-text.test.ts 955 'should not synthesize a client tool error for an
// invalid provider-executed tool call'.
func TestR73Gen_InvalidProviderExecutedCallNoClientToolError(t *testing.T) {
	providerErr := map[string]interface{}{"type": "web_search_tool_result_error", "errorCode": "invalid_tool_input"}
	isErr := true
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, &types.GenerateResult{
			FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_use",
			ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true}},
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "call-1", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "call-1", ToolName: "web_search", Result: providerErr, IsError: &isErr, ProviderExecuted: true},
			},
		}),
		Tools: []types.Tool{{
			Name: "web_search", Type: types.ToolTypeProviderDefined, ProviderID: "test.web_search", ProviderExecuted: true,
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}},
		}},
		Prompt: "Search the web.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("content = %#v, want tool-call + tool-error", res.Content)
	}
	call, ok := res.Content[0].(types.ToolCallContent)
	if !ok || call.ToolCallID != "call-1" || call.ToolName != "web_search" || !call.Invalid || !call.Dynamic || !call.ProviderExecuted {
		t.Fatalf("content[0] = %#v, want invalid dynamic provider-executed tool-call", res.Content[0])
	}
	if s, _ := call.Error.(string); !strings.HasPrefix(s, "Invalid input for tool web_search: ") {
		t.Fatalf("content[0].Error = %#v", call.Error)
	}
	te, ok := res.Content[1].(types.ToolErrorContent)
	if !ok || te.ToolCallID != "call-1" || te.ToolName != "web_search" || !te.Dynamic || !te.ProviderExecuted || te.Err != nil {
		t.Fatalf("content[1] = %#v, want dynamic provider-executed tool-error", res.Content[1])
	}
	if !reflect.DeepEqual(te.Error, providerErr) {
		t.Fatalf("tool-error error = %#v, want the provider error object", te.Error)
	}
	if len(te.Input) != 0 || te.Input == nil {
		t.Fatalf("tool-error input = %#v, want {}", te.Input)
	}
	want := `[{"role":"assistant","content":[
		{"type":"tool-call","toolCallId":"call-1","toolName":"web_search","input":{},"providerExecuted":true},
		{"type":"tool-result","toolCallId":"call-1","toolName":"web_search","output":{"type":"error-json","value":{"errorCode":"invalid_tool_input","type":"web_search_tool_result_error"}}}]}]`
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if g := r72genTSMessages(t, res.ResponseMessages); !reflect.DeepEqual(g, w) {
		gj, _ := json.Marshal(g)
		t.Fatalf("response messages =\n%s\nwant\n%s", gj, want)
	}
}

// r73genCallStep is a tool-calls step calling tool1 with value.
func r73genCallStep(id, value string) *types.GenerateResult {
	return r72genCalls(types.ToolCall{ID: id, ToolName: "tool1", Arguments: map[string]interface{}{"value": value}, RawArguments: `{ "value": "` + value + `" }`})
}

// generate-text.test.ts 1419 'should send warnings from all steps to
// onFinish'.
func TestR73Gen_OnEndWarningsFromAllSteps(t *testing.T) {
	w0 := types.Warning{Type: "other", Message: "step 0 warning"}
	w1 := types.Warning{Type: "other", Message: "step 1 warning"}
	s0 := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"})
	s0.Warnings = []types.Warning{w0}
	s1 := r72genText("Done.")
	s1.Warnings = []types.Warning{w1}
	var mu sync.Mutex
	var end OnFinishEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, s0, s1),
		Prompt: "prompt",
		Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		StopWhen:   []StopCondition{StepCountIs(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(end.Warnings, []types.Warning{w0, w1}) {
		t.Fatalf("onEnd warnings = %#v", end.Warnings)
	}
	if len(end.Steps) != 2 || !reflect.DeepEqual(end.Steps[1].Warnings, []types.Warning{w1}) {
		t.Fatalf("last step warnings = %#v", end.Steps)
	}
}

// generate-text.test.ts 2326 'should allow prepareStep to replace a required
// tool choice'.
func TestR73Gen_PrepareStepReplacesRequiredToolChoice(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:      r72genSeqModel(t, r72genText("No tool call.")),
		Tools:      []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}}},
		ToolChoice: types.RequiredToolChoice(),
		PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{ToolChoice: types.AutoToolChoice()}
		},
		Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "No tool call." {
		t.Fatalf("text = %q", res.Text)
	}
}

// generate-text.test.ts 3486 / 3509 / 3593 / 4248: onStepStart steps (empty
// on the first step, prior results and text afterwards) and matching step
// numbers with onStepFinish.
func TestR73Gen_OnStepStartSteps(t *testing.T) {
	var mu sync.Mutex
	var starts []OnStepStartEvent
	var startNums, finishNums []int
	s0 := r73genCallStep("call-1", "step0")
	s0.Text = "Thinking..."
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    r72genSeqModel(t, s0, r73genCallStep("call-2", "step1"), r72genText("Final answer.")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(4)},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			starts = append(starts, e)
			startNums = append(startNums, e.StepNumber)
		},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			finishNums = append(finishNums, e.StepNumber)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 3 {
		t.Fatalf("step starts = %d", len(starts))
	}
	if starts[0].StepNumber != 0 || starts[0].Steps == nil || len(starts[0].Steps) != 0 {
		t.Fatalf("step 0 steps = %#v, want an empty non-nil array (TS [])", starts[0].Steps)
	}
	s := starts[1].Steps
	if len(s) != 1 || s[0].FinishReason != types.FinishReasonToolCalls || len(s[0].ToolCalls) != 1 || s[0].ToolCalls[0].ToolName != "tool1" ||
		len(s[0].ToolResults) != 1 || s[0].ToolResults[0].Result != "step0-result" || s[0].Text != "Thinking..." {
		t.Fatalf("step 1 steps = %#v", s)
	}
	s = starts[2].Steps
	if len(s) != 2 || s[0].FinishReason != types.FinishReasonToolCalls || s[1].FinishReason != types.FinishReasonToolCalls || s[1].ToolResults[0].Result != "step1-result" {
		t.Fatalf("step 2 steps = %#v", s)
	}
	if !reflect.DeepEqual(startNums, []int{0, 1, 2}) || !reflect.DeepEqual(finishNums, startNums) {
		t.Fatalf("start numbers %v, finish numbers %v", startNums, finishNums)
	}
}

// generate-text.test.ts 3671 / 3865: onStepStart sees the runtimeContext and
// toolsContext prepareStep returned; the tool sees the updated context.
func TestR73Gen_OnStepStartPreparedContexts(t *testing.T) {
	var mu sync.Mutex
	var starts []OnStepStartEvent
	var prepTools []interface{}
	var recorded interface{}
	tool := r72genValueTool("tool1")
	tool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"label": map[string]interface{}{"type": "string"}}, "required": []interface{}{"label"}})
	tool.Execute = func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		mu.Lock()
		recorded = o.ToolContext
		mu.Unlock()
		return "test-result", nil
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:          r72genSeqModel(t, r73genCallStep("call-1", "test"), r72genText("Done.")),
		Prompt:         "test-input",
		Tools:          []types.Tool{tool},
		RuntimeContext: map[string]interface{}{"counter": 0},
		ToolsContext:   map[string]interface{}{"tool1": map[string]interface{}{"label": "initial"}},
		StopWhen:       []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, p PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepTools = append(prepTools, p.ToolsContext)
			mu.Unlock()
			out := PrepareStepOptions{RuntimeContext: map[string]interface{}{"counter": p.RuntimeContext.(map[string]interface{})["counter"].(int) + 1, "stepNumber": p.StepNumber}}
			if p.StepNumber == 0 {
				out.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"label": "updated"}}
			}
			return out
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			starts = append(starts, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 2 {
		t.Fatalf("starts = %d", len(starts))
	}
	if !reflect.DeepEqual(starts[0].RuntimeContext, map[string]interface{}{"counter": 1, "stepNumber": 0}) ||
		!reflect.DeepEqual(starts[1].RuntimeContext, map[string]interface{}{"counter": 2, "stepNumber": 1}) {
		t.Fatalf("runtime contexts = %#v, %#v", starts[0].RuntimeContext, starts[1].RuntimeContext)
	}
	initial := map[string]interface{}{"tool1": map[string]interface{}{"label": "initial"}}
	updated := map[string]interface{}{"tool1": map[string]interface{}{"label": "updated"}}
	if !reflect.DeepEqual(prepTools, []interface{}{initial, updated}) {
		t.Fatalf("prepareStep toolsContexts = %#v", prepTools)
	}
	if !reflect.DeepEqual(starts[0].ToolsContext, updated) || !reflect.DeepEqual(starts[1].ToolsContext, updated) {
		t.Fatalf("step start toolsContexts = %#v, %#v", starts[0].ToolsContext, starts[1].ToolsContext)
	}
	if !reflect.DeepEqual(recorded, map[string]interface{}{"label": "updated"}) {
		t.Fatalf("tool context = %#v", recorded)
	}
}

// generate-text.test.ts 3973 'should report the step that failed in a
// multi-step run'.
func TestR73Gen_OnStepErrorReportsFailedStep(t *testing.T) {
	providerErr := errors.New("provider failure")
	var mu sync.Mutex
	n := 0
	var stepErrs []OnStepErrorEvent
	ends := 0
	zero := 0
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			n++
			if n > 1 {
				return nil, providerErr
			}
			return r73genCallStep("call-1", "value"), nil
		}},
		Tools:      []types.Tool{r72genValueTool("tool1")},
		Prompt:     "test-input",
		MaxRetries: &zero,
		StopWhen:   []StopCondition{StepCountIs(3)},
		OnStepError: func(_ context.Context, e OnStepErrorEvent) {
			mu.Lock()
			defer mu.Unlock()
			stepErrs = append(stepErrs, e)
		},
		OnStepFinishEvent: func(context.Context, OnStepFinishEvent) { mu.Lock(); ends++; mu.Unlock() },
	})
	if !errors.Is(err, providerErr) {
		t.Fatalf("err = %v, want the provider error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if ends != 1 || len(stepErrs) != 1 || stepErrs[0].StepNumber != 1 || !errors.Is(stepErrs[0].Error, providerErr) {
		t.Fatalf("step ends = %d, step errors = %#v", ends, stepErrs)
	}
}

func r73genToolCallbackOpts(t *testing.T, tool types.Tool, results ...*types.GenerateResult) GenerateTextOptions {
	return GenerateTextOptions{Model: r72genSeqModel(t, results...), Tools: []types.Tool{tool}, Prompt: "prompt"}
}

// generate-text.test.ts 4509 / 4543 / 4897 / 4931: the deprecated
// experimental_onToolCallStart/Finish run only when the new callback is
// unset.
func TestR73Gen_DeprecatedToolCallbacksFallback(t *testing.T) {
	for _, preferNew := range []bool{false, true} {
		var mu sync.Mutex
		var order []string
		add := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
		opts := r73genToolCallbackOpts(t, r72genValueTool("tool1"), r73genCallStep("call-1", "test"))
		opts.OnToolCallStart = func(context.Context, OnToolCallStartEvent) { add("experimental_onToolCallStart") }
		opts.OnToolCallFinish = func(context.Context, OnToolCallFinishEvent) { add("experimental_onToolCallFinish") }
		want := []string{"experimental_onToolCallStart", "experimental_onToolCallFinish"}
		if preferNew {
			opts.OnToolExecutionStart = func(context.Context, OnToolCallStartEvent) { add("onToolExecutionStart") }
			opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { add("onToolExecutionEnd") }
			want = []string{"onToolExecutionStart", "onToolExecutionEnd"}
		}
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if !reflect.DeepEqual(order, want) {
			t.Errorf("preferNew=%v: order = %v, want %v", preferNew, order, want)
		}
		mu.Unlock()
	}
}

// generate-text.test.ts 4642 / 4800 / 5179: one start/end per call, none
// for tools without execute.
func TestR73Gen_ToolExecutionCallbacksPerCall(t *testing.T) {
	var mu sync.Mutex
	var starts []OnToolCallStartEvent
	two := r72genCalls(
		types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "a"}},
		types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "b"}})
	opts := r73genToolCallbackOpts(t, r72genValueTool("tool1"), two)
	opts.OnToolExecutionStart = func(_ context.Context, e OnToolCallStartEvent) { mu.Lock(); starts = append(starts, e); mu.Unlock() }
	if _, err := GenerateText(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(starts) != 2 {
		t.Fatalf("starts = %#v", starts)
	}
	for i, id := range []string{"call-1", "call-2"} {
		e := starts[i]
		if e.ToolCall.ID != id || e.ToolCall.ToolName != "tool1" || e.ToolContext != nil || len(e.Messages) != 1 || e.Messages[0].Role != types.RoleUser {
			t.Errorf("start %d = %#v", i, e)
		}
	}
	if starts[0].ToolCall.Arguments["value"] != "a" || starts[1].ToolCall.Arguments["value"] != "b" {
		t.Errorf("start inputs = %#v, %#v", starts[0].ToolCall.Arguments, starts[1].ToolCall.Arguments)
	}
	mu.Unlock()

	fired := 0
	noExec := r72genValueTool("tool1")
	noExec.Execute = nil
	opts = r73genToolCallbackOpts(t, noExec, r73genCallStep("call-1", "test"))
	opts.OnToolExecutionStart = func(context.Context, OnToolCallStartEvent) { mu.Lock(); fired++; mu.Unlock() }
	opts.OnToolExecutionEnd = func(context.Context, OnToolCallFinishEvent) { mu.Lock(); fired++; mu.Unlock() }
	if _, err := GenerateText(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if fired != 0 {
		t.Fatalf("callbacks fired %d times for a tool without execute", fired)
	}
}

// generate-text.test.ts 4833 / 5212 / 5285: the tool context reaches the
// start and end events, on success and on error (toolOutput is a tool-error
// with the thrown error).
func TestR73Gen_ToolExecutionCallbacksContext(t *testing.T) {
	toolErr := errors.New("Tool execution failed")
	for _, fail := range []bool{false, true} {
		tool := r72genValueTool("tool1")
		tool.ContextSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []interface{}{"context"}})
		if fail {
			tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, toolErr
			}
		}
		var mu sync.Mutex
		var starts []OnToolCallStartEvent
		var ends []OnToolCallFinishEvent
		opts := r73genToolCallbackOpts(t, tool, r73genCallStep("call-1", "test"))
		opts.ToolsContext = map[string]interface{}{"tool1": map[string]interface{}{"context": "test"}}
		opts.OnToolExecutionStart = func(_ context.Context, e OnToolCallStartEvent) { mu.Lock(); starts = append(starts, e); mu.Unlock() }
		opts.OnToolExecutionEnd = func(_ context.Context, e OnToolCallFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() }
		if _, err := GenerateText(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		want := map[string]interface{}{"context": "test"}
		if len(starts) != 1 || !reflect.DeepEqual(starts[0].ToolContext, want) {
			t.Fatalf("fail=%v starts = %#v", fail, starts)
		}
		if len(ends) != 1 || !reflect.DeepEqual(ends[0].ToolContext, want) {
			t.Fatalf("fail=%v ends = %#v", fail, ends)
		}
		out := ends[0].ToolOutput
		if out.ToolCallID != "call-1" || out.ToolName != "tool1" || out.Dynamic || out.Input["value"] != "test" {
			t.Fatalf("fail=%v toolOutput = %#v", fail, out)
		}
		if fail {
			if out.Error != toolErr || out.Result != nil {
				t.Fatalf("toolOutput = %#v, want the thrown error", out)
			}
		} else if out.Error != nil || out.Result != "test-result" {
			t.Fatalf("toolOutput = %#v, want test-result", out)
		}
		mu.Unlock()
	}
}

// generate-text.test.ts 5444 'should fire tool call callbacks for each tool
// in a multi-step loop': the second step's events carry the conversation so
// far (prompt, assistant tool call, tool result).
func TestR73Gen_ToolExecutionCallbacksMultiStep(t *testing.T) {
	var mu sync.Mutex
	var starts []OnToolCallStartEvent
	var ends []OnToolCallFinishEvent
	opts := r73genToolCallbackOpts(t, r72genValueTool("tool1"), r73genCallStep("call-1", "step0"), r73genCallStep("call-2", "step1"), r72genText("Done."))
	opts.StopWhen = []StopCondition{StepCountIs(4)}
	opts.OnToolExecutionStart = func(_ context.Context, e OnToolCallStartEvent) { mu.Lock(); starts = append(starts, e); mu.Unlock() }
	opts.OnToolExecutionEnd = func(_ context.Context, e OnToolCallFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() }
	if _, err := GenerateText(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 2 || len(ends) != 2 {
		t.Fatalf("starts = %d, ends = %d", len(starts), len(ends))
	}
	if starts[0].ToolCall.ID != "call-1" || starts[1].ToolCall.ID != "call-2" || ends[0].ToolOutput.Result != "step0-result" || ends[1].ToolOutput.Result != "step1-result" {
		t.Fatalf("events = %#v / %#v", starts, ends)
	}
	want := `[{"role":"user","content":[{"type":"text","text":"prompt"}]},
		{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"step0"}}]},
		{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"step0-result"}}]}]`
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	for _, msgs := range [][]types.Message{starts[1].Messages, ends[1].Messages} {
		if g := r72genTSMessages(t, msgs); !reflect.DeepEqual(g, w) {
			gj, _ := json.Marshal(g)
			t.Fatalf("step 1 messages =\n%s\nwant\n%s", gj, want)
		}
	}
	if len(starts[0].Messages) != 1 || len(ends[0].Messages) != 1 {
		t.Fatalf("step 0 messages = %#v", starts[0].Messages)
	}
}

func r73genApprovedHistory() []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}},
			types.ToolApprovalRequestContent{ApprovalID: "id-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "id-1", Approved: true}}},
	}
}

// generate-text.test.ts 12750 'should pass the initial tool result in
// responseMessages to prepareStep' and 13288 'should serialize the tool error
// as error-text in the continuation prompt' (approved call whose tool
// throws).
func TestR73Gen_ApprovedResumeResponseMessagesAndToolError(t *testing.T) {
	var mu sync.Mutex
	var prepResponse [][]types.Message
	tool := r72genValueTool("tool1")
	tool.ToolApproval = types.ToolApprovalStatusUserApproval
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    r72genSeqModel(t, r72genText("Hello, world!")),
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(3)},
		Messages: r73genApprovedHistory(),
		PrepareStep: func(_ context.Context, p PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepResponse = append(prepResponse, append([]types.Message(nil), p.ResponseMessages...))
			mu.Unlock()
			return PrepareStepOptions{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[[{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"text","value":"result1"}}]}]]`
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	var got []interface{}
	for _, msgs := range prepResponse {
		got = append(got, r72genTSMessages(t, msgs))
	}
	mu.Unlock()
	if g := r72genJSON(t, got); !reflect.DeepEqual(g, w) {
		gj, _ := json.Marshal(g)
		t.Fatalf("prepareStep responseMessages =\n%s\nwant\n%s", gj, want)
	}

	// The approved tool throws.
	var prompts [][]types.Message
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return nil, errors.New("No valid token for plugin")
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			prompts = append(prompts, o.Prompt.Messages)
			mu.Unlock()
			return r72genText("Hello, world!"), nil
		}},
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(3)},
		Messages: r73genApprovedHistory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	want = `[{"role":"user","content":[{"type":"text","text":"test-input"}]},
		{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"}}]},
		{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"tool1","output":{"type":"error-text","value":"Error: No valid token for plugin"}}]}]`
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 1 {
		t.Fatalf("model calls = %d", len(prompts))
	}
	if g := r72genTSMessages(t, prompts[0]); !reflect.DeepEqual(g, w) {
		gj, _ := json.Marshal(g)
		t.Fatalf("continuation prompt =\n%s\nwant\n%s", gj, want)
	}
	// TS keeps the resumed tool output in the response messages, not in
	// step content.
	if len(res.Steps) != 1 || len(r73genToolErrors(res.Steps[0].Content)) != 0 || len(res.ResponseMessages) < 1 || res.ResponseMessages[0].Role != types.RoleTool {
		t.Fatalf("steps = %#v, response messages = %#v", res.Steps, res.ResponseMessages)
	}
}

// generate-text.test.ts 13492 'should not duplicate an existing
// execution-denied result'.
func TestR73Gen_DeniedResultNotDuplicated(t *testing.T) {
	var mu sync.Mutex
	var prompts [][]types.Message
	tool := r72genValueTool("tool1")
	tool.ToolApproval = types.ToolApprovalStatusUserApproval
	msgs := r73genApprovedHistory()
	msgs[2].Content = []types.ContentPart{
		types.ToolApprovalResponseContent{ApprovalID: "id-1", Approved: false, Reason: "User denied the request"},
		types.ToolResultContent{ToolCallID: "call-1", ToolName: "tool1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User denied the request"}},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			prompts = append(prompts, o.Prompt.Messages)
			mu.Unlock()
			return r72genText("Hello, world!"), nil
		}},
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(3)},
		Messages: msgs,
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	pj, _ := json.Marshal(r72genTSMessages(t, prompts[0]))
	rj, _ := json.Marshal(r72genTSMessages(t, res.ResponseMessages))
	if n := strings.Count(string(pj), "execution-denied"); n != 1 {
		t.Fatalf("prompt execution-denied occurrences = %d: %s", n, pj)
	}
	if n := strings.Count(string(rj), "execution-denied"); n != 0 {
		t.Fatalf("response execution-denied occurrences = %d: %s", n, rj)
	}
}

func r73genMCPHistory(approved bool) []types.Message {
	resp := types.ToolApprovalResponseContent{ApprovalID: "mcp-approval-1", Approved: approved, ProviderExecuted: true, ProviderExecutedSet: true}
	if !approved {
		resp.Reason = "User denied the request"
	}
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Shorten this URL: https://example.com"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{resp}},
	}
}

// generate-text.test.ts 14396 / 14552: a provider-executed (MCP) approval
// response reaches the model exactly once; a denial also sends an
// execution-denied tool result carrying the approval id.
func TestR73Gen_MCPApprovalResponseSentOnce(t *testing.T) {
	mcp := types.Tool{Name: "mcp_tool", Type: types.ToolTypeProviderDefined, ProviderID: "test.mcp_tool", ProviderExecuted: true,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}}}
	for _, approved := range []bool{true, false} {
		var mu sync.Mutex
		var prompts [][]types.Message
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
				mu.Lock()
				prompts = append(prompts, o.Prompt.Messages)
				mu.Unlock()
				return r72genText("Done"), nil
			}},
			Tools:    []types.Tool{mcp},
			StopWhen: []StopCondition{StepCountIs(3)},
			Messages: r73genMCPHistory(approved),
		})
		if err != nil {
			t.Fatal(err)
		}
		toolContent := `[{"type":"tool-approval-response","approvalId":"mcp-approval-1","approved":true}]`
		if !approved {
			toolContent = `[{"type":"tool-approval-response","approvalId":"mcp-approval-1","approved":false,"reason":"User denied the request"},
				{"type":"tool-result","toolCallId":"mcp-call-1","toolName":"mcp_tool","output":{"type":"execution-denied","reason":"User denied the request"}}]`
		}
		want := `[{"role":"user","content":[{"type":"text","text":"Shorten this URL: https://example.com"}]},
			{"role":"assistant","content":[{"type":"tool-call","toolCallId":"mcp-call-1","toolName":"mcp_tool","input":{"query":"test"},"providerExecuted":true}]},
			{"role":"tool","content":` + toolContent + `}]`
		var w interface{}
		if err := json.Unmarshal([]byte(want), &w); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		g := r72genTSMessages(t, prompts[0])
		// r73 gen CR-1 to the integrator: TS LanguageModelV4 approval
		// responses have no providerExecuted (convert-to-language-model-
		// prompt.ts {type, approvalId, approved, reason}); Go's
		// tool_approval_resume.go keepResponse sets it. Remove this
		// normalization once the CR lands.
		for _, part := range g.([]interface{})[2].(map[string]interface{})["content"].([]interface{}) {
			if m := part.(map[string]interface{}); m["type"] == "tool-approval-response" {
				delete(m, "providerExecuted")
			}
		}
		if !reflect.DeepEqual(g, w) {
			gj, _ := json.Marshal(g)
			t.Fatalf("approved=%v prompt =\n%s\nwant\n%s", approved, gj, want)
		}
		if !approved {
			denied := prompts[0][2].Content[1].(types.ToolResultContent)
			if !reflect.DeepEqual(denied.Output.ProviderOptions, map[string]interface{}{"openai": map[string]interface{}{"approvalId": "mcp-approval-1"}}) {
				t.Fatalf("denied output providerOptions = %#v", denied.Output.ProviderOptions)
			}
		}
		mu.Unlock()
	}
}

// r73genRecordID marshals as a plain string (TS toJSON on an ObjectId-like
// value).
type r73genRecordID struct{}

func (r73genRecordID) MarshalJSON() ([]byte, error) { return []byte(`"507f1f77bcf86cd799439011"`), nil }

// generate-text.test.ts 6375 'result.responseMessages should JSON serialize
// object tool outputs'.
func TestR73Gen_ResponseMessagesJSONSerializeToolOutputs(t *testing.T) {
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "lookupRecord", Arguments: map[string]interface{}{}, RawArguments: "{}"}), r72genText("done")),
		Tools: []types.Tool{{Name: "lookupRecord", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"id": r73genRecordID{}}, nil
			}}},
		Prompt:   "look up the record",
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"assistant","content":[{"type":"tool-call","toolCallId":"call-1","toolName":"lookupRecord","input":{}}]},
		{"role":"tool","content":[{"type":"tool-result","toolCallId":"call-1","toolName":"lookupRecord","output":{"type":"json","value":{"id":"507f1f77bcf86cd799439011"}}}]},
		{"role":"assistant","content":[{"type":"text","text":"done"}]}]`
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if g := r72genTSMessages(t, res.ResponseMessages); !reflect.DeepEqual(g, w) {
		gj, _ := json.Marshal(g)
		t.Fatalf("response messages =\n%s\nwant\n%s", gj, want)
	}
	// The output value is plain JSON data (TS toJSONValue), not the Go value.
	out := res.ResponseMessages[1].Content[0].(types.ToolResultContent).Output
	if v, ok := out.Value.(map[string]interface{}); !ok || v["id"] != "507f1f77bcf86cd799439011" {
		t.Fatalf("output value = %#v, want JSON data", out.Value)
	}
}

// generate-text.test.ts 6535 'onFinishResult should expose deprecated AI SDK
// 6 final-step properties'.
func TestR73Gen_OnEndFinalStepProperties(t *testing.T) {
	s0 := r73genCallStep("call-1", "value")
	s0.ProviderMetadata = map[string]interface{}{"p": map[string]interface{}{"step": 0}}
	s0.ResponseMetadata = &types.ResponseMetadata{ID: "id-0", ModelID: "m-0", Timestamp: time.Unix(0, 0).UTC()}
	s0.RawRequest = map[string]interface{}{"step": 0}
	s1 := r72genText("Hello, world!")
	s1.Content = []types.ContentPart{types.ReasoningContent{Text: "I will open the conversation"}, types.TextContent{Text: "Hello, world!"}}
	s1.ProviderMetadata = map[string]interface{}{"p": map[string]interface{}{"step": 1}}
	s1.ResponseMetadata = &types.ResponseMetadata{ID: "id-1", ModelID: "m-1", Timestamp: time.Unix(10, 0).UTC(), Headers: map[string]string{"h": "v"}}
	s1.RawRequest = map[string]interface{}{"step": 1}
	var mu sync.Mutex
	var end OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, s0, s1), Prompt: "test-input", Tools: []types.Tool{r72genValueTool("tool1")},
		StopWhen:   []StopCondition{StepCountIs(3)},
		Include:    &IncludeOptions{RequestMessages: true},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); end = e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	fs := res.FinalStep
	if len(fs.Reasoning) != 1 || !reflect.DeepEqual(end.Reasoning, fs.Reasoning) || end.ReasoningText != fs.ReasoningText || fs.ReasoningText != "I will open the conversation" {
		t.Fatalf("reasoning = %#v / %q, final step %#v / %q", end.Reasoning, end.ReasoningText, fs.Reasoning, fs.ReasoningText)
	}
	if !reflect.DeepEqual(end.Request.Body, fs.Request.Body) || !reflect.DeepEqual(end.Request.Messages, fs.Request.Messages) || len(fs.Request.Messages) == 0 {
		t.Fatalf("request = %#v, final step %#v", end.Request, fs.Request)
	}
	if end.Response.ID != fs.Response.ID || !end.Response.Timestamp.Equal(fs.Response.Timestamp) || end.Response.ModelID != fs.Response.ModelID ||
		!reflect.DeepEqual(end.Response.Headers, fs.Response.Headers) || !reflect.DeepEqual(end.Response.Messages, fs.Response.Messages) || fs.Response.ID != "id-1" {
		t.Fatalf("response = %#v, final step %#v", end.Response, fs.Response)
	}
	if !reflect.DeepEqual(end.ProviderMetadata, fs.ProviderMetadata) || !reflect.DeepEqual(fs.ProviderMetadata, s1.ProviderMetadata) {
		t.Fatalf("provider metadata = %#v, final step %#v", end.ProviderMetadata, fs.ProviderMetadata)
	}
}

// generate-text.test.ts 7459 / 7463 (2 stop conditions): every condition is
// called with the steps; the run stops after the first step.
func TestR73Gen_TwoStopConditions(t *testing.T) {
	type call struct {
		number int
		steps  int
	}
	var calls []call
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, r73genCallStep("call-1", "value")), Prompt: "test-input", Tools: []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{
			func(s StopConditionState) string { calls = append(calls, call{0, len(s.Steps)}); return "" },
			func(s StopConditionState) string { calls = append(calls, call{1, len(s.Steps)}); return "stop" },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(res.Steps))
	}
	if !reflect.DeepEqual(calls, []call{{0, 1}, {1, 1}}) {
		t.Fatalf("stop condition calls = %#v", calls)
	}
}

// generate-text.test.ts 3020: the start event has no telemetry fields
// (isEnabled, functionId) whichever telemetry option is set.
func TestR73Gen_StartEventHasNoTelemetryFields(t *testing.T) {
	typ := reflect.TypeOf(OnStartEvent{})
	for _, name := range []string{"IsEnabled", "FunctionID", "FunctionId", "Telemetry", "ExperimentalTelemetry"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("OnStartEvent has field %s", name)
		}
	}
}

// generate-text.test.ts 911 'should not execute tools when the finish reason
// is %s' (every non-tool-calls finish reason TS lists).
func TestR73Gen_NoToolExecutionForUnsafeFinishReasons(t *testing.T) {
	for _, reason := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonError, types.FinishReasonContentFilter, "interrupted", types.FinishReasonOther} {
		executed := false
		tool := r72genValueTool("testTool")
		tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "tool-result", nil
		}
		step := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{"value":"test"}`})
		step.FinishReason, step.RawFinishReason = reason, string(reason)
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: r72genSeqModel(t, step), Prompt: "test-input", Tools: []types.Tool{tool}})
		if err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		if len(res.ToolCalls) != 1 || executed {
			t.Errorf("%s: tool calls = %d, executed = %v", reason, len(res.ToolCalls), executed)
		}
	}
}

// generate-text.test.ts 1256 'should include reasoning-file in content and
// exclude from files'.
func TestR73Gen_ReasoningFileInContentNotFiles(t *testing.T) {
	sig := json.RawMessage(`{"google":{"thoughtSignature":"sig123"}}`)
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, &types.GenerateResult{
			FinishReason: types.FinishReasonStop,
			Content: []types.ContentPart{
				types.TextContent{Text: "Here is a thought image:"},
				types.ReasoningFileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{10, 20, 30}}, MediaType: "image/png", ProviderMetadata: sig},
				types.GeneratedFileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{40, 50, 60}}, MediaType: "image/jpeg"},
			},
		}),
		Prompt: "prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	if tc, ok := res.Content[0].(types.TextContent); !ok || tc.Text != "Here is a thought image:" {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
	rf, ok := res.Content[1].(types.ReasoningFileContent)
	if !ok || rf.MediaType != "image/png" || string(rf.ProviderMetadata) != string(sig) {
		t.Fatalf("content[1] = %#v", res.Content[1])
	}
	gf, ok := res.Content[2].(types.GeneratedFileContent)
	if !ok || gf.MediaType != "image/jpeg" {
		t.Fatalf("content[2] = %#v", res.Content[2])
	}
	if len(res.Files) != 1 || res.Files[0].MediaType != "image/jpeg" {
		t.Fatalf("files = %#v, want only the jpeg", res.Files)
	}
	if got := res.FinalStep.ReasoningFiles(); len(got) != 1 || got[0].MediaType != "image/png" {
		t.Fatalf("ReasoningFiles = %#v", got)
	}
}

// generate-text.test.ts 4416 'should provide parsed tool calls on model-call
// end'.
func TestR73Gen_ModelCallEndParsedToolCalls(t *testing.T) {
	step := r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test-arg"}, RawArguments: `{ "value": "test-arg" }`})
	step.Content = []types.ContentPart{
		types.TextContent{Text: "Before tool."},
		types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Input: `{ "value": "test-arg" }`},
	}
	step.RawFinishReason = "tool-calls"
	step.ResponseMetadata = &types.ResponseMetadata{ID: "response-1", ModelID: "mock-response-model", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Headers: map[string]string{"x-test": "true"}}
	var mu sync.Mutex
	var ends []LanguageModelCallEndEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t, step), Prompt: "prompt", Tools: []types.Tool{r72genValueTool("tool1")},
		Internal:               &InternalOptions{GenerateCallID: func() string { return "test-telemetry-call-id" }},
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ends) != 1 {
		t.Fatalf("model call ends = %d", len(ends))
	}
	e := ends[0]
	if e.CallID != "test-telemetry-call-id" || e.Provider != "mock-provider" || e.ModelID != "mock-response-model" || e.ResponseID != "response-1" ||
		e.FinishReason != types.FinishReasonToolCalls || e.Usage.InputTokens == nil || *e.Usage.InputTokens != 3 || *e.Usage.OutputTokens != 10 || *e.Usage.TotalTokens != 13 {
		t.Fatalf("event = %#v", e)
	}
	if len(e.Content) != 2 {
		t.Fatalf("content = %#v", e.Content)
	}
	if tc, ok := e.Content[0].(types.TextContent); !ok || tc.Text != "Before tool." {
		t.Fatalf("content[0] = %#v", e.Content[0])
	}
	call, ok := e.Content[1].(types.ToolCallContent)
	if !ok || call.ToolCallID != "call-1" || call.ToolName != "tool1" || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"value": "test-arg"}) || call.ProviderExecuted || call.Title != "" {
		t.Fatalf("content[1] = %#v, want the parsed call", e.Content[1])
	}
}

// generate-text.test.ts 11672 / 11680 / 11731 (single invalid tool call with
// user approval): no approval request, no execution, a dynamic tool-error
// part (TS error = getErrorMessage = the toString text, Go ErrorText; Err the
// typed error) and an error-text tool result in the response messages.
func TestR73Gen_InvalidToolCallErrorResult(t *testing.T) {
	executed := false
	tool := types.Tool{Name: "cityAttractions", ToolApproval: types.ToolApprovalStatusUserApproval,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return nil, nil
		}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  r72genSeqModel(t, r72genCalls(types.ToolCall{ID: "call-1", ToolName: "cityAttractions", RawArguments: `{ "cities": "San Francisco" }`})),
		Tools:  []types.Tool{tool},
		Prompt: "What are the tourist attractions in San Francisco?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatal("invalid call executed")
	}
	if len(res.Content) != 2 {
		t.Fatalf("content = %#v", res.Content)
	}
	call, ok := res.Content[0].(types.ToolCallContent)
	if !ok || !call.Invalid || !call.Dynamic || call.ToolCallID != "call-1" || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"cities": "San Francisco"}) {
		t.Fatalf("content[0] = %#v", res.Content[0])
	}
	te, ok := res.Content[1].(types.ToolErrorContent)
	if !ok || !te.Dynamic || te.ProviderExecuted || !reflect.DeepEqual(te.Input, map[string]interface{}{"cities": "San Francisco"}) {
		t.Fatalf("content[1] = %#v", res.Content[1])
	}
	var target *InvalidToolInputError
	if !errors.As(te.Err, &target) {
		t.Fatalf("Err = %#v", te.Err)
	}
	prefix := "AI_InvalidToolInputError: Invalid input for tool cityAttractions: "
	if !strings.HasPrefix(te.ErrorText, prefix) {
		t.Fatalf("ErrorText = %q", te.ErrorText)
	}
	if len(res.ResponseMessages) != 2 || res.ResponseMessages[1].Role != types.RoleTool {
		t.Fatalf("response messages = %#v", res.ResponseMessages)
	}
	tr := res.ResponseMessages[1].Content[0].(types.ToolResultContent)
	if tr.ToolCallID != "call-1" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || tr.Output.Value != te.ErrorText {
		t.Fatalf("tool result = %#v", tr)
	}
}
