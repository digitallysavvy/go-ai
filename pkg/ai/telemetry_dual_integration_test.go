package ai

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// H5: with both LegacyOpenTelemetry and OpenTelemetry (GenAI) registered at
// once, FireOnStart/FireOnStepStart/etc used to thread ctx through both
// integrations in turn (each integration's OnStart embeds its own span in
// the returned ctx via OTel's single well-known "current span" context key),
// and root-span resolution in OnEnd/OnError/OnAbort used
// trace.SpanFromContext(ctx), which only ever finds the LAST integration's
// root span — so the OTHER integration's root span never got its end
// attributes and never ended, even on success (a span leak on every call).
//
// These tests register both integrations, each with its own dedicated
// tracer/recorder so their spans can be inspected independently, and assert
// that every span BOTH integrations started also ended exactly once, for
// generateText, streamText, generateObject, streamObject, embed, rerank,
// evaluate, and a tool call — covering success, error, and abort.

// dualIntegrationSettings creates one dedicated tracer/recorder pair per
// integration and a *telemetry.Options with both LegacyOpenTelemetry and
// OpenTelemetry (GenAI) registered as per-call integrations, independent of
// whatever is globally registered.
func dualIntegrationSettings(t *testing.T) (legacyRec, genAIRec *tracetest.SpanRecorder, settings *telemetry.Options) {
	t.Helper()
	legacyRec = tracetest.NewSpanRecorder()
	genAIRec = tracetest.NewSpanRecorder()
	legacyTP := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(legacyRec))
	genAITP := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(genAIRec))
	t.Cleanup(func() {
		_ = legacyTP.Shutdown(context.Background())
		_ = genAITP.Shutdown(context.Background())
	})
	settings = &telemetry.Options{
		IsEnabled:     telemetry.Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: legacyTP.Tracer("h5-dual-legacy")}),
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: genAITP.Tracer("h5-dual-genai")}),
		},
	}
	return legacyRec, genAIRec, settings
}

// assertAllSpansEndedExactlyOnce is the core H5 regression check: every span
// an integration started must also have ended, exactly once. A leaked
// (started-but-never-ended) span — the H5 bug — makes len(started) >
// len(ended). tracetest.SpanRecorder appends one Ended() entry per End()
// call, so a double-End (the OTHER possible symptom of resolving the wrong
// integration's span) would also surface as a mismatch here in the unlikely
// case it ended a span this integration didn't start.
func assertAllSpansEndedExactlyOnce(t *testing.T, label string, rec *tracetest.SpanRecorder) {
	t.Helper()
	started := rec.Started()
	ended := rec.Ended()
	if len(started) == 0 {
		t.Fatalf("%s: expected at least one span to have been started — the integration was not dispatched to at all", label)
	}
	if len(started) != len(ended) {
		t.Fatalf("%s: started spans = %d %v, ended spans = %d %v; every started span must end exactly once (H5 leak)",
			label, len(started), namedSpanNames(started), len(ended), namedSpanNames(ended))
	}
}

// namedSpan is satisfied by both sdktrace.ReadWriteSpan (tracetest.SpanRecorder's
// Started() view) and sdktrace.ReadOnlySpan (its Ended() view — ReadWriteSpan
// embeds ReadOnlySpan), so one helper covers both.
type namedSpan interface{ Name() string }

func namedSpanNames[T namedSpan](spans []T) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	return namedSpanNames(spans)
}

// isEnded reports whether span has a non-zero EndTime (i.e. End() was
// called on it).
func isEnded(span sdktrace.ReadOnlySpan) bool {
	if span == nil {
		return false
	}
	return !span.EndTime().IsZero()
}

// assertRootSpanErrorStatus finds the root span by exact name and asserts
// its status code (or that it has one for abort, without asserting Error
// specifically — TS onAbort records no error status).
func assertRootSpanEnded(t *testing.T, label string, rec *tracetest.SpanRecorder, rootName string, wantErrorStatus bool) {
	t.Helper()
	span := findSpanByName(rec.Ended(), rootName)
	if span == nil {
		t.Fatalf("%s: expected a %q root span among ended spans, got %v", label, rootName, spanNames(rec.Ended()))
	}
	if !isEnded(span) {
		t.Fatalf("%s: root span %q was recorded but never ended", label, rootName)
	}
	gotError := span.Status().Code.String() == "Error"
	if gotError != wantErrorStatus {
		t.Fatalf("%s: root span %q error status = %v, want %v", label, rootName, gotError, wantErrorStatus)
	}
}

// --- generateText ---------------------------------------------------------

func TestDualIntegration_GenerateText_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateText", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

func TestDualIntegration_GenerateText_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, wantErr
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err == nil {
		t.Fatal("expected GenerateText to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateText", true)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", true)
}

func TestDualIntegration_GenerateText_Abort(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, context.Canceled
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err == nil {
		t.Fatal("expected GenerateText to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	// Abort records no error status (mirrors TS onAbort's plain span.end()).
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateText", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

// --- streamText -------------------------------------------------------------

func TestDualIntegration_StreamText_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.streamText", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

func TestDualIntegration_StreamText_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(wantErr), nil
		},
	}

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err != nil {
		t.Fatalf("StreamText() constructor error = %v", err)
	}
	if _, err := stream.ReadAll(); err == nil {
		t.Fatal("expected ReadAll to surface the provider error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.streamText", true)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", true)
}

func TestDualIntegration_StreamText_Abort(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStreamWithError(context.Canceled), nil
		},
	}

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hello",
		Telemetry: settings,
	})
	if err != nil {
		t.Fatalf("StreamText() constructor error = %v", err)
	}
	if _, err := stream.ReadAll(); err == nil {
		t.Fatal("expected ReadAll to surface the cancellation")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.streamText", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

// --- generateObject / streamObject -----------------------------------------

func dualIntegrationTestSchema() schema.Schema {
	return schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	})
}

func TestDualIntegration_GenerateObject_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"name":"Jane"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                dualIntegrationTestSchema(),
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateObject", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

func TestDualIntegration_GenerateObject_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, wantErr
		},
	}

	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                dualIntegrationTestSchema(),
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected GenerateObject to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateObject", true)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", true)
}

func TestDualIntegration_StreamObject_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"name":"Jane"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                dualIntegrationTestSchema(),
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("StreamObject() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.streamObject", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)
}

func TestDualIntegration_StreamObject_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockLanguageModel{
		ProviderName:      "test-provider",
		ModelName:         "test-model",
		StructuredSupport: true,
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, wantErr
		},
	}

	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:                 model,
		Prompt:                "Generate a person",
		Schema:                dualIntegrationTestSchema(),
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected StreamObject to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.streamObject", true)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", true)
}

// --- embed ------------------------------------------------------------------

func TestDualIntegration_Embed_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &mockEmbeddingModel{}

	_, err := Embed(context.Background(), EmbedOptions{
		Model:                 model,
		Input:                 "hello",
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.embed", false)
	assertRootSpanEnded(t, "genai", genAIRec, "embeddings test-embedding-model", false)
}

func TestDualIntegration_Embed_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-embedding-model",
		DoEmbedFunc: func(_ context.Context, _ string, _ *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return nil, wantErr
		},
	}

	_, err := Embed(context.Background(), EmbedOptions{
		Model:                 model,
		Input:                 "hello",
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected Embed to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.embed", true)
	assertRootSpanEnded(t, "genai", genAIRec, "embeddings test-embedding-model", true)
}

// --- embedMany ----------------------------------------------------------------

// TestDualIntegration_EmbedMany_Success exercises the batching path in
// pkg/ai/embed_many_batching.go, which opens a nested "doEmbed" span per
// batch (keyed by its own embedCallId, not the top-level callId) under the
// "ai.embedMany" root — the embedSpans map case in legacyCallState/
// genAICallState (H5), distinct from Embed's single top-level doEmbed span.
func TestDualIntegration_EmbedMany_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-embedding-model",
	}

	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:                 model,
		Inputs:                []string{"hello", "world"},
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("EmbedMany() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.embedMany", false)
	assertRootSpanEnded(t, "genai", genAIRec, "embeddings test-embedding-model", false)
}

// TestDualIntegration_EmbedMany_Error covers the provider-error path through
// embedManyCalls: OnEmbedStart opens the nested doEmbed span, DoEmbedMany
// fails before OnEmbedEnd can close it, and FireOnError must close both the
// leaked nested doEmbed span (via the embedSpans defensive cleanup, H5) and
// the root span for both integrations.
func TestDualIntegration_EmbedMany_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-embedding-model",
		DoEmbedManyFunc: func(_ context.Context, _ []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			return nil, wantErr
		},
	}

	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:                 model,
		Inputs:                []string{"hello", "world"},
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected EmbedMany to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.embedMany", true)
	assertRootSpanEnded(t, "genai", genAIRec, "embeddings test-embedding-model", true)
}

// --- rerank -----------------------------------------------------------------

func TestDualIntegration_Rerank_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(_ context.Context, _ *provider.RerankOptions) (*types.RerankResult, error) {
			return &types.RerankResult{
				Ranking: []types.RerankItem{
					{Index: 0, RelevanceScore: 0.9},
					{Index: 1, RelevanceScore: 0.1},
				},
				Response: types.RerankResponse{ModelID: "mock-reranking"},
			}, nil
		},
	}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:                 model,
		Documents:             []string{"doc1", "doc2"},
		Query:                 "search query",
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.rerank", false)
	assertRootSpanEnded(t, "genai", genAIRec, "rerank mock-reranking", false)
}

func TestDualIntegration_Rerank_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &testutil.MockRerankingModel{
		DoRerankFunc: func(_ context.Context, _ *provider.RerankOptions) (*types.RerankResult, error) {
			return nil, wantErr
		},
	}

	_, err := Rerank(context.Background(), RerankOptions{
		Model:                 model,
		Documents:             []string{"doc1", "doc2"},
		Query:                 "search query",
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected Rerank to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.rerank", true)
	assertRootSpanEnded(t, "genai", genAIRec, "rerank mock-reranking", true)
}

// --- evaluate ----------------------------------------------------------------

func TestDualIntegration_Evaluate_Success(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	questions, model := evaluateTelemetryFixture()

	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:                 model,
		State:                 "hello",
		Questions:             questions,
		ExperimentalTelemetry: settings,
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.evaluate", false)
	assertRootSpanEnded(t, "genai", genAIRec, "evaluate test-model", false)
}

func TestDualIntegration_Evaluate_Error(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	wantErr := errors.New("provider boom")
	model := &mockEvaluationModel{
		providerName: "test-provider",
		modelID:      "test-model",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return nil, wantErr
		},
	}
	questions := map[string]provider.EvaluationQuestion{
		"q1": {Type: "boolean", Instructions: "Is it correct?"},
	}

	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:                 model,
		State:                 "hello",
		Questions:             questions,
		ExperimentalTelemetry: settings,
	})
	if err == nil {
		t.Fatal("expected ExperimentalEvaluate to return an error")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.evaluate", true)
	assertRootSpanEnded(t, "genai", genAIRec, "evaluate test-model", true)
}

// --- tool call ---------------------------------------------------------------

// TestDualIntegration_ToolCall covers a locally-executed tool call within
// GenerateText, asserting that both integrations get their own
// execute_tool/ai.toolCall span (Legacy: "ai.toolCall"; GenAI: "execute_tool
// <name>") and that every span either integration started — root, step, and
// tool — ends exactly once.
func TestDualIntegration_ToolCall(t *testing.T) {
	legacyRec, genAIRec, settings := dualIntegrationSettings(t)

	toolCalled := false
	tools := []types.Tool{
		{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters:  map[string]interface{}{"type": "object"},
			Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				toolCalled = true
				return map[string]interface{}{"temperature": 72}, nil
			},
		},
	}

	callCount := 0
	model := &testutil.MockLanguageModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		ToolSupport:  true,
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if callCount == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{"location": "NYC"}},
					},
				}, nil
			}
			return &types.GenerateResult{Text: "72 degrees", FinishReason: types.FinishReasonStop}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     model,
		Prompt:    "What's the weather in NYC?",
		Tools:     tools,
		StopWhen:  []StopCondition{StepCountIs(5)},
		Telemetry: settings,
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if !toolCalled {
		t.Fatal("expected the tool to be called")
	}

	assertAllSpansEndedExactlyOnce(t, "legacy", legacyRec)
	assertAllSpansEndedExactlyOnce(t, "genai", genAIRec)
	assertRootSpanEnded(t, "legacy", legacyRec, "ai.generateText", false)
	assertRootSpanEnded(t, "genai", genAIRec, "invoke_agent test-model", false)

	legacyToolSpan := findSpanByName(legacyRec.Ended(), "ai.toolCall")
	if legacyToolSpan == nil {
		t.Fatalf("legacy: expected an 'ai.toolCall' span, got %v", spanNames(legacyRec.Ended()))
	}
	if !isEnded(legacyToolSpan) {
		t.Fatal("legacy: expected the ai.toolCall span to have ended")
	}

	genAIToolSpan := findSpanByName(genAIRec.Ended(), "execute_tool get_weather")
	if genAIToolSpan == nil {
		t.Fatalf("genai: expected an 'execute_tool get_weather' span, got %v", spanNames(genAIRec.Ended()))
	}
	if !isEnded(genAIToolSpan) {
		t.Fatal("genai: expected the execute_tool span to have ended")
	}
}
