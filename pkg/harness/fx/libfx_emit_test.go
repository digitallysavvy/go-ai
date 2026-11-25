package fx

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS emit is synchronous and doSuspendTurn/doStop/doPromptTurn return
// promises, so an emit handler may call them. In Go those calls block until
// the turn settles, which must not depend on the Emit returning.

// within fails the test if f does not return in time.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s (deadlock)", what, d)
	}
}

func pendingCustomRun(m *mockRuntime, id string) {
	m.setRun(func(_ context.Context, o runtimeOptions) error {
		_, err := invoke(o, "custom", id, map[string]any{})
		return err
	})
}

func TestLibfxEmitMaySuspendSynchronously(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	pendingCustomRun(m, "emit-id")
	sb := newFakeSandbox()
	ctx := context.Background()
	start := startOpts(sb, "emit-suspend")
	s, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	opts := harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}}
	var state *harness.ContinueTurnState
	var suspendErr error
	before := &partLog{}
	before.on = func(p harness.StreamPart) {
		if p.PartType() != harness.PartTypeToolCall {
			return
		}
		state, suspendErr = s.DoSuspendTurn(ctx)
	}
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("go"), Emit: before.emit})
	if err != nil {
		t.Fatal(err)
	}
	within(t, 10*time.Second, "suspend from Emit", func() { <-control.Done() })
	if suspendErr != nil || state == nil {
		t.Fatalf("suspend = %v, %v", state, suspendErr)
	}
	if n := len(before.ofType(harness.PartTypeFinishStep)); n != 0 {
		t.Errorf("finish-steps delivered after the suspension = %d", n)
	}
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: state})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	waiting := newSignal()
	after := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	// The turn stopped before its waiting finish-step, so the continuation
	// emits it before the call can be answered.
	waiting.wait(t)
	if err := continued.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: "emit-id", Output: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatal(err)
	}
	if n := len(after.ofType(harness.PartTypeToolCall)); n != 0 {
		t.Errorf("continuation re-emitted the delivered tool call %d times", n)
	}
}

func TestLibfxEmitMayStopSynchronously(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	pendingCustomRun(m, "stop-id")
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(newFakeSandbox(), "emit-stop"))
	if err != nil {
		t.Fatal(err)
	}
	var stopped *harness.ResumeSessionState
	var stopErr error
	events := &partLog{}
	events.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeToolCall {
			stopped, stopErr = s.DoStop(ctx)
		}
	}
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}},
		Prompt:       harness.TextPrompt("go"), Emit: events.emit,
	})
	if err != nil {
		t.Fatal(err)
	}
	within(t, 10*time.Second, "DoStop from Emit", func() { <-control.Done() })
	if stopErr != nil || stopped == nil || stopped.ContinueFrom == nil {
		t.Fatalf("stop = %+v, %v", stopped, stopErr)
	}
}

func TestLibfxEmitMayStartNextTurnOnFinish(t *testing.T) {
	h, _ := newMockHarness(t, Settings{})
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(newFakeSandbox(), "emit-next"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(ctx) //nolint:errcheck
	next := make(chan harness.PromptControl, 1)
	var nextErr error
	first := &partLog{}
	first.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinish {
			var c harness.PromptControl
			c, nextErr = s.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("again"), Emit: func(harness.StreamPart) {}})
			next <- c
		}
	}
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: first.emit})
	if err != nil {
		t.Fatal(err)
	}
	within(t, 10*time.Second, "DoPromptTurn from Emit(finish)", func() { <-control.Done() })
	if nextErr != nil {
		t.Fatal(nextErr)
	}
	c := <-next
	if err := waitDone(t, c); err != nil {
		t.Fatal(err)
	}
}

// A slow Emit must not hold up the turn: parts queue for the dispatcher
// and Done waits for every one of them.
func TestLibfxDoneWaitsForQueuedParts(t *testing.T) {
	h, _ := newMockHarness(t, Settings{})
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(newFakeSandbox(), "emit-order"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(ctx) //nolint:errcheck
	events := &partLog{on: func(harness.StreamPart) { time.Sleep(20 * time.Millisecond) }}
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: events.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, control); err != nil {
		t.Fatal(err)
	}
	parts := events.snapshot()
	if len(parts) == 0 || parts[len(parts)-1].PartType() != harness.PartTypeFinish {
		t.Fatalf("last part is not finish: %+v", parts)
	}
}

// Parts still queued when the turn starts suspending are dropped and their
// keys unrecorded, so the continuation emits them again.
func TestLibfxSuspendDiscardsUndeliveredParts(t *testing.T) {
	turn := &fxTurnState{EmittedKeys: []string{"start", "call:a"}}
	a := &activeTurn{turn: turn, emitted: map[string]bool{"start": true, "call:a": true}}
	a.emitOnceLocked("approval:a", &harness.ToolApprovalRequestPart{ApprovalID: "fx:a", ToolCallID: "a"})
	a.emitLocked(&harness.TextDeltaPart{Delta: "x"})
	a.emitOnceLocked("waiting:a", &harness.FinishStepPart{})
	a.suspending = true
	a.discardQueueLocked()
	if len(a.emitQueue) != 0 {
		t.Errorf("queue = %d", len(a.emitQueue))
	}
	if !reflect.DeepEqual(turn.EmittedKeys, []string{"start", "call:a"}) {
		t.Errorf("emitted keys = %v", turn.EmittedKeys)
	}
	if a.emitted["approval:a"] || a.emitted["waiting:a"] || !a.emitted["call:a"] {
		t.Errorf("emitted = %v", a.emitted)
	}
	a.emitOnceLocked("approval:a", &harness.ToolApprovalRequestPart{})
	if len(a.emitQueue) != 0 {
		t.Error("a suspending turn queued a part")
	}
}

func TestLibfxEmitMaySubmitToolResultSynchronously(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	pendingCustomRun(m, "submit-id")
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(newFakeSandbox(), "emit-submit"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(ctx) //nolint:errcheck
	var control harness.PromptControl
	var submitErr error
	ready := make(chan struct{})
	events := &partLog{}
	events.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeToolCall {
			<-ready
			submitErr = control.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: "submit-id", Output: "ok"})
		}
	}
	control, err = s.DoPromptTurn(ctx, harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Tools: []harness.ToolSpec{{Name: "custom"}}},
		Prompt:       harness.TextPrompt("go"), Emit: events.emit,
	})
	if err != nil {
		t.Fatal(err)
	}
	close(ready)
	within(t, 10*time.Second, "SubmitToolResult from Emit", func() { <-control.Done() })
	if submitErr != nil || control.Err() != nil {
		t.Fatalf("submit = %v, turn = %v", submitErr, control.Err())
	}
	if n := len(events.ofType(harness.PartTypeToolResult)); n != 1 {
		t.Errorf("tool results = %d", n)
	}
	if n := len(events.ofType(harness.PartTypeFinish)); n != 1 {
		t.Errorf("finish parts = %d", n)
	}
}

// SubmitUserMessage waits for the turn to reach the core; its own ctx ends
// that wait (the engine here never finishes starting).
func TestLibfxSubmitUserMessageHonorsCallerContext(t *testing.T) {
	h, m := newMockHarness(t, Settings{})
	release := make(chan struct{})
	factory := m.factory
	h.newEngine = func(ctx context.Context, o runtimeOptions) (fxEngine, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return factory(ctx, o)
	}
	sb := newFakeSandbox()
	ctx := context.Background()
	s, err := h.DoStart(ctx, startOpts(sb, "steer-ctx"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DoDestroy(ctx) //nolint:errcheck
	defer close(release)
	control, err := s.DoPromptTurn(ctx, harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	steerCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	var steerErr error
	within(t, 5*time.Second, "SubmitUserMessage with an expired ctx", func() {
		steerErr = control.(harness.UserMessageSubmitter).SubmitUserMessage(steerCtx, "later")
	})
	if steerErr != context.DeadlineExceeded {
		t.Fatalf("SubmitUserMessage = %v, want context.DeadlineExceeded", steerErr)
	}
}
