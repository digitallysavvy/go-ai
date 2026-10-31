package harness

import (
	"context"
	"sync"
)

// scriptedHarness drives a turn like a real runtime: start emits the opening
// parts, and onSubmit reacts to each host tool result the host submits (for
// example by echoing it back as a tool-result part, which real adapters do
// only after the result was submitted).
type scriptedHarness struct {
	*mockHarnessResult
}

func newScriptedHarness(
	start func(emit func(StreamPart)),
	onSubmit func(sub ToolResultSubmission, emit func(StreamPart)),
) *scriptedHarness {
	res := &mockHarnessResult{}
	sess := &mockSession{id: "scripted-1"}
	sess.doPromptTurn = func(ctx context.Context, o PromptTurnOptions) (PromptControl, error) {
		done := make(chan struct{})
		var once sync.Once
		emit := func(p StreamPart) {
			o.Emit(p)
			if _, ok := p.(*FinishPart); ok {
				once.Do(func() { close(done) })
			}
		}
		base := &mockPromptControl{
			toolResults: &res.toolResults, toolApprovals: &res.toolApprovals, done: done,
			onSubmitResult: func(sub ToolResultSubmission) {
				if onSubmit != nil {
					onSubmit(sub, emit)
				}
			},
		}
		go start(emit)
		return base, nil
	}
	res.session = sess
	res.harness = &mockHarnessAdapter{id: "mock", session: sess}
	return &scriptedHarness{res}
}
