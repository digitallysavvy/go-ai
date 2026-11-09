package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 89 gen tests (TS 382d0b8eaf).

// r89genStreamObjectStepEnd runs StreamObject over chunks and returns the
// step end event.
func r89genStreamObjectStepEnd(t *testing.T, chunks []provider.StreamChunk) ObjectOnStepFinishEvent {
	t.Helper()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}
	var mu sync.Mutex
	var got ObjectOnStepFinishEvent
	called := false
	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "prompt",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			got, called = e, true
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("OnStepEnd was not called")
	}
	return got
}

// TS GenerateObjectStepEndEvent.msToFirstChunk (structured-output-events.ts
// 157-158): streamObject's step end event carries the time from the start of
// the stream to the first chunk (stream-object.ts 657, 703-706, 875);
// generateObject passes undefined (generate-object.ts 467).
func TestR89genObjectStepEndMsToFirstChunk(t *testing.T) {
	t.Run("streamObject", func(t *testing.T) {
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: `{"x":1}`},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		})
		if got.MsToFirstChunk == nil || *got.MsToFirstChunk < 0 {
			t.Fatalf("MsToFirstChunk = %v, want a duration", got.MsToFirstChunk)
		}
	})
	t.Run("streamObject without chunks after stream-start", func(t *testing.T) {
		// TS isFirstChunk is set only by a part that passes the stream-start
		// check, so a stream of stream-start alone leaves it undefined.
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
		})
		if got.MsToFirstChunk != nil {
			t.Fatalf("MsToFirstChunk = %d, want nil", *got.MsToFirstChunk)
		}
	})
	t.Run("streamObject ignores parts TS drops before timing", func(t *testing.T) {
		// TS's first transformer forwards only text-delta, response-metadata,
		// finish, error and stream-start, so a text-start or reasoning part
		// does not start the clock; the stream here has none of the timed
		// parts.
		got := r89genStreamObjectStepEnd(t, []provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeReasoning, Reasoning: "think"},
		})
		if got.MsToFirstChunk != nil {
			t.Fatalf("MsToFirstChunk = %d, want nil", *got.MsToFirstChunk)
		}
	})
	t.Run("generateObject", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:         `{"x":1}`,
					Content:      []types.ContentPart{types.TextContent{Text: `{"x":1}`}},
					FinishReason: types.FinishReasonStop,
				}, nil
			},
		}
		var mu sync.Mutex
		var got ObjectOnStepFinishEvent
		called := false
		if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model:  model,
			Prompt: "prompt",
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
			OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) {
				mu.Lock()
				defer mu.Unlock()
				got, called = e, true
			},
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if !called || got.MsToFirstChunk != nil {
			t.Fatalf("called = %v, MsToFirstChunk = %v, want nil", called, got.MsToFirstChunk)
		}
	})
}

// TS generateText pushes the tool errors of invalid tool calls into
// clientToolOutputs before the executed results ("insert error tool outputs
// for invalid tool calls", generate-text.ts 1486-1528), so a step's content
// lists them first. Expected order recorded by running TS generateText
// (ai dist at 382d0b8eaf) on the same model output: tool-call c1, c2, c3,
// tool-error c2, tool-result c1, tool-result c3; the tool message stays in
// call order.
func TestR89genInvalidToolErrorsBeforeExecutedResults(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
			}
			toolCalls := []types.ToolCall{
				{ID: "c1", ToolName: "a", RawArguments: `{"x":1}`},
				{ID: "c2", ToolName: "nope", RawArguments: `{}`},
				{ID: "c3", ToolName: "a", RawArguments: `{"x":3}`},
			}
			return &types.GenerateResult{ToolCalls: toolCalls, FinishReason: types.FinishReasonToolCalls}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "p",
		StopWhen: []StopCondition{func(StopConditionState) string { return "" }},
		Tools: []types.Tool{{
			Name:       "a",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"x": map[string]interface{}{"type": "number"}}},
			Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return input["x"], nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, part := range result.Steps[0].Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			got = append(got, "tool-call:"+p.ToolCallID)
		case types.ToolResultContent:
			got = append(got, "tool-result:"+p.ToolCallID)
		case types.ToolErrorContent:
			got = append(got, "tool-error:"+p.ToolCallID)
		default:
			got = append(got, "other")
		}
	}
	want := []string{"tool-call:c1", "tool-call:c2", "tool-call:c3", "tool-error:c2", "tool-result:c1", "tool-result:c3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("content = %v, want %v", got, want)
	}
	var toolMessage []string
	for _, m := range result.Steps[0].ResponseMessages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, part := range m.Content {
			if p, ok := part.(types.ToolResultContent); ok {
				toolMessage = append(toolMessage, p.ToolCallID)
			}
		}
	}
	if !reflect.DeepEqual(toolMessage, []string{"c1", "c2", "c3"}) {
		t.Fatalf("tool message = %v", toolMessage)
	}
}

// TS convertToLanguageModelPrompt calls downloadAssets, and so the download
// function, on every conversion: with an empty list when the prompt has no
// URL to plan (convert-to-language-model-prompt.ts 52-56, 494).
func TestR89genDownloadCalledWithEmptyPlan(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", Content: []types.ContentPart{types.TextContent{Text: "ok"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
	var calls []int
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "p",
		ExperimentalDownload: func(_ context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			if requests == nil {
				t.Error("requests is nil, want an empty list")
			}
			calls = append(calls, len(requests))
			return []*DownloadResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []int{0}) {
		t.Fatalf("download calls = %v, want [0]", calls)
	}
}

// TS DefaultSteering.add defaults the id only when options.id is undefined
// (steering.ts `id = generateSteeringId()`), so "" is a real id: a second
// message with it returns the first outcome.
func TestR89genSteeringEmptyIDIsAnID(t *testing.T) {
	s := NewSteering()
	first := s.Steer("a", WithSteerID(""))
	second := s.Steer("b", WithSteerID(""))
	if first != second {
		t.Fatal("a repeated empty id must return the first handle")
	}
	run, err := startSteeringRun(s)
	if err != nil {
		t.Fatal(err)
	}
	taken := run.takeMessages()
	run.resolveTakenMessages(0, taken)
	if o := first.Outcome(); o.ID != "" || !o.Delivered || len(taken) != 1 {
		t.Fatalf("outcome = %+v, taken = %d", o, len(taken))
	}
	if h := s.Steer("c"); h == first {
		t.Fatal("a message without an id must get a generated id")
	}
}

// TS parsePartialJson returns a parsed JSON null as a value (only a failed
// parse is undefined), and isDeepEqualData(undefined, null) is false, so
// streamObject publishes a null partial object (stream-object.ts 711-744).
func TestR89genStreamObjectPublishesNullPartial(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "null"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var partials []interface{}
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:      model,
		Prompt:     "p",
		OutputMode: ObjectModeNoSchema,
		OnChunk:    func(p interface{}) { partials = append(partials, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(partials) != 1 || partials[0] != nil {
		t.Fatalf("partials = %#v, want one nil partial", partials)
	}
	_ = result
}
