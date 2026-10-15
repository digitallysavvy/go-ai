package ai

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func valueObjectOutput() Output[valueObj, valueObj] {
	return ObjectOutput[valueObj](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}),
	})
}

// TS applies the partial-output stage after the user transforms, so the
// partial follows the transformed text.
func TestStreamTransform_PartialOutputFollowsTransformedText(t *testing.T) {
	drop := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeText && c.Text == "garbage" {
			return nil
		}
		return []provider.StreamChunk{c}
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "garbage"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: `{"value":"done"}`},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
		),
		Prompt:                "p",
		Output:                valueObjectOutput(),
		ExperimentalTransform: []StreamTransformFunc{drop},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.PartialOutput(); !reflect.DeepEqual(got, valueObj{Value: "done"}) && !reflect.DeepEqual(got, map[string]interface{}{"value": "done"}) {
		t.Fatalf("partial output = %#v, want the transformed text's value", got)
	}
}

// TS createOutputTransformStream parses the partial from the step's first text
// block only.
func TestStreamText_PartialOutputUsesFirstTextBlockOnly(t *testing.T) {
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "a"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "a", Text: `{"value":"one"}`},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "a"},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "b"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "b", Text: `{"value":"two"}`},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "b"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
		),
		Prompt: "p",
		Output: valueObjectOutput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.PartialOutput(); !reflect.DeepEqual(got, valueObj{Value: "one"}) && !reflect.DeepEqual(got, map[string]interface{}{"value": "one"}) {
		t.Fatalf("partial output = %#v, want first block's value", got)
	}
}

// TS execute-tools-from-stream enqueues tool-approval-request right after its
// tool-call part (not after the whole step), and the recorded content has the
// same order.
func TestStreamText_ApprovalRequestFollowsItsToolCall(t *testing.T) {
	var mu sync.Mutex
	var order []provider.ChunkType
	needsCalls := 0
	mk := func(name string) types.Tool {
		return types.Tool{
			Name:       name,
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			NeedsApproval: types.ToolNeedsApprovalFunc(func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool {
				mu.Lock()
				needsCalls++
				mu.Unlock()
				return true
			}),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}},
			provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c2", ToolName: "b", Arguments: map[string]interface{}{}}},
			provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "after"},
			provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
		),
		Prompt:   "p",
		Tools:    []types.Tool{mk("a"), mk("b")},
		StopWhen: []StopCondition{StepCountIs(1)},
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			switch c.Type {
			case provider.ChunkTypeToolCall, provider.ChunkTypeToolApprovalRequest, provider.ChunkTypeText, provider.ChunkTypeFinishStep:
				order = append(order, c.Type)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []provider.ChunkType{provider.ChunkTypeToolCall, provider.ChunkTypeToolApprovalRequest, provider.ChunkTypeToolCall, provider.ChunkTypeToolApprovalRequest, provider.ChunkTypeText, provider.ChunkTypeFinishStep}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("chunk order = %v, want %v", order, want)
	}
	if needsCalls != 2 {
		t.Fatalf("needsApproval invoked %d times, want 2 (once per call)", needsCalls)
	}
	steps := res.Steps()
	var kinds []string
	for _, p := range steps[0].Content {
		switch p.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "call")
		case types.ToolApprovalRequestContent:
			kinds = append(kinds, "request")
		case types.TextContent:
			kinds = append(kinds, "text")
		}
	}
	if !reflect.DeepEqual(kinds, []string{"call", "request", "call", "request", "text"}) {
		t.Fatalf("content order = %v", kinds)
	}
}

// stream-text.test.ts 'multiple stream consumption' > 'should support text
// stream, ai stream, full stream on single result object': each stream of one
// result is consumed completely, one after the other.
func TestTSReplay_MultipleStreamConsumption(t *testing.T) {
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: transformTestModel(helloWorldChunks()...), Prompt: "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	var full []string
	for _, p := range collectTSParts(t, res.Stream()) {
		full = append(full, p)
	}
	want := []string{"start", "start-step", "text-start:1", "text-delta:1:Hello", "text-delta:1:, ", "text-delta:1:world!", "text-end:1", "finish-step:stop", "finish:stop"}
	if !reflect.DeepEqual(full, want) {
		t.Fatalf("full stream\n got %q\nwant %q", full, want)
	}
	// A second, independent full stream after the first one is exhausted.
	if again := collectTSParts(t, res.NewStream()); !reflect.DeepEqual(again, want) {
		t.Fatalf("second full stream\n got %q\nwant %q", again, want)
	}
	// The UI message stream still sees every chunk.
	chunks, errCh := res.ToUIMessageStream(context.Background())
	var types_ []string
	for c := range chunks {
		types_ = append(types_, fmt.Sprint(c["type"]))
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(types_, []string{"start", "start-step", "text-start", "text-delta", "text-delta", "text-delta", "text-end", "finish-step", "finish"}) {
		t.Fatalf("ui message stream chunk types = %v", types_)
	}
	// And the text stream response carries the whole text.
	resp, err := res.ToTextStreamResponse(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Hello, world!" {
		t.Fatalf("text stream body = %q", body)
	}
}
