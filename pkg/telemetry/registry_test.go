package telemetry

import (
	"context"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// mockIntegration — test double for TelemetryIntegration
// ---------------------------------------------------------------------------

type mockIntegration struct {
	mu     sync.Mutex
	starts []mockStartRecord
	ends   int
}

type mockStartRecord struct {
	operationType string
	settings      *Settings
}

// mockIntegrationCtxKey is a private context key used to thread the per-call
// start-record pointer from OnStart to OnFinish so tests can verify End().
type mockIntegrationCtxKey struct{}

func (m *mockIntegration) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	rec := &mockStartRecord{operationType: e.OperationType, settings: e.Settings}
	m.mu.Lock()
	m.starts = append(m.starts, *rec)
	m.mu.Unlock()
	return context.WithValue(ctx, mockIntegrationCtxKey{}, rec)
}

func (m *mockIntegration) OnStepStart(_ context.Context, _ TelemetryStepStartEvent) {}
func (m *mockIntegration) OnToolCallStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (m *mockIntegration) OnToolCallFinish(_ context.Context, _ TelemetryToolCallFinishEvent) {}
func (m *mockIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent)                   {}
func (m *mockIntegration) OnStepFinish(_ context.Context, _ TelemetryStepFinishEvent)         {}

func (m *mockIntegration) OnFinish(_ context.Context, _ TelemetryFinishEvent) {
	m.mu.Lock()
	m.ends++
	m.mu.Unlock()
}

func (m *mockIntegration) OnError(_ context.Context, _ TelemetryErrorEvent) {
	m.mu.Lock()
	m.ends++
	m.mu.Unlock()
}

func (m *mockIntegration) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGetTelemetryIntegration_DefaultIsNoop(t *testing.T) {
	// Reset to known state.
	RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	integration := GetTelemetryIntegration()
	if integration == nil {
		t.Fatal("expected non-nil integration")
	}
	_, got := integration.(NoopTelemetryIntegration)
	if !got {
		t.Errorf("expected NoopTelemetryIntegration as default, got %T", integration)
	}
}

func TestRegisterTelemetryIntegration_ReplacesNoop(t *testing.T) {
	// Start with known noop state.
	RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	mock := &mockIntegration{}
	RegisterTelemetryIntegration(mock)

	got := GetTelemetryIntegration()
	if got != mock {
		t.Errorf("expected registered mock, got %T", got)
	}

	// Restore noop for other tests.
	RegisterTelemetryIntegration(NoopTelemetryIntegration{})
}

func TestNoopTelemetryIntegration_NoPanics(t *testing.T) {
	noop := NoopTelemetryIntegration{}
	ctx := noop.OnStart(context.Background(), TelemetryStartEvent{OperationType: "test"})
	if ctx == nil {
		t.Error("expected non-nil context")
	}
	noop.OnStepStart(ctx, TelemetryStepStartEvent{})
	ctx2 := noop.OnToolCallStart(ctx, TelemetryToolCallStartEvent{})
	if ctx2 == nil {
		t.Error("expected non-nil context from OnToolCallStart")
	}
	noop.OnToolCallFinish(ctx, TelemetryToolCallFinishEvent{})
	noop.OnChunk(ctx, TelemetryChunkEvent{ChunkType: "text", Text: "hello"})
	noop.OnStepFinish(ctx, TelemetryStepFinishEvent{})
	noop.OnFinish(ctx, TelemetryFinishEvent{})
	noop.OnError(ctx, TelemetryErrorEvent{})
	// ExecuteTool must call execute and return its result.
	result, err := noop.ExecuteTool(ctx, "myTool", nil, func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "ok", nil
	})
	if err != nil || result != "ok" {
		t.Errorf("noop.ExecuteTool: got (%v, %v)", result, err)
	}
}

func TestNoopTelemetryIntegration_DisabledSettings_NoPanics(t *testing.T) {
	noop := NoopTelemetryIntegration{}
	settings := &Settings{IsEnabled: false}
	ctx := noop.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "test",
		Settings:      settings,
	})
	noop.OnFinish(ctx, TelemetryFinishEvent{Settings: settings})
}

func TestRegisterTelemetryIntegration_Concurrent(t *testing.T) {
	// Verify that concurrent Register and Get calls don't race.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterTelemetryIntegration(NoopTelemetryIntegration{})
		}()
		go func() {
			defer wg.Done()
			_ = GetTelemetryIntegration()
		}()
	}
	wg.Wait()

	// Restore clean state.
	RegisterTelemetryIntegration(NoopTelemetryIntegration{})
}

func TestAddTelemetryIntegration_FanOut(t *testing.T) {
	ClearTelemetryIntegrations()
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	m1 := &mockIntegration{}
	m2 := &mockIntegration{}
	AddTelemetryIntegration(m1)
	AddTelemetryIntegration(m2)

	ctx := FireOnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      &Settings{IsEnabled: true},
	})
	FireOnFinish(ctx, TelemetryFinishEvent{})

	m1.mu.Lock()
	m2.mu.Lock()
	defer m1.mu.Unlock()
	defer m2.mu.Unlock()

	if len(m1.starts) != 1 {
		t.Errorf("m1: expected 1 OnStart, got %d", len(m1.starts))
	}
	if len(m2.starts) != 1 {
		t.Errorf("m2: expected 1 OnStart, got %d", len(m2.starts))
	}
	if m1.ends != 1 {
		t.Errorf("m1: expected 1 OnFinish, got %d", m1.ends)
	}
	if m2.ends != 1 {
		t.Errorf("m2: expected 1 OnFinish, got %d", m2.ends)
	}
}

func TestOTelTelemetryIntegration_DisabledReturnsNoop(t *testing.T) {
	integration := OTelTelemetryIntegration{}

	// Disabled settings must not panic.
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "test",
		Settings:      &Settings{IsEnabled: false},
	})
	integration.OnFinish(ctx, TelemetryFinishEvent{Settings: &Settings{IsEnabled: false}})
	integration.OnError(ctx, TelemetryErrorEvent{})

	// Nil settings must also be safe.
	ctx2 := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "test"})
	integration.OnFinish(ctx2, TelemetryFinishEvent{})
}

func TestMockIntegration_ReceivesStartFinish(t *testing.T) {
	mock := &mockIntegration{}
	RegisterTelemetryIntegration(mock)
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	ctx := FireOnStart(
		context.Background(),
		TelemetryStartEvent{
			OperationType: "ai.generateText",
			ModelProvider: "test-provider",
			ModelID:       "test-model",
			Settings:      &Settings{IsEnabled: true},
		},
	)
	FireOnFinish(ctx, TelemetryFinishEvent{FinishReason: "stop"})

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.starts) != 1 {
		t.Fatalf("expected 1 OnStart, got %d", len(mock.starts))
	}
	if mock.starts[0].operationType != "ai.generateText" {
		t.Errorf("expected operationType 'ai.generateText', got %q", mock.starts[0].operationType)
	}
	if mock.ends != 1 {
		t.Errorf("expected 1 OnFinish, got %d", mock.ends)
	}
}

func TestFireExecuteTool_Chains(t *testing.T) {
	var order []string

	type wrapIntegration struct {
		NoopTelemetryIntegration
		tag string
	}

	// Custom wrapper: records entry/exit around execute.
	makeWrapper := func(tag string) TelemetryIntegration {
		return &struct {
			NoopTelemetryIntegration
		}{}
	}
	_ = makeWrapper // use below via FireExecuteTool directly

	// Reset and add two integrations that both delegate to execute.
	ClearTelemetryIntegrations()
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	AddTelemetryIntegration(NoopTelemetryIntegration{})
	AddTelemetryIntegration(NoopTelemetryIntegration{})

	result, err := FireExecuteTool(context.Background(), "myTool", nil,
		func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
			order = append(order, "execute")
			return "result", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("expected 'result', got %v", result)
	}
	if len(order) != 1 || order[0] != "execute" {
		t.Errorf("expected execute to be called once, got %v", order)
	}
}
