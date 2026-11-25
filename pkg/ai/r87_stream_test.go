package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS executeToolsFromStream enqueues the tool-call part before it resolves
// the call's approval, so a failing resolution (here the tool context fails
// its schema before needsApproval runs) errors the stream after the part.
// Recorded with streamText under Node v24 (packages/ai dist at 3fab50aa9b):
// onChunk and fullStream see start, start-step, tool-call, then fullStream
// rejects with the validation error; needsApproval is never called and the
// model is called once.
func TestR87streamApprovalErrorAfterToolCallPart(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}), nil
	}}
	approvalCalled := false
	var onChunk []string
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x",
		Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"},
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}, "required": []interface{}{"a"}}),
			ToolApproval: types.ToolNeedsApprovalFunc(func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool {
				approvalCalled = true
				return true
			}),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return 1, nil
			}}},
		ToolsContext: map[string]interface{}{"t": map[string]interface{}{"a": 1}},
		StopWhen:     []StopCondition{StepCountIs(3)},
		OnChunk: func(c provider.StreamChunk) {
			if !isInternalStreamMarker(c.Type) { // Go-only lifecycle markers
				onChunk = append(onChunk, string(c.Type))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for c := range r.Chunks() {
		parts = append(parts, string(c.Type))
	}
	want := []string{"start", "start-step", "tool-call"}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("full stream = %v, want %v", parts, want)
	}
	if want := []string{"start", "start-step", "tool-call"}; !reflect.DeepEqual(onChunk, want) {
		t.Errorf("onChunk = %v, want %v", onChunk, want)
	}
	var validationErr *providererrors.ValidationError
	if !errors.As(r.Err(), &validationErr) || validationErr.Context == nil || validationErr.Context.Field != "tool context" {
		t.Errorf("Err() = %T %v, want the tool context ValidationError", r.Err(), r.Err())
	}
	if approvalCalled || calls != 1 {
		t.Errorf("approval called = %v, model calls = %d; want false, 1", approvalCalled, calls)
	}
}

// TS converts tool outputs (createToolModelOutput) when it builds the step's
// response messages, after the step's finish-step part, so a throwing
// toModelOutput errors the stream only after every part of the step. Recorded
// with streamText under Node v24 (packages/ai dist at 3fab50aa9b), fullStream:
//   - provider-executed result: start, start-step, tool-call, tool-result,
//     text-start, text-delta, text-end, finish-step, then it rejects;
//   - local result: start, start-step, tool-call, text-start, text-delta,
//     text-end, tool-result, finish-step, then it rejects.
//
// onStepEnd is not called and the model is called once.
func TestR87streamToModelOutputErrorAfterFinishStep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider bool
		want     []string
	}{
		{"provider-executed", true, []string{"start", "start-step", "tool-call", "tool-result", "text-start", "text", "text-end", "finish-step"}},
		{"local", false, []string{"start", "start-step", "tool-call", "text-start", "text", "text-end", "tool-result", "finish-step"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			chunks := []provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, ProviderExecuted: tc.provider}}}
			if tc.provider {
				chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c1", ToolName: "t", Result: map[string]interface{}{"x": 1}}})
			}
			chunks = append(chunks,
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "done"},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
			)
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				calls++
				return testutil.NewMockTextStream(chunks), nil
			}}
			tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
				ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
					return nil, errors.New("tmo-boom")
				}}
			if !tc.provider {
				tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return 1, nil
				}
			}
			stepEnds := 0
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x",
				Tools:        []types.Tool{tool},
				StopWhen:     []StopCondition{StepCountIs(3)},
				OnStepFinish: func(context.Context, types.StepResult, interface{}) { stepEnds++ },
			})
			if err != nil {
				t.Fatal(err)
			}
			var parts []string
			for c := range r.Chunks() {
				parts = append(parts, string(c.Type))
			}
			if !reflect.DeepEqual(parts, tc.want) {
				t.Errorf("full stream = %v, want %v", parts, tc.want)
			}
			if r.Err() == nil || r.Err().Error() != "tmo-boom" {
				t.Errorf("Err() = %v, want tmo-boom", r.Err())
			}
			if stepEnds != 0 || calls != 1 {
				t.Errorf("step ends = %d, model calls = %d; want 0, 1", stepEnds, calls)
			}
		})
	}
}

// TS LanguageModelRequestMetadata.url and LanguageModelCallEndEvent
// request?.url (934100cad4): the origin serializes as "url" and is omitted
// when empty.
func TestR87streamRequestURLFieldsJSON(t *testing.T) {
	for _, tc := range []struct {
		v    interface{}
		want string
	}{
		{GenerateStepRequest{URL: "https://h:8443"}, `{"url":"https://h:8443"}`},
		{GenerateStepRequest{}, `{}`},
		{LanguageModelCallRequest{URL: "https://h:8443"}, `{"url":"https://h:8443"}`},
		{LanguageModelCallRequest{}, `{}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Errorf("json(%#v) = %s, %v; want %s", tc.v, b, err, tc.want)
		}
	}
	if (LanguageModelCallEndEvent{}).Request != nil {
		t.Error("zero LanguageModelCallEndEvent.Request is not nil")
	}
}

// TS smoothStream's detectChunk throws inside the transform when a custom
// detector breaks its contract or a RegExp matches the empty string, so the
// stream errors there. Recorded with streamText under Node v24 (packages/ai
// dist at 3fab50aa9b), chunking `() => ”`: fullStream yields start,
// start-step, text-start and then rejects with "Chunking function must return
// a non-empty string."; onError, onStepEnd and onEnd are not called and
// result.text rejects. Go passed the rest through after an error part and
// finished the step normally.
func TestR87streamSmoothStreamDetectorErrorEndsRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chunking interface{}
		want     string
	}{
		{"empty function match", ChunkDetector(func(string) (string, bool) { return "", true }), "Chunking function must return a non-empty string."},
		{"empty regexp match", regexp.MustCompile(`x*`), "Chunking RegExp must not match an empty string."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "1"},
					{Type: provider.ChunkTypeText, ID: "1", Text: "hello "},
					{Type: provider.ChunkTypeText, ID: "1", Text: "world "},
					{Type: provider.ChunkTypeTextEnd, ID: "1"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
				}), nil
			}}
			zero := 0
			sm, err := SmoothStream(SmoothStreamOptions{DelayInMs: &zero, Chunking: tc.chunking})
			if err != nil {
				t.Fatal(err)
			}
			var callbacks []string
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x", ExperimentalTransform: []StreamTransformFunc{sm},
				OnError:    func(context.Context, error) { callbacks = append(callbacks, "onError") },
				OnStepEnd:  func(context.Context, types.StepResult, interface{}) { callbacks = append(callbacks, "onStepEnd") },
				OnEndEvent: func(context.Context, OnFinishEvent) { callbacks = append(callbacks, "onEnd") },
			})
			if err != nil {
				t.Fatal(err)
			}
			var parts []string
			for c := range r.Chunks() {
				parts = append(parts, string(c.Type))
			}
			if want := []string{"start", "start-step", "text-start"}; !reflect.DeepEqual(parts, want) {
				t.Errorf("full stream = %v, want %v", parts, want)
			}
			if _, err := r.ReadAll(); err == nil || err.Error() != tc.want {
				t.Errorf("ReadAll() error = %v, want %q", err, tc.want)
			}
			if len(callbacks) != 0 {
				t.Errorf("callbacks = %v, want none", callbacks)
			}
		})
	}
}
