package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func stopFinishScript() []harness.StreamPart {
	return []harness.StreamPart{
		&harness.StreamStartPart{},
		&harness.TextStartPart{ID: "t1"},
		&harness.TextDeltaPart{ID: "t1", Delta: "x"},
		&harness.TextEndPart{ID: "t1"},
		&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
		&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
	}
}

// Ports TS "provider errors detach the session and return fresh resume state".
func TestRunHarnessAgentTimeSlice_ProviderErrorDetachesSession(t *testing.T) {
	fake := &fakeSession{opts: fakeHarnessOptions{script: func() []harness.StreamPart {
		return []harness.StreamPart{&harness.StreamStartPart{}, &harness.ErrorPart{Error: "provider unavailable"}}
	}}}
	a, err := harness.NewAgent(harness.AgentSettings{Harness: &fakeHarnessAdapter{session: fake}, Sandbox: fakeSandboxProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := harness.NewResumeSessionState("fake", map[string]any{"tag": "stale"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: a, State: CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1", ResumeFrom: stale}),
		Writable: &collectingWriter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != HarnessWorkflowStatusFailed || next.Error != "harness turn emitted an error" || next.ResumeFrom == nil {
		t.Fatalf("next = %+v", next)
	}
	if detach, destroy := fake.counts(); detach != 1 || destroy != 0 {
		t.Fatalf("detach=%d destroy=%d, want 1, 0", detach, destroy)
	}
}

// Ports TS "failed runs still destroy the session when destroyOnFinish is enabled".
func TestRunHarnessAgentTimeSlice_FailedRunDestroysWhenDestroyOnFinish(t *testing.T) {
	fake := &fakeSession{}
	real, err := harness.NewAgent(harness.AgentSettings{Harness: &fakeHarnessAdapter{session: fake}, Sandbox: fakeSandboxProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := real.CreateSession(context.Background(), harness.CreateSessionOptions{SessionID: "ses_1"})
	if err != nil {
		t.Fatal(err)
	}
	stub := stubHarnessWorkflowAgent{
		createSession: func(context.Context, harness.CreateSessionOptions) (*harness.AgentSession, error) { return session, nil },
		stream: func(context.Context, agent.AgentStreamOptions) (*ai.StreamTextResult, error) {
			return nil, errors.New("configuration unavailable")
		},
	}
	stale, _ := harness.NewResumeSessionState("fake", map[string]any{"tag": "stale"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: stub, DestroyOnFinish: true, Writable: &collectingWriter{},
		State: CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("hi"), SessionID: "ses_1", ResumeFrom: stale}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != HarnessWorkflowStatusFailed || next.ResumeFrom != nil {
		t.Fatalf("next = %+v", next)
	}
	if detach, destroy := fake.counts(); detach != 0 || destroy != 1 {
		t.Fatalf("detach=%d destroy=%d, want 0, 1", detach, destroy)
	}
}

// Ports TS "new user turn resumes the warm session and sends the prompt (multi-turn)".
func TestRunHarnessAgentTimeSlice_NewUserTurnResumesWarmSession(t *testing.T) {
	fake := &fakeSession{opts: fakeHarnessOptions{script: stopFinishScript}}
	real, err := harness.NewAgent(harness.AgentSettings{Harness: &fakeHarnessAdapter{session: fake}, Sandbox: fakeSandboxProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	var created harness.CreateSessionOptions
	var streamed agent.AgentStreamOptions
	stub := stubHarnessWorkflowAgent{
		createSession: func(ctx context.Context, o harness.CreateSessionOptions) (*harness.AgentSession, error) {
			created = o
			return real.CreateSession(ctx, o)
		},
		stream: func(ctx context.Context, o agent.AgentStreamOptions) (*ai.StreamTextResult, error) {
			streamed = o
			return real.Stream(ctx, o)
		},
		continueStream: func(context.Context, agent.AgentStreamOptions, []types.ToolApprovalResponseContent, []types.ToolResultContent) (*ai.StreamTextResult, error) {
			return nil, errors.New("continue should not be called for a new user turn")
		},
	}
	prior, _ := harness.NewResumeSessionState("fake", map[string]any{"tag": "prior-run"})
	next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
		Agent: stub, Writable: &collectingWriter{},
		State: CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("turn 2"), SessionID: "ses_1", ResumeFrom: prior}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.SessionID != "ses_1" || created.ResumeFrom != prior || created.ContinueFrom != nil {
		t.Fatalf("CreateSession options = %+v", created)
	}
	if streamed.Prompt != "turn 2" || streamed.Messages != nil {
		t.Fatalf("Stream options = %+v", streamed.AgentGenerateOptions)
	}
	if next.Status != HarnessWorkflowStatusFinished {
		t.Fatalf("status = %v", next.Status)
	}
}

// Ports TS "destroys the session when output validation fails with destroyOnFinish".
func TestRunHarnessAgentTimeSlice_OutputFailureDestroysWhenDestroyOnFinish(t *testing.T) {
	fake := &fakeSession{opts: fakeHarnessOptions{script: func() []harness.StreamPart {
		s := stopFinishScript()
		s[2] = &harness.TextDeltaPart{ID: "t1", Delta: "not json"}
		return s
	}}}
	a, err := harness.NewAgent(harness.AgentSettings{
		Harness: &fakeHarnessAdapter{session: fake}, Sandbox: fakeSandboxProvider{},
		Output: ai.ObjectOutput[scoreOutput](ai.ObjectOutputOptions{Schema: ai.SchemaFor[scoreOutput]()}),
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := RunHarnessAgentStep(context.Background(), RunHarnessAgentStepOptions{
		Agent: a, DestroyOnFinish: true, Writable: &collectingWriter{},
		State: CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Score this."), SessionID: "ses_1"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != HarnessWorkflowStatusFailed || next.ResumeFrom != nil {
		t.Fatalf("next = %+v", next)
	}
	if detach, destroy := fake.counts(); detach != 0 || destroy != 1 {
		t.Fatalf("detach=%d destroy=%d, want 0, 1", detach, destroy)
	}
}
