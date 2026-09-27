package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestGenerateObject_Telemetry covers H3 item 1: GenerateObject previously
// fired only ai.generateObject start/end telemetry, with no step or
// language-model-call ("chat") spans at all. It should now mirror
// generateText/streamText's start -> step -> chat -> step-end -> end
// lifecycle, and LegacyOpenTelemetry's OperationType dispatch should use the
// generateObject-specific attribute shape (ai.response.object, not
// ai.response.text).
func TestGenerateObject_Telemetry(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"name":"John"}`,
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: int64Ptr(10), OutputTokens: int64Ptr(5)},
			}, nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	})

	telemetrySettings := &telemetry.Settings{
		IsEnabled:     telemetry.Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "obj-test",
	}

	result, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                testSchema,
		ExperimentalTelemetry: telemetrySettings,
	})
	if err != nil {
		t.Fatalf("GenerateObject failed: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	rootSpan := spanByOperationName(spans, "ai.generateObject", "ai.generateObject obj-test")
	if rootSpan == nil {
		t.Fatal("expected an ai.generateObject root span")
	}
	if v, ok := attrValueTelemetry(rootSpan, "ai.response.object"); !ok || v != `{"name":"John"}` {
		t.Errorf("ai.response.object = %v (ok=%v), want the generated object JSON", v, ok)
	}
	if _, ok := attrValueTelemetry(rootSpan, "ai.response.text"); ok {
		t.Error("ai.generateObject root span should not carry ai.response.text (that's the generateText shape)")
	}

	// Step span: the real span name is "ai.generateObject step 0" (Go keeps
	// its own descriptive name); legacyStepOperationID's
	// "ai.generateObject.doGenerate" surfaces only via the operation.name
	// attribute, mirroring the root span's naming rule.
	stepSpan := spanByOperationName(spans, "ai.generateObject step 0", "ai.generateObject.doGenerate obj-test")
	if stepSpan == nil {
		t.Fatal("expected an ai.generateObject.doGenerate step span — GenerateObject previously fired no step span at all (H3 item 1)")
	}
	if v, ok := attrValueTelemetry(stepSpan, "ai.response.object"); !ok || v != `{"name":"John"}` {
		t.Errorf("step span ai.response.object = %v (ok=%v)", v, ok)
	}

	// Chat (language-model-call) span.
	chatSpan := findSpanByName(spans, "chat test-model")
	if chatSpan == nil {
		t.Fatal("expected a 'chat test-model' language-model-call span — GenerateObject previously fired none (H3 item 1)")
	}

	if result.Object == nil {
		t.Fatal("expected a non-nil result object")
	}
}

// TestStreamObject_Telemetry covers H3 item 1: StreamObject previously fired
// no telemetry spans whatsoever (no FireOnStart/FireOnEnd, unlike
// GenerateObject/GenerateText/StreamText).
func TestStreamObject_Telemetry(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	usage := types.Usage{InputTokens: int64Ptr(8), OutputTokens: int64Ptr(4)}
	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"Jane"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &usage},
			}), nil
		},
	}

	testSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	})

	telemetrySettings := &telemetry.Settings{
		IsEnabled:     telemetry.Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "stream-obj-test",
	}

	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                testSchema,
		ExperimentalTelemetry: telemetrySettings,
	})
	if err != nil {
		t.Fatalf("StreamObject failed: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	rootSpan := spanByOperationName(spans, "ai.streamObject", "ai.streamObject stream-obj-test")
	if rootSpan == nil {
		t.Fatal("expected an ai.streamObject root span — StreamObject previously fired no telemetry spans at all (H3 item 1)")
	}
	if v, ok := attrValueTelemetry(rootSpan, "ai.response.object"); !ok || v != `{"name":"Jane"}` {
		t.Errorf("ai.response.object = %v (ok=%v), want the generated object JSON", v, ok)
	}

	stepSpan := spanByOperationName(spans, "ai.streamObject step 0", "ai.streamObject.doStream stream-obj-test")
	if stepSpan == nil {
		t.Fatal("expected an ai.streamObject.doStream step span")
	}

	chatSpan := findSpanByName(spans, "chat test-model")
	if chatSpan == nil {
		t.Fatal("expected a 'chat test-model' language-model-call span")
	}

	if result.Object == nil {
		t.Fatal("expected a non-nil result object")
	}
}

// findSpanByName returns the first span with the given exact name, or nil.
func findSpanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// attrValueTelemetry returns a span's string attribute value by key.
func attrValueTelemetry(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	if span == nil {
		return "", false
	}
	for _, a := range span.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}
	return "", false
}
