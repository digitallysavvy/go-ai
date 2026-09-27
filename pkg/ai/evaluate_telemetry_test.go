package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// This file tests ExperimentalEvaluate's telemetry *dispatch* — that it
// fires the restricted evaluate-only events (experimental_onEvaluateStart/
// End, experimental_onEvaluationModelCallStart/End, onError) rather than
// creating OTel spans itself (P1-7b G4: core pkg/ai never creates spans).
// The resulting span shapes and attribute gating are covered in
// pkg/telemetry (TestLegacyOpenTelemetryEvaluateSpans and friends).

func evaluateTelemetryFixture() (map[string]provider.EvaluationQuestion, *mockEvaluationModel) {
	prob := 0.9
	model := &mockEvaluationModel{
		providerName: "test-provider",
		modelID:      "test-model",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			inputTokens, outputTokens := 3, 7
			return &provider.EvaluationResult{
				Answers:          map[string]provider.EvaluationAnswer{"q1": {Type: "boolean", Probability: &prob}},
				Usage:            &provider.EvaluationUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
				ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"key": "value"}},
			}, nil
		},
	}
	questions := map[string]provider.EvaluationQuestion{
		"q1": {Type: "boolean", Instructions: "Is it correct?"},
	}
	return questions, model
}

// TestExperimentalEvaluate_NoSpanWithoutIntegration verifies that when no
// registered integration implements the evaluate-specific handlers (the
// zero-value / Noop case), ExperimentalEvaluate creates no OTel span at
// all — evaluate.go itself never touches the tracer.
func TestExperimentalEvaluate_NoSpanWithoutIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	questions, model := evaluateTelemetryFixture()

	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:     model,
		State:     "hello",
		Questions: questions,
		ExperimentalTelemetry: &telemetry.Settings{
			IsEnabled: telemetry.Bool(true),
			// A per-call integration list containing only Noop, so this test
			// is independent of whatever is globally registered.
			Integrations: []telemetry.TelemetryIntegration{telemetry.NoopTelemetryIntegration{}},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}

	if spans := rec.Ended(); len(spans) != 0 {
		names := make([]string, len(spans))
		for i, s := range spans {
			names[i] = s.Name()
		}
		t.Fatalf("expected no spans without a registered evaluate integration, got %v", names)
	}
}

// TestExperimentalEvaluate_ExactSpansWithIntegration verifies that
// registering a LegacyOpenTelemetry integration (per-call) produces exactly
// the two expected spans — the root "ai.evaluate" span and the nested
// "ai.evaluate.doEvaluate" span — no more, no less.
func TestExperimentalEvaluate_ExactSpansWithIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("evaluate-dispatch-test")

	questions, model := evaluateTelemetryFixture()

	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:     model,
		State:     "hello",
		Questions: questions,
		ExperimentalTelemetry: &telemetry.Settings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations: []telemetry.TelemetryIntegration{
				telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer}),
			},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}

	spans := rec.Ended()
	if len(spans) != 2 {
		names := make([]string, len(spans))
		for i, s := range spans {
			names[i] = s.Name()
		}
		t.Fatalf("expected exactly 2 spans (root + nested), got %d: %v", len(spans), names)
	}
	var haveRoot, haveNested bool
	for _, s := range spans {
		switch s.Name() {
		case "ai.evaluate":
			haveRoot = true
		case "ai.evaluate.doEvaluate":
			haveNested = true
		}
	}
	if !haveRoot {
		t.Error("missing root 'ai.evaluate' span")
	}
	if !haveNested {
		t.Error("missing nested 'ai.evaluate.doEvaluate' span")
	}
}

// mockEvaluateDispatchIntegration records every restricted evaluate event it
// receives, plus whether the generic OnStart/OnEnd/OnFinish were ever
// called — TS's evaluate.test.ts explicitly asserts the generic onStart/
// onEnd are NOT invoked for evaluate (restricted-telemetry-dispatcher.ts
// routes them to experimental_onEvaluateStart/End instead).
type mockEvaluateDispatchIntegration struct {
	telemetry.NoopTelemetryIntegration

	mu sync.Mutex

	genericOnStartCalled bool
	genericOnEndCalled   bool
	genericOnFinishCalls int

	evaluateStart          *telemetry.EvaluateStartEvent
	evaluateEnd            *telemetry.EvaluateEndEvent
	evaluationModelStart   *telemetry.EvaluationModelCallStartEvent
	evaluationModelEnd     *telemetry.EvaluationModelCallEndEvent
	errorEvent             *telemetry.TelemetryErrorEvent
	evaluationModelEndSeen bool
}

func (m *mockEvaluateDispatchIntegration) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	m.mu.Lock()
	m.genericOnStartCalled = true
	m.mu.Unlock()
	return ctx
}

func (m *mockEvaluateDispatchIntegration) OnEnd(_ context.Context, _ telemetry.TelemetryFinishEvent) {
	m.mu.Lock()
	m.genericOnEndCalled = true
	m.mu.Unlock()
}

func (m *mockEvaluateDispatchIntegration) OnFinish(_ context.Context, _ telemetry.TelemetryFinishEvent) {
	m.mu.Lock()
	m.genericOnFinishCalls++
	m.mu.Unlock()
}

func (m *mockEvaluateDispatchIntegration) OnEvaluateStart(ctx context.Context, e telemetry.EvaluateStartEvent) context.Context {
	m.mu.Lock()
	m.evaluateStart = &e
	m.mu.Unlock()
	return ctx
}

func (m *mockEvaluateDispatchIntegration) OnEvaluateEnd(_ context.Context, e telemetry.EvaluateEndEvent) {
	m.mu.Lock()
	m.evaluateEnd = &e
	m.mu.Unlock()
}

func (m *mockEvaluateDispatchIntegration) OnEvaluationModelCallStart(_ context.Context, e telemetry.EvaluationModelCallStartEvent) {
	m.mu.Lock()
	m.evaluationModelStart = &e
	m.mu.Unlock()
}

func (m *mockEvaluateDispatchIntegration) OnEvaluationModelCallEnd(_ context.Context, e telemetry.EvaluationModelCallEndEvent) {
	m.mu.Lock()
	m.evaluationModelEnd = &e
	m.evaluationModelEndSeen = true
	m.mu.Unlock()
}

func (m *mockEvaluateDispatchIntegration) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	m.mu.Lock()
	m.errorEvent = &e
	m.mu.Unlock()
}

// TestExperimentalEvaluate_TelemetryDispatchShape mirrors TS's evaluate.test.ts
// "emits operation and model-call lifecycle events": the restricted
// dispatcher must route evaluate's start/end through
// experimental_onEvaluateStart/End (never the generic onStart/onEnd/
// onFinish), and experimental_onEvaluationModelCallStart/End must carry the
// same CallID plus the "ai.evaluate.doEvaluate" nested operationId.
func TestExperimentalEvaluate_TelemetryDispatchShape(t *testing.T) {
	questions, model := evaluateTelemetryFixture()
	mock := &mockEvaluateDispatchIntegration{}

	settings := &telemetry.Settings{
		IsEnabled:             telemetry.Bool(true),
		RecordInputs:          true,
		RecordOutputs:         true,
		FunctionID:            "evaluate-test",
		IncludeRuntimeContext: map[string]bool{"requestId": true},
		Integrations:          []telemetry.TelemetryIntegration{mock},
	}

	result, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:                 model,
		State:                 map[string]interface{}{"message": "refund"},
		Questions:             questions,
		ExperimentalTelemetry: settings,
		RuntimeContext:        map[string]interface{}{"requestId": "request-1", "secret": "hidden"},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.genericOnStartCalled || mock.genericOnEndCalled || mock.genericOnFinishCalls != 0 {
		t.Errorf("expected the generic OnStart/OnEnd/OnFinish to never be called for evaluate, got OnStart=%v OnEnd=%v OnFinish calls=%d",
			mock.genericOnStartCalled, mock.genericOnEndCalled, mock.genericOnFinishCalls)
	}

	if mock.evaluateStart == nil {
		t.Fatal("expected OnEvaluateStart to be called")
	}
	if mock.evaluateStart.OperationID != "ai.evaluate" {
		t.Errorf("evaluateStart.OperationID = %q", mock.evaluateStart.OperationID)
	}
	if mock.evaluateStart.ModelProvider != "test-provider" || mock.evaluateStart.ModelID != "test-model" {
		t.Errorf("evaluateStart provider/model = %q/%q", mock.evaluateStart.ModelProvider, mock.evaluateStart.ModelID)
	}
	if got := mock.evaluateStart.RuntimeContext; len(got) != 1 || got["requestId"] != "request-1" {
		t.Errorf("evaluateStart.RuntimeContext = %+v, want only requestId (secret filtered)", got)
	}
	callID := mock.evaluateStart.CallID
	if callID == "" {
		t.Fatal("expected a non-empty CallID")
	}

	if mock.evaluationModelStart == nil {
		t.Fatal("expected OnEvaluationModelCallStart to be called")
	}
	if mock.evaluationModelStart.CallID != callID {
		t.Errorf("evaluationModelStart.CallID = %q, want %q", mock.evaluationModelStart.CallID, callID)
	}
	if mock.evaluationModelStart.OperationID != "ai.evaluate.doEvaluate" {
		t.Errorf("evaluationModelStart.OperationID = %q", mock.evaluationModelStart.OperationID)
	}

	if !mock.evaluationModelEndSeen || mock.evaluationModelEnd == nil {
		t.Fatal("expected OnEvaluationModelCallEnd to be called")
	}
	if mock.evaluationModelEnd.CallID != callID {
		t.Errorf("evaluationModelEnd.CallID = %q, want %q", mock.evaluationModelEnd.CallID, callID)
	}
	if mock.evaluationModelEnd.Usage.InputTokens == nil || *mock.evaluationModelEnd.Usage.InputTokens != 3 {
		t.Errorf("evaluationModelEnd.Usage.InputTokens = %v", mock.evaluationModelEnd.Usage.InputTokens)
	}
	if mock.evaluationModelEnd.Usage.OutputTokens == nil || *mock.evaluationModelEnd.Usage.OutputTokens != 7 {
		t.Errorf("evaluationModelEnd.Usage.OutputTokens = %v", mock.evaluationModelEnd.Usage.OutputTokens)
	}
	if mock.evaluationModelEnd.ProviderMetadata == nil {
		t.Error("expected evaluationModelEnd.ProviderMetadata to be forwarded")
	}

	if mock.evaluateEnd == nil {
		t.Fatal("expected OnEvaluateEnd to be called")
	}
	if mock.evaluateEnd.CallID != callID {
		t.Errorf("evaluateEnd.CallID = %q, want %q", mock.evaluateEnd.CallID, callID)
	}
	if mock.evaluateEnd.Answers == nil {
		t.Error("expected evaluateEnd.Answers to be populated")
	}

	if mock.errorEvent != nil {
		t.Errorf("expected no error event, got %+v", mock.errorEvent)
	}

	if result == nil || len(result.Answers) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestExperimentalEvaluate_TelemetryDispatchShape_Error mirrors TS's
// evaluate.test.ts "emits an error event when evaluation fails": OnError
// must receive the same CallID that was used for OnEvaluateStart, and
// OnEvaluationModelCallEnd must never be called on this path.
func TestExperimentalEvaluate_TelemetryDispatchShape_Error(t *testing.T) {
	questions, _ := evaluateTelemetryFixture()
	wantErr := &mockEvalError{msg: "evaluation failed"}
	model := &mockEvaluationModel{
		providerName: "test-provider",
		modelID:      "test-model",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return nil, wantErr
		},
	}
	mock := &mockEvaluateDispatchIntegration{}

	zero := 0
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:      model,
		State:      "hello",
		Questions:  questions,
		MaxRetries: &zero,
		ExperimentalTelemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{mock},
		},
	})
	if err == nil {
		t.Fatal("expected ExperimentalEvaluate to return an error")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.evaluateStart == nil {
		t.Fatal("expected OnEvaluateStart to be called")
	}
	callID := mock.evaluateStart.CallID

	if mock.errorEvent == nil {
		t.Fatal("expected OnError to be called")
	}
	if mock.errorEvent.CallID != callID {
		t.Errorf("errorEvent.CallID = %q, want %q", mock.errorEvent.CallID, callID)
	}
	if mock.errorEvent.Error != wantErr {
		t.Errorf("errorEvent.Error = %v, want %v", mock.errorEvent.Error, wantErr)
	}
	if mock.evaluationModelEndSeen {
		t.Error("expected OnEvaluationModelCallEnd to NOT be called on the error path")
	}
	if mock.evaluateEnd != nil {
		t.Error("expected OnEvaluateEnd to NOT be called on the error path")
	}
}

type mockEvalError struct{ msg string }

func (e *mockEvalError) Error() string { return e.msg }
