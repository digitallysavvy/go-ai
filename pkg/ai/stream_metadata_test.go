package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamProviderMetadataPassedWithChunks verifies that provider metadata
// emitted in stream chunks is accumulated and exposed via ProviderMetadata().
func TestStreamProviderMetadataPassedWithChunks(t *testing.T) {
	t.Parallel()

	meta := json.RawMessage(`{"cost":0.001,"model":"gpt-4"}`)

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hello"},
				{Type: provider.ChunkTypeText, Text: " world", ProviderMetadata: meta},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}

	got := result.ProviderMetadata()
	if string(got) != string(meta) {
		t.Errorf("ProviderMetadata = %s, want %s", got, meta)
	}
}

// TestStreamProviderMetadataAbsent verifies that ProviderMetadata returns nil
// when no chunk carries metadata.
func TestStreamProviderMetadataAbsent(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("StreamText failed: %v", err)
	}

	_, err = result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if got := result.ProviderMetadata(); len(got) != 0 {
		t.Errorf("expected nil ProviderMetadata, got %s", got)
	}
}

// TestDynamicToolResultIncludesInput verifies that provider-executed (dynamic)
// tool results have their Input field set to the original tool arguments.
func TestDynamicToolResultIncludesInput(t *testing.T) {
	t.Parallel()

	args := map[string]interface{}{"query": "go generics"}

	// First call returns a provider-executed tool call; second call finishes.
	callCount := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					ToolCalls: []types.ToolCall{
						{ID: "tc1", ToolName: "web-search", Arguments: args},
					},
					FinishReason: types.FinishReasonToolCalls,
				}, nil
			}
			return &types.GenerateResult{
				Text:         "result",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	// web-search is a provider-executed tool — ProviderExecuted must be set on the
	// Tool definition so executeTools skips local execution.
	webSearch := types.Tool{
		Name:             "web-search",
		ProviderExecuted: true,
		Execute:          nil, // provider-executed; Execute won't be called
	}

	var captured []types.ToolResult
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "search for something",
		Tools:    []types.Tool{webSearch},
		StopWhen: []StopCondition{StepCountIs(5)},
		OnToolCallFinish: func(_ context.Context, _ OnToolCallFinishEvent) {
			// provider-executed tools don't fire this callback
		},
	})
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	captured = result.ToolResults

	if len(captured) == 0 {
		t.Fatal("expected at least one tool result")
	}

	tr := captured[0]
	if tr.ToolName != "web-search" {
		t.Errorf("ToolName = %q, want %q", tr.ToolName, "web-search")
	}
	if tr.Input == nil {
		t.Error("expected ToolResult.Input to be set for dynamic tool results")
	}
	if tr.Input["query"] != args["query"] {
		t.Errorf("Input[query] = %v, want %v", tr.Input["query"], args["query"])
	}
}

// TestTelemetryUsageAttributesComplete verifies that the OTel span emits the
// extended usage attributes (cache, reasoning) when the provider returns them.
// Not parallel — uses global OTel tracer provider.
func TestTelemetryUsageAttributesComplete(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	cacheRead := int64(50)
	cacheWrite := int64(100)
	reasoning := int64(200)
	input := int64(10)
	output := int64(20)
	total := int64(30)

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					InputTokens:  &input,
					OutputTokens: &output,
					TotalTokens:  &total,
					InputDetails: &types.InputTokenDetails{
						CacheReadTokens:  &cacheRead,
						CacheWriteTokens: &cacheWrite,
					},
					OutputDetails: &types.OutputTokenDetails{
						ReasoningTokens: &reasoning,
					},
				},
			}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalTelemetry: &TelemetrySettings{
			IsEnabled:  true,
			FunctionID: "usage-test",
		},
	})
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Small sleep to allow async spans to flush
	time.Sleep(10 * time.Millisecond)

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	var found bool
	for _, span := range spans {
		if span.Name() == "ai.generateText.usage-test" {
			found = true
			attrMap := map[string]int64{}
			for _, a := range span.Attributes() {
				if v, ok := a.Value.AsInterface().(int64); ok {
					attrMap[string(a.Key)] = v
				}
			}

			check := func(key string, want int64) {
				if got, ok := attrMap[key]; !ok {
					t.Errorf("attribute %q not found in span", key)
				} else if got != want {
					t.Errorf("attribute %q = %d, want %d", key, got, want)
				}
			}
			// Gen AI semantic convention attributes
			check("gen_ai.usage.input_tokens", input)
			check("gen_ai.usage.output_tokens", output)
			// Legacy ai.usage.* attributes (TS SDK emits both namespaces)
			check("ai.usage.inputTokens", input)
			check("ai.usage.outputTokens", output)
			check("ai.usage.inputTokenDetails.cacheReadTokens", cacheRead)
			check("ai.usage.inputTokenDetails.cacheWriteTokens", cacheWrite)
			check("ai.usage.reasoningTokens", reasoning)
		}
	}
	if !found {
		t.Error("span ai.generateText.usage-test not found")
	}
}

// TestTelemetryModelAttributesFlattened verifies that the OTel span uses
// gen_ai.system and gen_ai.request.model (not ai.model.provider/ai.model.id).
// Not parallel — uses global OTel tracer provider.
func TestTelemetryModelAttributesFlattened(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalTelemetry: &TelemetrySettings{
			IsEnabled:  true,
			FunctionID: "flat-test",
		},
	})
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	spans := spanRecorder.Ended()
	for _, span := range spans {
		if span.Name() == "ai.generateText.flat-test" {
			for _, a := range span.Attributes() {
				key := string(a.Key)
				if key == "ai.model.provider" || key == "ai.model.id" {
					t.Errorf("found deprecated attribute %q; should use gen_ai.system / gen_ai.request.model", key)
				}
			}
			var hasSystem, hasModel bool
			for _, a := range span.Attributes() {
				switch string(a.Key) {
				case "gen_ai.system":
					hasSystem = true
				case "gen_ai.request.model":
					hasModel = true
				}
			}
			if !hasSystem {
				t.Error("attribute gen_ai.system not found")
			}
			if !hasModel {
				t.Error("attribute gen_ai.request.model not found")
			}
			return
		}
	}
	t.Error("span ai.generateText.flat-test not found")
}
