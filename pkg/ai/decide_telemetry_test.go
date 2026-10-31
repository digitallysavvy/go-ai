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

// This file tests ExperimentalDecide's telemetry *dispatch* — that it
// fires the restricted decide-only events (experimental_onDecideStart/
// End, experimental_onDecisionModelCallStart/End, onError) rather than
// creating OTel spans itself (P1-7b G4: core pkg/ai never creates spans).
// The resulting span shapes and attribute gating are covered in
// pkg/telemetry (TestLegacyOpenTelemetryDecideSpans and friends).

func decideTelemetryFixture() (map[string]provider.DecisionQuestion, *mockDecisionModel) {
	prob := 0.9
	model := &mockDecisionModel{
		providerName: "test-provider",
		modelID:      "test-model",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			inputTokens, outputTokens := 3, 7
			return &provider.DecisionResult{
				Answers:          map[string]provider.DecisionAnswer{"q1": {Type: "boolean", Probability: &prob}},
				Usage:            &provider.DecisionUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
				ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"key": "value"}},
			}, nil
		},
	}
	questions := map[string]provider.DecisionQuestion{
		"q1": {Type: "boolean", Instructions: "Is it correct?"},
	}
	return questions, model
}

// TestExperimentalDecide_NoSpanWithoutIntegration verifies that when no
// registered integration implements the decide-specific handlers (the
// zero-value / Noop case), ExperimentalDecide creates no OTel span at
// all — decide.go itself never touches the tracer.
func TestExperimentalDecide_NoSpanWithoutIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	questions, model := decideTelemetryFixture()

	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:     model,
		State:     "hello",
		Questions: questions,
		ExperimentalTelemetry: &telemetry.Options{
			IsEnabled: telemetry.Bool(true),
			// A per-call integration list containing only Noop, so this test
			// is independent of whatever is globally registered.
			Integrations: []telemetry.TelemetryIntegration{telemetry.NoopTelemetryIntegration{}},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}

	if spans := rec.Ended(); len(spans) != 0 {
		names := make([]string, len(spans))
		for i, s := range spans {
			names[i] = s.Name()
		}
		t.Fatalf("expected no spans without a registered decide integration, got %v", names)
	}
}

// TestExperimentalDecide_ExactSpansWithIntegration verifies that
// registering a LegacyOpenTelemetry integration (per-call) produces exactly
// the two expected spans — the root "ai.decide" span and the nested
// "ai.decide.doDecide" span — no more, no less.
func TestExperimentalDecide_ExactSpansWithIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("decide-dispatch-test")

	questions, model := decideTelemetryFixture()

	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:     model,
		State:     "hello",
		Questions: questions,
		ExperimentalTelemetry: &telemetry.Options{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  telemetry.Bool(true),
			RecordOutputs: telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{
				telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer}),
			},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
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
		case "ai.decide":
			haveRoot = true
		case "ai.decide.doDecide":
			haveNested = true
		}
	}
	if !haveRoot {
		t.Error("missing root 'ai.decide' span")
	}
	if !haveNested {
		t.Error("missing nested 'ai.decide.doDecide' span")
	}
}

// mockDecideDispatchIntegration records every restricted decide event it
// receives, plus whether the generic OnStart/OnEnd/OnFinish were ever
// called — TS's decide.test.ts explicitly asserts the generic onStart/
// onEnd are NOT invoked for decide (restricted-telemetry-dispatcher.ts
// routes them to experimental_onDecideStart/End instead).
type mockDecideDispatchIntegration struct {
	telemetry.NoopTelemetryIntegration

	mu sync.Mutex

	genericOnStartCalled bool
	genericOnEndCalled   bool
	genericOnFinishCalls int

	decideStart          *telemetry.DecideStartEvent
	decideEnd            *telemetry.DecideEndEvent
	decisionModelStart   *telemetry.DecisionModelCallStartEvent
	decisionModelEnd     *telemetry.DecisionModelCallEndEvent
	errorEvent           *telemetry.TelemetryErrorEvent
	decisionModelEndSeen bool
}

func (m *mockDecideDispatchIntegration) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	m.mu.Lock()
	m.genericOnStartCalled = true
	m.mu.Unlock()
	return ctx
}

func (m *mockDecideDispatchIntegration) OnEnd(_ context.Context, _ telemetry.TelemetryFinishEvent) {
	m.mu.Lock()
	m.genericOnEndCalled = true
	m.mu.Unlock()
}

func (m *mockDecideDispatchIntegration) OnFinish(_ context.Context, _ telemetry.TelemetryFinishEvent) {
	m.mu.Lock()
	m.genericOnFinishCalls++
	m.mu.Unlock()
}

func (m *mockDecideDispatchIntegration) OnDecideStart(ctx context.Context, e telemetry.DecideStartEvent) context.Context {
	m.mu.Lock()
	m.decideStart = &e
	m.mu.Unlock()
	return ctx
}

func (m *mockDecideDispatchIntegration) OnDecideEnd(_ context.Context, e telemetry.DecideEndEvent) {
	m.mu.Lock()
	m.decideEnd = &e
	m.mu.Unlock()
}

func (m *mockDecideDispatchIntegration) OnDecisionModelCallStart(_ context.Context, e telemetry.DecisionModelCallStartEvent) {
	m.mu.Lock()
	m.decisionModelStart = &e
	m.mu.Unlock()
}

func (m *mockDecideDispatchIntegration) OnDecisionModelCallEnd(_ context.Context, e telemetry.DecisionModelCallEndEvent) {
	m.mu.Lock()
	m.decisionModelEnd = &e
	m.decisionModelEndSeen = true
	m.mu.Unlock()
}

func (m *mockDecideDispatchIntegration) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	m.mu.Lock()
	m.errorEvent = &e
	m.mu.Unlock()
}

// TestExperimentalDecide_TelemetryDispatchShape mirrors TS's decide.test.ts
// "emits operation and model-call lifecycle events": the restricted
// dispatcher must route decide's start/end through
// experimental_onDecideStart/End (never the generic onStart/onEnd/
// onFinish), and experimental_onDecisionModelCallStart/End must carry the
// same CallID plus the "ai.decide.doDecide" nested operationId.
func TestExperimentalDecide_TelemetryDispatchShape(t *testing.T) {
	questions, model := decideTelemetryFixture()
	mock := &mockDecideDispatchIntegration{}

	settings := &telemetry.Options{
		IsEnabled:             telemetry.Bool(true),
		RecordInputs:          telemetry.Bool(true),
		RecordOutputs:         telemetry.Bool(true),
		FunctionID:            "decide-test",
		IncludeRuntimeContext: map[string]bool{"requestId": true},
		Integrations:          []telemetry.TelemetryIntegration{mock},
	}

	result, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:                 model,
		State:                 map[string]interface{}{"message": "refund"},
		Questions:             questions,
		ExperimentalTelemetry: settings,
		RuntimeContext:        map[string]interface{}{"requestId": "request-1", "secret": "hidden"},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.genericOnStartCalled || mock.genericOnEndCalled || mock.genericOnFinishCalls != 0 {
		t.Errorf("expected the generic OnStart/OnEnd/OnFinish to never be called for decide, got OnStart=%v OnEnd=%v OnFinish calls=%d",
			mock.genericOnStartCalled, mock.genericOnEndCalled, mock.genericOnFinishCalls)
	}

	if mock.decideStart == nil {
		t.Fatal("expected OnDecideStart to be called")
	}
	if mock.decideStart.OperationID != "ai.decide" {
		t.Errorf("decideStart.OperationID = %q", mock.decideStart.OperationID)
	}
	if mock.decideStart.ModelProvider != "test-provider" || mock.decideStart.ModelID != "test-model" {
		t.Errorf("decideStart provider/model = %q/%q", mock.decideStart.ModelProvider, mock.decideStart.ModelID)
	}
	if got := mock.decideStart.RuntimeContext; len(got) != 1 || got["requestId"] != "request-1" {
		t.Errorf("decideStart.RuntimeContext = %+v, want only requestId (secret filtered)", got)
	}
	callID := mock.decideStart.CallID
	if callID == "" {
		t.Fatal("expected a non-empty CallID")
	}

	if mock.decisionModelStart == nil {
		t.Fatal("expected OnDecisionModelCallStart to be called")
	}
	if mock.decisionModelStart.CallID != callID {
		t.Errorf("decisionModelStart.CallID = %q, want %q", mock.decisionModelStart.CallID, callID)
	}
	if mock.decisionModelStart.OperationID != "ai.decide.doDecide" {
		t.Errorf("decisionModelStart.OperationID = %q", mock.decisionModelStart.OperationID)
	}

	if !mock.decisionModelEndSeen || mock.decisionModelEnd == nil {
		t.Fatal("expected OnDecisionModelCallEnd to be called")
	}
	if mock.decisionModelEnd.CallID != callID {
		t.Errorf("decisionModelEnd.CallID = %q, want %q", mock.decisionModelEnd.CallID, callID)
	}
	if mock.decisionModelEnd.Usage.InputTokens == nil || *mock.decisionModelEnd.Usage.InputTokens != 3 {
		t.Errorf("decisionModelEnd.Usage.InputTokens = %v", mock.decisionModelEnd.Usage.InputTokens)
	}
	if mock.decisionModelEnd.Usage.OutputTokens == nil || *mock.decisionModelEnd.Usage.OutputTokens != 7 {
		t.Errorf("decisionModelEnd.Usage.OutputTokens = %v", mock.decisionModelEnd.Usage.OutputTokens)
	}
	if mock.decisionModelEnd.ProviderMetadata == nil {
		t.Error("expected decisionModelEnd.ProviderMetadata to be forwarded")
	}

	if mock.decideEnd == nil {
		t.Fatal("expected OnDecideEnd to be called")
	}
	if mock.decideEnd.CallID != callID {
		t.Errorf("decideEnd.CallID = %q, want %q", mock.decideEnd.CallID, callID)
	}
	if mock.decideEnd.Answers == nil {
		t.Error("expected decideEnd.Answers to be populated")
	}

	if mock.errorEvent != nil {
		t.Errorf("expected no error event, got %+v", mock.errorEvent)
	}

	if result == nil || len(result.Answers) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestExperimentalDecide_TelemetryDispatchShape_Error mirrors TS's
// decide.test.ts "emits an error event when decision fails": OnError
// must receive the same CallID that was used for OnDecideStart, and
// OnDecisionModelCallEnd must never be called on this path.
func TestExperimentalDecide_TelemetryDispatchShape_Error(t *testing.T) {
	questions, _ := decideTelemetryFixture()
	wantErr := &mockEvalError{msg: "decision failed"}
	model := &mockDecisionModel{
		providerName: "test-provider",
		modelID:      "test-model",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return nil, wantErr
		},
	}
	mock := &mockDecideDispatchIntegration{}

	zero := 0
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:      model,
		State:      "hello",
		Questions:  questions,
		MaxRetries: &zero,
		ExperimentalTelemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{mock},
		},
	})
	if err == nil {
		t.Fatal("expected ExperimentalDecide to return an error")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if mock.decideStart == nil {
		t.Fatal("expected OnDecideStart to be called")
	}
	callID := mock.decideStart.CallID

	if mock.errorEvent == nil {
		t.Fatal("expected OnError to be called")
	}
	if mock.errorEvent.CallID != callID {
		t.Errorf("errorEvent.CallID = %q, want %q", mock.errorEvent.CallID, callID)
	}
	if mock.errorEvent.Error != wantErr {
		t.Errorf("errorEvent.Error = %v, want %v", mock.errorEvent.Error, wantErr)
	}
	if mock.decisionModelEndSeen {
		t.Error("expected OnDecisionModelCallEnd to NOT be called on the error path")
	}
	if mock.decideEnd != nil {
		t.Error("expected OnDecideEnd to NOT be called on the error path")
	}
}

type mockEvalError struct{ msg string }

func (e *mockEvalError) Error() string { return e.msg }

// TestExperimentalDecide_TelemetryEndEventCarriesStartFields mirrors TS
// decide.ts, which notifies onEnd with `{ ...startEvent, ...decisionResult }`:
// the decide end event must carry the start event's state, questions,
// maxRetries and headers, not only the call/model identifiers.
func TestExperimentalDecide_TelemetryEndEventCarriesStartFields(t *testing.T) {
	questions, model := decideTelemetryFixture()
	mock := &mockDecideDispatchIntegration{}
	maxRetries := 1
	state := map[string]interface{}{"message": "refund"}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:      model,
		State:      state,
		Questions:  questions,
		MaxRetries: &maxRetries,
		Headers:    map[string]string{"x-test": "1"},
		ExperimentalTelemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{mock},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.decideEnd == nil {
		t.Fatal("expected OnDecideEnd to be called")
	}
	end := mock.decideEnd
	if got, ok := end.State.(map[string]interface{}); !ok || got["message"] != "refund" {
		t.Errorf("decideEnd.State = %#v, want the start state", end.State)
	}
	if got, ok := end.Questions.(map[string]provider.DecisionQuestion); !ok || len(got) != len(questions) {
		t.Errorf("decideEnd.Questions = %#v, want the start questions", end.Questions)
	}
	if end.MaxRetries != 1 || end.Headers["x-test"] != "1" {
		t.Errorf("decideEnd MaxRetries/Headers = %d/%v", end.MaxRetries, end.Headers)
	}
	if mock.decideStart == nil || end.MaxRetries != mock.decideStart.MaxRetries {
		t.Errorf("decideEnd.MaxRetries should equal the start event's")
	}
}
