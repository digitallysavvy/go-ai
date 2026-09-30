package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// invalidToolInputMessage is the generic error surfaced to both the harness
// runtime (via SubmitToolResult) and the consumer stream (as a tool-error's
// Error) for a host tool call whose input failed schema validation. Mirrors
// TS `invalidToolInputMessage` (internal/run-prompt.ts) — the detailed
// NoSuchToolError/InvalidToolInputError from validation is never sent back to
// the runtime/model or exposed to the consumer; it exists only for local
// diagnostics.
const invalidToolInputMessage = "Tool input validation failed."

// nowMs returns the current wall-clock time in Unix milliseconds, used for
// telemetry.go's model-call response-time measurement (mirrors TS
// turn-telemetry.ts's `Date.now()`-based `modelCallStartedAt`).
func nowMs() int64 { return time.Now().UnixMilli() }

// unclosedStepErrorMessage is the protocol-error message a HarnessAgent turn
// fails with when the adapter's terminal `finish` part arrives without a
// preceding `finish-step` for the step in progress. Mirrors TS
// `HarnessStreamTextResult.finish()`'s "received terminal finish with
// unclosed step content" check: every harness-v1 adapter step must be closed
// by an explicit finish-step before the turn-closing finish.
const unclosedStepErrorMessage = "HarnessAgent: received terminal finish with unclosed step content. Harness adapters must emit `finish-step` before `finish`."

// chunkChannelStream is a provider.TextStream backed by a Go channel, used to
// feed ai.NewStreamTextResultFromParts from run_prompt's own translation
// loop. Plays the role TS's `toHarnessStream` ReadableStream plays, adapted
// to Go's pull-based provider.TextStream contract.
type chunkChannelStream struct {
	ch  chan provider.StreamChunk
	err error
}

func newChunkChannelStream() *chunkChannelStream {
	return &chunkChannelStream{ch: make(chan provider.StreamChunk, 32)}
}

func (s *chunkChannelStream) push(c provider.StreamChunk) { s.ch <- c }

// fail records err and stops accepting more chunks; the next Next() call
// after the buffered chunks are drained returns err instead of io.EOF.
func (s *chunkChannelStream) fail(err error) {
	s.err = err
	close(s.ch)
}

func (s *chunkChannelStream) closeOK() { close(s.ch) }

// fail settles the turn as a soft (data-preserving) failure: a
// ChunkTypeError chunk carrying msg, then a normal channel close. Mirrors TS
// `result.fail(err)` (settleFailure's non-abort branch), which always
// surfaces failures — a harness `error` part, a host tool execution
// exception, an unclosed-step protocol violation — as a terminal `error`
// stream part rather than rejecting/discarding whatever the turn already
// produced. Distinct from chunkChannelStream.fail, which is a hard,
// non-data-preserving channel failure reserved for driver-internal errors
// that occur before or between chunks (e.g. ctx cancellation, DoPromptTurn
// itself returning an error before any content exists to preserve).
func (d *turnDriver) fail(err error) {
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeError, Text: err.Error()})
	d.stream.closeOK()
}

// abort settles the turn as a caller-initiated stop: a ChunkTypeAbort chunk
// carrying the ctx's cancellation reason (falling back to err), then a
// normal channel close — instead of the ChunkTypeError chunk `fail` would
// push. Mirrors TS `HarnessStreamTextResult.abort()` (TS 86a84c9).
func (d *turnDriver) abort(err error) {
	reason := ""
	if ctxErr := d.ctx.Err(); ctxErr != nil {
		reason = ctxErr.Error()
	} else if err != nil {
		reason = err.Error()
	}
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: reason})
	d.stream.closeOK()
}

// settleFailure settles a failed turn, firing OnTurnFailed first (so the
// session's turn tracking returns to idle regardless of which branch below
// runs — mirrors TS "Both outcomes notify onTurnFailed"). When the caller's
// own ctx has already been cancelled or timed out, the failure is a
// user-initiated stop, not a real error: it settles through d.abort (an
// `abort` stream part) instead of d.fail (an `error` part), matching TS
// `settleFailure`'s `input.abortSignal?.aborted` branch (TS 86a84c9). Every
// runPrompt failure path (turn bootstrap, a wire-level `error` event, the
// consume loop itself failing) settles through this single helper so the
// abort/error classification is applied consistently everywhere, exactly as
// TS applies it "at all three settle points".
func (d *turnDriver) settleFailure(err error) {
	if d.in.OnTurnFailed != nil {
		d.in.OnTurnFailed()
	}
	// telAbort/telError mirror pkg/ai/generate.go's own abort-vs-error
	// telemetry split (telemetry.FireOnAbort vs FireOnError): a caller
	// cancellation ends the turn's telemetry span cleanly, the same way the
	// stream itself settles through an `abort` chunk rather than an `error`
	// one just below.
	if d.ctx.Err() != nil {
		d.telAbort(err)
		d.abort(err)
		return
	}
	d.telError(err)
	d.fail(err)
}

func (s *chunkChannelStream) Next() (*provider.StreamChunk, error) {
	c, ok := <-s.ch
	if !ok {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	return &c, nil
}

func (s *chunkChannelStream) Err() error   { return s.err }
func (s *chunkChannelStream) Close() error { return nil }

// runPromptInput is the input of runPrompt. Mirrors TS `runPrompt`'s input
// object.
type runPromptInput struct {
	Harness Harness
	Session Session

	// Mode selects the turn entry point: "" or "prompt" starts a new turn
	// from Prompt; "continue" continues the in-flight turn via
	// DoContinueTurn and ignores Prompt.
	Mode   string
	Prompt Prompt

	Model        string
	Skills       []Skill
	Instructions string

	// Tools is the full merged tool set (harness builtins + user tools),
	// used for tool-call validation. ActiveTools is the (possibly narrower)
	// subset the runtime is currently allowed to invoke; it always defaults
	// to Tools when the agent has no activeTools/inactiveTools restriction.
	Tools        map[string]types.Tool
	ToolsContext map[string]interface{}
	ActiveTools  map[string]types.Tool
	ToolSpecs    []ToolSpec

	BuiltinToolFiltering *BuiltinToolFiltering
	SandboxSession       providerutils.SandboxSession
	SessionWorkDir       string
	ResponseFormat       *ResponseFormat
	// Output is AgentSettings.Output, forwarded to
	// ai.ExternalStreamOptions.Output so the returned *ai.StreamTextResult's
	// Output()/OutputErr()/PartialOutput() accessors work for a HarnessAgent
	// turn exactly like they do for a StreamText call. See settings.go's
	// outputResponseFormatter doc for why ResponseFormat is derived
	// separately instead of through this value.
	Output interface{}

	// Telemetry configures OpenTelemetry span/attribute reporting for this
	// turn via pkg/telemetry's dispatch pattern (telemetry.go). Nil disables
	// it (telemetry.FireOn* are no-ops without registered integrations
	// anyway, but a nil Settings also skips the per-call RecordInputs/
	// RecordOutputs/IncludeRuntimeContext/IncludeToolsContext filtering).
	// Mirrors TS `HarnessAgentSettings.telemetry`.
	Telemetry *telemetry.Settings

	Callbacks      Callbacks
	StopConditions []ai.StopCondition
	ToolApproval   ToolApprovalConfiguration

	PendingToolApprovals      []PendingToolApproval
	PendingToolResults        []PendingToolResult
	ToolApprovalContinuations []types.ToolApprovalResponseContent
	ToolResultContinuations   []types.ToolResultContent

	OnPendingToolApproval    func(PendingToolApproval)
	OnToolApprovalSettled    func(approvalID string)
	OnPendingToolResult      func(PendingToolResult)
	OnToolResultSettled      func(toolCallID string)
	OnTurnFinished           func()
	OnTurnFailed             func()
	OnPromptControlAvailable func(PromptControl)

	// OnStopConditionMet is called from suspendOrFinishNow when a
	// StopCondition matches after a completed step: it should suspend the
	// underlying harness session's turn in place (wired to
	// AgentSession.captureStopConditionBoundary) so a caller can resume it
	// later via ContinueGenerate/ContinueStream, and return the resulting
	// ContinueTurnState. A nil error means the turn is now suspended and
	// resumable; suspendOrFinishNow falls back to a hard finish (the
	// original WG4 behavior) on a nil func or a non-nil error (e.g. the
	// adapter returned CapabilityUnsupportedError). Nil when no
	// AgentSession is driving this turn (e.g. a bare runPrompt call in a
	// test). Mirrors TS `runPrompt`'s `input.onStopConditionMet`.
	OnStopConditionMet func(ctx context.Context) (*ContinueTurnState, error)

	// RuntimeContext flows through to callback events.
	RuntimeContext interface{}
}

// runPromptOutput is the output of runPrompt. Mirrors TS `runPrompt`'s return
// value.
type runPromptOutput struct {
	// Result is the streaming result for this turn, built on
	// ai.NewStreamTextResultFromParts. Awaiting Result.Err() (after the
	// stream is fully consumed) is equivalent to TS awaiting `done`.
	Result *ai.StreamTextResult
	// Done is closed when the driver goroutine has finished (whether the
	// turn succeeded, failed, or paused for host input).
	Done <-chan struct{}
}

// runPrompt drives one prompt turn end to end:
//   - calls session.DoPromptTurn/DoContinueTurn
//   - translates harness events to provider.StreamChunk and feeds
//     ai.NewStreamTextResultFromParts
//   - executes host-side user tools when their tool-call events arrive
//     (concurrently; joined at each step boundary) and submits results back
//     to the harness
//   - closes the result when the harness signals `finish` (or on error, or
//     when the turn pauses for host input / an approval / a stop condition)
//
// Mirrors TS `runPrompt` (internal/run-prompt.ts). See the package doc for a
// list of deliberate simplifications versus the 1590-line TS original
// (mostly bridge/workflow-slice-specific machinery out of WG4's scope).
func runPrompt(ctx context.Context, in runPromptInput) *runPromptOutput {
	stream := newChunkChannelStream()
	// Shared between the result (correlating callback/telemetry events with
	// this turn's stream) and turnDriver's own telemetry.go calls, mirroring
	// TS `runPrompt`'s single `callId` reused by both the result and
	// createTurnLifecycle.
	callID := newID()
	// The result consumes the driver's channel with a context that is not
	// cancelled with the caller's: cancellation is handled by the driver,
	// which settles the turn through settleFailure (OnTurnFailed first, then
	// an abort chunk and a close). Reading until that close guarantees the
	// session's turn state is settled before Err()/Text() return, and that
	// the channel is always drained, so the driver never blocks on push after
	// the caller cancels.
	result := ai.NewStreamTextResultFromParts(context.WithoutCancel(ctx), stream, ai.ExternalStreamOptions{
		CallID:   callID,
		Provider: "harness:" + in.Harness.HarnessID(),
		ModelID:  in.Model,
		Output:   in.Output,
	})

	done := make(chan struct{})
	d := &turnDriver{ctx: ctx, in: in, stream: stream, telCallID: callID}
	go func() {
		defer close(done)
		d.run()
	}()

	return &runPromptOutput{Result: result, Done: done}
}

// turnDriver holds the mutable state of one runPrompt invocation. Every
// field is owned by the single goroutine running Run(); host tool executions
// run in their own goroutines but only touch shared state through execMu.
type turnDriver struct {
	ctx    context.Context
	in     runPromptInput
	stream *chunkChannelStream

	control        PromptControl
	activeToolSet  map[string]struct{}
	stripToolInput func(part StreamPart) []StreamPart

	// execToolCallsByID holds the *unstripped* tool-call event for host tool
	// execution (execution needs the real absolute paths); toolCallsByID
	// holds the parsed, display (work-dir-stripped) public tool call used
	// for approvals, classification and callbacks. validatedHostCallsByID
	// holds the schema-validated call with *unstripped* Arguments actually
	// used for execution (nil for a toolCallID that isn't a host tool call,
	// e.g. provider-executed or the client-executed askUserQuestions
	// builtin) — mirrors TS run-prompt.ts's per-tool-call
	// `validatedHostToolCall` closure variable, made addressable by ID since
	// Go's driver revisits a call's validation result from separate
	// functions (handleHostToolCall, processApprovalContinuation) rather
	// than a single closure scope.
	execToolCallsByID      map[string]*ToolCallPart
	toolCallsByID          map[string]types.ToolCall
	validatedHostCallsByID map[string]*types.ToolCall
	providerExecByID       map[string]bool

	// toolsList is d.in.Tools flattened once per turn, for ai.ParseToolCall
	// (which — like TS parseToolCall/doParseToolCall — needs the merged tool
	// set to resolve a call's schema).
	toolsList []types.Tool

	settledHostIDs    map[string]struct{}
	settledBuiltinIDs map[string]struct{}

	pendingApprovalsByApprovalID map[string]*PendingToolApproval
	pendingApprovalsByToolCallID map[string]*PendingToolApproval
	pendingResultsByToolCallID   map[string]*PendingToolResult
	continuationsByApprovalID    map[string]types.ToolApprovalResponseContent
	continuationsByToolCallID    map[string]types.ToolResultContent

	// Step accumulation, independent of ai.StreamTextResult's own internal
	// bookkeeping, kept so OnStepEnd/OnToolExecutionEnd callbacks can fire in
	// real time with accurate data instead of only after the whole turn
	// settles (see package doc).
	stepText        string
	stepReasoning   string
	stepToolCalls   []types.ToolCall
	stepToolResults []types.ToolResult
	toolExecMs      map[string]int64
	stepNumber      int
	stepOpen        bool
	startCalled     bool

	// expectedStepToolCallCount/observedStepToolCallCount/
	// pauseAfterStepToolCalls implement TS 32349cc's multi-approval grouping:
	// an adapter that knows a step's complete tool-call cardinality up front
	// (populates ToolCallPart.StepToolCallCount on every tool call in the
	// step) lets the host collect every approval/result request from that
	// step before pausing once, instead of pausing after the first one and
	// leaving the rest permanently stuck (the bug 32349cc fixed: a step with
	// two approval-required host tool calls exposed only the first). Applies
	// to every host tool call pause point (see handleHostToolCall), not only
	// harness-pi (the adapter TS shipped it for first) — an adapter that
	// never sets StepToolCallCount keeps the original pause-on-first
	// behavior, since expectedStepToolCallCount then stays nil.
	expectedStepToolCallCount *int
	observedStepToolCallCount int
	pauseAfterStepToolCalls   bool

	// completedSteps accumulates the locally-tracked StepResult from every
	// completeStep call, used to evaluate StopConditions (which need the
	// real completed-step history, e.g. ai.IsStepCount) — independent of
	// ai.StreamTextResult's own Steps(), for the same reason as the rest of
	// this local step tracking (see struct doc above).
	completedSteps []types.StepResult

	bufferedResultChunks [][]provider.StreamChunk

	execWG  sync.WaitGroup
	execMu  sync.Mutex
	execErr error

	closingResumedStep bool

	// pendingStopBoundary is set at a finish-step whose completed step might
	// satisfy a StopCondition, and resolved against the *next* part read
	// (TS's "one event of lookahead") before actually deciding to suspend.
	// See suspendOrFinishNow's doc and consumeLoop's pendingStopBoundary
	// handling.
	pendingStopBoundary *pendingStopBoundary

	// Telemetry span/context state — see telemetry.go. telCallID is
	// generated once per turn (runPrompt) and shared across every step's
	// language-model-call span, mirroring how pkg/ai/generate.go reuses one
	// CallID across steps of a single generateText call. telCtx/telStepCtx
	// are the root/current-step contexts returned by telStart/telStepStart,
	// used to parent every later telemetry event so integrations (e.g.
	// telemetry.OpenTelemetry) nest turn -> step -> model-call/tool-execution
	// spans correctly. telEnded guards telEnd/telError against firing twice
	// for the same turn (TS turn-telemetry.ts's `ended` flag).
	telCallID             string
	telCtx                context.Context
	telStepCtx            context.Context
	telModelCallStartedAt int64
	telEnded              bool

	// pendingFinishChunk holds the terminal ChunkTypeFinish chunk once the
	// harness's own `finish` part has been observed, deferred until run()
	// has called OnTurnFinished. Mirrors TS runPrompt's `finalFinish`, which
	// is likewise only turned into an enqueued stream part inside the final
	// `result.finish()` call, after `input.onTurnFinished?.()` has already
	// run. d.stream is a buffered channel: pushing the finish chunk here
	// (inside consumeLoop, on the same goroutine that will later call
	// OnTurnFinished) would make it visible to a concurrent reader —
	// pkg/ai's StreamTextResult sets its status to done as soon as it reads
	// that chunk, without waiting for the channel to close — so a caller
	// blocked in Err()/etc. could observe the turn as finished, and start a
	// new one on the same AgentSession, before finishTrackedTurn has reset
	// the session's turnState back to idle.
	pendingFinishChunk *provider.StreamChunk
}

func (d *turnDriver) run() {
	d.execToolCallsByID = map[string]*ToolCallPart{}
	d.toolCallsByID = map[string]types.ToolCall{}
	d.validatedHostCallsByID = map[string]*types.ToolCall{}
	d.providerExecByID = map[string]bool{}
	d.toolsList = make([]types.Tool, 0, len(d.in.Tools))
	for _, tool := range d.in.Tools {
		d.toolsList = append(d.toolsList, tool)
	}
	d.settledHostIDs = map[string]struct{}{}
	d.settledBuiltinIDs = map[string]struct{}{}
	d.pendingApprovalsByApprovalID = map[string]*PendingToolApproval{}
	d.pendingApprovalsByToolCallID = map[string]*PendingToolApproval{}
	d.pendingResultsByToolCallID = map[string]*PendingToolResult{}
	d.continuationsByApprovalID = map[string]types.ToolApprovalResponseContent{}
	d.continuationsByToolCallID = map[string]types.ToolResultContent{}
	d.toolExecMs = map[string]int64{}
	d.stripToolInput = NewToolInputWorkDirStripper(d.in.SessionWorkDir)

	d.activeToolSet = map[string]struct{}{}
	for name := range d.in.Tools {
		_, userActive := d.in.ActiveTools[name]
		_, isBuiltin := d.in.Harness.BuiltinTools()[name]
		if userActive || (isBuiltin && IsBuiltinToolIncluded(name, d.in.BuiltinToolFiltering)) {
			d.activeToolSet[name] = struct{}{}
		}
	}

	for _, a := range d.in.PendingToolApprovals {
		a := a
		d.pendingApprovalsByApprovalID[a.ApprovalID] = &a
		d.pendingApprovalsByToolCallID[a.ToolCallID] = &a
	}
	for _, r := range d.in.PendingToolResults {
		r := r
		d.pendingResultsByToolCallID[r.ToolCallID] = &r
	}
	for _, c := range d.in.ToolApprovalContinuations {
		d.continuationsByApprovalID[c.ApprovalID] = c
	}
	for _, c := range d.in.ToolResultContinuations {
		d.continuationsByToolCallID[c.ToolCallID] = c
	}

	partsCh := make(chan StreamPart, 64)
	emit := func(part StreamPart) {
		select {
		case partsCh <- part:
		case <-d.ctx.Done():
		}
	}

	var control PromptControl
	var err error
	if d.in.Mode == "continue" {
		control, err = d.in.Session.DoContinueTurn(d.ctx, ContinueTurnOptions{
			TurnSettings:   TurnSettings{Model: d.in.Model, Skills: d.in.Skills, Instructions: d.in.Instructions, Tools: d.in.ToolSpecs},
			ResponseFormat: d.in.ResponseFormat,
			Emit:           emit,
		})
	} else {
		control, err = d.in.Session.DoPromptTurn(d.ctx, PromptTurnOptions{
			TurnSettings:   TurnSettings{Model: d.in.Model, Skills: d.in.Skills, Instructions: d.in.Instructions, Tools: d.in.ToolSpecs},
			Prompt:         d.in.Prompt,
			ResponseFormat: d.in.ResponseFormat,
			Emit:           emit,
		})
	}
	if err != nil {
		// OnTurnFailed is called before the stream is pushed to/closed (see
		// settleFailure): a caller blocked on the returned
		// *ai.StreamTextResult's Err()/Text()/etc — which unblock once the
		// stream settles — must never observe that before the session's
		// turn-state callback has already run, or AgentSession.
		// HasUnfinishedTurn() could still (harmlessly but confusingly)
		// report true for an instant after the caller's own wait returned.
		d.settleFailure(err)
		return
	}
	d.control = control
	if d.in.OnPromptControlAvailable != nil {
		d.in.OnPromptControlAvailable(control)
	}

	go func() {
		<-control.Done()
		close(partsCh)
	}()

	if outcome, err := d.processStartupContinuations(); err != nil {
		d.settleFailure(err)
		return
	} else if outcome == turnOutcomeAwaitingToolResult {
		// Already fully settled (pushed+closed) by pauseForHostInput inside
		// processApprovalContinuation; the session's turn state was already
		// set to awaiting-approval/awaiting-tool-result when the pending
		// approval/result was recorded.
		return
	}

	finished, alreadySettled, turnErr := d.consumeLoop(partsCh)
	if alreadySettled {
		return
	}
	if turnErr != nil {
		d.settleFailure(turnErr)
		return
	}
	if finished && d.in.OnTurnFinished != nil {
		d.in.OnTurnFinished()
	}
	// Push the terminal finish chunk (if any) only now, after
	// OnTurnFinished has already run — see pendingFinishChunk's doc.
	if d.pendingFinishChunk != nil {
		d.stream.push(*d.pendingFinishChunk)
	}
	d.stream.closeOK()
}

type turnOutcome int

const (
	turnOutcomeContinue turnOutcome = iota
	turnOutcomeAwaitingToolResult
)

// processStartupContinuations submits continuations for approvals/results
// that were pending when this turn resumed a suspended one. Mirrors the
// pre-loop `for (const approval of pendingToolApprovals)` /
// `for (const pendingResult of pendingToolResults)` passes in TS run-prompt.ts.
func (d *turnDriver) processStartupContinuations() (turnOutcome, error) {
	for _, approval := range append([]PendingToolApproval(nil), d.in.PendingToolApprovals...) {
		continuation, ok := d.continuationsByApprovalID[approval.ApprovalID]
		if !ok {
			continue
		}
		outcome, err := d.processApprovalContinuation(approval, continuation)
		if err != nil {
			return turnOutcomeContinue, err
		}
		if outcome == turnOutcomeAwaitingToolResult {
			return outcome, nil
		}
		d.closingResumedStep = true
	}
	for _, pending := range append([]PendingToolResult(nil), d.in.PendingToolResults...) {
		continuation, hasContinuation := d.continuationsByToolCallID[pending.ToolCallID]
		if !hasContinuation && pending.CompletedResult == nil {
			continue
		}
		var cont *types.ToolResultContent
		if hasContinuation {
			cont = &continuation
		}
		if err := d.processResultContinuation(pending, cont); err != nil {
			return turnOutcomeContinue, err
		}
		if pending.CompletedResult == nil {
			d.closingResumedStep = true
		}
	}
	return turnOutcomeContinue, nil
}

// consumeLoop reads harness stream parts until the channel closes (adapter
// finished the turn) or the driver pauses/stops early.
//
// finished is true only when the harness's own terminal `finish` part was
// observed and forwarded. alreadySettled is true when some other path
// (pauseForHostInput, suspendOrFinishNow) already pushed the closing chunk(s) and
// closed d.stream itself — the caller (run) must not touch d.stream again in
// that case, and must not fire OnTurnFinished/OnTurnFailed (whichever
// settled it already did, if appropriate: a pause fires neither, matching
// TS's finishForHostInputPause).
func (d *turnDriver) consumeLoop(partsCh <-chan StreamPart) (finished bool, alreadySettled bool, err error) {
	// Guarantees the checkpoint pin (if any) is released on every exit path —
	// including ctx.Done() at the top of the read select and every mid-loop
	// error return — not just the two paths that release it explicitly below.
	// Mirrors TS run-prompt.ts's top-level `try { ... } finally {
	// releasePendingStopBoundary(); }` around the whole read loop.
	defer d.releasePendingStopBoundary()
	for {
		// TS 32349cc: every tool call in the current step has now been
		// observed (a handleHostToolCall pause point deferred instead of
		// pausing immediately, via shouldDeferPause) — pause once for all of
		// them now, rather than waiting for a part that will never arrive.
		if d.pauseAfterStepToolCalls && d.expectedStepToolCallCount != nil && d.observedStepToolCallCount >= *d.expectedStepToolCallCount {
			if err := d.pauseForHostInput(); err != nil {
				return false, false, err
			}
			return false, true, nil
		}

		var part StreamPart
		var ok bool
		select {
		case part, ok = <-partsCh:
		case <-d.ctx.Done():
			return false, false, d.ctx.Err()
		}
		if !ok {
			d.releasePendingStopBoundary()
			if !finished {
				// The adapter ended the turn (closed its Done channel)
				// without ever emitting `finish`. Mirrors TS's fallback
				// `else { input.onTurnFailed?.(); }` branch for a reader
				// loop that ends without reaching a terminal `finish`.
				return false, false, errors.New("harness: adapter ended the turn without emitting `finish`")
			}
			return true, false, nil
		}

		// One event of lookahead (TS run-prompt.ts's `pendingStopBoundary`
		// check, evaluated against the *next* part read after a
		// StopConditions-eligible finish-step): if that next part is the
		// harness's own terminal `finish`, the turn was ending on its own
		// anyway — release the checkpoint and let it finish naturally
		// instead of suspending redundantly right before it. Otherwise,
		// evaluate StopConditions now (using the step just completed) and
		// either suspend or fall through to process this part normally.
		if d.pendingStopBoundary != nil {
			psb := d.pendingStopBoundary
			d.pendingStopBoundary = nil
			if _, isFinish := part.(*FinishPart); isFinish {
				psb.release()
			} else if reason := d.evaluateStopConditions(); reason != "" {
				psb.release()
				d.suspendOrFinishNow(psb.finishReason, psb.usage)
				return false, true, nil
			} else {
				psb.release()
			}
		}

		if sp, isStart := part.(*StreamStartPart); isStart {
			d.ensureStarted()
			for _, c := range TranslatePart(sp, TranslateOptions{}) {
				d.stream.push(c)
			}
			continue
		}

		if id := toolInputStreamID(part); id != "" {
			if d.isSettled(id) {
				continue
			}
			d.ensureStepOpen()
			for _, displayed := range d.stripToolInput(part) {
				for _, c := range TranslatePart(displayed, d.translateOpts()) {
					d.stream.push(c)
				}
			}
			continue
		}

		display := StripWorkDir(part, d.in.SessionWorkDir)

		if id, hasID := toolCallIDOf(display); hasID && isReplayable(display) && d.isSettled(id) {
			continue
		}

		if _, isFinishStep := display.(*FinishStepPart); isFinishStep && d.closingResumedStep {
			d.closingResumedStep = false
			if err := d.joinOutstandingExecutions(); err != nil {
				return false, false, err
			}
			d.flushBufferedResultChunks()
			d.resetStepAccum()
			continue
		}

		// Open the step lazily before the first real content of each step,
		// but not for step/turn boundaries or the error part themselves —
		// mirrors TS's exact exclusion list (`!== 'finish-step' && !==
		// 'finish' && !== 'error'`; 'stream-start' is handled earlier above
		// and never reaches this point). This is also what makes the
		// unclosed-step check below meaningful: d.stepOpen only reflects
		// genuine unflushed content, never a boundary part opening "itself".
		switch display.(type) {
		case *FinishStepPart, *FinishPart, *ErrorPart:
		default:
			d.ensureStepOpen()
		}

		if ep, isErr := display.(*ErrorPart); isErr {
			// Returned as an error, not handled inline: run() converts every
			// non-nil consumeLoop error into a soft (data-preserving)
			// ChunkTypeError chunk via d.fail — so any text/tool calls
			// already accumulated in the in-progress step are still flushed
			// onto Steps()/Text() instead of being silently discarded,
			// mirroring TS `settleFailure`/`result.fail()` adapted to Go's
			// Err()-based error surface (see pkg/ai/stream_external_test.go
			// TestNewStreamTextResultFromParts_ErrorChunkPreservesPartialStep).
			_ = d.joinOutstandingExecutions()
			return false, false, fmt.Errorf("%v", ep.Error)
		}

		if tc, isCall := display.(*ToolCallPart); isCall {
			rawTC := part.(*ToolCallPart)
			d.execToolCallsByID[tc.ToolCallID] = rawTC
			displayCall, hostCall, verr := d.classifyAndValidateToolCall(rawTC, tc)
			if verr != nil {
				_ = d.joinOutstandingExecutions()
				return false, false, verr
			}
			d.toolCallsByID[tc.ToolCallID] = displayCall
			d.validatedHostCallsByID[tc.ToolCallID] = hostCall
			d.providerExecByID[tc.ToolCallID] = tc.ProviderExecuted
			d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &displayCall})
			d.stepToolCalls = append(d.stepToolCalls, displayCall)
			// TS 32349cc: counted for every tool call in the step (not only
			// host ones), and the first non-nil StepToolCallCount observed
			// wins (mirrors TS `expectedStepToolCallCount ??=
			// value.stepToolCallCount`).
			d.observedStepToolCallCount++
			if d.expectedStepToolCallCount == nil {
				d.expectedStepToolCallCount = tc.StepToolCallCount
			}
		} else if tr, isResult := display.(*ToolResultPart); isResult {
			chunks := TranslatePart(tr, d.translateOpts())
			d.bufferedResultChunks = append(d.bufferedResultChunks, chunks)
			d.recordToolResult(tr)
		} else {
			for _, c := range TranslatePart(display, d.translateOpts()) {
				d.stream.push(c)
			}
		}

		switch v := part.(type) {
		case *TextDeltaPart:
			d.stepText += v.Delta
		case *ReasoningDeltaPart:
			d.stepReasoning += v.Delta
		}

		if tar, isApproval := display.(*ToolApprovalRequestPart); isApproval {
			awaiting, err := d.handleApprovalRequest(tar)
			if err != nil {
				return false, false, err
			}
			if awaiting {
				// Already fully settled by pauseForHostInput.
				return false, true, nil
			}
			continue
		}

		if fs, isFinishStep := display.(*FinishStepPart); isFinishStep {
			if err := d.joinOutstandingExecutions(); err != nil {
				return false, false, err
			}
			d.completeStep(fs.FinishReason, fs.Usage)
			// Deferred to the *next* part read — see the pendingStopBoundary
			// handling above (TS's "one event of lookahead") — rather than
			// evaluated immediately: StopConditions are meaningless with none
			// configured, so this only fires when the caller actually asked
			// for early-stop behavior.
			if len(d.in.StopConditions) > 0 {
				var release func()
				if pinner, ok := d.control.(CheckpointPinner); ok {
					release = pinner.PinCheckpoint()
				}
				d.pendingStopBoundary = &pendingStopBoundary{
					finishReason: fs.FinishReason, usage: fs.Usage, releaseCheckpoint: release,
				}
			}
		}

		if fp, isFinish := display.(*FinishPart); isFinish {
			if err := d.joinOutstandingExecutions(); err != nil {
				return false, false, err
			}
			// A terminal `finish` must not carry unclosed step content: every
			// step (including the last) must be closed by its own
			// `finish-step` first. A harness adapter that skips this is a
			// protocol violation, surfaced as a failure rather than silently
			// accepted as a valid (and wrongly step-boundary-less) finish.
			// Mirrors TS `HarnessStreamTextResult.finish()`'s
			// `currentStepContent.length > 0` check.
			if d.stepOpen {
				return false, false, errors.New(unclosedStepErrorMessage)
			}
			usage := harnessUsageToTypesUsage(fp.TotalUsage)
			// TS's terminal `finish` handler ends the root telemetry span
			// with the bridge's own totalUsage, the real end-of-turn total
			// (as opposed to pauseForHostInput's zero usage or
			// suspendOrFinishNow's last-step usage).
			d.telEnd(d.completedSteps, *usage)
			// Deferred, not pushed here — see turnDriver.pendingFinishChunk's
			// doc. run() pushes it once OnTurnFinished has been called.
			d.pendingFinishChunk = &provider.StreamChunk{
				Type:            provider.ChunkTypeFinish,
				FinishReason:    unifiedFinishReason(fp.FinishReason),
				RawFinishReason: fp.FinishReason.Raw,
				Usage:           usage,
			}
			finished = true
		}

		if tc, isCall := part.(*ToolCallPart); isCall && !tc.ProviderExecuted {
			awaiting, err := d.handleHostToolCall(tc)
			if err != nil {
				return false, false, err
			}
			if awaiting {
				// Already fully settled by pauseForHostInput.
				return finished, true, nil
			}
		}
	}
}

func (d *turnDriver) isSettled(id string) bool {
	if _, ok := d.settledHostIDs[id]; ok {
		return true
	}
	_, ok := d.settledBuiltinIDs[id]
	return ok
}

func toolInputStreamID(part StreamPart) string {
	switch p := part.(type) {
	case *ToolInputStartPart:
		return p.ID
	case *ToolInputDeltaPart:
		return p.ID
	case *ToolInputEndPart:
		return p.ID
	}
	return ""
}

func toolCallIDOf(part StreamPart) (string, bool) {
	switch p := part.(type) {
	case *ToolCallPart:
		return p.ToolCallID, true
	case *ToolResultPart:
		return p.ToolCallID, true
	case *ToolApprovalRequestPart:
		return p.ToolCallID, true
	}
	return "", false
}

func isReplayable(part StreamPart) bool {
	switch part.(type) {
	case *ToolCallPart, *ToolResultPart, *ToolApprovalRequestPart:
		return true
	}
	return false
}

func (d *turnDriver) translateOpts() TranslateOptions {
	return TranslateOptions{IsProviderExecuted: func(toolCallID string) bool {
		executed, ok := d.providerExecByID[toolCallID]
		return !ok || executed
	}}
}

// classifyAndValidateToolCall validates a tool-call event's input against the
// merged tool set's schema, reusing ai.ParseToolCall — the same validator
// (pkg/schema, via pkg/ai's tool_call_pipeline.go) pkg/ai's own generate/
// stream loop uses for tool inputs, so the harness and core behave the same.
// Mirrors TS's inline `isHostTool`/`validateToolCall`/`displayHostToolCall`
// sequence in run-prompt.ts.
//
// A host tool call (not provider-executed, present in the merged tool set,
// and not the client-executed askUserQuestions builtin) is validated against
// the RAW (unstripped) event, so the validated/transformed input keeps the
// absolute working-directory paths a schema may require for execution; the
// returned hostCall carries that unstripped input and is what
// handleHostToolCall/processApprovalContinuation must execute with. Every
// other tool call — provider-executed, or the client-executed
// askUserQuestions builtin, both never executed through this path — is
// validated against the display (already work-dir-stripped) event instead,
// and hostCall is nil.
//
// displayCall is always the consumer-facing, work-dir-stripped call: for a
// host tool call, its (already-parsed) Arguments are stripped by
// displayHostToolCall from the *validated* value (not re-parsed from raw
// JSON), and an invalid call's detailed validation error is replaced by the
// generic invalidToolInputMessage so schema diagnostics never reach the
// runtime/model or the consumer stream.
func (d *turnDriver) classifyAndValidateToolCall(raw, display *ToolCallPart) (displayCall types.ToolCall, hostCall *types.ToolCall, err error) {
	// isHostTool checks ActiveTools (TS: `hasTool({ tools: activeTools,
	// ... })`), not the full merged Tools — a tool present but filtered out
	// of the active set is never executed through this path (handled by the
	// active-tool-filtering "execution-denied" check ahead of this in
	// handleHostToolCall), so it must be validated the same way a
	// provider-executed/builtin call is: against the display value, not raw.
	_, hasActiveToolEntry := d.in.ActiveTools[raw.ToolName]
	_, harnessHasBuiltin := d.in.Harness.BuiltinTools()[raw.ToolName]
	isHostTool := !raw.ProviderExecuted && hasActiveToolEntry &&
		!(raw.ToolName == string(BuiltinToolAskUserQuestions) && harnessHasBuiltin)

	eventPart := display
	if isHostTool {
		eventPart = raw
	}
	parsed, perr := ai.ParseToolCall(d.ctx, ai.ParseToolCallOptions{
		ToolCall: toolCallPartToCall(eventPart),
		Tools:    d.toolsList,
	})
	if perr != nil {
		return types.ToolCall{}, nil, perr
	}

	if !isHostTool {
		return parsed, nil, nil
	}
	hc := parsed
	return displayHostToolCall(parsed, d.in.SessionWorkDir), &hc, nil
}

// toolCallPartToCall converts a harness ToolCallPart event into the
// types.ToolCall shape ai.ParseToolCall expects, preserving the raw streamed
// JSON input (RawArguments) so parsing/validation happens against the exact
// text the event carried — mirrors TS's `LanguageModelV4ToolCall` shape
// `validateToolCall` builds from a `HarnessV1StreamPart` tool-call event.
func toolCallPartToCall(p *ToolCallPart) types.ToolCall {
	return types.ToolCall{
		ID:               p.ToolCallID,
		ToolName:         p.ToolName,
		RawArguments:     p.Input,
		ProviderExecuted: p.ProviderExecuted,
		Dynamic:          p.Dynamic,
		ProviderMetadata: convertProviderMetadata(ProviderMetadata(p.ProviderMetadata)),
	}
}

// displayHostToolCall strips the session working-directory prefix from a
// validated host tool call's already-parsed Arguments, for consumer display.
// Mirrors TS `displayHostToolCall`.
func displayHostToolCall(call types.ToolCall, sessionWorkDir string) types.ToolCall {
	out := call
	out.Arguments, _ = stripParsedToolInputWorkDir(call.Arguments, sessionWorkDir).(map[string]interface{})
	if call.Invalid {
		out.Error = errors.New(invalidToolInputMessage)
	}
	return out
}

func parseJSONObject(input string) (map[string]interface{}, error) {
	if input == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (d *turnDriver) ensureStarted() {
	if d.startCalled {
		return
	}
	d.startCalled = true
	d.telStart()
	if d.in.Callbacks.OnStart != nil {
		d.in.Callbacks.OnStart(d.ctx, ai.OnStartEvent{
			OperationID:    "ai.harnessAgent",
			ModelID:        d.in.Model,
			Instructions:   d.in.Instructions,
			RuntimeContext: d.in.RuntimeContext,
			ToolsContext:   d.in.ToolsContext,
		})
	}
}

func (d *turnDriver) ensureStepOpen() {
	d.ensureStarted()
	if d.stepOpen {
		return
	}
	d.stepOpen = true
	d.telStepStart()
	if d.in.Callbacks.OnStepStart != nil {
		d.in.Callbacks.OnStepStart(d.ctx, ai.OnStepStartEvent{
			StepNumber:     d.stepNumber,
			ModelID:        d.in.Model,
			Instructions:   d.in.Instructions,
			RuntimeContext: d.in.RuntimeContext,
			ToolsContext:   d.in.ToolsContext,
		})
	}
	if d.in.Callbacks.OnLanguageModelCallStart != nil {
		d.in.Callbacks.OnLanguageModelCallStart(d.ctx, ai.LanguageModelCallStartEvent{
			Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.in.Model,
			Instructions: d.in.Instructions,
		})
	}
}

func (d *turnDriver) resetStepAccum() {
	d.stepText = ""
	d.stepReasoning = ""
	d.stepToolCalls = nil
	d.stepToolResults = nil
	d.toolExecMs = map[string]int64{}
	d.stepOpen = false
	d.expectedStepToolCallCount = nil
	d.observedStepToolCallCount = 0
	d.pauseAfterStepToolCalls = false
}

// shouldDeferPause reports whether a pause for host input should be deferred
// until every tool call in the current step has been observed, so the host
// collects every approval/result request from the step in one pass instead
// of pausing on the first one (TS 32349cc). See the turnDriver field doc.
func (d *turnDriver) shouldDeferPause() bool {
	return d.expectedStepToolCallCount != nil && d.observedStepToolCallCount < *d.expectedStepToolCallCount
}

func (d *turnDriver) flushBufferedResultChunks() {
	for _, chunks := range d.bufferedResultChunks {
		for _, c := range chunks {
			d.stream.push(c)
		}
	}
	d.bufferedResultChunks = nil
}

// recordToolResult correlates a tool-result event with its originating call
// for step accounting and OnToolExecutionStart/End callback data. Mirrors
// TS's `toolExecutions` map, populated for every tool call regardless of who
// executed it (host or the runtime's own builtin), and flushed at each step
// boundary via `publishToolExecutions`.
func (d *turnDriver) recordToolResult(tr *ToolResultPart) {
	call, ok := d.toolCallsByID[tr.ToolCallID]
	if !ok {
		call = types.ToolCall{ID: tr.ToolCallID, ToolName: tr.ToolName}
	}
	var toolResult types.ToolResult
	if tr.IsError {
		toolResult = types.ToolResult{ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, Error: fmt.Errorf("%v", tr.Result)}
	} else {
		toolResult = types.ToolResult{ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, Result: tr.Result}
	}
	d.stepToolResults = append(d.stepToolResults, toolResult)

	if d.in.Callbacks.OnToolExecutionStart != nil {
		d.in.Callbacks.OnToolExecutionStart(d.ctx, ai.OnToolCallStartEvent{
			ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, ToolCall: call,
			RuntimeContext: d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}
	if d.in.Callbacks.OnToolExecutionEnd != nil {
		d.in.Callbacks.OnToolExecutionEnd(d.ctx, ai.OnToolCallFinishEvent{
			ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, ToolCall: call, ToolOutput: toolResult,
			ToolExecutionMs: d.toolExecMs[tr.ToolCallID],
			RuntimeContext:  d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}
	d.telToolExecution(call, toolResult, d.toolExecMs[tr.ToolCallID])
}

// completeStep flushes buffered tool-result chunks, fires the model-call-end
// and step-end callbacks from locally-tracked step data (see turnDriver
// doc), pushes the step boundary chunk, and advances stepNumber.
func (d *turnDriver) completeStep(finishReason FinishReason, usage Usage) {
	if d.in.Callbacks.OnLanguageModelCallEnd != nil {
		d.in.Callbacks.OnLanguageModelCallEnd(d.ctx, ai.LanguageModelCallEndEvent{
			Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.in.Model,
			FinishReason: unifiedFinishReason(finishReason),
			Usage:        harnessUsageToTypesUsageValue(usage),
		})
	}
	d.telLanguageModelCallEnd(finishReason, usage)
	d.flushBufferedResultChunks()

	step := types.StepResult{
		StepNumber:    d.stepNumber,
		Model:         types.StepModel{Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.in.Model},
		Text:          d.stepText,
		ReasoningText: d.stepReasoning,
		ToolCalls:     append([]types.ToolCall(nil), d.stepToolCalls...),
		ToolResults:   append([]types.ToolResult(nil), d.stepToolResults...),
		FinishReason:  unifiedFinishReason(finishReason),
		Usage:         harnessUsageToTypesUsageValue(usage),
	}
	if d.in.Callbacks.OnStepEnd != nil {
		d.in.Callbacks.OnStepEnd(d.ctx, ai.OnStepFinishEvent{
			StepNumber: step.StepNumber, Model: step.Model, Text: step.Text,
			ToolCalls: step.ToolCalls, ToolResults: step.ToolResults,
			FinishReason: step.FinishReason, Usage: step.Usage,
			RuntimeContext: d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}
	d.telStepEnd(step.StepNumber, step)

	d.stream.push(provider.StreamChunk{
		Type:            provider.ChunkTypeFinishStep,
		FinishReason:    unifiedFinishReason(finishReason),
		RawFinishReason: finishReason.Raw,
		Usage:           harnessUsageToTypesUsage(usage),
	})

	d.completedSteps = append(d.completedSteps, step)
	d.stepNumber++
	d.resetStepAccum()
}

func (d *turnDriver) evaluateStopConditions() string {
	if len(d.in.StopConditions) == 0 {
		return ""
	}
	return ai.EvaluateStopConditions(d.in.StopConditions, ai.StopConditionState{Steps: d.completedSteps})
}

// joinOutstandingExecutions waits for every host tool execution goroutine
// started since the last join and returns the first error any of them
// produced, mirroring TS `waitForOutstandingHostToolExecutions` (built on
// `Promise.allSettled`).
func (d *turnDriver) joinOutstandingExecutions() error {
	d.execWG.Wait()
	d.execMu.Lock()
	err := d.execErr
	d.execErr = nil
	d.execMu.Unlock()
	return err
}

// pauseForHostInput settles the local result for a turn that is pausing to
// wait on host input (an approval decision or a client-supplied tool
// result), joining outstanding host tool executions first. The underlying
// harness Session/PromptControl are left exactly as they are — this only
// settles the *ai.StreamTextResult this runPrompt call returned, so its
// caller (Agent.Generate/.Stream) does not hang waiting on a turn that
// deliberately isn't finished; AgentSession's turn-state (awaiting-approval /
// awaiting-tool-result) is what actually tracks the unfinished turn, and
// ContinueGenerate/ContinueStream resume it. Mirrors TS
// `finishForHostInputPause`. completeCurrentStep is derived from whether any
// content has been seen since the last real step boundary (d.stepOpen),
// which serves the same purpose as TS's per-call-site flag: closing a step
// only when there is one to close, instead of manufacturing an empty one.
func (d *turnDriver) pauseForHostInput() error {
	if err := d.joinOutstandingExecutions(); err != nil {
		return err
	}
	if d.stepOpen {
		d.completeStep(FinishReason{Unified: FinishReasonToolCalls}, Usage{})
	} else {
		d.flushBufferedResultChunks()
	}
	// TS `finishForHostInputPause` ends the turn's root telemetry span here
	// too (with zero usage), even though the underlying harness session turn
	// is only paused, not really finished — each runPrompt invocation is its
	// own traced operation; ContinueGenerate/ContinueStream starts a fresh
	// one with its own telStart.
	d.telEnd(d.completedSteps, types.Usage{})
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeFinish})
	d.stream.closeOK()
	return nil
}

// pendingStopBoundary is a completed step's finish-step data, captured when
// StopConditions are configured, awaiting the next part read before
// consumeLoop decides whether to actually suspend. Mirrors TS
// `pendingStopBoundary` in run-prompt.ts.
type pendingStopBoundary struct {
	finishReason      FinishReason
	usage             Usage
	releaseCheckpoint func()
}

// release calls releaseCheckpoint if set. Safe on a nil releaseCheckpoint
// (no CheckpointPinner was available).
func (p *pendingStopBoundary) release() {
	if p.releaseCheckpoint != nil {
		p.releaseCheckpoint()
	}
}

// releasePendingStopBoundary releases and clears d.pendingStopBoundary, if
// any. Mirrors TS `releasePendingStopBoundary`.
func (d *turnDriver) releasePendingStopBoundary() {
	if d.pendingStopBoundary != nil {
		d.pendingStopBoundary.release()
		d.pendingStopBoundary = nil
	}
}

// suspendOrFinishNow settles the local *ai.StreamTextResult right now, ahead
// of any later `finish` the adapter might still emit — used only by the
// StopConditions early-stop path. finishReason is the last completed step's;
// usage is deliberately left off the pushed chunk so the locally-summed
// total stands, since this is not a real bridge-reported total the way a
// genuine terminal `finish` carries one (contrast pauseForHostInput/the
// FinishPart branch in consumeLoop) — but it is still the right usage for
// the telemetry span's totals (see telEnd's call below), mirroring TS's
// `pendingStopBoundary.usage`.
//
// Whether the *underlying harness session's turn* survives this for a later
// resume depends on d.in.OnStopConditionMet (wired to
// AgentSession.captureStopConditionBoundary, which calls the adapter's
// DoSuspendTurn): when it succeeds, the turn is genuinely suspended — OnTurn-
// Finished is NOT called, so the session stays in its "suspended" state
// (HasUnfinishedTurn() true) instead of returning to idle, and a caller can
// continue it with ContinueGenerate/ContinueStream. When it's unavailable or
// fails (nil OnStopConditionMet — e.g. a bare runPrompt call outside an
// Agent/AgentSession — or the adapter can't honor DoSuspendTurn), this falls
// back to WG4's original behavior: a hard finish, OnTurnFinished fires, and
// the session returns to idle. Mirrors TS `runPrompt`'s early-stop branch
// (`input.onStopConditionMet?.()` then `lifecycle.end`/`result.finish()`,
// with no `onTurnFinished` call in that branch either).
//
// Implemented (WG13): (1) TS's "one event of lookahead" refinement — a
// finish-step whose step might satisfy a StopCondition only sets
// d.pendingStopBoundary; consumeLoop's next iteration peeks at the following
// part and skips suspending entirely when that turns out to be the harness's
// own natural `finish` arriving right after this step anyway (avoiding a
// redundant suspend immediately before a turn that was ending on its own),
// or evaluates StopConditions and suspends only when they still match. See
// consumeLoop's pendingStopBoundary handling. (2) The bridge-level
// replay-checkpoint pinning (TS `pinSandboxChannelEventCheckpoint`), via
// spec.go's CheckpointPinner: the pendingStopBoundary above pins one (when
// d.control implements it) for the duration of the lookahead, so a live
// bridge connection's event buffer isn't garbage-collected while the stop
// decision is pending. Correctness never depended on this — DoSuspendTurn
// (not this driver) is what actually freezes the adapter's cursor — it is
// purely the replay-safety optimization CheckpointPinner's own doc describes.
func (d *turnDriver) suspendOrFinishNow(finishReason FinishReason, usage Usage) {
	suspended := false
	if d.in.OnStopConditionMet != nil {
		if _, err := d.in.OnStopConditionMet(d.ctx); err == nil {
			suspended = true
		}
	}
	// TS's StopConditions early-stop path ends the root telemetry span with
	// the matched step boundary's own usage (`pendingStopBoundary.usage`),
	// not a fresh/zero one — mirrors that exactly, regardless of whether the
	// underlying session turn was suspended or hard-finished.
	d.telEnd(d.completedSteps, harnessUsageToTypesUsageValue(usage))
	// OnTurnFinished must run before the finish chunk is pushed onto
	// d.stream, not after: d.stream is a buffered channel, so pushing makes
	// the chunk visible to a concurrent reader (pkg/ai's StreamTextResult
	// marks itself done as soon as it reads a ChunkTypeFinish chunk, without
	// waiting for the channel to close) immediately, before this goroutine
	// would otherwise get around to calling OnTurnFinished — letting a
	// caller blocked on Err()/etc. see the turn as finished, and start a new
	// one on the same AgentSession, before finishTrackedTurn has actually
	// reset the session's turnState back to idle. See run()'s identical
	// ordering for the natural-finish path (pendingFinishChunk).
	if !suspended && d.in.OnTurnFinished != nil {
		d.in.OnTurnFinished()
	}
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: unifiedFinishReason(finishReason), RawFinishReason: finishReason.Raw})
	d.stream.closeOK()
}

func unifiedFinishReason(r FinishReason) types.FinishReason {
	return types.FinishReason(r.Unified)
}

func harnessUsageToTypesUsage(u Usage) *types.Usage {
	v := harnessUsageToTypesUsageValue(u)
	return &v
}

func harnessUsageToTypesUsageValue(u Usage) types.Usage {
	out := types.Usage{}
	if u.InputTokens.Total != nil {
		v := int64(*u.InputTokens.Total)
		out.InputTokens = &v
	}
	if u.OutputTokens.Total != nil {
		v := int64(*u.OutputTokens.Total)
		out.OutputTokens = &v
	}
	if u.InputTokens.Total != nil && u.OutputTokens.Total != nil {
		v := int64(*u.InputTokens.Total + *u.OutputTokens.Total)
		out.TotalTokens = &v
	}
	if u.InputTokens.CacheRead != nil || u.InputTokens.CacheWrite != nil || u.InputTokens.NoCache != nil {
		details := &types.InputTokenDetails{}
		if u.InputTokens.CacheRead != nil {
			v := int64(*u.InputTokens.CacheRead)
			details.CacheReadTokens = &v
		}
		if u.InputTokens.CacheWrite != nil {
			v := int64(*u.InputTokens.CacheWrite)
			details.CacheWriteTokens = &v
		}
		if u.InputTokens.NoCache != nil {
			v := int64(*u.InputTokens.NoCache)
			details.NoCacheTokens = &v
		}
		out.InputDetails = details
	}
	if u.OutputTokens.Reasoning != nil || u.OutputTokens.Text != nil {
		details := &types.OutputTokenDetails{}
		if u.OutputTokens.Reasoning != nil {
			v := int64(*u.OutputTokens.Reasoning)
			details.ReasoningTokens = &v
		}
		if u.OutputTokens.Text != nil {
			v := int64(*u.OutputTokens.Text)
			details.TextTokens = &v
		}
		out.OutputDetails = details
	}
	return out
}

// executionDeniedOutput builds the `{type:"execution-denied",reason}` output
// value submitted back to the harness when a tool call is filtered out or
// denied. Mirrors TS's inline object literal of the same shape.
func executionDeniedOutput(reason string) map[string]interface{} {
	return map[string]interface{}{"type": "execution-denied", "reason": reason}
}

// handleApprovalRequest processes a `tool-approval-request` event from the
// adapter (a built-in tool asking for permission): filters it against
// BuiltinToolFiltering, records it as pending, resolves any startup
// continuation, and otherwise emits the request and pauses the turn. Mirrors
// the `displayValue.type === 'tool-approval-request'` handling in TS
// run-prompt.ts (split across its early filtering check and its main
// handling block).
func (d *turnDriver) handleApprovalRequest(part *ToolApprovalRequestPart) (awaiting bool, err error) {
	call, ok := d.toolCallsByID[part.ToolCallID]
	if !ok {
		return false, fmt.Errorf("harness '%s' emitted approval request '%s' for unknown tool call '%s'", d.in.Harness.HarnessID(), part.ApprovalID, part.ToolCallID)
	}
	raw := d.execToolCallsByID[part.ToolCallID]
	toolName := call.ToolName
	if raw != nil {
		toolName = raw.ToolName
	}
	if !IsBuiltinToolIncluded(toolName, d.in.BuiltinToolFiltering) {
		submitter, ok := d.control.(ToolApprovalSubmitter)
		if !ok {
			return false, fmt.Errorf("harness '%s' emitted a built-in tool approval request but does not support approval responses", d.in.Harness.HarnessID())
		}
		if err := submitter.SubmitToolApproval(d.ctx, ToolApprovalSubmission{ApprovalID: part.ApprovalID, Approved: false, Reason: BuiltinToolFilteringDenialReason(toolName)}); err != nil {
			return false, err
		}
		return false, nil
	}

	pending := d.pendingApprovalsByApprovalID[part.ApprovalID]
	if pending == nil {
		providerExecuted := true
		nativeName := ""
		input := ""
		if raw != nil {
			providerExecuted = raw.ProviderExecuted
			nativeName = raw.NativeName
			input = raw.Input
		}
		pending = &PendingToolApproval{
			ApprovalID: part.ApprovalID, ToolCallID: part.ToolCallID, ToolName: toolName,
			Input: input, Kind: PendingToolApprovalBuiltin, ProviderExecuted: &providerExecuted, NativeName: nativeName,
		}
		d.pendingApprovalsByApprovalID[pending.ApprovalID] = pending
		d.pendingApprovalsByToolCallID[pending.ToolCallID] = pending
	}

	if continuation, ok := d.continuationsByApprovalID[pending.ApprovalID]; ok {
		outcome, err := d.processApprovalContinuation(*pending, continuation)
		if err != nil {
			return false, err
		}
		return outcome == turnOutcomeAwaitingToolResult, nil
	}

	if d.in.OnPendingToolApproval != nil {
		d.in.OnPendingToolApproval(*pending)
	}
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
		ApprovalID: pending.ApprovalID, ToolCallID: pending.ToolCallID, ToolCall: call,
	}})
	if err := d.pauseForHostInput(); err != nil {
		return false, err
	}
	return true, nil
}

// handleHostToolCall executes (or denies, or pauses for) a non-provider-
// executed tool call. Mirrors the `value.type === 'tool-call' &&
// !value.providerExecuted` block in TS run-prompt.ts.
func (d *turnDriver) handleHostToolCall(raw *ToolCallPart) (awaiting bool, err error) {
	call := d.toolCallsByID[raw.ToolCallID]

	_, harnessHasBuiltin := d.in.Harness.BuiltinTools()[raw.ToolName]
	isClientExecutedBuiltin := raw.ToolName == string(BuiltinToolAskUserQuestions) && harnessHasBuiltin

	if _, active := d.activeToolSet[raw.ToolName]; !isClientExecutedBuiltin && !active {
		output := executionDeniedOutput(BuiltinToolFilteringDenialReason(raw.ToolName))
		if err := d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output}); err != nil {
			return false, err
		}
		return false, nil
	}

	if isClientExecutedBuiltin {
		d.recordPendingResult(raw)
		if d.shouldDeferPause() {
			d.pauseAfterStepToolCalls = true
			return false, nil
		}
		if err := d.pauseForHostInput(); err != nil {
			return false, err
		}
		return true, nil
	}

	hostCall := d.validatedHostCallsByID[raw.ToolCallID]
	if hostCall == nil {
		return false, fmt.Errorf("harness '%s' could not validate host tool '%s'", d.in.Harness.HarnessID(), raw.ToolName)
	}
	// Schema-invalid input is rejected here, before any approval decision is
	// made and without ever calling tool.Execute — mirrors TS's
	// `validatedHostToolCall.invalid` short-circuit, which runs ahead of
	// resolveCustomToolApproval.
	if hostCall.Invalid {
		return false, d.rejectInvalidHostToolCall(raw, call)
	}

	decision := ResolveCustomToolApproval(raw.ToolName, d.in.ToolApproval)
	switch decision.Type {
	case CustomToolApprovalDeny:
		approvalID := generateApprovalID()
		d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
			ApprovalID: approvalID, ToolCallID: raw.ToolCallID, ToolCall: call, IsAutomatic: true,
		}})
		d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{
			ApprovalID: approvalID, ToolCallID: raw.ToolCallID, ToolCall: call, Approved: false, Reason: decision.Reason,
		}})
		output := executionDeniedOutput(decision.Reason)
		if err := d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output}); err != nil {
			return false, err
		}
		return false, nil

	case CustomToolApprovalRequest:
		pending := d.pendingApprovalsByToolCallID[raw.ToolCallID]
		if pending == nil {
			pending = &PendingToolApproval{
				ApprovalID: generateApprovalID(), ToolCallID: raw.ToolCallID, ToolName: raw.ToolName,
				Input: raw.Input, Kind: PendingToolApprovalCustom, NativeName: raw.NativeName,
			}
			d.pendingApprovalsByApprovalID[pending.ApprovalID] = pending
			d.pendingApprovalsByToolCallID[pending.ToolCallID] = pending
		}
		if continuation, ok := d.continuationsByApprovalID[pending.ApprovalID]; ok {
			outcome, err := d.processApprovalContinuation(*pending, continuation)
			if err != nil {
				return false, err
			}
			return outcome == turnOutcomeAwaitingToolResult, nil
		}
		if d.in.OnPendingToolApproval != nil {
			d.in.OnPendingToolApproval(*pending)
		}
		d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
			ApprovalID: pending.ApprovalID, ToolCallID: pending.ToolCallID, ToolCall: call,
		}})
		if d.shouldDeferPause() {
			d.pauseAfterStepToolCalls = true
			return false, nil
		}
		if err := d.pauseForHostInput(); err != nil {
			return false, err
		}
		return true, nil

	default: // allow (not-applicable / approved)
		tool, executable := d.in.ActiveTools[raw.ToolName]
		if !executable || tool.Execute == nil {
			d.recordPendingResult(raw)
			if d.shouldDeferPause() {
				d.pauseAfterStepToolCalls = true
				return false, nil
			}
			if err := d.pauseForHostInput(); err != nil {
				return false, err
			}
			return true, nil
		}
		d.executeHostToolAsync(tool, raw, hostCall.Arguments)
		return false, nil
	}
}

// rejectInvalidHostToolCall surfaces a schema-invalid host tool call as a
// consumer-facing tool-error (buffered like every other tool-result chunk,
// flushed at the next step boundary) and settles it back to the harness
// runtime as an error result, without ever calling the tool's Execute.
// Mirrors TS's `validatedHostToolCall.invalid` branch (the generic
// invalidToolInputMessage on both sides; the detailed validation error stays
// local-only). The call's ID is marked settled so a later echoed
// tool-result/tool-approval-request event for it is replayed-and-skipped,
// same as every other host-settled call (see isSettled/isReplayable in
// consumeLoop).
func (d *turnDriver) rejectInvalidHostToolCall(raw *ToolCallPart, displayCall types.ToolCall) error {
	d.settledHostIDs[raw.ToolCallID] = struct{}{}
	d.bufferedResultChunks = append(d.bufferedResultChunks, []provider.StreamChunk{{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID: raw.ToolCallID,
			ToolName:   raw.ToolName,
			Input:      displayCall.Arguments,
			Error:      errors.New(invalidToolInputMessage),
			Dynamic:    true,
		},
	}})
	return d.control.SubmitToolResult(d.ctx, ToolResultSubmission{
		ToolCallID: raw.ToolCallID,
		Output:     map[string]interface{}{"error": invalidToolInputMessage},
		IsError:    true,
	})
}

// recordPendingResult parks a client-side (non-executable) or client-
// answered (askUserQuestions) tool call as a pending result the caller must
// supply via ContinueStream's ToolResultContinuations.
func (d *turnDriver) recordPendingResult(raw *ToolCallPart) {
	pending := &PendingToolResult{ToolCallID: raw.ToolCallID, ToolName: raw.ToolName, Input: raw.Input}
	if len(raw.ProviderMetadata) > 0 {
		pending.ProviderOptions = map[string]map[string]any(raw.ProviderMetadata)
	}
	d.pendingResultsByToolCallID[raw.ToolCallID] = pending
	if d.in.OnPendingToolResult != nil {
		d.in.OnPendingToolResult(*pending)
	}
}

// executeHostToolAsync runs tool.Execute in its own goroutine (so
// independent host tool calls within a step run concurrently, matching TS
// row 8d717b3) and submits the result back to the harness when it completes.
// Joined at the next step boundary via joinOutstandingExecutions. execArgs is
// the schema-validated, *unstripped* input (types.ToolCall.Arguments from
// classifyAndValidateToolCall's hostCall) — the tool always executes with the
// real absolute paths a schema may require, never the work-dir-stripped
// display value. Mirrors TS `maybeExecuteHostTool`'s
// `input.parsedToolCall.input` (the raw-validated `validatedHostToolCall`,
// not the display `parsedToolCall`).
func (d *turnDriver) executeHostToolAsync(tool types.Tool, raw *ToolCallPart, execArgs map[string]interface{}) {
	// Snapshotted here, synchronously in the caller's (turnDriver.run's)
	// goroutine, rather than read as d.telStepCtx from inside the spawned
	// goroutine below: d.telStepCtx is mutated by the main goroutine at
	// later step boundaries (telStepStart/telStepEnd), and a host tool
	// execution can outlive the step it started in until
	// joinOutstandingExecutions catches up, so reading the live field from
	// the exec goroutine would be a data race.
	execTelCtx := d.telStepCtx
	if execTelCtx == nil {
		execTelCtx = d.ctx
	}
	d.execWG.Add(1)
	go func() {
		defer d.execWG.Done()
		start := time.Now()
		var toolCtx interface{}
		if ctxValue, ok := d.in.ToolsContext[raw.ToolName]; ok {
			toolCtx = ctxValue
		}
		if tool.ContextSchema != nil {
			// Apply schema defaults before validating (matching
			// pkg/agent/toolloop.go's validateAgentToolContext and
			// pkg/ai/tool_approval.go's identical check): a context
			// field missing a schema default must not fail validation,
			// and the tool sees the defaulted value.
			normalizedToolCtx := schema.ApplyDefaults(toolCtx, tool.ContextSchema)
			if err := tool.ContextSchema.Validator().Validate(normalizedToolCtx); err != nil {
				_ = d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": "Tool context validation failed."}, IsError: true})
				d.recordExecError(raw.ToolCallID, err)
				return
			}
			toolCtx = normalizedToolCtx
		}
		// Wrapped by telemetry.FireExecuteToolWithSettings so integrations
		// can create nested spans for tool -> generateText chains (TS
		// 59a2306's `wrappedExecuteTool`, matching pkg/ai/generate.go's own
		// tool-loop wrapping of the same call).
		output, err := telemetry.FireExecuteToolWithSettings(execTelCtx, d.in.Telemetry, raw.ToolName, execArgs,
			func(execCtx context.Context, args map[string]interface{}) (interface{}, error) {
				return tool.Execute(execCtx, args, types.ToolExecutionOptions{
					ToolCallID: raw.ToolCallID, RuntimeContext: d.in.RuntimeContext, ToolContext: toolCtx,
					ExperimentalSandbox: d.in.SandboxSession,
				})
			})
		d.execMu.Lock()
		d.toolExecMs[raw.ToolCallID] = time.Since(start).Milliseconds()
		d.execMu.Unlock()
		if err != nil {
			_ = d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": err.Error()}, IsError: true})
			return
		}
		_ = d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output})
	}()
}

func (d *turnDriver) recordExecError(toolCallID string, err error) {
	d.execMu.Lock()
	defer d.execMu.Unlock()
	if d.execErr == nil {
		d.execErr = err
	}
}

// approvalAsToolCallPart reconstructs the ToolCallPart shape of a pending
// approval's originating tool call from the approval record itself. Used
// when no runtime event for it exists in this turn (a resumed/suspended
// turn's startup continuation) and, for a custom approval, unconditionally —
// mirrors TS's `rawToolCall` fallback object literal in
// `processPendingApprovalContinuation`, which only prefers a turn-local raw
// event (`rawToolCallsByToolCallId`) for a *builtin* approval; a custom
// approval always reconstructs from `approval.input`, the unstripped input
// captured when the approval was first recorded (handleApprovalRequest /
// handleHostToolCall).
func approvalAsToolCallPart(approval PendingToolApproval) ToolCallPart {
	providerExecuted := false
	if approval.ProviderExecuted != nil {
		providerExecuted = *approval.ProviderExecuted
	}
	return ToolCallPart{
		ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input,
		ProviderExecuted: providerExecuted, NativeName: approval.NativeName,
	}
}

// processApprovalContinuation submits a resolved approval decision back to
// the harness (builtin via PromptControl.SubmitToolApproval, custom via a
// denial/execution result), mirroring TS `processPendingApprovalContinuation`.
//
// A custom approval that was just approved is *revalidated* against its raw
// (unstripped) input here — the approval's Input was only ever JSON-parsed,
// never schema-validated, when it was first recorded, and an approval
// continuation can equally arrive for a resumed/suspended turn where no
// earlier validation happened in this process at all. Mirrors TS's
// `validateToolCall({ event: rawToolCall, tools: input.tools })` re-run
// inside `processPendingApprovalContinuation`.
func (d *turnDriver) processApprovalContinuation(approval PendingToolApproval, continuation types.ToolApprovalResponseContent) (turnOutcome, error) {
	rawToolCall := approvalAsToolCallPart(approval)
	if approval.Kind == PendingToolApprovalBuiltin {
		if r := d.execToolCallsByID[approval.ToolCallID]; r != nil {
			rawToolCall = *r
		}
	}

	// validatedHostToolCall carries the unstripped, schema-validated
	// Arguments actually used for execution below; call is always the
	// consumer-facing (display) tool call embedded in the
	// tool-approval-response chunk. Only set for a just-approved custom
	// approval — mirrors TS's `validatedToolCall`/`toolCall` split.
	var validatedHostToolCall *types.ToolCall
	var call types.ToolCall
	if approval.Kind == PendingToolApprovalCustom && continuation.Approved {
		parsed, perr := ai.ParseToolCall(d.ctx, ai.ParseToolCallOptions{
			ToolCall: toolCallPartToCall(&rawToolCall),
			Tools:    d.toolsList,
		})
		if perr != nil {
			return turnOutcomeContinue, perr
		}
		vc := parsed
		validatedHostToolCall = &vc
		call = displayHostToolCall(parsed, d.in.SessionWorkDir)
	} else {
		// No schema validation for a denial or a builtin approval: neither
		// executes through this path, so only a plain JSON parse (for
		// display) is needed — mirrors TS's `safeParseJSON`-only else
		// branch.
		args, _ := parseJSONObject(rawToolCall.Input)
		call = types.ToolCall{ID: rawToolCall.ToolCallID, ToolName: rawToolCall.ToolName, Arguments: args, ProviderExecuted: rawToolCall.ProviderExecuted}
	}

	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{
		ApprovalID: approval.ApprovalID, ToolCallID: approval.ToolCallID, ToolCall: call,
		Approved: continuation.Approved, Reason: continuation.Reason,
	}})
	if d.in.OnToolApprovalSettled != nil {
		d.in.OnToolApprovalSettled(approval.ApprovalID)
	}
	delete(d.pendingApprovalsByApprovalID, approval.ApprovalID)
	delete(d.pendingApprovalsByToolCallID, approval.ToolCallID)

	if approval.Kind == PendingToolApprovalBuiltin {
		d.settledBuiltinIDs[approval.ToolCallID] = struct{}{}
		submitter, ok := d.control.(ToolApprovalSubmitter)
		if !ok {
			return turnOutcomeContinue, fmt.Errorf("harness '%s' emitted a built-in tool approval request but does not support approval responses", d.in.Harness.HarnessID())
		}
		if err := submitter.SubmitToolApproval(d.ctx, ToolApprovalSubmission{ApprovalID: approval.ApprovalID, Approved: continuation.Approved, Reason: continuation.Reason}); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeContinue, nil
	}

	d.settledHostIDs[approval.ToolCallID] = struct{}{}
	if !continuation.Approved {
		output := executionDeniedOutput(continuation.Reason)
		if err := d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: approval.ToolCallID, Output: output}); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeContinue, nil
	}

	if validatedHostToolCall == nil {
		return turnOutcomeContinue, fmt.Errorf("harness '%s' could not validate approved host tool '%s'", d.in.Harness.HarnessID(), approval.ToolName)
	}
	// Revalidation rejected the input: settle as a tool-error, exactly like
	// a first-pass invalid host tool call (rejectInvalidHostToolCall),
	// without ever calling tool.Execute. Mirrors TS's
	// `validatedToolCall.invalid` branch here.
	if validatedHostToolCall.Invalid {
		if err := d.control.SubmitToolResult(d.ctx, ToolResultSubmission{
			ToolCallID: approval.ToolCallID,
			Output:     map[string]interface{}{"error": invalidToolInputMessage},
			IsError:    true,
		}); err != nil {
			return turnOutcomeContinue, err
		}
		d.bufferedResultChunks = append(d.bufferedResultChunks, []provider.StreamChunk{{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: approval.ToolCallID, ToolName: approval.ToolName,
				Input: call.Arguments, Error: errors.New(invalidToolInputMessage), Dynamic: true,
			},
		}})
		return turnOutcomeContinue, nil
	}

	tool, executable := d.in.ActiveTools[approval.ToolName]
	if !executable || tool.Execute == nil {
		d.recordPendingResult(&ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input})
		if err := d.pauseForHostInput(); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeAwaitingToolResult, nil
	}
	d.executeHostToolAsync(tool, &ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input}, validatedHostToolCall.Arguments)
	return turnOutcomeContinue, nil
}

// processResultContinuation submits a client-supplied tool result (or a
// result the caller already recorded on suspend) back to the harness.
// Mirrors TS `processPendingToolResultContinuation`.
func (d *turnDriver) processResultContinuation(pending PendingToolResult, continuation *types.ToolResultContent) error {
	var submission ToolResultSubmission
	submission.ToolCallID = pending.ToolCallID
	switch {
	case continuation != nil:
		output, isError := unwrapToolResultOutput(*continuation)
		submission.Output = output
		submission.IsError = isError
		submission.ToolResult = continuation
	case pending.CompletedResult != nil:
		submission.Output = pending.CompletedResult.Output
		if pending.CompletedResult.IsError != nil {
			submission.IsError = *pending.CompletedResult.IsError
		}
		submission.ToolResult = pending.CompletedResult.ToolResult
	default:
		return nil
	}

	d.settledHostIDs[pending.ToolCallID] = struct{}{}
	if d.in.OnToolResultSettled != nil {
		d.in.OnToolResultSettled(pending.ToolCallID)
	}
	if err := d.control.SubmitToolResult(d.ctx, submission); err != nil {
		return err
	}
	delete(d.pendingResultsByToolCallID, pending.ToolCallID)
	return nil
}

// unwrapToolResultOutput extracts the plain output value (and whether it
// represents an error) from a client-supplied tool-result content part.
// Mirrors TS `unwrapToolResultOutput`.
func unwrapToolResultOutput(result types.ToolResultContent) (output interface{}, isError bool) {
	if result.Output == nil {
		if result.Error != "" {
			return result.Error, true
		}
		return result.Result, false
	}
	switch result.Output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputJSON:
		return result.Output.Value, false
	case types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON, types.ToolResultOutputError:
		return result.Output.Value, true
	case types.ToolResultOutputExecutionDenied:
		return map[string]interface{}{"type": string(result.Output.Type), "reason": result.Output.Reason}, false
	case types.ToolResultOutputContent:
		return result.Output, false
	default:
		return result.Result, false
	}
}

func generateApprovalID() string { return newID() }
