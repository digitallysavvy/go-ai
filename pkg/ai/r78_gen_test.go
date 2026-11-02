package ai

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r78genTitledTool has a title, metadata and a required string value; its
// Execute fails when value is "boom".
func r78genTitledTool() types.Tool {
	return types.Tool{
		Name:       "tool1",
		Title:      "Tool One",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Metadata:   map[string]interface{}{"owner": "team"},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			if in["value"] == "boom" {
				return nil, errors.New("exec failed")
			}
			return "ok", nil
		},
	}
}

func r78genToolErrors(parts []types.ContentPart) map[string]types.ToolErrorContent {
	out := map[string]types.ToolErrorContent{}
	for _, p := range parts {
		if te, ok := p.(types.ToolErrorContent); ok {
			out[te.ToolCallID] = te
		}
	}
	return out
}

// Ruling (c)(1): TS streamLanguageModelCall's tool-error for an invalid
// client call carries `title: toolCall.title` and the toolMetadata
// (stream-language-model-call.ts 746-756); an executed failure has no title
// (execute-tool-call.ts 163-175); generateText's invalid tool-error has
// neither (generate-text.ts 1484-1492).
func TestR78Gen_InvalidToolErrorTitle(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		step1 := []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "bad", ToolName: "tool1", RawArguments: `{}`}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, r71streamHelloWorld), Prompt: "x", Tools: []types.Tool{r78genTitledTool()}})
		errs := r78genToolErrors(r.Steps()[0].Content)
		bad, fail := errs["bad"], errs["fail"]
		if bad.Title != "Tool One" || bad.ToolMetadata["owner"] != "team" || !bad.Dynamic {
			t.Fatalf("invalid tool error = %#v, want title, metadata, dynamic", bad)
		}
		if fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
	t.Run("stream with transform", func(t *testing.T) {
		// Content recorded from the transformed stream
		// (toolResultContentFromToolResult) keeps the title as well.
		step1 := []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "bad", ToolName: "tool1", RawArguments: `{}`}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}
		identity := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
			return []provider.StreamChunk{c}
		}
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step1, r71streamHelloWorld), Prompt: "x", Tools: []types.Tool{r78genTitledTool()}, ExperimentalTransform: []StreamTransformFunc{identity}})
		errs := r78genToolErrors(r.Steps()[0].Content)
		if bad := errs["bad"]; bad.Title != "Tool One" {
			t.Fatalf("invalid tool error = %#v, want title", bad)
		}
		if fail := errs["fail"]; fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
	t.Run("generate", func(t *testing.T) {
		calls := 0
		model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{
				{ID: "bad", ToolName: "tool1", RawArguments: `{}`},
				{ID: "fail", ToolName: "tool1", RawArguments: `{"value":"boom"}`},
			}}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{r78genTitledTool()}})
		if err != nil {
			t.Fatal(err)
		}
		errs := r78genToolErrors(res.Steps[0].Content)
		if bad := errs["bad"]; bad.ToolCallID != "bad" || bad.Title != "" || bad.ToolMetadata != nil {
			t.Fatalf("invalid tool error = %#v, want no title and no metadata", bad)
		}
		if fail := errs["fail"]; fail.ToolCallID != "fail" || fail.Title != "" {
			t.Fatalf("executed failure = %#v, want no title", fail)
		}
	})
}

// Port of parse-tool-call.test.ts 680-710 "should return invalid tool call
// with ToolCallRepairError when repair throws": the message is TS
// `Error repairing tool call: ${getErrorMessage(cause)}`, the cause's
// toString() ("Error: test error").
func TestR78Gen_ToolCallRepairErrorMessage(t *testing.T) {
	got, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: "invalid json"},
		Tools: []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"param1": map[string]interface{}{"type": "string"}, "param2": map[string]interface{}{"type": "number"}},
			"required":   []interface{}{"param1", "param2"},
		}}},
		RepairToolCall: func(context.Context, ToolCallRepairOptions) (*types.ToolCall, error) {
			return nil, errors.New("test error")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Invalid || !got.Dynamic || got.RawArguments != "invalid json" || !IsToolCallRepairError(got.Error) {
		t.Fatalf("got %#v", got)
	}
	if msg := got.Error.Error(); msg != "Error repairing tool call: Error: test error" {
		t.Fatalf("message = %q", msg)
	}
	if text := providerutils.ToolErrorText(got.Error); text != "AI_ToolCallRepairError: Error repairing tool call: Error: test error" {
		t.Fatalf("model-facing text = %q", text)
	}
}

// r78genToolCallModel returns a generate model whose first call returns the
// given t1 calls (finish tool-calls) and later calls "done".
func r78genToolCallModel(inputs ...string) *testutil.MockLanguageModel {
	n := 0
	return &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n > len(inputs) {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "call-" + strconv.Itoa(n), ToolName: "t1", RawArguments: inputs[n-1]}}}, nil
	}}
}

type r78genSandbox struct{ name string }

// Ports of generate-text.test.ts "context" (11129-11365) and options.output
// text (8932), the partial rows of ts-test-port-coverage.md Appendix A.
func TestR78Gen_GenerateContextPorts(t *testing.T) {
	ctx := context.Background()
	valueParams := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}
	t.Run("should send context to tool execution", func(t *testing.T) {
		var recorded interface{}
		tool := types.Tool{Name: "t1", Parameters: valueParams,
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"context": map[string]interface{}{"type": "string"}}, "required": []interface{}{"context"}}),
			Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
				recorded = o.ToolContext
				return map[string]interface{}{"value": "test-result"}, nil
			}}
		if _, err := GenerateText(ctx, GenerateTextOptions{Model: r78genToolCallModel(`{ "value": "test" }`), Tools: []types.Tool{tool}, ToolsContext: map[string]interface{}{"t1": map[string]interface{}{"context": "test"}}, Prompt: "test-input"}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(recorded, map[string]interface{}{"context": "test"}) {
			t.Fatalf("context = %#v", recorded)
		}
	})
	t.Run("should pass sandbox to tool execution and prepareStep", func(t *testing.T) {
		sandbox := &r78genSandbox{name: "test sandbox"}
		var toolSandbox, prepareSandbox interface{}
		tool := types.Tool{Name: "t1", Parameters: valueParams, Execute: func(_ context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			toolSandbox = o.ExperimentalSandbox
			return in, nil
		}}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: r78genToolCallModel(`{ "value": "test" }`), Tools: []types.Tool{tool}, ExperimentalSandbox: sandbox, Prompt: "test-input",
			PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
				prepareSandbox = s.ExperimentalSandbox
				return PrepareStepOptions{}
			}})
		if err != nil {
			t.Fatal(err)
		}
		if toolSandbox != sandbox || prepareSandbox != sandbox {
			t.Fatalf("tool sandbox %v, prepareStep sandbox %v", toolSandbox, prepareSandbox)
		}
	})
	t.Run("should use sandbox returned from prepareStep for that step only", func(t *testing.T) {
		sandbox, stepSandbox := &r78genSandbox{name: "default"}, &r78genSandbox{name: "step"}
		var recorded []interface{}
		tool := types.Tool{Name: "t1", Parameters: valueParams, Execute: func(_ context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			recorded = append(recorded, o.ExperimentalSandbox)
			return in, nil
		}}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: r78genToolCallModel(`{ "value": "first" }`, `{ "value": "second" }`), Tools: []types.Tool{tool}, ExperimentalSandbox: sandbox, Prompt: "test-input",
			StopWhen: []StopCondition{StepCountIs(3)},
			PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
				if s.StepNumber == 0 {
					return PrepareStepOptions{ExperimentalSandbox: stepSandbox}
				}
				return PrepareStepOptions{}
			}})
		if err != nil {
			t.Fatal(err)
		}
		if len(recorded) != 2 || recorded[0] != stepSandbox || recorded[1] != sandbox {
			t.Fatalf("sandboxes = %v", recorded)
		}
	})
	t.Run("should pass runtimeContext to prepareStep and onFinish", func(t *testing.T) {
		var prepareCtx, finishCtx, endEventCtx interface{}
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "Hello, world!", FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}, nil
		}}
		_, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "test", RuntimeContext: map[string]interface{}{"myData": "test-value"},
			PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
				prepareCtx = s.RuntimeContext
				return PrepareStepOptions{}
			},
			OnFinish:   func(_ context.Context, _ *GenerateTextResult, rc interface{}) { finishCtx = rc },
			OnEndEvent: func(_ context.Context, e OnFinishEvent) { endEventCtx = e.RuntimeContext },
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]interface{}{"myData": "test-value"}
		if !reflect.DeepEqual(prepareCtx, want) || !reflect.DeepEqual(finishCtx, want) || !reflect.DeepEqual(endEventCtx, want) {
			t.Fatalf("prepareStep %v, onFinish %v, end event %v", prepareCtx, finishCtx, endEventCtx)
		}
	})
	t.Run("text output sets responseFormat text and keeps the prompt", func(t *testing.T) {
		var got *provider.GenerateOptions
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			got = o
			return &types.GenerateResult{Text: "Hello, world!", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "prompt", Output: TextOutput()})
		if err != nil {
			t.Fatal(err)
		}
		if got.ResponseFormat == nil || got.ResponseFormat.Type != "text" || got.ResponseFormat.Schema != nil {
			t.Fatalf("responseFormat = %#v", got.ResponseFormat)
		}
		msgs := got.Prompt.Messages
		if got.Prompt.System != "" || len(msgs) != 1 || msgs[0].Role != types.RoleUser || len(msgs[0].Content) != 1 {
			t.Fatalf("prompt = %#v", got.Prompt)
		}
		if tc, ok := msgs[0].Content[0].(types.TextContent); !ok || tc.Text != "prompt" {
			t.Fatalf("prompt content = %#v", msgs[0].Content)
		}
		if got.Headers["user-agent"] == "" || !strings.HasPrefix(got.Headers["user-agent"], "ai/") {
			t.Fatalf("headers = %#v", got.Headers)
		}
		if res.Output != "Hello, world!" {
			t.Fatalf("output = %#v", res.Output)
		}
	})
}

// Ports of parse-tool-call.test.ts 89, 202, 233, 716-1005 (refiner lookup by
// own name, empty input, dynamic, title and tool metadata propagation).
func TestR78Gen_ParseToolCallPorts(t *testing.T) {
	ctx := context.Background()
	emptySchema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	weather := types.Tool{Name: "weather", Type: "dynamic", Title: "Weather", Metadata: map[string]interface{}{"clientName": "MyMCPClient"},
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}, "required": []interface{}{"location"}, "additionalProperties": false}}
	t.Run("refiner registered for another tool is not used", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "123", ToolName: "toString", RawArguments: `{"value": "raw"}`},
			Tools:    []types.Tool{{Name: "toString", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}}}},
			RefineToolInput: map[string]ToolInputRefiner{"testTool": func(context.Context, ToolInputRefinementOptions) (map[string]interface{}, error) {
				return map[string]interface{}{"value": "refined"}, nil
			}},
		})
		if err != nil || got.Invalid || !reflect.DeepEqual(got.Arguments, map[string]interface{}{"value": "raw"}) {
			t.Fatalf("got %#v, %v", got, err)
		}
	})
	for _, input := range []string{"", "{}"} {
		t.Run("empty input "+strconv.Quote(input), func(t *testing.T) {
			got, err := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "123", ToolName: "testTool", RawArguments: input}, Tools: []types.Tool{{Name: "testTool", Parameters: emptySchema}}})
			if err != nil || got.Invalid || got.Dynamic || got.Title != "" || !reflect.DeepEqual(got.Arguments, map[string]interface{}{}) {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
	t.Run("dynamic tool, title and metadata kept apart from provider metadata", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{
			ToolCall: types.ToolCall{ID: "call-3", ToolName: "weather", RawArguments: `{"location":"Paris"}`, ProviderMetadata: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
			Tools:    []types.Tool{weather},
		})
		if err != nil || got.Invalid || !got.Dynamic || got.Title != "Weather" {
			t.Fatalf("got %#v, %v", got, err)
		}
		if !reflect.DeepEqual(got.ToolMetadata, map[string]interface{}{"clientName": "MyMCPClient"}) || !reflect.DeepEqual(got.ProviderMetadata, map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}) {
			t.Fatalf("metadata: tool %#v provider %#v", got.ToolMetadata, got.ProviderMetadata)
		}
	})
	t.Run("static tool title and metadata", func(t *testing.T) {
		static := weather
		static.Type = ""
		got, err := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "call-2", ToolName: "weather", RawArguments: `{"location":"Paris"}`}, Tools: []types.Tool{static}})
		if err != nil || got.Invalid || got.Dynamic || got.Title != "Weather" || got.ToolMetadata["clientName"] != "MyMCPClient" {
			t.Fatalf("got %#v, %v", got, err)
		}
	})
	t.Run("invalid tool call keeps title and metadata", func(t *testing.T) {
		got, err := ParseToolCall(ctx, ParseToolCallOptions{ToolCall: types.ToolCall{ID: "call-4", ToolName: "weather", RawArguments: "invalid json"}, Tools: []types.Tool{weather}})
		if err != nil || !got.Invalid || !got.Dynamic || got.Title != "Weather" || got.ToolMetadata["clientName"] != "MyMCPClient" || !IsInvalidToolInputError(got.Error) {
			t.Fatalf("got %#v, %v", got, err)
		}
	})
}
