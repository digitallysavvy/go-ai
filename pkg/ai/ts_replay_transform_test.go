package ai

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// stream-text.test.ts describe('options.transform'): the recorded result
// (text, response messages, usage, finish reason, tool calls/results, steps,
// provider metadata, callbacks) comes from the transformed stream, and a
// transform can end the call with stopStream.

func upperCaseTransform(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
	switch c.Type {
	case provider.ChunkTypeText, provider.ChunkTypeReasoning:
		c.Text = strings.ToUpper(c.Text)
	case provider.ChunkTypeToolCall:
		if c.ToolCall != nil && !c.ToolCall.Dynamic {
			call := *c.ToolCall
			args := map[string]interface{}{}
			for k, v := range call.Arguments {
				args[k] = v
			}
			if v, ok := args["value"].(string); ok {
				args["value"] = strings.ToUpper(v)
			}
			call.Arguments = args
			c.ToolCall = &call
		}
	case provider.ChunkTypeToolResult:
		if c.ToolResult != nil && !c.ToolResult.Dynamic {
			tr := *c.ToolResult
			if s, ok := tr.Result.(string); ok {
				tr.Result = strings.ToUpper(s)
			}
			if in := tr.Input; in != nil {
				args := map[string]interface{}{}
				for k, v := range in {
					args[k] = v
				}
				if v, ok := args["value"].(string); ok {
					args["value"] = strings.ToUpper(v)
				}
				tr.Input = args
			}
			c.ToolResult = &tr
		}
	case provider.ChunkTypeFinishStep:
		if len(c.ProviderMetadata) > 0 {
			c.ProviderMetadata = json.RawMessage(`{"testProvider":{"testKey":"TEST VALUE"}}`)
		}
	}
	return []provider.StreamChunk{c}
}

func transformTestModel(chunks ...provider.StreamChunk) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(chunks), nil
		},
	}
}

func helloWorldChunks() []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
		{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10),
			ProviderMetadata: json.RawMessage(`{"testProvider":{"testKey":"testValue"}}`)},
	}
}

func TestTSReplay_StreamTransform_RecordedResultFollowsTransform(t *testing.T) {
	var mu sync.Mutex
	var stepEnd *types.StepResult
	var onEnd OnFinishEvent
	var chunks []string
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(helloWorldChunks()...), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{upperCaseTransform},
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			if c.Type == provider.ChunkTypeText {
				chunks = append(chunks, c.Text)
			}
		},
		OnStepEnd: func(_ context.Context, s types.StepResult, _ interface{}) {
			mu.Lock()
			stepEnd = &s
			mu.Unlock()
		},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			onEnd = e
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Text(); got != "HELLO, WORLD!" {
		t.Fatalf("text = %q, want HELLO, WORLD!", got)
	}
	steps := result.Steps()
	if len(steps) != 1 || steps[0].Text != "HELLO, WORLD!" {
		t.Fatalf("steps = %+v", steps)
	}
	msgs := steps[0].ResponseMessages
	if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant {
		t.Fatalf("response messages = %+v", msgs)
	}
	if got := textOfContent(msgs[0].Content); got != "HELLO, WORLD!" {
		t.Fatalf("response message text = %q", got)
	}
	if got := string(result.ProviderMetadata()); !strings.Contains(got, "TEST VALUE") {
		t.Fatalf("providerMetadata = %s, want transformed", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if stepEnd == nil || stepEnd.Text != "HELLO, WORLD!" {
		t.Fatalf("onStepEnd step = %+v", stepEnd)
	}
	if onEnd.Text != "HELLO, WORLD!" {
		t.Fatalf("onEnd text = %q", onEnd.Text)
	}
	if strings.Join(chunks, "|") != "HELLO|, |WORLD!" {
		t.Fatalf("onChunk text chunks = %v", chunks)
	}
}

func TestTSReplay_StreamTransform_FinishReasonAndTotalUsage(t *testing.T) {
	in, out := int64(200), int64(300)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength, RawFinishReason: "raw-length", Usage: usage(3, 10)},
		),
		Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
			if c.Type == provider.ChunkTypeFinish {
				c.FinishReason = types.FinishReasonStop
				c.Usage = &types.Usage{InputTokens: &in, OutputTokens: &out}
			}
			return []provider.StreamChunk{c}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.FinishReason(); got != types.FinishReasonStop {
		t.Fatalf("finishReason = %v, want stop", got)
	}
	u := result.Usage()
	if u.InputTokens == nil || *u.InputTokens != 200 || u.OutputTokens == nil || *u.OutputTokens != 300 {
		t.Fatalf("usage = %+v, want 200/300", u)
	}
}

func TestTSReplay_StreamTransform_ToolCallsAndResultsAreTransformed(t *testing.T) {
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
		),
		Prompt: "test-input", Tools: []types.Tool{tool1()},
		StopWhen:              []StopCondition{StepCountIs(1)},
		ExperimentalTransform: []StreamTransformFunc{upperCaseTransform},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := result.ToolCalls()
	if len(calls) != 1 || calls[0].Arguments["value"] != "VALUE" {
		t.Fatalf("toolCalls = %+v", calls)
	}
	results := result.ToolResults()
	if len(results) != 1 || results[0].Result != "RESULT1" {
		t.Fatalf("toolResults = %+v", results)
	}
	if results[0].Input["value"] != "VALUE" {
		t.Fatalf("toolResult input = %+v", results[0].Input)
	}
	steps := result.Steps()
	if len(steps) != 1 || len(steps[0].ToolCalls) != 1 || steps[0].ToolCalls[0].Arguments["value"] != "VALUE" {
		t.Fatalf("steps = %+v", steps)
	}
	// The assistant message replays the transformed input.
	var found bool
	for _, m := range steps[0].ResponseMessages {
		for _, tc := range m.ToolCalls {
			found = true
			if !strings.Contains(strings.ToUpper(tc.RawArguments), "VALUE") || strings.Contains(tc.RawArguments, "\"value\": \"value\"") {
				t.Fatalf("response tool call raw arguments = %q", tc.RawArguments)
			}
		}
	}
	if !found {
		t.Fatalf("no tool call in response messages: %+v", steps[0].ResponseMessages)
	}
}

func TestTSReplay_StreamTransform_MultipleTransforms(t *testing.T) {
	addComma := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeText {
			c.Text = strings.ToUpper(c.Text) + ","
		}
		return []provider.StreamChunk{c}
	}
	omitComma := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeText {
			c.Text = strings.ReplaceAll(c.Text, ",", "")
		}
		return []provider.StreamChunk{c}
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(helloWorldChunks()...), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{addComma, omitComma},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Text(); got != "HELLO WORLD!" {
		t.Fatalf("text = %q, want %q", got, "HELLO WORLD!")
	}
}

func TestTSReplay_StreamTransform_StopStream(t *testing.T) {
	stopWord := func(ctx context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type != provider.ChunkTypeText || !strings.Contains(c.Text, "STOP") {
			return []provider.StreamChunk{c}
		}
		stop, ok := StreamTransformStopFromContext(ctx)
		if !ok {
			t.Error("stop function missing from ctx")
			return nil
		}
		stop()
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}, Response: &types.StepResponse{ID: "response-id", ModelID: "mock-model-id"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}
	}
	model := transformTestModel(
		provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "STOP"},
		provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: " World"},
		provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
	)
	var calls int
	model.DoStreamFunc = func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
			{Type: provider.ChunkTypeText, ID: "1", Text: "STOP"},
			{Type: provider.ChunkTypeText, ID: "1", Text: " World"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
		}), nil
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{stopWord},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := collectTSParts(t, result.Stream())
	want := []string{"start", "start-step", "text-start:1", "text-delta:1:Hello, ", "finish-step:stop", "finish:stop"}
	if strings.Join(parts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts =\n%s\nwant\n%s", strings.Join(parts, "\n"), strings.Join(want, "\n"))
	}
	if got := result.Text(); got != "Hello, " {
		t.Fatalf("text = %q, want %q", got, "Hello, ")
	}
	if got := result.FinishReason(); got != types.FinishReasonStop {
		t.Fatalf("finishReason = %v", got)
	}
	if steps := result.Steps(); len(steps) != 1 || steps[0].Response.ID != "response-id" {
		t.Fatalf("steps = %+v", steps)
	}
	if calls != 1 {
		t.Fatalf("model called %d times", calls)
	}
}

// A transform stopping before it emitted any finish-step records no step
// (TS: NoOutputGeneratedError when nothing was recorded).
func TestTSReplay_StreamTransform_StopStreamWithoutFinishStepRecordsNothing(t *testing.T) {
	stopNow := func(ctx context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeText {
			if stop, ok := StreamTransformStopFromContext(ctx); ok {
				stop()
			}
			return nil
		}
		return []provider.StreamChunk{c}
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(helloWorldChunks()...), Prompt: "test-input",
		ExperimentalTransform: []StreamTransformFunc{stopNow},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := result.ReadAll()
	if !IsNoOutputGeneratedError(readErr) && !IsNoOutputGeneratedError(result.Err()) {
		t.Fatalf("expected NoOutputGeneratedError, got readErr=%v err=%v", readErr, result.Err())
	}
}
