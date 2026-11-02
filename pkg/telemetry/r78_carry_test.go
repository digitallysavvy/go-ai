package telemetry

import (
	"context"
	"sort"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Round 78 carry (ruling d): TS names GenAI spans `${operationName}
// ${modelId}` with no empty-model guard, so an empty model ID (a harness turn
// without a model, or a model whose ModelID() is "") keeps the trailing space.
func TestR78Carry_EmptyModelIDSpanNamesKeepTrailingSpace(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ot := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai")})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "r78carry-gen", OperationType: "ai.generateText", Settings: settings})
	stepCtx := ot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "r78carry-gen", Settings: settings})
	ot.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: "r78carry-gen", Settings: settings})

	ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "r78carry-harness", OperationType: "ai.harness", Settings: settings})

	embedCtx := ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "r78carry-embed", OperationType: "ai.embed", Settings: settings})
	ot.OnEmbedStart(embedCtx, EmbeddingModelCallStartEvent{CallID: "r78carry-embed", EmbedCallID: "e1", OperationID: "ai.embed.doEmbed", Settings: settings, Values: []string{"a"}})

	rerankCtx := ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "r78carry-rerank", OperationType: "ai.rerank", Settings: settings})
	ot.OnRerankStart(rerankCtx, RerankingModelCallStartEvent{CallID: "r78carry-rerank", OperationID: "ai.rerank.doRerank", Settings: settings})

	decideCtx := ot.OnDecideStart(context.Background(), DecideStartEvent{CallID: "r78carry-decide", OperationID: "ai.decide", Settings: settings})
	ot.OnDecisionModelCallStart(decideCtx, DecisionModelCallStartEvent{CallID: "r78carry-decide", OperationID: "ai.decide.doGenerate", Settings: settings})

	var got []string
	for _, s := range rec.Started() {
		got = append(got, s.Name())
	}
	sort.Strings(got)
	want := []string{"ai.harness ", "chat ", "decide ", "decide ", "embeddings ", "embeddings ", "invoke_agent ", "rerank ", "rerank ", "step 1"}
	if len(got) != len(want) {
		t.Fatalf("span names = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("span names = %q, want %q", got, want)
		}
	}
}
