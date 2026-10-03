// Package workflow's harness.go ports TS `@ai-sdk/workflow-harness`
// (packages/workflow-harness/src/*.ts at ai@7.0.127): helpers for running a
// pkg/harness.Agent turn as one execution of a durable workflow step,
// suspending and resuming across executions (a tool-approval pause, a
// time-slice budget, or a semantic step boundary), and persisting the turn's
// UI-message stream and validated Output in the returned state.
//
// TS targets Vercel's Workflow DevKit, whose `'use step'` functions persist
// their return value as the durable checkpoint between steps and expose a
// process-wide `getWritable()` output stream. Go has no direct equivalent of
// that runtime, so RunHarnessAgentOptions.Writable is always explicit (TS's
// default resolveWorkflowWritable() has no Go analog) and the caller supplies
// their own step/durability wrapper around RunHarnessAgent* — see
// examples/workflow/harness for the intended usage shape.

package workflow

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// HarnessWorkflowStatus is where a workflow-driven harness run sits in its
// execution loop. Mirrors TS `HarnessWorkflowStatus`.
type HarnessWorkflowStatus string

const (
	// HarnessWorkflowStatusNotStarted is the fresh state before any
	// execution has run.
	HarnessWorkflowStatusNotStarted HarnessWorkflowStatus = "not_started"
	// HarnessWorkflowStatusReadyForNextStep means the turn remains
	// unfinished and ContinueFrom carries the cursor for the next
	// execution.
	HarnessWorkflowStatusReadyForNextStep HarnessWorkflowStatus = "ready_for_next_step"
	// HarnessWorkflowStatusAwaitingToolApproval means the turn emitted one
	// or more tool approval/result requests and ContinueFrom carries the
	// suspended turn.
	HarnessWorkflowStatusAwaitingToolApproval HarnessWorkflowStatus = "awaiting_tool_approval"
	// HarnessWorkflowStatusFinished means the agent turn completed on its
	// own; FinalResult is set.
	HarnessWorkflowStatusFinished HarnessWorkflowStatus = "finished"
	// HarnessWorkflowStatusFailed means the turn errored; Error is set.
	HarnessWorkflowStatusFailed HarnessWorkflowStatus = "failed"
	// HarnessWorkflowStatusTimedOut is returned by the deprecated
	// RunHarnessAgentSlice in place of HarnessWorkflowStatusReadyForNextStep.
	//
	// Deprecated: use HarnessWorkflowStatusReadyForNextStep.
	HarnessWorkflowStatusTimedOut HarnessWorkflowStatus = "timed_out"
)

// HarnessWorkflowUsageSummary is a minimal token-usage summary. Mirrors TS
// `HarnessWorkflowUsageSummary`.
type HarnessWorkflowUsageSummary struct {
	InputTokens  *int64 `json:"inputTokens,omitempty"`
	OutputTokens *int64 `json:"outputTokens,omitempty"`
}

// HarnessWorkflowFinalResult is the terminal result of a finished or
// suspended-awaiting-approval run. Mirrors TS `HarnessWorkflowFinalResult`.
type HarnessWorkflowFinalResult struct {
	SessionID    string                       `json:"sessionId"`
	FinishReason string                       `json:"finishReason"`
	Usage        *HarnessWorkflowUsageSummary `json:"usage,omitempty"`
	// Output is the agent's parsed and schema-validated output when the
	// agent has an output specification (HarnessWorkflowAgent.HasOutput).
	Output any `json:"output,omitempty"`
}

// HarnessWorkflowActiveToolInput is a partially streamed tool input carried
// across an execution boundary. Mirrors the TS
// `activeToolInputs[toolCallId]` shape.
type HarnessWorkflowActiveToolInput struct {
	Start ai.UIMessageChunk `json:"start"`
	Text  string            `json:"text"`
}

// HarnessWorkflowStreamContext is the serializable subset of in-flight
// UI-message state carried across an execution boundary so a continued
// execution can reopen a part it left open, and so a completed tool input can
// be emitted exactly once even though it started in one execution and
// resolved in the next. Mirrors TS `HarnessWorkflowStreamContext`.
type HarnessWorkflowStreamContext struct {
	ActiveTextParts      map[string]ai.UIMessageChunk              `json:"activeTextParts,omitempty"`
	ActiveReasoningParts map[string]ai.UIMessageChunk              `json:"activeReasoningParts,omitempty"`
	ActiveToolInputs     map[string]HarnessWorkflowActiveToolInput `json:"activeToolInputs,omitempty"`
	PendingToolInputs    map[string]ai.UIMessageChunk              `json:"pendingToolInputs,omitempty"`
}

// HarnessWorkflowState is the serializable state machine threaded between
// workflow executions — the entire durable state of a harness run. Every
// field must be JSON-serializable. Mirrors TS `HarnessWorkflowState`.
//
// Two independent lifecycle states drive the engine: ResumeFrom reattaches
// to a warm session before starting this run's new user turn; ContinueFrom
// reattaches to a suspended turn from this same run and continues it without
// sending Prompt again.
type HarnessWorkflowState struct {
	// SessionID is the stable harness session id; doubles as the sandbox
	// name across processes.
	SessionID string `json:"sessionId"`
	// Prompt is the new user turn for this run. Sent once, on the
	// execution that starts the turn.
	Prompt harness.Prompt `json:"prompt"`
	// Messages carries full model messages for continuing a suspended
	// approval turn (e.g. tool-approval-response content). When non-nil,
	// the next execution sends these instead of Prompt/ContinueFrom.
	Messages []types.Message       `json:"messages,omitempty"`
	Status   HarnessWorkflowStatus `json:"status"`
	// ResumeFrom carries resume coordinates for the next user turn.
	ResumeFrom *harness.ResumeSessionState `json:"resumeFrom,omitempty"`
	// ContinueFrom carries continuation coordinates for this run's
	// current suspended turn.
	ContinueFrom  *harness.ContinueTurnState    `json:"continueFrom,omitempty"`
	StreamContext *HarnessWorkflowStreamContext `json:"streamContext,omitempty"`
	FinalResult   *HarnessWorkflowFinalResult   `json:"finalResult,omitempty"`
	Error         string                        `json:"error,omitempty"`
}

// HarnessWorkflowInput is the input for one user turn — the argument to
// CreateHarnessWorkflowState. SessionID is required and must be
// caller-supplied (reuse the conversation id so the sandbox name is stable
// across turns). Pass ResumeFrom to resume a warm conversation; omit it only
// for the first turn of a new conversation. Mirrors TS `HarnessWorkflowInput`.
type HarnessWorkflowInput struct {
	Prompt       harness.Prompt
	Messages     []types.Message
	SessionID    string
	ResumeFrom   *harness.ResumeSessionState
	ContinueFrom *harness.ContinueTurnState
}

// CreateHarnessWorkflowState builds the initial state for one user turn.
// Mirrors TS `createHarnessWorkflowState`.
func CreateHarnessWorkflowState(input HarnessWorkflowInput) HarnessWorkflowState {
	return HarnessWorkflowState{
		SessionID:    input.SessionID,
		Prompt:       input.Prompt,
		Messages:     input.Messages,
		Status:       HarnessWorkflowStatusNotStarted,
		ResumeFrom:   input.ResumeFrom,
		ContinueFrom: input.ContinueFrom,
	}
}

// FinalizeHarnessWorkflow collapses a terminal state into its result.
// Returns an error if the run failed; returns the captured FinalResult when
// finished, or a best-effort result otherwise. Mirrors TS
// `finalizeHarnessWorkflow`.
func FinalizeHarnessWorkflow(state HarnessWorkflowState) (HarnessWorkflowFinalResult, error) {
	if state.Status == HarnessWorkflowStatusFailed {
		msg := state.Error
		if msg == "" {
			msg = "harness workflow failed"
		}
		return HarnessWorkflowFinalResult{}, errors.New(msg)
	}
	if state.FinalResult != nil {
		return *state.FinalResult, nil
	}
	return HarnessWorkflowFinalResult{SessionID: state.SessionID, FinishReason: "unknown"}, nil
}

// HarnessWorkflowWriter is the sink for one execution's UI-message chunks —
// the Go analog of TS's `WritableStream<HarnessWorkflowChunk>` writer
// (`options.writable`/`getWritable()`). Write is called once per chunk in
// order; Close is called only when a run reaches HarnessWorkflowStatusFinished
// (ready_for_next_step/awaiting_tool_approval/failed deliberately do not
// close — a later execution keeps writing, or the failure propagates).
type HarnessWorkflowWriter interface {
	Write(chunk ai.UIMessageChunk) error
	Close() error
}

// ChanHarnessWorkflowWriter adapts a channel of ai.UIMessageChunk into a
// HarnessWorkflowWriter. Close closes the channel. Not safe to reuse across
// concurrent writers once closed.
type ChanHarnessWorkflowWriter struct {
	ch chan<- ai.UIMessageChunk
}

// NewChanHarnessWorkflowWriter builds a HarnessWorkflowWriter that forwards
// every chunk onto ch and closes ch on Close.
func NewChanHarnessWorkflowWriter(ch chan<- ai.UIMessageChunk) *ChanHarnessWorkflowWriter {
	return &ChanHarnessWorkflowWriter{ch: ch}
}

func (w *ChanHarnessWorkflowWriter) Write(chunk ai.UIMessageChunk) error {
	w.ch <- chunk
	return nil
}

func (w *ChanHarnessWorkflowWriter) Close() error {
	close(w.ch)
	return nil
}

// HarnessWorkflowAgent is the subset of *harness.Agent the runner drives.
// *harness.Agent satisfies this automatically; tests build a real
// *harness.Agent over a fake harness.Harness instead of faking this
// interface directly, since HarnessAgentSession/StreamTextResult are
// concrete types (see pkg/harness/agent_test.go's mock-harness pattern,
// mirrored locally in harness_test.go). Mirrors TS `HarnessWorkflowAgent`.
type HarnessWorkflowAgent interface {
	// HasOutput reports whether the agent exposes a parsed output for
	// completed turns.
	HasOutput() bool
	CreateSession(ctx context.Context, opts harness.CreateSessionOptions) (*harness.AgentSession, error)
	Stream(ctx context.Context, opts agent.AgentStreamOptions) (*ai.StreamTextResult, error)
	ContinueStream(ctx context.Context, opts agent.AgentStreamOptions, toolApprovalContinuations []types.ToolApprovalResponseContent, toolResultContinuations []types.ToolResultContent) (*ai.StreamTextResult, error)
}

var _ HarnessWorkflowAgent = (*harness.Agent)(nil)

// RunHarnessAgentOptions configures RunHarnessAgent. Mirrors TS
// `RunHarnessAgentOptions`.
type RunHarnessAgentOptions struct {
	Agent HarnessWorkflowAgent
	State HarnessWorkflowState
	// SandboxSession, when set, is forwarded to HarnessWorkflowAgent.
	// CreateSession as a caller-owned sandbox (harness.CreateSessionOptions.
	// SandboxSession) instead of letting the agent's own SandboxProvider
	// create/resume one. Mirrors TS `RunHarnessAgentOptions.sandboxSession`.
	SandboxSession providerutils.SandboxSession
	// TimeSliceSeconds is the wall-clock budget for this execution. Zero
	// means no time slice: the run continues until the harness's own turn
	// (or, with a StopWhen-configured agent, step) boundary.
	TimeSliceSeconds float64
	// DestroyOnFinish controls whether to destroy the sandbox when the run
	// finishes or fails. Defaults to false: the session is parked and a
	// fresh resume state is returned in ResumeFrom, so the next user turn
	// reattaches to the same conversation (multi-turn chat). Set true for
	// a one-shot run that should release the sandbox when the run ends.
	DestroyOnFinish bool
	// Writable is where to write the turn's UI-message chunks. Required —
	// TS's default resolveWorkflowWritable() (a Workflow DevKit
	// getWritable()) has no Go equivalent.
	Writable HarnessWorkflowWriter
}

// RunHarnessAgent runs one durable execution of a harness agent turn.
//
// Intended to be the body of a caller's own durable-step wrapper: it resumes
// (or starts) the session and streams the turn's chunks to Writable. When
// TimeSliceSeconds is positive, it also races the turn against that
// wall-clock budget and suspends the turn when the time slice completes.
//
// The returned HarnessWorkflowState is serializable and is meant to be the
// step's return value — the caller's durable-workflow runtime persists it as
// the checkpoint between executions. Mirrors TS `runHarnessAgent`.
func RunHarnessAgent(ctx context.Context, opts RunHarnessAgentOptions) (HarnessWorkflowState, error) {
	a := opts.Agent
	state := opts.State
	destroyOnFinish := opts.DestroyOnFinish

	session, err := createHarnessWorkflowSession(ctx, a, state, opts.SandboxSession)
	if err != nil {
		return HarnessWorkflowState{}, err
	}

	result, err := startHarnessWorkflowTurn(ctx, a, session, state)
	if err != nil {
		resumeFrom, continueFrom, ferr := endFailedHarnessSession(ctx, session, destroyOnFinish, nil)
		if ferr != nil {
			return HarnessWorkflowState{}, ferr
		}
		return HarnessWorkflowState{
			SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusFailed,
			ResumeFrom: resumeFrom, ContinueFrom: continueFrom, Error: err.Error(),
		}, nil
	}

	writer := opts.Writable
	streamCtx := newMutableStreamContext(state.StreamContext)
	partState := newExecutionPartState()
	skipFirstStartStep := state.ContinueFrom != nil && len(streamCtx.activeToolInputs) > 0

	suspend := newSuspendGate(ctx, session, opts.TimeSliceSeconds)

	// OnError: pkg/ai's default UI-message error formatter replaces every
	// error with the fixed string "An error occurred." — isAbortErrorValue
	// below needs the real message to recognize a suspend-induced closure,
	// so this engine always supplies its own passthrough formatter (TS's
	// `errorText` already carries the underlying error/rejection reason).
	chunksCh, errCh := ai.ToUIMessageStream(ctx, result.Stream(), ai.UIMessageStreamResultOptions{
		OnError: func(err error) string { return err.Error() },
	})

	sawError := false
	var pipelineErr error
	for chunk := range chunksCh {
		typ, _ := chunk["type"].(string)
		// One continuous assistant message per user turn: the execution
		// that starts the turn keeps the opening `start`; executions that
		// continue an already suspended turn drop it. Intermediate
		// `finish` chunks are dropped and the loop writes a single
		// terminal `finish` itself when the run completes. A new user
		// turn keeps its `start` so the UI renders it as a fresh
		// assistant message.
		if typ == "start" && state.ContinueFrom != nil {
			continue
		}
		if typ == "finish" {
			continue
		}
		if typ == "start-step" && skipFirstStartStep {
			skipFirstStartStep = false
			continue
		}
		if typ == "error" {
			// When a suspend is in flight we tore the turn down at the
			// execution boundary, so an *abort* error is the expected
			// consequence — swallow it. Any non-abort error is
			// unanticipated and must surface.
			if suspend.triggered() && isAbortErrorValue(chunk["errorText"]) {
				continue
			}
			sawError = true
		}
		if werr := writeWorkflowChunk(writer, chunk, streamCtx, partState); werr != nil {
			pipelineErr = werr
			break
		}
	}
	suspend.stopTimer()
	if pipelineErr == nil {
		select {
		case e := <-errCh:
			// pkg/ai's ToUIMessageStream delivers a soft ChunkTypeError as
			// both a normal "error"-type chunk (handled above) AND, once the
			// underlying provider.TextStream closes, its own terminal Err()
			// here — the same underlying failure surfacing twice. Apply the
			// identical suspend-in-flight abort filter so a suspend-induced
			// turn closure doesn't fail the execution via this second path
			// after already being swallowed as a chunk.
			if e != nil && suspend.triggered() && isAbortErrorValue(e.Error()) {
				e = nil
			}
			pipelineErr = e
		default:
		}
	}
	if pipelineErr != nil {
		return HarnessWorkflowState{}, pipelineErr
	}

	// A non-abort error (anything sawError survived the abort filter for)
	// is a real failure and takes priority over a completed time slice.
	if sawError {
		var continueFromOnFail *harness.ContinueTurnState
		if suspend.triggered() {
			st, serr := suspend.wait()
			if destroyOnFinish {
				if serr == nil {
					continueFromOnFail = st
				}
			} else {
				if serr != nil {
					return HarnessWorkflowState{}, serr
				}
				continueFromOnFail = st
			}
		}
		resumeFrom, continueFrom, ferr := endFailedHarnessSession(ctx, session, destroyOnFinish, continueFromOnFail)
		if ferr != nil {
			return HarnessWorkflowState{}, ferr
		}
		return HarnessWorkflowState{
			SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusFailed,
			ResumeFrom: resumeFrom, ContinueFrom: continueFrom, Error: "harness turn emitted an error",
		}, nil
	}

	// The time slice completed: the turn keeps running in the sandbox.
	// Persist the cursor so the next time slice attaches to the same
	// in-flight turn.
	if suspend.triggered() {
		continueFrom, serr := suspend.wait()
		if serr != nil {
			return HarnessWorkflowState{}, serr
		}
		if cerr := closeOpenExecutionParts(writer, streamCtx, partState); cerr != nil {
			return HarnessWorkflowState{}, cerr
		}
		return HarnessWorkflowState{
			SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusReadyForNextStep,
			ContinueFrom: continueFrom, StreamContext: serializeStreamContext(streamCtx),
		}, nil
	}

	finishReason := result.FinishReason()
	usage := result.Usage()

	if session.HasUnfinishedTurn() {
		continueFrom, serr := session.SuspendTurn(ctx)
		if serr != nil {
			return HarnessWorkflowState{}, serr
		}

		if !hasPendingHostInput(continueFrom) {
			if cerr := closeOpenExecutionParts(writer, streamCtx, partState); cerr != nil {
				return HarnessWorkflowState{}, cerr
			}
			return HarnessWorkflowState{
				SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusReadyForNextStep,
				ContinueFrom: continueFrom, StreamContext: serializeStreamContext(streamCtx),
			}, nil
		}

		if writer != nil {
			if werr := writer.Write(ai.UIMessageChunk{"type": "finish", "finishReason": string(finishReason)}); werr != nil {
				return HarnessWorkflowState{}, werr
			}
			if werr := writer.Close(); werr != nil {
				return HarnessWorkflowState{}, werr
			}
		}
		return HarnessWorkflowState{
			SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusAwaitingToolApproval,
			ContinueFrom: continueFrom, ResumeFrom: toResumeState(continueFrom),
			FinalResult: &HarnessWorkflowFinalResult{SessionID: state.SessionID, FinishReason: string(finishReason), Usage: toUsageSummary(usage)},
		}, nil
	}

	var output any
	shouldCaptureOutput := a.HasOutput()
	if shouldCaptureOutput {
		output = result.Output()
		oerr := result.OutputErr()
		// TS's `result.output` is a promise property that is only ever
		// absent (`outputPromise == null`) for a structurally-incompatible
		// HarnessWorkflowAgent implementation — a real HarnessAgent's output
		// getter always returns a promise, rejecting instead when no output
		// specification was configured. Go's `*ai.StreamTextResult.Output`/
		// `.OutputErr` collapse "never configured" and "not yet available"
		// into a silent (nil, nil) rather than an error, so mirror TS's
		// explicit null-check with the equivalent Go condition: HasOutput
		// promised structured output but none — and no parse error either
		// — actually came back. Both cases funnel into the same
		// failed-session handling TS's single try/catch already shares.
		if oerr == nil && output == nil {
			oerr = errors.New("harness agent result does not expose structured output")
		}
		if oerr != nil {
			resumeFrom, continueFrom, ferr := endFailedHarnessSession(ctx, session, destroyOnFinish, nil)
			if ferr != nil {
				return HarnessWorkflowState{}, ferr
			}
			return HarnessWorkflowState{
				SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusFailed,
				ResumeFrom: resumeFrom, ContinueFrom: continueFrom, Error: oerr.Error(),
			}, nil
		}
	}

	// The turn finished on its own: write the single terminal `finish` for
	// the UI message, then CLOSE the writable — this is what lets a
	// consumer piping the run's chunks into a UI-message-stream response
	// terminate. ready_for_next_step and failed deliberately do NOT close.
	if writer != nil {
		if werr := writer.Write(ai.UIMessageChunk{"type": "finish"}); werr != nil {
			return HarnessWorkflowState{}, werr
		}
		if werr := writer.Close(); werr != nil {
			return HarnessWorkflowState{}, werr
		}
	}

	// Capture resume state for the *next user turn* before ending this
	// local session handle. Detach parks the session without stopping the
	// sandbox; a one-shot consumer opts into DestroyOnFinish instead.
	// A detach failure must propagate, not be swallowed in favor of the
	// previous (now stale) turn's resumeFrom: silently returning success
	// with outdated resume state would resume the WRONG point (TS #21591,
	// "workflow harness suppressing detach failures and returning stale
	// resume state"). Mirrors TS `run-harness-agent.ts`'s
	// `resumeFrom = await session.detach();` (no `.catch` fallback).
	var resumeFrom *harness.ResumeSessionState
	if destroyOnFinish {
		_ = session.Destroy(ctx)
	} else {
		rf, derr := session.Detach(ctx)
		if derr != nil {
			return HarnessWorkflowState{}, derr
		}
		resumeFrom = rf
	}

	finalResult := &HarnessWorkflowFinalResult{SessionID: state.SessionID, FinishReason: string(finishReason), Usage: toUsageSummary(usage)}
	if shouldCaptureOutput {
		finalResult.Output = output
	}

	return HarnessWorkflowState{
		SessionID: state.SessionID, Prompt: state.Prompt, Status: HarnessWorkflowStatusFinished,
		ResumeFrom: resumeFrom, FinalResult: finalResult,
	}, nil
}

// RunHarnessAgentStepOptions configures RunHarnessAgentStep. Mirrors TS
// `RunHarnessAgentStepOptions`.
type RunHarnessAgentStepOptions struct {
	Agent           HarnessWorkflowAgent
	State           HarnessWorkflowState
	SandboxSession  providerutils.SandboxSession
	DestroyOnFinish bool
	Writable        HarnessWorkflowWriter
}

// RunHarnessAgentStep runs a harness agent until its next semantic step
// boundary.
//
// Configure the agent with a StopWhen condition such as ai.IsStepCount(1).
// When that condition completes a result while the underlying turn remains
// unfinished, the returned state has status HarnessWorkflowStatusReadyForNextStep
// and carries the continuation state for the next workflow step. Mirrors TS
// `runHarnessAgentStep`.
func RunHarnessAgentStep(ctx context.Context, opts RunHarnessAgentStepOptions) (HarnessWorkflowState, error) {
	return RunHarnessAgent(ctx, RunHarnessAgentOptions{
		Agent: opts.Agent, State: opts.State, SandboxSession: opts.SandboxSession,
		DestroyOnFinish: opts.DestroyOnFinish, Writable: opts.Writable,
	})
}

// DefaultTimeSliceSeconds is RunHarnessAgentTimeSlice's default budget.
// Vercel Fluid Compute recycles a function instance at approximately 800
// seconds; completing a time slice at 750 seconds leaves a safety buffer so
// the next invocation can reattach to the still-running sandbox.
const DefaultTimeSliceSeconds = 750

// RunHarnessAgentTimeSliceOptions configures RunHarnessAgentTimeSlice.
// Mirrors TS `RunHarnessAgentTimeSliceOptions`.
type RunHarnessAgentTimeSliceOptions struct {
	Agent          HarnessWorkflowAgent
	State          HarnessWorkflowState
	SandboxSession providerutils.SandboxSession
	// TimeSliceSeconds defaults to DefaultTimeSliceSeconds when zero.
	TimeSliceSeconds float64
	DestroyOnFinish  bool
	Writable         HarnessWorkflowWriter
}

// RunHarnessAgentTimeSlice runs one time-boxed slice of a durable harness
// agent turn.
//
// When the time slice completes before the turn, the returned state has
// status HarnessWorkflowStatusReadyForNextStep and carries the continuation
// state for the next slice. Mirrors TS `runHarnessAgentTimeSlice`.
func RunHarnessAgentTimeSlice(ctx context.Context, opts RunHarnessAgentTimeSliceOptions) (HarnessWorkflowState, error) {
	seconds := opts.TimeSliceSeconds
	if seconds == 0 {
		seconds = DefaultTimeSliceSeconds
	}
	return RunHarnessAgent(ctx, RunHarnessAgentOptions{
		Agent: opts.Agent, State: opts.State, SandboxSession: opts.SandboxSession, TimeSliceSeconds: seconds,
		DestroyOnFinish: opts.DestroyOnFinish, Writable: opts.Writable,
	})
}

// RunHarnessAgentSliceOptions configures RunHarnessAgentSlice.
//
// Deprecated: use RunHarnessAgentTimeSliceOptions.
type RunHarnessAgentSliceOptions struct {
	Agent            HarnessWorkflowAgent
	State            HarnessWorkflowState
	SandboxSession   providerutils.SandboxSession
	TimeSliceSeconds float64
	// SliceTimeoutSeconds is used when TimeSliceSeconds is zero.
	//
	// Deprecated: use TimeSliceSeconds.
	SliceTimeoutSeconds float64
	DestroyOnFinish     bool
	Writable            HarnessWorkflowWriter
}

// RunHarnessAgentSlice is a deprecated alias of RunHarnessAgentTimeSlice that
// maps a completed time slice's HarnessWorkflowStatusReadyForNextStep to the
// deprecated HarnessWorkflowStatusTimedOut.
//
// Deprecated: use RunHarnessAgentTimeSlice.
func RunHarnessAgentSlice(ctx context.Context, opts RunHarnessAgentSliceOptions) (HarnessWorkflowState, error) {
	seconds := opts.TimeSliceSeconds
	if seconds == 0 {
		seconds = opts.SliceTimeoutSeconds
	}
	state, err := RunHarnessAgentTimeSlice(ctx, RunHarnessAgentTimeSliceOptions{
		Agent: opts.Agent, State: opts.State, SandboxSession: opts.SandboxSession, TimeSliceSeconds: seconds,
		DestroyOnFinish: opts.DestroyOnFinish, Writable: opts.Writable,
	})
	if err != nil {
		return state, err
	}
	if state.Status == HarnessWorkflowStatusReadyForNextStep {
		state.Status = HarnessWorkflowStatusTimedOut
	}
	return state, nil
}

// ─── internals ─────────────────────────────────────────────────────────

func createHarnessWorkflowSession(ctx context.Context, a HarnessWorkflowAgent, state HarnessWorkflowState, sandboxSession providerutils.SandboxSession) (*harness.AgentSession, error) {
	switch {
	case state.ContinueFrom != nil:
		return a.CreateSession(ctx, harness.CreateSessionOptions{SessionID: state.SessionID, ContinueFrom: state.ContinueFrom, SandboxSession: sandboxSession})
	case state.ResumeFrom != nil:
		return a.CreateSession(ctx, harness.CreateSessionOptions{SessionID: state.SessionID, ResumeFrom: state.ResumeFrom, SandboxSession: sandboxSession})
	default:
		return a.CreateSession(ctx, harness.CreateSessionOptions{SessionID: state.SessionID, SandboxSession: sandboxSession})
	}
}

func startHarnessWorkflowTurn(ctx context.Context, a HarnessWorkflowAgent, session *harness.AgentSession, state HarnessWorkflowState) (*ai.StreamTextResult, error) {
	switch {
	case state.Messages != nil:
		return a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{
			Messages: state.Messages, HarnessSession: session,
		}})
	case state.ContinueFrom != nil:
		return a.ContinueStream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{
			HarnessSession: session,
		}}, nil, nil)
	default:
		genOpts := agent.AgentGenerateOptions{HarnessSession: session}
		if state.Prompt.Message != nil {
			genOpts.Messages = []types.Message{*state.Prompt.Message}
		} else {
			genOpts.Prompt = state.Prompt.Text
		}
		return a.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: genOpts})
	}
}

// suspendGate races an in-flight harness turn against a wall-clock time
// slice. When TimeSliceSeconds is positive, it starts a timer that calls
// session.SuspendTurn once the slice elapses, provided the session still has
// an unfinished turn at that instant. triggered/wait let the caller ask (from
// a different goroutine than the timer) whether the suspend fired and, if
// so, block for its result. Mirrors TS runHarnessAgent's
// `suspendPromise`/`timer` pair.
type suspendGate struct {
	timer   *time.Timer
	firedCh chan struct{}
	doneCh  chan struct{}
	state   *harness.ContinueTurnState
	err     error
}

func newSuspendGate(ctx context.Context, session *harness.AgentSession, timeSliceSeconds float64) *suspendGate {
	g := &suspendGate{}
	if timeSliceSeconds <= 0 {
		return g
	}
	g.firedCh = make(chan struct{})
	g.doneCh = make(chan struct{})
	dur := time.Duration(timeSliceSeconds * float64(time.Second))
	g.timer = time.AfterFunc(dur, func() {
		if !session.HasUnfinishedTurn() {
			return
		}
		close(g.firedCh)
		st, err := session.SuspendTurn(ctx)
		g.state, g.err = st, err
		close(g.doneCh)
	})
	return g
}

func (g *suspendGate) triggered() bool {
	if g.firedCh == nil {
		return false
	}
	select {
	case <-g.firedCh:
		return true
	default:
		return false
	}
}

func (g *suspendGate) wait() (*harness.ContinueTurnState, error) {
	<-g.doneCh
	return g.state, g.err
}

func (g *suspendGate) stopTimer() {
	if g.timer != nil {
		g.timer.Stop()
	}
}

func endFailedHarnessSession(ctx context.Context, session *harness.AgentSession, destroyOnFinish bool, continueFrom *harness.ContinueTurnState) (resumeFrom *harness.ResumeSessionState, outContinueFrom *harness.ContinueTurnState, err error) {
	if destroyOnFinish {
		_ = session.Destroy(ctx)
		return nil, nil, nil
	}
	if continueFrom != nil {
		return toResumeState(continueFrom), continueFrom, nil
	}
	rf, derr := session.Detach(ctx)
	if derr != nil {
		return nil, nil, derr
	}
	return rf, nil, nil
}

func toResumeState(c *harness.ContinueTurnState) *harness.ResumeSessionState {
	if c == nil {
		return nil
	}
	return &harness.ResumeSessionState{
		Type: harness.LifecycleStateResumeSession, HarnessID: c.HarnessID,
		SpecificationVersion: c.SpecificationVersion, Data: c.Data, ContinueFrom: c,
	}
}

func hasPendingHostInput(s *harness.ContinueTurnState) bool {
	if s == nil {
		return false
	}
	return len(s.PendingToolApprovals) > 0 || len(s.PendingToolResults) > 0
}

func toUsageSummary(u types.Usage) *HarnessWorkflowUsageSummary {
	if u.InputTokens == nil && u.OutputTokens == nil {
		return nil
	}
	return &HarnessWorkflowUsageSummary{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
}

// abortErrorPattern mirrors TS `isAbortError`'s regex plus one Go-specific
// addition: pkg/harness's run_prompt.go models a bridge closing its
// connection mid-turn (exactly what a real adapter's DoSuspendTurn does) as
// a soft `ChunkTypeError` carrying "adapter ended the turn without emitting
// `finish`" (see run_prompt.go's consumeLoop, `!ok` branch), rather than TS's
// clean, error-free ReadableStream close (to-harness-stream.ts's
// `control.done` resolving calls safeClose() with no error at all). Both are
// the same event — a suspend-induced closure of the in-flight turn — so both
// must be swallowed here under the same suspend-in-flight guard.
var abortErrorPattern = regexp.MustCompile(`(?i)\baborted\b|AbortError|operation was aborted|ended the turn without emitting`)

// isAbortErrorValue reports whether a UI-message error chunk's errorText
// describes an abort — the expected consequence of SuspendTurn tearing the
// turn down at a slice boundary. Mirrors TS `isAbortError` (see
// abortErrorPattern's doc for the one Go-specific addition).
func isAbortErrorValue(v any) bool {
	s, ok := v.(string)
	if !ok || s == "" {
		return false
	}
	return abortErrorPattern.MatchString(s)
}

// mutableStreamContext / executionPartState / the chunk-buffering functions
// below port TS run-harness-agent.ts's MutableStreamContext /
// ExecutionPartState / writeWorkflowChunk / writeRequiredPrelude /
// recordWorkflowChunk / closeOpenExecutionParts verbatim.

type activeToolInput struct {
	start ai.UIMessageChunk
	text  string
}

type mutableStreamContext struct {
	activeTextParts      map[string]ai.UIMessageChunk
	activeReasoningParts map[string]ai.UIMessageChunk
	activeToolInputs     map[string]activeToolInput
	pendingToolInputs    map[string]ai.UIMessageChunk
}

type executionPartState struct {
	openedTextParts      map[string]struct{}
	openedReasoningParts map[string]struct{}
	openedToolInputs     map[string]struct{}
}

func newMutableStreamContext(ctx *HarnessWorkflowStreamContext) *mutableStreamContext {
	sc := &mutableStreamContext{
		activeTextParts:      map[string]ai.UIMessageChunk{},
		activeReasoningParts: map[string]ai.UIMessageChunk{},
		activeToolInputs:     map[string]activeToolInput{},
		pendingToolInputs:    map[string]ai.UIMessageChunk{},
	}
	if ctx == nil {
		return sc
	}
	for k, v := range ctx.ActiveTextParts {
		sc.activeTextParts[k] = v
	}
	for k, v := range ctx.ActiveReasoningParts {
		sc.activeReasoningParts[k] = v
	}
	for k, v := range ctx.ActiveToolInputs {
		sc.activeToolInputs[k] = activeToolInput{start: v.Start, text: v.Text}
	}
	for k, v := range ctx.PendingToolInputs {
		sc.pendingToolInputs[k] = v
	}
	return sc
}

func newExecutionPartState() *executionPartState {
	return &executionPartState{
		openedTextParts:      map[string]struct{}{},
		openedReasoningParts: map[string]struct{}{},
		openedToolInputs:     map[string]struct{}{},
	}
}

func writeWorkflowChunk(w HarnessWorkflowWriter, chunk ai.UIMessageChunk, sc *mutableStreamContext, ps *executionPartState) error {
	if err := writeRequiredPrelude(w, chunk, sc, ps); err != nil {
		return err
	}
	if w != nil {
		if err := w.Write(chunk); err != nil {
			return err
		}
	}
	recordWorkflowChunk(chunk, sc, ps)
	return nil
}

func writeRequiredPrelude(w HarnessWorkflowWriter, chunk ai.UIMessageChunk, sc *mutableStreamContext, ps *executionPartState) error {
	typ, _ := chunk["type"].(string)
	id, _ := chunk["id"].(string)
	toolCallID, _ := chunk["toolCallId"].(string)

	if (typ == "text-delta" || typ == "text-end") && id != "" {
		if start, ok := sc.activeTextParts[id]; ok {
			if _, opened := ps.openedTextParts[id]; !opened {
				if w != nil {
					if err := w.Write(start); err != nil {
						return err
					}
				}
				ps.openedTextParts[id] = struct{}{}
			}
		}
	}

	if (typ == "reasoning-delta" || typ == "reasoning-end") && id != "" {
		if start, ok := sc.activeReasoningParts[id]; ok {
			if _, opened := ps.openedReasoningParts[id]; !opened {
				if w != nil {
					if err := w.Write(start); err != nil {
						return err
					}
				}
				ps.openedReasoningParts[id] = struct{}{}
			}
		}
	}

	if typ == "tool-input-delta" && toolCallID != "" {
		if active, ok := sc.activeToolInputs[toolCallID]; ok {
			if _, opened := ps.openedToolInputs[toolCallID]; !opened {
				if w != nil {
					if err := w.Write(active.start); err != nil {
						return err
					}
					if len(active.text) > 0 {
						if err := w.Write(ai.UIMessageChunk{"type": "tool-input-delta", "toolCallId": toolCallID, "inputTextDelta": active.text}); err != nil {
							return err
						}
					}
				}
				ps.openedToolInputs[toolCallID] = struct{}{}
			}
		}
	}
	return nil
}

func recordWorkflowChunk(chunk ai.UIMessageChunk, sc *mutableStreamContext, ps *executionPartState) {
	typ, _ := chunk["type"].(string)
	id, _ := chunk["id"].(string)
	toolCallID, _ := chunk["toolCallId"].(string)

	switch {
	case typ == "text-start" && id != "":
		sc.activeTextParts[id] = cloneChunk(chunk)
		ps.openedTextParts[id] = struct{}{}
		return
	case typ == "text-end" && id != "":
		delete(sc.activeTextParts, id)
		delete(ps.openedTextParts, id)
		return
	case typ == "reasoning-start" && id != "":
		sc.activeReasoningParts[id] = cloneChunk(chunk)
		ps.openedReasoningParts[id] = struct{}{}
		return
	case typ == "reasoning-end" && id != "":
		delete(sc.activeReasoningParts, id)
		delete(ps.openedReasoningParts, id)
		return
	case typ == "tool-input-start" && toolCallID != "":
		sc.activeToolInputs[toolCallID] = activeToolInput{start: cloneChunk(chunk)}
		ps.openedToolInputs[toolCallID] = struct{}{}
		return
	case typ == "tool-input-delta" && toolCallID != "":
		if active, ok := sc.activeToolInputs[toolCallID]; ok {
			if delta, ok := chunk["inputTextDelta"].(string); ok {
				sc.activeToolInputs[toolCallID] = activeToolInput{start: active.start, text: active.text + delta}
			}
		}
		return
	case typ == "tool-input-available" && toolCallID != "":
		delete(sc.activeToolInputs, toolCallID)
		delete(ps.openedToolInputs, toolCallID)
		sc.pendingToolInputs[toolCallID] = cloneChunk(chunk)
		return
	case typ == "tool-input-error" && toolCallID != "":
		delete(sc.activeToolInputs, toolCallID)
		delete(ps.openedToolInputs, toolCallID)
		delete(sc.pendingToolInputs, toolCallID)
		return
	}

	if toolCallID != "" && (typ == "tool-output-error" || typ == "tool-output-denied" ||
		(typ == "tool-output-available" && chunk["preliminary"] != true)) {
		delete(sc.pendingToolInputs, toolCallID)
	}
}

func closeOpenExecutionParts(w HarnessWorkflowWriter, sc *mutableStreamContext, ps *executionPartState) error {
	for id := range ps.openedTextParts {
		if _, ok := sc.activeTextParts[id]; ok && w != nil {
			if err := w.Write(ai.UIMessageChunk{"type": "text-end", "id": id}); err != nil {
				return err
			}
		}
	}
	ps.openedTextParts = map[string]struct{}{}

	for id := range ps.openedReasoningParts {
		if _, ok := sc.activeReasoningParts[id]; ok && w != nil {
			if err := w.Write(ai.UIMessageChunk{"type": "reasoning-end", "id": id}); err != nil {
				return err
			}
		}
	}
	ps.openedReasoningParts = map[string]struct{}{}
	return nil
}

func serializeStreamContext(sc *mutableStreamContext) *HarnessWorkflowStreamContext {
	out := &HarnessWorkflowStreamContext{}
	any := false
	if len(sc.activeTextParts) > 0 {
		out.ActiveTextParts = sc.activeTextParts
		any = true
	}
	if len(sc.activeReasoningParts) > 0 {
		out.ActiveReasoningParts = sc.activeReasoningParts
		any = true
	}
	if len(sc.activeToolInputs) > 0 {
		m := make(map[string]HarnessWorkflowActiveToolInput, len(sc.activeToolInputs))
		for k, v := range sc.activeToolInputs {
			m[k] = HarnessWorkflowActiveToolInput{Start: v.start, Text: v.text}
		}
		out.ActiveToolInputs = m
		any = true
	}
	if len(sc.pendingToolInputs) > 0 {
		out.PendingToolInputs = sc.pendingToolInputs
		any = true
	}
	if !any {
		return nil
	}
	return out
}

func cloneChunk(chunk ai.UIMessageChunk) ai.UIMessageChunk {
	out := make(ai.UIMessageChunk, len(chunk))
	for k, v := range chunk {
		out[k] = v
	}
	return out
}
