package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Ports TS "tool input errors clear partial input state" at the chunk level.
func TestWriteWorkflowChunk_ToolInputErrorClearsPartialAndPendingInput(t *testing.T) {
	sc := newMutableStreamContext(&HarnessWorkflowStreamContext{
		ActiveToolInputs: map[string]HarnessWorkflowActiveToolInput{
			"call_1": {Start: ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": "call_1", "toolName": "write"}, Text: `{"path":`},
		},
	})
	ps := newExecutionPartState()
	writer := &collectingWriter{}
	err := writeWorkflowChunk(writer, ai.UIMessageChunk{
		"type": "tool-input-error", "toolCallId": "call_1", "toolName": "write", "input": `{"path":`,
		"errorText": "invalid input", "providerExecuted": true,
	}, sc, ps)
	if err != nil {
		t.Fatal(err)
	}
	if ctx := serializeStreamContext(sc); ctx != nil && (len(ctx.ActiveToolInputs) != 0 || len(ctx.PendingToolInputs) != 0) {
		t.Fatalf("stream context = %+v, want no tool inputs", ctx)
	}
	if got := writer.types(); len(got) != 1 || got[0] != "tool-input-error" {
		t.Fatalf("chunks = %v", got)
	}
}

func outputScript(text string) func() []harness.StreamPart {
	return func() []harness.StreamPart {
		return []harness.StreamPart{
			&harness.StreamStartPart{},
			&harness.TextStartPart{ID: "t1"},
			&harness.TextDeltaPart{ID: "t1", Delta: text},
			&harness.TextEndPart{ID: "t1"},
			&harness.FinishStepPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, Usage: usageParts(1, 1)},
			&harness.FinishPart{FinishReason: harness.FinishReason{Unified: harness.FinishReasonStop}, TotalUsage: usageParts(1, 1)},
		}
	}
}

// Ports TS "fails the workflow for $name": the failed state carries the exact
// NoObjectGeneratedError text and no final result.
func TestRunHarnessAgentTimeSlice_OutputFailureMessages(t *testing.T) {
	for _, tc := range []struct{ name, text, message string }{
		{"malformed JSON", "not json", "No object generated: could not parse the response."},
		{"schema-invalid JSON", `{"score":"three"}`, "No object generated: response did not match schema."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeHarnessAgent(t, fakeHarnessOptions{script: outputScript(tc.text)}, func(s *harness.AgentSettings) {
				s.Output = ai.ObjectOutput[scoreOutput](ai.ObjectOutputOptions{Schema: ai.SchemaFor[scoreOutput]()})
			})
			writer := &collectingWriter{}
			next, err := RunHarnessAgentTimeSlice(context.Background(), RunHarnessAgentTimeSliceOptions{
				Agent: a, State: CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Score this."), SessionID: "ses_1"}),
				Writable: writer,
			})
			if err != nil {
				t.Fatal(err)
			}
			if next.Status != HarnessWorkflowStatusFailed || next.Error != tc.message || next.FinalResult != nil || writer.isClosed() {
				t.Fatalf("next = %+v closed=%v, want failed with %q", next, writer.isClosed(), tc.message)
			}
		})
	}
}

// Ports TS "forwards a caller-owned sandbox session for %s runs".
func TestRunHarnessAgent_ForwardsCallerSandboxSession(t *testing.T) {
	resumeFrom, _ := harness.NewResumeSessionState("fake", map[string]any{})
	continueFrom, _ := harness.NewContinueTurnState("fake", map[string]any{})
	for _, branch := range []string{"fresh", "resume", "continue"} {
		t.Run(branch, func(t *testing.T) {
			real := newFakeHarnessAgent(t, fakeHarnessOptions{script: stopFinishScript, continueScript: stopFinishScript})
			var got harness.CreateSessionOptions
			agent := stubHarnessWorkflowAgent{
				createSession: func(ctx context.Context, o harness.CreateSessionOptions) (*harness.AgentSession, error) {
					got = o
					return real.CreateSession(ctx, o)
				},
				stream: real.Stream, continueStream: real.ContinueStream,
			}
			state := CreateHarnessWorkflowState(HarnessWorkflowInput{Prompt: harness.TextPrompt("Hello."), SessionID: "session-1"})
			switch branch {
			case "resume":
				state.ResumeFrom = resumeFrom
			case "continue":
				state.ContinueFrom = continueFrom
			}
			sandbox := fakeNetworkSandbox{}
			if _, err := RunHarnessAgent(context.Background(), RunHarnessAgentOptions{Agent: agent, State: state, SandboxSession: sandbox, Writable: &collectingWriter{}}); err != nil {
				t.Fatal(err)
			}
			if got.SessionID != "session-1" || got.SandboxSession == nil {
				t.Fatalf("CreateSession options = %+v", got)
			}
			if (branch == "resume") != (got.ResumeFrom != nil) || (branch == "continue") != (got.ContinueFrom != nil) {
				t.Fatalf("branch %s: ResumeFrom=%v ContinueFrom=%v", branch, got.ResumeFrom != nil, got.ContinueFrom != nil)
			}
		})
	}
}
