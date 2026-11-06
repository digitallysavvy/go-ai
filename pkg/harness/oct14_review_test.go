package harness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// recordingIntegration captures the terminal telemetry events a turn emits.
type recordingIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu      sync.Mutex
	startID string
	errors  []telemetry.TelemetryErrorEvent
	aborts  []telemetry.TelemetryAbortEvent
}

func (r *recordingIntegration) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.startID = e.CallID
	r.mu.Unlock()
	return ctx
}

func (r *recordingIntegration) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	r.errors = append(r.errors, e)
	r.mu.Unlock()
}

func (r *recordingIntegration) OnAbort(_ context.Context, e telemetry.TelemetryAbortEvent) {
	r.mu.Lock()
	r.aborts = append(r.aborts, e)
	r.mu.Unlock()
}

func runFailingTurn(t *testing.T, ctx context.Context, rec *recordingIntegration, errText string) {
	t.Helper()
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}, &ErrorPart{Error: errText}}
		},
	})
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{rec},
		},
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	result, err := a.Stream(ctx, agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "hi", HarnessSession: session},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if result.Err() == nil {
		t.Fatal("Err() = nil, want a failure")
	}
}

// TS #21947: a failed turn dispatches onError with the turn's callId, and a
// caller-aborted turn dispatches onAbort (callId, steps, reason) and never
// onError.
func TestAgent_TelemetryErrorEventCarriesCallID(t *testing.T) {
	rec := &recordingIntegration{}
	runFailingTurn(t, context.Background(), rec, "boom")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errors) != 1 || len(rec.aborts) != 0 {
		t.Fatalf("errors=%d aborts=%d, want 1/0", len(rec.errors), len(rec.aborts))
	}
	if rec.errors[0].CallID == "" || rec.errors[0].CallID != rec.startID {
		t.Fatalf("error CallID = %q, want the start event's %q", rec.errors[0].CallID, rec.startID)
	}
	if rec.errors[0].Error == nil {
		t.Fatal("error event has nil Error")
	}
}

func TestAgent_TelemetryAbortEventCarriesCallIDStepsReason(t *testing.T) {
	rec := &recordingIntegration{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runFailingTurn(t, ctx, rec, "AbortError: This operation was aborted")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.aborts) != 1 || len(rec.errors) != 0 {
		t.Fatalf("aborts=%d errors=%d, want 1/0", len(rec.aborts), len(rec.errors))
	}
	ev := rec.aborts[0]
	if ev.CallID == "" || ev.CallID != rec.startID {
		t.Fatalf("abort CallID = %q, want %q", ev.CallID, rec.startID)
	}
	if ev.Reason == nil {
		t.Fatal("abort Reason is nil")
	}
	if len(ev.Steps) != 0 {
		t.Fatalf("abort Steps = %d, want 0 (no step completed)", len(ev.Steps))
	}
}

// A deferred pause must stay bounded by ctx: if the runtime requests an
// approval but never sends a finish-step, finish, or closes its stream, the
// turn ends with the caller's cancellation instead of hanging.
func TestRunPrompt_DeferredPauseBoundedByContext(t *testing.T) {
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`})
			// No finish-step, no finish, stream stays open.
		},
		nil,
	)
	tools := map[string]types.Tool{"weather": weatherTool()}
	ctx, cancel := context.WithCancel(context.Background())
	out := runPrompt(ctx, runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
	})
	time.AfterFunc(100*time.Millisecond, cancel)

	finished := make(chan struct{})
	go func() {
		stream := out.Result.Stream()
		for {
			if _, err := stream.Next(); err != nil {
				break
			}
		}
		<-out.Done
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not end after ctx cancellation while waiting for a finish-step")
	}
}
