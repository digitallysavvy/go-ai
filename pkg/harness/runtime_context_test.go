package harness

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// captureEndIntegration is a minimal telemetry.TelemetryIntegration that
// only records the OnEnd event, delegating every other method to the
// embedded no-op. Mirrors the bare `{ onEnd: event => {...} }` integration
// object TS's harness-agent.test.ts registers directly.
type captureEndIntegration struct {
	telemetry.NoopTelemetryIntegration
	onEnd func(telemetry.TelemetryFinishEvent)
}

func (c captureEndIntegration) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.onEnd(e)
}

func finishStepParts() []StreamPart {
	return []StreamPart{
		&StreamStartPart{},
		&TextStartPart{ID: "t1"},
		&TextDeltaPart{ID: "t1", Delta: "ok"},
		&TextEndPart{ID: "t1"},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
	}
}

// TestAgent_RuntimeContext_ForwardedThroughEveryEntryPoint ports TS
// harness-agent.test.ts "forwards configured runtime context through every
// public turn entry point" (TS #21597 / d99d6dcd75). Before this fix,
// HarnessAgent.generate/stream/continueGenerate/continueStream always
// substituted an empty object for the configured
// HarnessAgentSettings.runtimeContext; Go's startTurn had the same bug,
// forwarding only a per-call agent.AgentGenerateOptions.RuntimeContext and
// silently dropping AgentSettings.RuntimeContext whenever a call didn't set
// one of its own. Exercises all four entry points, and asserts lifecycle
// callbacks see the full configured value while telemetry sees only the
// Telemetry.IncludeRuntimeContext-allowlisted subset.
func TestAgent_RuntimeContext_ForwardedThroughEveryEntryPoint(t *testing.T) {
	runtimeContext := map[string]interface{}{"conversationId": "conversation-1", "secret": "s3cr3t"}
	var lifecycleContexts []interface{}
	var telemetryContexts []map[string]interface{}

	mock := newMockHarness(mockHarnessOptions{
		script:         func(func(string, interface{})) []StreamPart { return finishStepParts() },
		continueScript: func(func(string, interface{})) []StreamPart { return finishStepParts() },
	})

	a, err := NewAgent(AgentSettings{
		Harness:        mock.harness,
		RuntimeContext: runtimeContext,
		Callbacks: Callbacks{
			OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
				lifecycleContexts = append(lifecycleContexts, e.RuntimeContext)
			},
		},
		Telemetry: &telemetry.Options{
			IsEnabled:             telemetry.Bool(true),
			IncludeRuntimeContext: map[string]bool{"conversationId": true},
			Integrations: []telemetry.TelemetryIntegration{captureEndIntegration{onEnd: func(e telemetry.TelemetryFinishEvent) {
				telemetryContexts = append(telemetryContexts, e.RuntimeContext)
			}}},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	ctx := context.Background()

	genSession, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession (generate): %v", err)
	}
	if _, err := a.Generate(ctx, agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: genSession}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	streamSession, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession (stream): %v", err)
	}
	streamed, err := a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: streamSession}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if err := streamed.Err(); err != nil {
		t.Fatalf("streamed.Err() = %v", err)
	}

	continueFrom1, err := NewContinueTurnState(mock.harness.HarnessID(), map[string]any{})
	if err != nil {
		t.Fatalf("NewContinueTurnState: %v", err)
	}
	continueGenSession, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox(), ContinueFrom: continueFrom1})
	if err != nil {
		t.Fatalf("CreateSession (continueGenerate): %v", err)
	}
	if _, err := a.ContinueGenerate(ctx, agent.AgentGenerateOptions{HarnessSession: continueGenSession}, nil, nil); err != nil {
		t.Fatalf("ContinueGenerate: %v", err)
	}

	continueFrom2, err := NewContinueTurnState(mock.harness.HarnessID(), map[string]any{})
	if err != nil {
		t.Fatalf("NewContinueTurnState: %v", err)
	}
	continueStreamSession, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox(), ContinueFrom: continueFrom2})
	if err != nil {
		t.Fatalf("CreateSession (continueStream): %v", err)
	}
	continuedStream, err := a.ContinueStream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{HarnessSession: continueStreamSession}}, nil, nil)
	if err != nil {
		t.Fatalf("ContinueStream: %v", err)
	}
	if err := continuedStream.Err(); err != nil {
		t.Fatalf("continuedStream.Err() = %v", err)
	}

	if len(lifecycleContexts) != 4 {
		t.Fatalf("OnStepEnd fired %d times, want 4", len(lifecycleContexts))
	}
	for i, got := range lifecycleContexts {
		if !reflect.DeepEqual(got, runtimeContext) {
			t.Errorf("lifecycleContexts[%d] = %#v, want %#v", i, got, runtimeContext)
		}
	}

	wantFiltered := map[string]interface{}{"conversationId": "conversation-1"}
	if len(telemetryContexts) != 4 {
		t.Fatalf("telemetry OnEnd fired %d times, want 4", len(telemetryContexts))
	}
	for i, got := range telemetryContexts {
		if !reflect.DeepEqual(got, wantFiltered) {
			t.Errorf("telemetryContexts[%d] = %#v, want %#v (secret must be filtered out)", i, got, wantFiltered)
		}
	}
}

// TestAgent_RuntimeContext_PrepareCallOverrides ports the override half of
// TS "uses prepared runtime context for each prompt turn and filters
// telemetry": PrepareCall receives the agent-configured runtime context as
// input and may replace it for the turn; lifecycle callbacks observe the
// replacement, not the configured default.
func TestAgent_RuntimeContext_PrepareCallOverrides(t *testing.T) {
	configured := map[string]interface{}{"requestId": "default", "secret": "configured"}
	overridden := map[string]interface{}{"requestId": "req-1", "secret": "private"}
	var preparedInput interface{}
	var lifecycleContext interface{}

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart { return finishStepParts() },
	})
	a, err := NewAgent(AgentSettings{
		Harness:        mock.harness,
		RuntimeContext: configured,
		PrepareCall: func(_ context.Context, opts PrepareCallOptions) (PrepareCallResult, error) {
			preparedInput = opts.RuntimeContext
			return PrepareCallResult{HasRuntimeContext: true, RuntimeContext: overridden}, nil
		},
		Callbacks: Callbacks{
			OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) { lifecycleContext = e.RuntimeContext },
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	ctx := context.Background()
	session, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := a.Generate(ctx, agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !reflect.DeepEqual(preparedInput, configured) {
		t.Errorf("PrepareCallOptions.RuntimeContext = %#v, want the configured default %#v", preparedInput, configured)
	}
	if !reflect.DeepEqual(lifecycleContext, overridden) {
		t.Errorf("OnStepEnd RuntimeContext = %#v, want PrepareCall's override %#v", lifecycleContext, overridden)
	}
}

// TestAgent_RuntimeContext_PrepareCallClears ports the clearing half of TS
// "defaults to empty runtime context when prepareCall clears it": a
// PrepareCall that explicitly clears the turn's runtime context (Go's
// `HasRuntimeContext: true, RuntimeContext: nil`, mirroring TS's
// `runtimeContext: undefined`) must not fall back to the agent's configured
// default.
func TestAgent_RuntimeContext_PrepareCallClears(t *testing.T) {
	var lifecycleContext interface{}
	fired := false

	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart { return finishStepParts() },
	})
	a, err := NewAgent(AgentSettings{
		Harness:        mock.harness,
		RuntimeContext: map[string]interface{}{"requestId": "configured"},
		PrepareCall: func(context.Context, PrepareCallOptions) (PrepareCallResult, error) {
			return PrepareCallResult{HasRuntimeContext: true, RuntimeContext: nil}, nil
		},
		Callbacks: Callbacks{
			OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
				lifecycleContext = e.RuntimeContext
				fired = true
			},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	ctx := context.Background()
	session, err := a.CreateSession(ctx, CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := a.Generate(ctx, agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !fired {
		t.Fatal("OnStepEnd never fired")
	}
	if lifecycleContext != nil {
		t.Errorf("OnStepEnd RuntimeContext = %#v, want nil (cleared by PrepareCall, not the configured default)", lifecycleContext)
	}
}

// TestAgent_RuntimeContext_ContinueFromRebind ports TS's "rebound context" /
// "constructor fallback" cases of "uses $name after recreating a suspended
// turn...": CreateSessionOptions.RuntimeContext rebinds a resumed session's
// runtime context, taking precedence over the agent's configured default;
// omitting it falls back to the configured default instead.
func TestAgent_RuntimeContext_ContinueFromRebind(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rebind bool
		want   interface{}
	}{
		{"rebound context", true, map[string]interface{}{"requestId": "rebound"}},
		{"constructor fallback", false, map[string]interface{}{"requestId": "default"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lifecycleContext interface{}
			mock := newMockHarness(mockHarnessOptions{
				continueScript: func(func(string, interface{})) []StreamPart { return finishStepParts() },
			})
			a, err := NewAgent(AgentSettings{
				Harness:        mock.harness,
				RuntimeContext: map[string]interface{}{"requestId": "default"},
				Callbacks: Callbacks{
					OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) { lifecycleContext = e.RuntimeContext },
				},
			})
			if err != nil {
				t.Fatalf("NewAgent: %v", err)
			}
			ctx := context.Background()
			continueFrom, err := NewContinueTurnState(mock.harness.HarnessID(), map[string]any{})
			if err != nil {
				t.Fatalf("NewContinueTurnState: %v", err)
			}
			opts := CreateSessionOptions{SandboxSession: testSandbox(), ContinueFrom: continueFrom}
			if tc.rebind {
				opts.RuntimeContext = map[string]interface{}{"requestId": "rebound"}
			}
			session, err := a.CreateSession(ctx, opts)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			if _, err := a.ContinueGenerate(ctx, agent.AgentGenerateOptions{HarnessSession: session}, nil, nil); err != nil {
				t.Fatalf("ContinueGenerate: %v", err)
			}
			if !reflect.DeepEqual(lifecycleContext, tc.want) {
				t.Errorf("OnStepEnd RuntimeContext = %#v, want %#v", lifecycleContext, tc.want)
			}
		})
	}
}
