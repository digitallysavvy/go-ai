package ai

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// stream-text.test.ts describe('tools with preliminary results'): a
// generator-style tool streams every yielded value as a preliminary
// tool-result part and the last yielded value as the final result.

func cityAttractionsTool() types.Tool {
	return types.Tool{
		Name:       "cityAttractions",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
		Execute: types.StreamingExecutor(func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
			city := input["city"]
			return func(yield func(interface{}, error) bool) {
				if !yield(map[string]interface{}{"status": "loading", "text": fmt.Sprintf("Getting weather for %v", city)}, nil) {
					return
				}
				yield(map[string]interface{}{"status": "success", "text": fmt.Sprintf("The weather in %v is 72°F", city), "temperature": 72}, nil)
			}
		}),
	}
}

func preliminaryModel() provider.LanguageModel {
	return uiModel(
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", Arguments: map[string]interface{}{"city": "San Francisco"}}},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
	)
}

func TestTSReplay_StreamText_PreliminaryToolResults(t *testing.T) {
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: preliminaryModel(), Prompt: "test-input", Tools: []types.Tool{cityAttractionsTool()},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var results []types.ToolResult
	var order []string
	s := result.Stream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult {
			results = append(results, *c.ToolResult)
			order = append(order, fmt.Sprintf("tool-result preliminary=%v", c.ToolResult.Preliminary))
		}
	}
	want := []string{"tool-result preliminary=true", "tool-result preliminary=true", "tool-result preliminary=false"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("tool-result order = %v, want %v", order, want)
	}
	if results[0].Result.(map[string]interface{})["status"] != "loading" || results[1].Result.(map[string]interface{})["status"] != "success" {
		t.Fatalf("preliminary results = %+v", results[:2])
	}
	if results[2].Result.(map[string]interface{})["status"] != "success" {
		t.Fatalf("final result = %+v", results[2])
	}

	// Only the final result is recorded.
	if got := result.ToolResults(); len(got) != 1 || got[0].Preliminary {
		t.Fatalf("ToolResults = %+v", got)
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	var resultParts int
	for _, p := range steps[0].Content {
		if tr, ok := p.(types.ToolResultContent); ok {
			resultParts++
			if tr.Preliminary {
				t.Fatalf("preliminary result recorded in content: %+v", tr)
			}
		}
	}
	if resultParts != 1 {
		t.Fatalf("content tool-result parts = %d, want 1", resultParts)
	}
}

func TestTSReplay_StreamText_PreliminaryToolResultsUIStream(t *testing.T) {
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: preliminaryModel(), Prompt: "test-input", Tools: []types.Tool{cityAttractionsTool()},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var flags []interface{}
	for _, p := range tsUIParts(t, result) {
		if p["type"] == "tool-output-available" {
			flags = append(flags, p["preliminary"])
		}
	}
	if !reflect.DeepEqual(flags, []interface{}{true, true, nil}) {
		t.Fatalf("tool-output-available preliminary flags = %v, want [true true <nil>]", flags)
	}
}

// generateText ignores preliminary results and records the last yielded value.
func TestTSReplay_GenerateText_PreliminaryToolResultsIgnored(t *testing.T) {
	model := &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "cityAttractions", Arguments: map[string]interface{}{"city": "San Francisco"}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "test-input", Tools: []types.Tool{cityAttractionsTool()},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.ToolResults
	if len(got) != 1 || got[0].Preliminary || got[0].Result.(map[string]interface{})["status"] != "success" {
		t.Fatalf("tool results = %+v", got)
	}
}

func TestStreamingExecutor_EmptyAndError(t *testing.T) {
	empty := types.StreamingExecutor(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
		return func(func(interface{}, error) bool) {}
	})
	if out, err := empty(context.Background(), nil, types.ToolExecutionOptions{}); out != nil || err != nil {
		t.Fatalf("empty = %v, %v", out, err)
	}
	boom := fmt.Errorf("boom")
	failing := types.StreamingExecutor(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
		return func(yield func(interface{}, error) bool) {
			yield("first", nil)
			yield(nil, boom)
		}
	})
	var seen []interface{}
	_, err := failing(context.Background(), nil, types.ToolExecutionOptions{OnPreliminaryResult: func(o interface{}) { seen = append(seen, o) }})
	if err != boom || len(seen) != 1 || seen[0] != "first" {
		t.Fatalf("err=%v seen=%v", err, seen)
	}
}
