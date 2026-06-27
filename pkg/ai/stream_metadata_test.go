package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

func TestStreamReadAllProviderInlineToolResultUsesToModelOutputAndPreservesRawResult(t *testing.T) {
	t.Parallel()

	raw := map[string]interface{}{"public": "provider result", "secret": "hide me"}
	input := map[string]interface{}{"query": "docs"}
	var gotOptions types.ToModelOutputOptions
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web-search", Arguments: input, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web-search", Result: raw, ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "search",
		Tools: []types.Tool{{
			Name:             "web-search",
			ProviderExecuted: true,
			ToModelOutput: func(_ context.Context, opts types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				gotOptions = opts
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: provider result"}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if gotOptions.ToolCallID != "call-1" || !reflect.DeepEqual(gotOptions.Input, input) || !reflect.DeepEqual(gotOptions.Output, raw) {
		t.Fatalf("ToModelOutput options = %+v, want TS-shaped provider result options", gotOptions)
	}
	toolResults := result.ToolResults()
	if len(toolResults) != 1 || !reflect.DeepEqual(toolResults[0].Result, raw) {
		t.Fatalf("ToolResults = %#v, want raw provider result", toolResults)
	}
	if !toolResults[0].ProviderExecuted || !reflect.DeepEqual(toolResults[0].Input, input) {
		t.Fatalf("ToolResults[0] = %+v, want provider-executed result with original input", toolResults[0])
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("Steps len = %d, want 1", len(steps))
	}
	var contentResult types.ToolResultContent
	found := false
	for _, part := range steps[0].Content {
		if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == "call-1" {
			contentResult = tr
			found = true
		}
	}
	if !found {
		t.Fatalf("missing tool result content in step: %#v", steps[0].Content)
	}
	if contentResult.Result != nil || contentResult.Output == nil || contentResult.Output.Type != types.ToolResultOutputText || contentResult.Output.Value != "model sees: provider result" {
		t.Fatalf("tool result content = %+v, want converted model output", contentResult)
	}
}

func TestStreamProcessProviderInlineToolResultUsesToModelOutputAndPreservesRawResult(t *testing.T) {
	t.Parallel()

	raw := map[string]interface{}{"public": "provider result", "secret": "hide me"}
	input := map[string]interface{}{"query": "docs"}
	var gotOptions types.ToModelOutputOptions
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web-search", Arguments: input, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web-search", Result: raw, ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "search",
		Tools: []types.Tool{{
			Name:             "web-search",
			ProviderExecuted: true,
			ToModelOutput: func(_ context.Context, opts types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				gotOptions = opts
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: provider result"}, nil
			},
		}},
		OnChunk: func(provider.StreamChunk) {},
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	toolResults := result.ToolResults()
	if gotOptions.ToolCallID != "call-1" || !reflect.DeepEqual(gotOptions.Input, input) || !reflect.DeepEqual(gotOptions.Output, raw) {
		t.Fatalf("ToModelOutput options = %+v, want TS-shaped provider result options", gotOptions)
	}
	if len(toolResults) != 1 || !reflect.DeepEqual(toolResults[0].Result, raw) {
		t.Fatalf("ToolResults = %#v, want raw provider result", toolResults)
	}
	if !toolResults[0].ProviderExecuted || !reflect.DeepEqual(toolResults[0].Input, input) {
		t.Fatalf("ToolResults[0] = %+v, want provider-executed result with original input", toolResults[0])
	}
}

func TestStreamReadAllProviderInlineToModelOutputErrorPropagates(t *testing.T) {
	t.Parallel()

	convertErr := errors.New("conversion failed")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web-search", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web-search", Result: "raw", ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "search",
		Tools: []types.Tool{{
			Name:             "web-search",
			ProviderExecuted: true,
			ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return nil, convertErr
			},
		}},
	})
	if err != nil {
		t.Fatalf("StreamText error = %v", err)
	}
	_, err = result.ReadAll()
	if !errors.Is(err, convertErr) {
		t.Fatalf("ReadAll error = %v, want conversion error", err)
	}
	if err.Error() != "conversion failed" {
		t.Fatalf("ReadAll error string = %q, want original conversion error", err.Error())
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
			IsEnabled:  telemetry.Bool(true),
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
			IsEnabled:  telemetry.Bool(true),
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
