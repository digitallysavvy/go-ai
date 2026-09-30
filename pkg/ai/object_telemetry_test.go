package ai

import (
	"context"
	"strings"
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

	// Step span: the real span name is the bare step operation id
	// "ai.generateObject.doGenerate" (TS: `tracer.startSpan(stepOperationId,
	// ...)`), matching the root span's naming rule (functionID only
	// surfaces via the operation.name attribute).
	stepSpan := spanByOperationName(spans, "ai.generateObject.doGenerate", "ai.generateObject.doGenerate obj-test")
	if stepSpan == nil {
		t.Fatal("expected an ai.generateObject.doGenerate step span — GenerateObject previously fired no step span at all (H3 item 1)")
	}
	if v, ok := attrValueTelemetry(stepSpan, "ai.response.object"); !ok || v != `{"name":"John"}` {
		t.Errorf("step span ai.response.object = %v (ok=%v)", v, ok)
	}

	// No "chat test-model" span here: setupTelemetryTest registers only the
	// Legacy integration, and LegacyOpenTelemetry no longer creates a nested
	// model-call/"chat" span (H4 item 1) — TS's LegacyOpenTelemetry never
	// did either. See TestStreamObject_LanguageModelCallEndContent below for
	// GenAI's "chat" span coverage.
	if chatSpan := findSpanByName(spans, "chat test-model"); chatSpan != nil {
		t.Error("LegacyOpenTelemetry should not create a 'chat test-model' span (H4 item 1)")
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

	stepSpan := spanByOperationName(spans, "ai.streamObject.doStream", "ai.streamObject.doStream stream-obj-test")
	if stepSpan == nil {
		t.Fatal("expected an ai.streamObject.doStream step span")
	}

	// No "chat test-model" span: only LegacyOpenTelemetry is registered here,
	// and it no longer creates a nested model-call/"chat" span (H4 item 1).
	if chatSpan := findSpanByName(spans, "chat test-model"); chatSpan != nil {
		t.Error("LegacyOpenTelemetry should not create a 'chat test-model' span (H4 item 1)")
	}

	if result.Object == nil {
		t.Fatal("expected a non-nil result object")
	}
}

// TestStreamObject_LanguageModelCallEndContent covers H3 item 4(b):
// fireObjectLanguageModelCallEnd used to pass a hardcoded nil Content for
// ai.streamObject, so the GenAI OpenTelemetry integration's "chat" span
// (which builds gen_ai.output.messages from LanguageModelCallEndEvent.Content
// via formatOutputMessages) recorded no output at all. It should now carry
// the accumulated text, matching TS's flush handler passing the accumulated
// text/reasoning to its language-model-call-end event regardless of
// streaming.
func TestStreamObject_LanguageModelCallEndContent(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()
	// setupTelemetryTest only registers the Legacy integration; also register
	// the GenAI one so the "chat" span (which carries gen_ai.output.messages)
	// gets created — LegacyOpenTelemetry no longer creates one at all
	// (H4 item 1), so exactly one "chat test-model" span is expected below.
	telemetry.RegisterTelemetryIntegration(telemetry.OTelTelemetryIntegration{}, telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{}))

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

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "Generate a person",
		Schema: testSchema,
		ExperimentalTelemetry: &telemetry.Settings{
			IsEnabled:     telemetry.Bool(true),
			RecordOutputs: true,
		},
	})
	if err != nil {
		t.Fatalf("StreamObject failed: %v", err)
	}

	spans := spanRecorder.Ended()
	// Exactly one "chat test-model" span now (GenAI's — LegacyOpenTelemetry no
	// longer creates one, H4 item 1), carrying gen_ai.output.messages.
	chatSpans := 0
	var found bool
	for _, s := range spans {
		if s.Name() != "chat test-model" {
			continue
		}
		chatSpans++
		if v, ok := attrValueTelemetry(s, "gen_ai.output.messages"); ok && v != "" {
			found = true
			if !strings.Contains(v, `Jane`) {
				t.Errorf("gen_ai.output.messages = %s, want it to contain the accumulated text", v)
			}
		}
	}
	if chatSpans != 1 {
		t.Fatalf("'chat test-model' spans = %d, want exactly 1 (GenAI's; Legacy no longer creates one)", chatSpans)
	}
	if !found {
		t.Fatal("expected the 'chat test-model' span to carry a non-empty gen_ai.output.messages")
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
