package ai

// Round 87e gen: known differences that are now fixed (TS 3fab50aa9b). Each
// test names the TS source it follows.

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r87egenTitledTool is a local tool with a title (TS tool({title, execute})).
func r87egenTitledTool() types.Tool {
	return types.Tool{
		Name:       "weather",
		Title:      "Weather",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
}

// TS execute-tool-call.ts builds the tool-result of a client tool without
// title (only the tool call and a preliminary result, `...toolCall`, have
// it). Known difference "Executed tool results carry the tool title".
func TestR87egenExecutedToolResultHasNoTitle(t *testing.T) {
	t.Parallel()
	call := types.ToolCall{ID: "c1", ToolName: "weather", RawArguments: `{"city":"Paris"}`}
	var mu sync.Mutex
	var finish []OnToolCallFinishEvent
	n := 0
	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			n++
			if n == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}},
		Prompt:   "go",
		Tools:    []types.Tool{r87egenTitledTool()},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnToolCallFinish: func(_ context.Context, e OnToolCallFinishEvent) {
			mu.Lock()
			finish = append(finish, e)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got := gen.Steps[0].ToolCalls[0].Title; got != "Weather" {
		t.Errorf("tool call title = %q, want Weather", got)
	}
	if got := gen.Steps[0].ToolResults[0].Title; got != "" {
		t.Errorf("GenerateText tool result title = %q, want none", got)
	}
	mu.Lock()
	if len(finish) != 1 || finish[0].ToolOutput.Title != "" || finish[0].ToolCall.Title != "Weather" {
		t.Errorf("OnToolCallFinish = %+v, want an output without title and the titled call", finish)
	}
	mu.Unlock()

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &call},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}},
		Prompt: "go",
		Tools:  []types.Tool{r87egenTitledTool()},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	sawResult := false
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
			sawResult = true
			if chunk.ToolResult.Title != "" {
				t.Errorf("StreamText tool-result part title = %q, want none", chunk.ToolResult.Title)
			}
		}
	}
	if !sawResult {
		t.Fatal("StreamText sent no tool-result part")
	}
}

// TS convert-language-model-content.ts turns a model tool-result with isError
// into a tool-error whose error is part.result: undefined when the result has
// no value, so the response message replays it as error-json null
// (createToolModelOutput toJSONValue(undefined)). Known difference "Provider
// tool error without a value".
func TestR87egenProviderToolErrorWithoutValue(t *testing.T) {
	t.Parallel()
	isErr := true
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", IsError: &isErr, ProviderExecuted: true},
			},
			ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var errPart *types.ToolErrorContent
	for _, part := range res.FinalStep.Content {
		if p, ok := part.(types.ToolErrorContent); ok {
			errPart = &p
		}
	}
	if errPart == nil || errPart.Error != nil {
		t.Fatalf("tool-error part = %+v, want one with a nil Error", errPart)
	}
	msgs := res.FinalStep.Response.Messages
	if len(msgs) != 1 || len(msgs[0].Content) != 2 {
		t.Fatalf("response messages = %+v, want one assistant message with the call and its result", msgs)
	}
	replayed, ok := msgs[0].Content[1].(types.ToolResultContent)
	if !ok || replayed.Output == nil || replayed.Output.Type != types.ToolResultOutputErrorJSON || replayed.Output.Value != nil {
		t.Fatalf("replayed tool result = %+v, want error-json null", msgs[0].Content[1])
	}

	// The stream path's ToolResult carries errProviderToolNoValue (a Go
	// error must be non-nil); its content part keeps the error undefined.
	part := toolResultContentFromToolResult(types.ToolResult{ToolCallID: "ws", ToolName: "web_search", Error: errProviderToolNoValue, ProviderExecuted: true})
	streamed, ok := part.(types.ToolErrorContent)
	if !ok || streamed.Error != nil || streamed.Err != nil {
		t.Fatalf("streamed tool-error part = %+v, want a nil Error and Err", part)
	}
}

// r87egenFailingStream sends its chunks and then fails Next with err.
type r87egenFailingStream struct {
	chunks []provider.StreamChunk
	err    error
}

func (s *r87egenFailingStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		if s.err == nil {
			return nil, io.EOF
		}
		return nil, s.err
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &c, nil
}
func (s *r87egenFailingStream) Err() error   { return nil }
func (s *r87egenFailingStream) Close() error { return nil }

// r87egenTelemetry records the telemetry events it gets, by name.
type r87egenTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
}

func (r *r87egenTelemetry) add(name string) {
	r.mu.Lock()
	r.events = append(r.events, name)
	r.mu.Unlock()
}
func (r *r87egenTelemetry) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	r.add("start")
	return ctx
}
func (r *r87egenTelemetry) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.add("stepEnd")
}
func (r *r87egenTelemetry) OnStepError(context.Context, telemetry.TelemetryErrorEvent) {
	r.add("stepError")
}
func (r *r87egenTelemetry) OnFinish(context.Context, telemetry.TelemetryFinishEvent) {
	r.add("end")
}
func (r *r87egenTelemetry) OnError(context.Context, telemetry.TelemetryErrorEvent) { r.add("error") }
func (r *r87egenTelemetry) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// TS stream-object.ts: a source stream that rejects reaches the stitchable
// stream's onError (rejectResultPromises + onError); the transform's flush
// handler never runs, so there is no onStepFinish and no onFinish. Go closes
// the spans with the error (TS leaves them open). Known difference
// "StreamObject is not lazy".
func TestR87egenStreamObjectRawStreamFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection reset")
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r87egenFailingStream{chunks: []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: `{"a":`}}, err: boom}, nil
	}}
	var mu sync.Mutex
	var calls []string
	record := func(name string) {
		mu.Lock()
		calls = append(calls, name)
		mu.Unlock()
	}
	rec := &r87egenTelemetry{}
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:     model,
		Prompt:    "gen",
		Schema:    schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
		OnError:   func(context.Context, error) { record("onError") },
		OnStepEnd: func(context.Context, ObjectOnStepFinishEvent) { record("onStepEnd") },
		OnEnd:     func(context.Context, ObjectOnFinishEvent) { record("onEnd") },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("StreamObject() error = %v, want the stream error", err)
	}
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "onError" {
		t.Errorf("callbacks = %v, want [onError]", got)
	}
	if ev := rec.got(); len(ev) != 3 || ev[0] != "start" || ev[1] != "stepError" || ev[2] != "error" {
		t.Errorf("telemetry = %v, want [start stepError error]", ev)
	}
}

type r87egenCtxKey struct{}

// r87egenRootCtx marks the context its OnStart returns (the root span's).
type r87egenRootCtx struct {
	telemetry.NoopTelemetryIntegration
}

func (r87egenRootCtx) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	return context.WithValue(ctx, r87egenCtxKey{}, true)
}

// TS notify([onStart, telemetry.onStart]) (generate-text.test.ts 14951) starts
// the user callback first, outside the root span; later callbacks run inside
// it. Known difference "Telemetry start before the start callbacks".
func TestR87egenUserOnStartBeforeTelemetryRoot(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var inRootAtStart, inRootAtStep bool
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		}},
		Prompt:    "x",
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{r87egenRootCtx{}}},
		OnStart: func(ctx context.Context, _ OnStartEvent) {
			mu.Lock()
			inRootAtStart = ctx.Value(r87egenCtxKey{}) != nil
			mu.Unlock()
		},
		OnStepStart: func(ctx context.Context, _ OnStepStartEvent) {
			mu.Lock()
			inRootAtStep = ctx.Value(r87egenCtxKey{}) != nil
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if inRootAtStart || !inRootAtStep {
		t.Errorf("OnStart in root = %v, OnStepStart in root = %v; want false, true", inRootAtStart, inRootAtStep)
	}
}

// TS doParseToolCall takes toolMetadata from the tool only and
// parseProviderExecutedDynamicToolCall builds the call without title or
// toolMetadata (a LanguageModelV4ToolCall has neither). Known difference
// "Tool call metadata from custom providers".
func TestR87egenModelToolCallMetadataIsTheTools(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var execMetadata map[string]interface{}
	tools := []types.Tool{{Name: "lookup", Parameters: map[string]interface{}{"type": "object"}, Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		mu.Lock()
		execMetadata = o.ToolMetadata
		mu.Unlock()
		return "ok", nil
	}}}
	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}, ToolMetadata: map[string]interface{}{"trace": "provider"}}}}, nil
		}},
		Prompt:   "x",
		Tools:    tools,
		StopWhen: []StopCondition{StepCountIs(1)},
		ExperimentalRefineToolInput: map[string]ToolInputRefiner{"lookup": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
			return map[string]interface{}{"q": "refined"}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if execMetadata != nil || gen.Steps[0].ToolCalls[0].ToolMetadata != nil {
		t.Errorf("tool metadata: execute %v, step call %v; want none", execMetadata, gen.Steps[0].ToolCalls[0].ToolMetadata)
	}
	mu.Unlock()
	dynamic, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "c2", ToolName: "remote", RawArguments: `{}`, ProviderExecuted: true, Dynamic: true, Title: "Provider title", ToolMetadata: map[string]interface{}{"trace": "provider"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dynamic.ToolMetadata != nil || dynamic.Title != "" || !dynamic.Dynamic || !dynamic.ProviderExecuted {
		t.Errorf("provider-executed dynamic call = %+v, want no title or metadata", dynamic)
	}
}
