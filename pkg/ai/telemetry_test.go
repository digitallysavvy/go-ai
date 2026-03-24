package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// valuesEqual compares two values, handling numeric type conversions
func valuesEqual(expected, actual interface{}) bool {
	// Direct equality
	if expected == actual {
		return true
	}

	// Handle numeric type conversions (int vs int64)
	switch e := expected.(type) {
	case int:
		if a, ok := actual.(int64); ok {
			return int64(e) == a
		}
	case int64:
		if a, ok := actual.(int); ok {
			return e == int64(a)
		}
	}

	return false
}

// MockModel for testing telemetry
type mockTelemetryModel struct {
	provider.LanguageModel
}

func (m *mockTelemetryModel) Provider() string {
	return "test-provider"
}

func (m *mockTelemetryModel) ModelID() string {
	return "test-model"
}

func (m *mockTelemetryModel) SpecificationVersion() string {
	return "v3"
}

func (m *mockTelemetryModel) SupportsTools() bool {
	return false
}

func (m *mockTelemetryModel) SupportsStructuredOutput() bool {
	return true
}

func (m *mockTelemetryModel) SupportsImageInput() bool {
	return false
}

func (m *mockTelemetryModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	inputTokens := int64(10)
	outputTokens := int64(20)
	totalTokens := int64(30)

	return &types.GenerateResult{
		Text:         "Test response",
		FinishReason: types.FinishReasonStop,
		Usage: types.Usage{
			InputTokens:  &inputTokens,
			OutputTokens: &outputTokens,
			TotalTokens:  &totalTokens,
		},
	}, nil
}

func (m *mockTelemetryModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, nil
}

// MockEmbeddingModel for testing telemetry
type mockEmbeddingModel struct {
	provider.EmbeddingModel
}

func (m *mockEmbeddingModel) Provider() string {
	return "test-provider"
}

func (m *mockEmbeddingModel) ModelID() string {
	return "test-embedding-model"
}

func (m *mockEmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	return &types.EmbeddingResult{
		Embedding: []float64{0.1, 0.2, 0.3},
		Usage: types.EmbeddingUsage{
			InputTokens: 5,
			TotalTokens: 5,
		},
	}, nil
}

func (m *mockEmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	embeddings := make([][]float64, len(inputs))
	for i := range inputs {
		embeddings[i] = []float64{0.1, 0.2, 0.3}
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: len(inputs) * 5,
			TotalTokens: len(inputs) * 5,
		},
	}, nil
}

func setupTelemetryTest(t *testing.T) (*tracetest.SpanRecorder, func()) {
	// Create span recorder to capture spans
	spanRecorder := tracetest.NewSpanRecorder()

	// Create tracer provider with recorder
	tp := trace.NewTracerProvider(
		trace.WithSpanProcessor(spanRecorder),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	// Register OTelTelemetryIntegration so generate.go/stream.go use OTel spans.
	telemetry.RegisterTelemetryIntegration(telemetry.OTelTelemetryIntegration{})

	cleanup := func() {
		// Restore noop integration so other tests are not affected.
		telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}

	return spanRecorder, cleanup
}

func TestGenerateText_Telemetry(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &mockTelemetryModel{}

	telemetrySettings := &telemetry.Settings{
		IsEnabled:     true,
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "test-function",
		Metadata: map[string]attribute.Value{
			"test_key": attribute.StringValue("test_value"),
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:                 model,
		Prompt:                "Test prompt",
		ExperimentalTelemetry: telemetrySettings,
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	if result.Text != "Test response" {
		t.Errorf("Expected 'Test response', got '%s'", result.Text)
	}

	// Verify span was created
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be recorded")
	}

	// Find the ai.generateText span
	var generateTextSpan trace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "ai.generateText.test-function" {
			generateTextSpan = span
			break
		}
	}

	if generateTextSpan == nil {
		t.Fatal("Expected ai.generateText.test-function span")
	}

	// Verify attributes
	attrs := generateTextSpan.Attributes()
	expectedAttrs := map[string]interface{}{
		"ai.operationId":                  "ai.generateText",
		"ai.model.provider":               "test-provider",
		"ai.model.id":                     "test-model",
		"ai.telemetry.functionId":         "test-function",
		"ai.telemetry.metadata.test_key":  "test_value",
		"ai.prompt":                       "Test prompt",
		"ai.response.text":                "Test response",
		"ai.response.finishReason":        "stop",
		"ai.usage.promptTokens":           int64(10),
		"ai.usage.completionTokens":       int64(20),
		"ai.usage.totalTokens":            int64(30),
	}

	for key, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key {
				found = true
				actualValue := attr.Value.AsInterface()
				if !valuesEqual(expectedValue, actualValue) {
					t.Errorf("Attribute %s: expected %v (%T), got %v (%T)", key, expectedValue, expectedValue, actualValue, actualValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected attribute %s not found", key)
		}
	}
}

func TestGenerateText_TelemetryDisabled(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &mockTelemetryModel{}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Test prompt",
		// No telemetry settings - should not create spans
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Verify no spans were created
	spans := spanRecorder.Ended()
	if len(spans) != 0 {
		t.Errorf("Expected no spans when telemetry is disabled, got %d", len(spans))
	}
}

func TestGenerateText_TelemetryRecordInputsDisabled(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &mockTelemetryModel{}

	telemetrySettings := &telemetry.Settings{
		IsEnabled:     true,
		RecordInputs:  false, // Don't record inputs
		RecordOutputs: true,
		FunctionID:    "test-function",
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:                 model,
		Prompt:                "Test prompt",
		ExperimentalTelemetry: telemetrySettings,
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be recorded")
	}

	// Verify prompt attribute is NOT present
	for _, span := range spans {
		if span.Name() == "ai.generateText.test-function" {
			attrs := span.Attributes()
			for _, attr := range attrs {
				if string(attr.Key) == "ai.prompt" {
					t.Error("Expected ai.prompt attribute to be absent when RecordInputs is false")
				}
			}
		}
	}
}

func TestEmbed_Telemetry(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &mockEmbeddingModel{}

	telemetrySettings := &telemetry.Settings{
		IsEnabled:     true,
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "embed-test",
	}

	result, err := Embed(context.Background(), EmbedOptions{
		Model:                 model,
		Input:                 "Test embedding input",
		ExperimentalTelemetry: telemetrySettings,
	})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(result.Embedding) != 3 {
		t.Errorf("Expected embedding length 3, got %d", len(result.Embedding))
	}

	// Verify span was created
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be recorded")
	}

	// Find the ai.embed span
	var embedSpan trace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "ai.embed.embed-test" {
			embedSpan = span
			break
		}
	}

	if embedSpan == nil {
		t.Fatal("Expected ai.embed.embed-test span")
	}

	// Verify attributes
	attrs := embedSpan.Attributes()
	expectedAttrs := map[string]interface{}{
		"ai.operationId":          "ai.embed",
		"ai.model.provider":       "test-provider",
		"ai.model.id":             "test-embedding-model",
		"ai.telemetry.functionId": "embed-test",
		"ai.value":                "Test embedding input",
		"ai.usage.tokens":         5,
	}

	for key, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key {
				found = true
				actualValue := attr.Value.AsInterface()
				if !valuesEqual(expectedValue, actualValue) {
					t.Errorf("Attribute %s: expected %v (%T), got %v (%T)", key, expectedValue, expectedValue, actualValue, actualValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected attribute %s not found", key)
		}
	}
}

func TestEmbedMany_Telemetry(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()

	model := &mockEmbeddingModel{}

	telemetrySettings := &telemetry.Settings{
		IsEnabled:  true,
		FunctionID: "embed-many-test",
	}

	inputs := []string{"input1", "input2", "input3"}
	result, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:                 model,
		Inputs:                inputs,
		ExperimentalTelemetry: telemetrySettings,
	})

	if err != nil {
		t.Fatalf("EmbedMany failed: %v", err)
	}

	if len(result.Embeddings) != 3 {
		t.Errorf("Expected 3 embeddings, got %d", len(result.Embeddings))
	}

	// Verify span was created
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("Expected at least one span to be recorded")
	}

	// Find the ai.embedMany span
	var embedManySpan trace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "ai.embedMany.embed-many-test" {
			embedManySpan = span
			break
		}
	}

	if embedManySpan == nil {
		t.Fatal("Expected ai.embedMany.embed-many-test span")
	}

	// Verify attributes
	attrs := embedManySpan.Attributes()
	expectedAttrs := map[string]interface{}{
		"ai.operationId":          "ai.embedMany",
		"ai.model.provider":       "test-provider",
		"ai.model.id":             "test-embedding-model",
		"ai.telemetry.functionId": "embed-many-test",
		"ai.values.count":         3,
		"ai.usage.tokens":         15, // 3 inputs * 5 tokens each
	}

	for key, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key {
				found = true
				actualValue := attr.Value.AsInterface()
				if !valuesEqual(expectedValue, actualValue) {
					t.Errorf("Attribute %s: expected %v (%T), got %v (%T)", key, expectedValue, expectedValue, actualValue, actualValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected attribute %s not found", key)
		}
	}
}

// TestGenerateTextWithNoTelemetry verifies that GenerateText works without any
// telemetry integration registered — no panics, no OTel dependency required.
func TestGenerateTextWithNoTelemetry(t *testing.T) {
	// Ensure noop is registered (default, but set explicitly for clarity).
	telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &mockTelemetryModel{}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		// No ExperimentalTelemetry set.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Test response" {
		t.Errorf("unexpected text: %s", result.Text)
	}
}

// TestGenerateTextWithMockTelemetry verifies that a registered mock integration
// receives OnStart/OnFinish events from GenerateText.
func TestGenerateTextWithMockTelemetry(t *testing.T) {
	mock := &mockTelemetryIntegration{}
	telemetry.RegisterTelemetryIntegration(mock)
	defer telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	model := &mockTelemetryModel{}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "Hello",
		ExperimentalTelemetry: &telemetry.Settings{
			IsEnabled: true,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.spans) == 0 {
		t.Fatal("expected mock integration to receive at least one span")
	}
	if mock.spans[0].name != "ai.generateText" {
		t.Errorf("expected span name 'ai.generateText', got %q", mock.spans[0].name)
	}
	if !mock.spans[0].ended {
		t.Error("expected span to be ended (OnFinish called)")
	}
}

// mockTelemetryCtxKey is the context key used to thread mock span records.
type mockTelemetryCtxKey struct{}

// mockTelemetryIntegration records operation names and end-states for assertions.
type mockTelemetryIntegration struct {
	mu    sync.Mutex
	spans []*mockTelemetrySpan
}

type mockTelemetrySpan struct {
	mu    sync.Mutex
	name  string
	ended bool
}

func (m *mockTelemetryIntegration) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	if e.Settings == nil || !e.Settings.IsEnabled {
		return ctx
	}
	sp := &mockTelemetrySpan{name: e.OperationType}
	m.mu.Lock()
	m.spans = append(m.spans, sp)
	m.mu.Unlock()
	// Embed the span pointer so OnFinish can mark it ended.
	return context.WithValue(ctx, mockTelemetryCtxKey{}, sp)
}

func (m *mockTelemetryIntegration) OnStepStart(_ context.Context, _ telemetry.TelemetryStepStartEvent) {
}
func (m *mockTelemetryIntegration) OnToolCallStart(ctx context.Context, _ telemetry.TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (m *mockTelemetryIntegration) OnToolCallFinish(_ context.Context, _ telemetry.TelemetryToolCallFinishEvent) {
}
func (m *mockTelemetryIntegration) OnChunk(_ context.Context, _ telemetry.TelemetryChunkEvent) {}
func (m *mockTelemetryIntegration) OnStepFinish(_ context.Context, _ telemetry.TelemetryStepFinishEvent) {
}

func (m *mockTelemetryIntegration) OnFinish(ctx context.Context, _ telemetry.TelemetryFinishEvent) {
	if sp, ok := ctx.Value(mockTelemetryCtxKey{}).(*mockTelemetrySpan); ok {
		sp.mu.Lock()
		sp.ended = true
		sp.mu.Unlock()
	}
}

func (m *mockTelemetryIntegration) OnError(ctx context.Context, _ telemetry.TelemetryErrorEvent) {
	if sp, ok := ctx.Value(mockTelemetryCtxKey{}).(*mockTelemetrySpan); ok {
		sp.mu.Lock()
		sp.ended = true
		sp.mu.Unlock()
	}
}

func (m *mockTelemetryIntegration) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}
