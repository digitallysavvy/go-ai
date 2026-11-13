package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS run-prompt `value.type === 'error'` awaits
// waitForOutstandingHostToolExecutions() inside the turn's try block: a failed
// host tool execution is rethrown there and settles the turn instead of the
// harness error part.
func TestRunPrompt_HostToolJoinErrorMasksHarnessErrorPart(t *testing.T) {
	submitErr := errors.New("submit failed")
	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(ctx context.Context, in map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		submitErr: submitErr,
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "echo", Input: `{}`},
				&ErrorPart{Error: "harness boom"},
			}
		},
	})
	tools := map[string]types.Tool{"echo": tool}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
	})
	<-out.Done
	err := out.Result.Err()
	if !errors.Is(err, submitErr) {
		t.Fatalf("Err() = %v, want the host tool join error", err)
	}
}

// TS calls finishForHostInputPause (not pauseForHostInput) when an approved
// host tool has no execute function: the open step is not completed.
func TestPauseForHostInputDoesNotCloseOpenStep(t *testing.T) {
	stream := newChunkChannelStream()
	d := &turnDriver{in: runPromptInput{Harness: &mockHarnessAdapter{id: "h"}}, stream: stream, stepOpen: true}
	if err := d.pauseForHostInput(); err != nil {
		t.Fatal(err)
	}
	for c := range stream.ch {
		if c.Type == provider.ChunkTypeFinishStep {
			t.Fatal("pauseForHostInput closed the open step")
		}
	}
	if !d.stepOpen {
		t.Fatal("step state was reset")
	}
}

// TS aborts carry getErrorMessage(abortSignal.reason): a caller's cancel cause.
func TestRunPrompt_AbortReasonIsCancelCause(t *testing.T) {
	keepAlive := make(chan struct{})
	t.Cleanup(func() { close(keepAlive) })
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{&StreamStartPart{}}
		},
		promptDone: func() <-chan struct{} { return keepAlive },
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	out := runPrompt(ctx, runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	cancel(errors.New("user pressed stop"))
	<-out.Done
	err := out.Result.Err()
	if err == nil || !strings.Contains(err.Error(), "user pressed stop") {
		t.Fatalf("Err() = %v, want the cancel cause", err)
	}
}
