package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
const invalidToolInputMessage = "Tool input validation failed." //nolint:staticcheck // exact TS parity string (run-prompt.ts's invalidToolInputMessage)

// hostToolOutcome is the result of running a host tool: its raw output, or
// the error it failed with. Mirrors TS `HostToolOutcome`.
type hostToolOutcome struct {
	output interface{}
	err    error
}

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
const unclosedStepErrorMessage = "HarnessAgent: received terminal finish with unclosed step content. Harness adapters must emit `finish-step` before `finish`." //nolint:staticcheck // exact TS parity string (harness-stream-text-result.ts)

// chunkChannelStream is a provider.TextStream backed by a Go channel, used to
// feed ai.NewStreamTextResultFromParts from run_prompt's own translation
// loop. Plays the role TS's `toHarnessStream` ReadableStream plays, adapted
// to Go's pull-based provider.TextStream contract.
type chunkChannelStream struct {
	ch  chan provider.StreamChunk
	err error

	// asyncMu/closed guard pushAsync (host tool goroutines) against a close
	// by the driver goroutine.
	asyncMu sync.Mutex
	closed  bool
}

func newChunkChannelStream() *chunkChannelStream {
	return &chunkChannelStream{ch: make(chan provider.StreamChunk, 32)}
}

func (s *chunkChannelStream) push(c provider.StreamChunk) { s.ch <- c }

func (s *chunkChannelStream) closeOK() {
	s.asyncMu.Lock()
	defer s.asyncMu.Unlock()
	s.closed = true
	close(s.ch)
}

// pushAsync is push for goroutines other than the driver (host tool
// executions): it drops the chunk once the stream is closed or ctx is done
// instead of panicking or blocking forever.
func (s *chunkChannelStream) pushAsync(ctx context.Context, c provider.StreamChunk) {
	s.asyncMu.Lock()
	defer s.asyncMu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- c:
	case <-ctx.Done():
	}
}

// stepResultTimeout bounds how long completeStep waits for the result's
// consuming goroutine to report a closed step before falling back to the
// driver's own view of it.
const stepResultTimeout = 10 * time.Second

// stepFinishEvent builds the OnStepEnd callback event from a finished step.
func stepFinishEvent(callID string, step types.StepResult) ai.OnStepFinishEvent {
	return ai.OnStepFinishEvent{
		CallID: callID, StepNumber: step.StepNumber, Model: step.Model,
		ModelProvider: step.Model.Provider, ModelID: step.Model.ModelID,
		Text: step.Text, Reasoning: step.Reasoning, ReasoningText: step.ReasoningText,
		ToolCalls: step.ToolCalls, StaticToolCalls: step.StaticToolCalls, DynamicToolCalls: step.DynamicToolCalls,
		ToolResults: step.ToolResults, StaticToolResults: step.StaticToolResults, DynamicToolResults: step.DynamicToolResults,
		FinishReason: step.FinishReason, RawFinishReason: step.RawFinishReason, Usage: step.Usage,
		Warnings: step.Warnings, Sources: step.Sources, Files: step.Files,
		ProviderMetadata: step.ProviderMetadata,
		ResponseHeaders:  step.Response.Headers,
		Request:          ai.GenerateStepRequest{Body: step.Request.Body},
		Response: ai.GenerateStepResponse{
			ID: step.Response.ID, Timestamp: step.Response.Timestamp, ModelID: step.Response.ModelID,
			Headers: step.Response.Headers, Messages: step.Response.Messages, Body: step.Response.Body,
		},
		RuntimeContext: step.RuntimeContext, ToolsContext: step.ToolsContext,
	}
}

// stepSink carries steps closed by the result's consuming goroutine back to the
// driver: completeStep registers a waiter, pushes the finish-step chunk, and
// receives the finished StepResult the result itself built (the one Steps()
// reports), as TS `result.finishStep()` returns it to `lifecycle.stepEnd`.
type stepSink struct {
	mu      sync.Mutex
	waiting chan types.StepResult
}

// expect registers a waiter for the next closed step.
func (s *stepSink) expect() <-chan types.StepResult {
	ch := make(chan types.StepResult, 1)
	s.mu.Lock()
	s.waiting = ch
	s.mu.Unlock()
	return ch
}

// deliver is ExternalStreamOptions.OnStep.
func (s *stepSink) deliver(step types.StepResult) {
	s.mu.Lock()
	ch := s.waiting
	s.waiting = nil
	s.mu.Unlock()
	if ch != nil {
		ch <- step
	}
}

// suspending reports whether the host is suspending this turn (see
// runPromptInput.IsTurnSuspending).
func (d *turnDriver) suspending() bool {
	return d.in.IsTurnSuspending != nil && d.in.IsTurnSuspending()
}

// fail settles the turn as a soft (data-preserving) failure: a
// ChunkTypeError chunk carrying msg, then a normal channel close. Mirrors TS
// `result.fail(err)` (settleFailure's non-abort branch), which always
// surfaces failures — a harness `error` part, a host tool execution
// exception, an unclosed-step protocol violation — as a terminal `error`
// stream part rather than rejecting/discarding whatever the turn already
// produced. Every runPrompt failure path settles through this single helper
// (via settleFailure below), so a separate hard, non-data-preserving
// channel-level failure path is never needed.
func (d *turnDriver) fail(err error) {
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeError, Err: err, Text: err.Error()})
	d.stream.closeOK()
}

// abort settles the turn as a caller-initiated stop: a ChunkTypeAbort chunk
// carrying the ctx's cancellation reason (falling back to err), then a
// normal channel close — instead of the ChunkTypeError chunk `fail` would
// push. Mirrors TS `HarnessStreamTextResult.abort()` (TS 86a84c9).
func (d *turnDriver) abort(err error) {
	reason := ""
	if ctxErr := d.ctx.Err(); ctxErr != nil {
		// TS reports getErrorMessage(abortSignal.reason): the caller's own
		// cancellation cause when one was given.
		if cause := context.Cause(d.ctx); cause != nil {
			ctxErr = cause
		}
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
	if d.in.OnTurnFailed != nil && !d.suspending() {
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
	Telemetry *telemetry.Options

	Callbacks      Callbacks
	StopConditions []ai.StopCondition
	ToolApproval   ToolApprovalConfiguration
	// ToolApprovalReasons are the per-tool reasons for ToolApproval entries.
	ToolApprovalReasons map[string]string

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

	// IsTurnSuspending reports that the adapter stream closed because the
	// host intentionally suspended the still-running turn (a workflow slice
	// boundary or AgentSession.SuspendTurn). While it returns true the driver
	// does not report the turn as finished or failed, does not submit tool
	// results to the (closed) runtime, and records host tool results that
	// complete after the suspension as completed pending results so a
	// continuation can submit them. Mirrors TS `runPrompt`'s
	// `isTurnSuspending`.
	IsTurnSuspending func() bool

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
	steps := &stepSink{}
	result := ai.NewStreamTextResultFromParts(context.WithoutCancel(ctx), stream, ai.ExternalStreamOptions{
		CallID:         callID,
		Provider:       "harness:" + in.Harness.HarnessID(),
		ModelID:        in.Model,
		Output:         in.Output,
		RuntimeContext: in.RuntimeContext,
		ToolsContext:   in.ToolsContext,
		OnStep:         steps.deliver,

		TerminalFinishClosesTurn: true,
	})

	done := make(chan struct{})
	in.Callbacks = in.Callbacks.panicSafe()
	d := &turnDriver{ctx: ctx, in: in, stream: stream, telCallID: callID, steps: steps}
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

	// steps hands the driver the StepResult the result built for each
	// finish-step chunk it pushed (see completeStep).
	steps *stepSink

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
	// stepApprovalRequests mirrors TS stepApprovalRequests: approval-request
	// parts enqueued this step, reported in onLanguageModelCallEnd content.
	stepApprovalRequests []types.ToolApprovalRequestContent
	toolExecMs           map[string]int64
	// hostOutcomes keeps the host-side outcome of each executed host tool
	// (raw output or the original error), so tool-execution-end callbacks
	// and telemetry report it rather than the runtime's echoed, stringified
	// copy (TS `toolExecution.toolOutput = toToolOutput(outcome)`). Guarded
	// by execMu.
	hostOutcomes map[string]hostToolOutcome
	stepNumber   int
	stepOpen     bool
	startCalled  bool

	// hostInputPauseRequested/deferredPauseFinishStep/ignoredFinishStep/
	// hostToolCallIDsAwaitingEcho implement TS #23260's parallel/sequential
	// approval handling. An approval request or a pending client tool result
	// does not end the response right away: the rest of the step (other tool
	// calls, native results, host tool executions) still has to be surfaced.
	// The pause happens at the next `finish-step`, once every host tool
	// started in this response has been echoed back by the runtime.
	// Replaces the earlier adapter-reported ToolCallPart.StepToolCallCount
	// grouping (TS 32349cc), which adapters no longer need to populate.
	//
	// ignoredFinishStep is a finish-step that arrived while host tools
	// started in this step had not been echoed back yet: the adapter is
	// signalling that the runtime waits on host input, not the end of the
	// step. It is remembered so the step can still be closed if the turn
	// finishes without another finish-step.
	hostInputPauseRequested     bool
	deferredPauseFinishStep     *FinishStepPart
	ignoredFinishStep           *FinishStepPart
	hostToolCallIDsAwaitingEcho map[string]struct{} // guarded by execMu

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

	// startedToolExecutions backs ensureToolExecutionStarted's idempotent
	// ai.Callbacks.OnToolExecutionStart AND telemetry's own toolExecutionStart
	// (TS #21696 "Start/end callbacks and fast tool results were delayed
	// behind slower tools in the same step"): it may be called once per tool
	// call from the main goroutine at any of several points — synchronously
	// in executeHostToolAsync just before a host tool's exec goroutine is
	// spawned, when a provider-executed tool call is observed, or (as a
	// fallback) when a tool-result arrives for a call that never got an
	// explicit early start (e.g. a client-submitted result) — and must fire
	// the start callback exactly once regardless of which call site gets
	// there first. Deliberately always called from the main goroutine, never
	// from inside a host tool's own exec goroutine — see
	// executeHostToolAsync's doc for why firing it there would race a
	// step's own telemetry span lifetime. Guarded by execMu like every
	// other field a host tool execution touches.
	startedToolExecutions map[string]bool
	// endedToolExecutions makes publishToolExecutionEnd idempotent per tool
	// call (TS `execution.endNotification ??= ...`): an automatic denial
	// publishes its end right after the denial is submitted, and the
	// runtime's later echo of that result must not publish it again. Guarded
	// by execMu.
	endedToolExecutions map[string]bool

	// telToolCtx stores, per ToolCallID, the ctx returned by telemetry's
	// toolExecutionStart event (see ensureToolExecutionStarted), so the
	// later toolExecutionEnd event (telToolExecutionEnd, always fired from
	// recordToolResult on the main goroutine) threads the exact span ctx
	// that start opened — mirroring TS's `toolExecutions` map, which holds
	// each tool's `startNotification` the same way. Guarded by execMu.
	telToolCtx map[string]context.Context

	// toolModelOutputs caches the normalized model-facing output of each
	// submitted tool result by toolCallId, so the corresponding tool-result
	// chunk (and therefore ResponseMessages) reports it instead of the raw
	// result (TS #22367 `toolModelOutputs`). Guarded by execMu.
	toolModelOutputs map[string]types.ToolResultOutput

	// callInfo remembers the name and raw input of every tool call the turn
	// has seen (or resumed with), so a result that completes after the host
	// suspended the turn can be stored as a completed pending result. Guarded
	// by execMu. Mirrors TS `rawToolCallsByToolCallId` /
	// `pendingResultsByToolCallId` / `pendingToolApprovals` lookups in
	// `submitToolResult`.
	callInfo map[string]suspendedCallInfo
	// suspendedOutcomes are host tool outcomes that completed while the turn
	// was suspending; run publishes them as tool-result parts (TS
	// `bufferedToolOutcomes` / `enqueueHostToolOutcome`). Guarded by execMu.
	suspendedOutcomes []*ToolResultPart

	closingResumedStep bool

	// streamErrorLogged is set once a harness `error` part has been written to
	// stderr, so run() does not log the same failure a second time.
	streamErrorLogged bool

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
	telCallID string
	// modelID is the model id telemetry and callback events report: the
	// requested model, or the runtime-resolved one a `stream-start` part
	// carried before the turn's first start event (TS lifecycle `modelId`).
	// respModelID is the id the result's steps and response report (TS
	// `result.setModelId`, accepted only until the first step completes).
	modelID               string
	respModelID           string
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

type suspendedCallInfo struct {
	name  string
	input string
	// parsed is true for a tool call observed on this turn's stream (as
	// opposed to one restored from pending lifecycle state).
	parsed bool
}

// rememberCall records the name and raw input of a tool call for
// submitToolResult's suspended path. Safe from any goroutine.
func (d *turnDriver) rememberCall(toolCallID, toolName, input string, parsed bool) {
	d.execMu.Lock()
	defer d.execMu.Unlock()
	if d.callInfo == nil {
		d.callInfo = map[string]suspendedCallInfo{}
	}
	d.callInfo[toolCallID] = suspendedCallInfo{name: toolName, input: input, parsed: parsed}
}

// turnErrorWrite replaces the stderr sink of turn failure logs (tests only).
var turnErrorWrite func(line string)

// logError writes a turn failure to stderr the way TS runPrompt's
// `logBridgeError` calls do.
func (d *turnDriver) logError(context string, err any) {
	LogBridgeError(LogBridgeErrorOptions{
		Write:     turnErrorWrite,
		HarnessID: d.in.Harness.HarnessID(),
		SessionID: d.in.Session.SessionID(),
		Context:   context,
		Error:     err,
	})
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
	d.hostOutcomes = map[string]hostToolOutcome{}
	d.startedToolExecutions = map[string]bool{}
	d.endedToolExecutions = map[string]bool{}
	d.telToolCtx = map[string]context.Context{}
	d.stripToolInput = NewToolInputWorkDirStripper(d.in.SessionWorkDir)
	d.modelID = d.in.Model
	d.respModelID = d.in.Model

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
		d.rememberCall(a.ToolCallID, a.ToolName, a.Input, false)
	}
	for _, r := range d.in.PendingToolResults {
		r := r
		d.pendingResultsByToolCallID[r.ToolCallID] = &r
		d.rememberCall(r.ToolCallID, r.ToolName, r.Input, false)
	}
	for _, c := range d.in.ToolApprovalContinuations {
		d.continuationsByApprovalID[c.ApprovalID] = c
	}
	for _, c := range d.in.ToolResultContinuations {
		d.continuationsByToolCallID[c.ToolCallID] = c
	}

	// partsCh plays the role of TS `toHarnessStream`'s ReadableStream: every
	// emit becomes a part, the stream closes when the adapter's control is
	// done, and a failed control surfaces as a trailing `error` part. emitMu
	// and partsClosed make an emit after that close a no-op (TS safeEnqueue)
	// instead of a send on a closed channel; exited unblocks a blocked send
	// once this driver has returned.
	partsCh := make(chan StreamPart, 64)
	exited := make(chan struct{})
	defer close(exited)
	var emitMu sync.Mutex
	partsClosed := false
	send := func(part StreamPart) {
		select {
		case partsCh <- part:
		case <-d.ctx.Done():
		case <-exited:
		}
	}
	emit := func(part StreamPart) {
		emitMu.Lock()
		defer emitMu.Unlock()
		if partsClosed {
			return
		}
		send(part)
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
		d.logError("failed to start harness turn", err)
		d.settleFailure(err)
		return
	}
	d.control = control
	if d.in.OnPromptControlAvailable != nil {
		d.in.OnPromptControlAvailable(control)
	}

	go func() {
		select {
		case <-control.Done():
		case <-exited:
			// The driver returned before the control settled (a pause for host
			// input): nothing reads partsCh any more.
			return
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		if err := control.Err(); err != nil {
			send(&ErrorPart{Error: err})
		}
		partsClosed = true
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
		// A harness `error` part already logged itself ("harness stream
		// error"); the caller's own cancellation is not a malfunction. Every
		// other failure is "harness turn failed" (TS run-prompt.ts catch).
		if !d.streamErrorLogged && (d.ctx.Err() == nil || !errors.Is(turnErr, d.ctx.Err())) {
			d.logError("harness turn failed", turnErr)
		}
		d.settleFailure(turnErr)
		return
	}
	if d.suspending() {
		// Mirrors TS runPrompt's `isTurnSuspending` branch: neither
		// onTurnFinished nor onTurnFailed is reported (the turn stays
		// unfinished for a later continuation), host tool outcomes that
		// completed during the suspension are published, and a step that
		// never reached its finish-step is dropped. Its partial content
		// stays in the bridge replay log for the next slice.
		if err := d.joinOutstandingExecutions(); err != nil {
			d.settleFailure(err)
			return
		}
		d.publishSuspendedOutcomes()
		// Results buffered for the step (e.g. a rejected host call) reach the
		// consumer's stream even though the step itself is dropped.
		d.flushBufferedResultChunks()
		if !finished {
			d.discardCurrentStep()
		}
	} else if finished {
		if d.in.OnTurnFinished != nil {
			d.in.OnTurnFinished()
		}
	} else {
		// The adapter's stream ended without a terminal `finish`: TS calls
		// onTurnFailed and then `result.finish(undefined)`, which fails the
		// result only when step content is still unclosed and otherwise
		// closes it with the last step's finish reason and the summed usage.
		if d.in.OnTurnFailed != nil {
			d.in.OnTurnFailed()
		}
		if d.stepOpen {
			d.telError(errors.New(unclosedStepErrorMessage)) //nolint:staticcheck // exact TS parity string
			d.fail(errors.New(unclosedStepErrorMessage))     //nolint:staticcheck // exact TS parity string
			return
		}
		reason := types.FinishReason("other")
		raw := ""
		if n := len(d.completedSteps); n > 0 {
			reason = d.completedSteps[n-1].FinishReason
			raw = d.completedSteps[n-1].RawFinishReason
		}
		d.pendingFinishChunk = &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: reason, RawFinishReason: raw}
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
		// A pause requested earlier is waiting on the echo of host tool
		// results started in this response; once they have all arrived,
		// pause at the finish-step that was deferred (TS #23260).
		if d.deferredPauseFinishStep != nil && d.awaitingEchoCount() == 0 {
			if err := d.pauseAtHostInputBoundary(d.deferredPauseFinishStep); err != nil {
				return false, false, err
			}
			return false, true, nil
		}

		var part StreamPart
		var ok bool
		select {
		case part, ok = <-partsCh:
		case <-d.ctx.Done():
			// Join outstanding host tool executions before returning, exactly
			// like every other error/exit path in this loop (ErrorPart,
			// invalid tool-call, finish-step, finish) — this is the one path
			// that read d.ctx.Done() directly instead of discovering the
			// cancellation through a stream part, so it used to skip the
			// join entirely. Without it, run()'s caller (Agent.startTurn via
			// OnTurnFailed) settles the turn and flips the session back to
			// idle while a host tool.Execute goroutine started by
			// executeHostToolAsync is still running: a second Generate/
			// Stream call could then start a new turn on the same session
			// while that stale goroutine is still alive, and its eventual
			// SubmitToolResult call would land on a turn it no longer
			// belongs to. Mirrors TS run-prompt.ts, where every exit from the
			// read loop — including one driven by the caller's own
			// abortSignal, which surfaces as an `error` part from the
			// harness's own stream rather than a side-channel signal — goes
			// through `await waitForOutstandingHostToolExecutions()` first.
			// The join error is intentionally discarded (as in the other
			// `_ = d.joinOutstandingExecutions()` sites): the ctx
			// cancellation is the turn's real, already-decided outcome.
			_ = d.joinOutstandingExecutions()
			return false, false, d.ctx.Err()
		}
		if !ok {
			d.releasePendingStopBoundary()
			suspending := d.suspending()
			if d.hostInputPauseRequested && !finished && !suspending {
				// The adapter ended the response without a closing
				// finish-step/finish: pause for the requested host input.
				if err := d.pauseAtHostInputBoundary(d.deferredPauseFinishStep); err != nil {
					return false, false, err
				}
				return false, true, nil
			}
			if !finished && suspending {
				// The host suspended the turn at a slice boundary and the
				// adapter closed its stream: a clean end, not a failure.
				// run() publishes the host tool outcomes that completed
				// meanwhile and discards the unfinished step.
				return false, false, nil
			}
			if !finished && d.ctx.Err() != nil {
				// The caller's ctx is already cancelled: emits racing the
				// cancellation may have been dropped (send selects on
				// ctx.Done), so an empty/short stream is the abort, not a
				// normal end. Settle it as one deterministically.
				_ = d.joinOutstandingExecutions()
				return false, false, d.ctx.Err()
			}
			if !finished {
				// The adapter ended the turn (closed its stream) without
				// ever emitting `finish`. TS reports onTurnFailed and then
				// finishes the result normally (`result.finish(undefined)`);
				// run() does the same.
				return false, false, nil
			}
			return true, false, nil
		}

		// `raw` is out-of-band adapter data: it must not open a step (and so a
		// telemetry span), become part of one, or trigger/consume a pending
		// stop-condition check, so it is passed straight through before any
		// of that. Mirrors TS run-prompt.ts's `value.type === 'raw'` early
		// `result.enqueueRaw(...)` branch (TS #22258).
		if rp, isRaw := part.(*RawPart); isRaw {
			for _, c := range TranslatePart(rp, TranslateOptions{}) {
				d.stream.push(c)
			}
			continue
		}

		if tr, isResult := part.(*ToolResultPart); isResult {
			d.clearAwaitingEcho(tr.ToolCallID)
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
				d.suspendOrFinishNow(psb.finishReason)
				return false, true, nil
			} else {
				psb.release()
			}
		}

		if sp, isStart := part.(*StreamStartPart); isStart {
			// TS: `modelId = value.modelId ?? input.model`; the result takes it
			// only while no step has completed, the lifecycle only on its first
			// start.
			if resolved := sp.ModelID; resolved != "" {
				if len(d.completedSteps) == 0 {
					d.respModelID = resolved
				}
				if !d.startCalled {
					d.modelID = resolved
				}
			}
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

		if fs, isFinishStep := display.(*FinishStepPart); isFinishStep {
			if d.hostInputPauseRequested {
				if err := d.joinOutstandingExecutions(); err != nil {
					return false, false, err
				}
				if d.awaitingEchoCount() == 0 {
					if err := d.pauseAtHostInputBoundary(fs); err != nil {
						return false, false, err
					}
					return false, true, nil
				}
				d.deferredPauseFinishStep = fs
				continue
			}
			if d.awaitingEchoCount() > 0 {
				d.ignoredFinishStep = fs
				continue
			}
			d.ignoredFinishStep = nil
		}
		if _, isFinish := display.(*FinishPart); isFinish {
			if d.hostInputPauseRequested {
				if err := d.pauseAtHostInputBoundary(d.deferredPauseFinishStep); err != nil {
					return false, false, err
				}
				return false, true, nil
			}
			if ignored := d.ignoredFinishStep; ignored != nil {
				d.ignoredFinishStep = nil
				if err := d.joinOutstandingExecutions(); err != nil {
					return false, false, err
				}
				if d.closingResumedStep {
					d.closingResumedStep = false
					d.flushBufferedResultChunks()
					d.resetStepAccum()
					d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeDiscardStep})
				} else {
					d.ensureStepOpen()
					d.completeStep(ignored.FinishReason, ignored.Usage, ignored.HarnessMetadata)
				}
			}
		}

		if _, isFinishStep := display.(*FinishStepPart); isFinishStep && d.closingResumedStep {
			d.closingResumedStep = false
			if err := d.joinOutstandingExecutions(); err != nil {
				return false, false, err
			}
			d.flushBufferedResultChunks()
			d.resetStepAccum()
			d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeDiscardStep})
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
			// A failed host tool execution is rethrown here, masking the
			// harness error, exactly like TS (`await
			// waitForOutstandingHostToolExecutions()` throws into the turn's
			// catch, which settles with the join error).
			if err := d.joinOutstandingExecutions(); err != nil {
				return false, false, err
			}
			// Stderr diagnostics keep the raw error (absolute paths help
			// debugging); a turn the caller itself cancelled ends with an
			// error-shaped part by construction and is not diagnosed.
			if d.ctx.Err() == nil {
				rawErr := ep.Error
				if rp, ok := part.(*ErrorPart); ok {
					rawErr = rp.Error
				}
				d.logError("harness stream error", rawErr)
			}
			d.streamErrorLogged = true
			if typed, ok := ep.Error.(error); ok {
				// Keep the typed error so errors.As works on the result's Err().
				return false, false, typed
			}
			return false, false, ep.AsError()
		}

		if tc, isCall := display.(*ToolCallPart); isCall {
			rawTC := part.(*ToolCallPart)
			d.execToolCallsByID[tc.ToolCallID] = rawTC
			d.rememberCall(rawTC.ToolCallID, rawTC.ToolName, rawTC.Input, true)
			displayCall, hostCall, verr := d.classifyAndValidateToolCall(rawTC, tc)
			if verr != nil {
				_ = d.joinOutstandingExecutions()
				return false, false, verr
			}
			d.toolCallsByID[tc.ToolCallID] = displayCall
			d.validatedHostCallsByID[tc.ToolCallID] = hostCall
			d.providerExecByID[tc.ToolCallID] = tc.ProviderExecuted
			if tc.ProviderExecuted {
				// TS #21696: a provider-executed tool call has no host
				// exec goroutine to fire the early start from — the
				// runtime is already executing it by the time this event
				// arrives — so fire it here instead of waiting for
				// recordToolResult's end-of-execution fallback. Runs on
				// the main goroutine, so reading d.telStepCtx live is
				// safe here.
				d.ensureToolExecutionStarted(d.telStepCtx, displayCall)
			}
			d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &displayCall})
			d.stepToolCalls = append(d.stepToolCalls, displayCall)
		} else if tr, isResult := display.(*ToolResultPart); isResult {
			// TS #21696: publish per tool as each result arrives instead of
			// buffering until finish-step, so a fast tool's result and its
			// OnToolExecutionEnd callback are no longer held back by a
			// slower sibling tool in the same step. recordToolResult (which
			// fires OnToolExecutionEnd) runs before the chunk is pushed,
			// matching TS's "End callbacks precede streamed results."
			chunks := TranslatePart(tr, d.translateOpts())
			d.execMu.Lock()
			modelOutput, hasModelOutput := d.toolModelOutputs[tr.ToolCallID]
			d.execMu.Unlock()
			if hasModelOutput {
				for i := range chunks {
					if chunks[i].ToolResult != nil && chunks[i].ToolResult.Error == nil {
						mo := modelOutput
						chunks[i].ToolResult.ModelOutput = &mo
					}
				}
			}
			d.recordToolResult(tr)
			for _, c := range chunks {
				d.stream.push(c)
			}
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
			d.completeStep(fs.FinishReason, fs.Usage, fs.HarnessMetadata)
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
					finishReason: fs.FinishReason, releaseCheckpoint: release,
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
			usage := harnessUsageToTypesUsage(fp.TotalUsage)
			// TS's terminal `finish` handler ends the root telemetry span
			// with the bridge's own totalUsage, the real end-of-turn total
			// (as opposed to pauseForHostInput's zero usage or
			// suspendOrFinishNow's last-step usage). TS calls lifecycle.end
			// when the `finish` arrives, before result.finish() rejects an
			// unclosed step, so with completed steps the turn ends (OnEnd)
			// and the later lifecycle.error is a no-op.
			d.telEnd(d.completedSteps, *usage)
			if d.stepOpen {
				return false, false, errors.New(unclosedStepErrorMessage) //nolint:staticcheck // invalidToolInputMessage/unclosedStepErrorMessage are exact TS parity strings
			}
			// Deferred, not pushed here — see turnDriver.pendingFinishChunk's
			// doc. run() pushes it once OnTurnFinished has been called.
			d.pendingFinishChunk = &provider.StreamChunk{
				Type:             provider.ChunkTypeFinish,
				FinishReason:     unifiedFinishReason(fp.FinishReason),
				RawFinishReason:  fp.FinishReason.Raw,
				Usage:            usage,
				ProviderMetadata: marshalMetadata(fp.HarnessMetadata),
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
	return TranslateOptions{
		IsProviderExecuted: func(toolCallID string) bool {
			executed, ok := d.providerExecByID[toolCallID]
			return !ok || executed
		},
		// Restores the tool-result's input and execution origin from its
		// originating (parsed) tool call — TS #23096.
		GetToolCall: func(toolCallID string) (ToolCallInfo, bool) {
			call, ok := d.toolCallsByID[toolCallID]
			if !ok {
				return ToolCallInfo{}, false
			}
			return ToolCallInfo{Input: call.Arguments, ProviderExecuted: call.ProviderExecuted}, true
		},
	}
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
		(raw.ToolName != string(BuiltinToolAskUserQuestions) || !harnessHasBuiltin)

	eventPart := display
	if isHostTool {
		eventPart = raw
	}
	// An MCP tool named `read` can take different input from a builtin `read`
	// that is disabled for this session. The builtin's schema remains in the
	// merged tool set, so validating a provider-executed dynamic call against
	// it would reject valid input. Skip that schema (schema-free dynamic
	// parsing) only when the call is provider-executed + dynamic, shares a
	// name with a filtered-out builtin, and no active host tool owns the
	// name. Mirrors TS `isFilteredBuiltinDynamicCall` (TS #22263).
	tools := d.toolsList
	if display.ProviderExecuted && display.Dynamic && harnessHasBuiltin &&
		!IsBuiltinToolIncluded(display.ToolName, d.in.BuiltinToolFiltering) && !hasActiveToolEntry {
		tools = nil
	}
	parsed, perr := ai.ParseToolCall(d.ctx, ai.ParseToolCallOptions{
		ToolCall: toolCallPartToCall(eventPart),
		Tools:    tools,
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
		out.Error = errors.New(invalidToolInputMessage) //nolint:staticcheck // invalidToolInputMessage/unclosedStepErrorMessage are exact TS parity strings
	}
	return out
}

func parseJSONObject(input string) (map[string]interface{}, error) {
	if input == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := securejson.Unmarshal([]byte(input), &m); err != nil {
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
		provider := "harness:" + d.in.Harness.HarnessID()
		d.in.Callbacks.OnStart(d.ctx, ai.OnStartEvent{
			CallID:         d.telCallID,
			OperationID:    "ai.harness",
			Provider:       provider,
			ModelID:        d.modelID,
			Instructions:   d.in.Instructions,
			Messages:       promptAsMessages(d.in.Prompt),
			Output:         d.in.Output,
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
	// The step's response id and model (TS `finishStep`: response.id
	// `${callId}-${stepNumber}`, response.modelId). A metadata chunk opens no
	// step of its own.
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
		ID: fmt.Sprintf("%s-%d", d.telCallID, d.stepNumber), Timestamp: time.Now(), ModelID: d.respModelID,
	}})
	d.telStepStart()
	if d.in.Callbacks.OnStepStart != nil {
		provider := "harness:" + d.in.Harness.HarnessID()
		d.in.Callbacks.OnStepStart(d.ctx, ai.OnStepStartEvent{
			CallID:         d.telCallID,
			StepNumber:     d.stepNumber,
			Provider:       provider,
			ModelID:        d.modelID,
			Instructions:   d.in.Instructions,
			Messages:       promptAsMessages(d.in.Prompt),
			Output:         d.in.Output,
			Steps:          append([]types.StepResult(nil), d.completedSteps...),
			PreviousSteps:  append([]types.StepResult(nil), d.completedSteps...),
			RuntimeContext: d.in.RuntimeContext,
			ToolsContext:   d.in.ToolsContext,
		})
	}
	if d.in.Callbacks.OnLanguageModelCallStart != nil {
		d.in.Callbacks.OnLanguageModelCallStart(d.ctx, ai.LanguageModelCallStartEvent{
			CallID: d.telCallID, Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.modelID,
			Instructions: d.in.Instructions, Messages: promptAsMessages(d.in.Prompt),
			Tools: toolSpecsAsTypesTools(d.in.ToolSpecs),
		})
	}
}

// pushApprovalRequest emits an approval-request chunk and records it for the
// step's language-model-call-end content (TS enqueueApprovalRequest).
func (d *turnDriver) pushApprovalRequest(req *types.ToolApprovalRequestContent) {
	d.stepApprovalRequests = append(d.stepApprovalRequests, *req)
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: req})
}

func (d *turnDriver) resetStepAccum() {
	d.stepText = ""
	d.stepReasoning = ""
	d.stepToolCalls = nil
	d.stepToolResults = nil
	d.stepApprovalRequests = nil
	d.toolExecMs = map[string]int64{}
	d.stepOpen = false
	d.ignoredFinishStep = nil
}

// markAwaitingEcho records that a host tool result was (or is about to be)
// submitted to the runtime, whose echoing `tool-result` part has not arrived
// yet. Safe to call from host tool execution goroutines.
func (d *turnDriver) markAwaitingEcho(toolCallID string) {
	d.execMu.Lock()
	defer d.execMu.Unlock()
	if d.hostToolCallIDsAwaitingEcho == nil {
		d.hostToolCallIDsAwaitingEcho = map[string]struct{}{}
	}
	d.hostToolCallIDsAwaitingEcho[toolCallID] = struct{}{}
}

func (d *turnDriver) clearAwaitingEcho(toolCallID string) {
	d.execMu.Lock()
	delete(d.hostToolCallIDsAwaitingEcho, toolCallID)
	d.execMu.Unlock()
}

func (d *turnDriver) awaitingEchoCount() int {
	d.execMu.Lock()
	defer d.execMu.Unlock()
	return len(d.hostToolCallIDsAwaitingEcho)
}

func (d *turnDriver) flushBufferedResultChunks() {
	for _, chunks := range d.bufferedResultChunks {
		for _, c := range chunks {
			d.stream.push(c)
		}
	}
	d.bufferedResultChunks = nil
}

// ensureToolExecutionStarted fires ai.Callbacks.OnToolExecutionStart AND
// telemetry's own toolExecutionStart event for call the first time it is
// invoked for call.ID, idempotently — mirrors TS's (TS #21696)
// `publishToolExecutionStart` (`execution.startNotification ??= ...`),
// which does the same for both the consumer-visible callback and the
// telemetry span in one call. Called as soon as a tool's execution
// actually begins (a host tool's own exec goroutine; consumeLoop when a
// provider-executed tool call is first observed), so neither
// OnToolExecutionStart nor the telemetry span waits behind slower sibling
// tools in the same step the way the combined start+end firing in
// recordToolResult used to. recordToolResult also calls this (idempotently)
// right before firing OnToolExecutionEnd/telToolExecutionEnd, as a fallback
// for any tool-result that never got an earlier explicit start (e.g. a
// client-submitted result for a call the host never executed itself) —
// this matches TS's `publishToolExecutionEnd` always calling
// `publishToolExecutionStart` first.
//
// telCtx is the ctx to parent the telemetry event under: the main
// goroutine passes its live d.telStepCtx directly (safe — same
// goroutine); a host tool's own exec goroutine must instead pass a
// snapshot taken synchronously before the goroutine was spawned (see
// executeHostToolAsync's execTelCtx) rather than read d.telStepCtx live,
// since the main goroutine mutates it at step boundaries. nil skips the
// telemetry event entirely (telemetry disabled, or no step context yet).
// See the telToolCtx field doc for why firing telemetry's start from a
// host tool's own goroutine is race-safe despite the ctx snapshot.
func (d *turnDriver) ensureToolExecutionStarted(telCtx context.Context, call types.ToolCall) {
	d.execMu.Lock()
	if d.startedToolExecutions[call.ID] {
		d.execMu.Unlock()
		return
	}
	d.startedToolExecutions[call.ID] = true
	d.execMu.Unlock()

	if d.in.Callbacks.OnToolExecutionStart != nil {
		d.in.Callbacks.OnToolExecutionStart(d.ctx, ai.OnToolCallStartEvent{
			CallID:     d.telCallID,
			ToolCallID: call.ID, ToolName: call.ToolName, ToolCall: call,
			RuntimeContext: d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}

	if telCtx != nil {
		toolCtx := telemetry.FireOnToolCallStart(telCtx, telemetry.TelemetryToolCallStartEvent{
			Settings:    d.in.Telemetry,
			CallID:      d.telCallID,
			ToolCallID:  call.ID,
			ToolName:    call.ToolName,
			Args:        call.Arguments,
			ToolContext: telemetryToolContext(d.in.Telemetry, call.ToolName, d.in.ToolsContext[call.ToolName]),
		})
		d.execMu.Lock()
		d.telToolCtx[call.ID] = toolCtx
		d.execMu.Unlock()
	}
}

// recordToolResult correlates a tool-result event with its originating call
// for step accounting and fires OnToolExecutionEnd/telToolExecutionEnd
// (ensuring the start events have fired first). Mirrors TS's
// `toolExecutions` map, populated for every tool call regardless of who
// executed it (host or the runtime's own builtin).
func (d *turnDriver) recordToolResult(tr *ToolResultPart) {
	call, ok := d.toolCallsByID[tr.ToolCallID]
	if !ok {
		call = types.ToolCall{ID: tr.ToolCallID, ToolName: tr.ToolName}
	}
	var toolResult types.ToolResult
	if tr.IsError {
		toolResult = types.ToolResult{ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, Input: call.Arguments, Error: fmt.Errorf("%v", tr.Result), ProviderExecuted: ok && call.ProviderExecuted, Dynamic: tr.Dynamic || call.Dynamic}
		if toolResult.ProviderExecuted {
			toolResult.Result = tr.Result
		}
	} else {
		toolResult = types.ToolResult{ToolCallID: tr.ToolCallID, ToolName: tr.ToolName, Input: call.Arguments, Result: tr.Result, ProviderExecuted: ok && call.ProviderExecuted, Dynamic: tr.Dynamic || call.Dynamic}
	}
	// Step content keeps the echoed (work-dir-stripped) result, as TS builds
	// it from the forwarded stream parts.
	d.stepToolResults = append(d.stepToolResults, toolResult)
	// A host-executed tool reports its own outcome (raw output / original
	// error) to the callbacks, not the echoed copy (TS keeps the first
	// `toolExecution.toolOutput`).
	d.execMu.Lock()
	host, hostRan := d.hostOutcomes[tr.ToolCallID]
	delete(d.hostOutcomes, tr.ToolCallID)
	d.execMu.Unlock()
	if hostRan {
		toolResult = hostOutcomeResult(call, host)
	}
	d.publishToolExecutionEnd(tr.ToolCallID, tr.ToolName, call, toolResult)
}

// hostOutcomeResult builds the types.ToolResult for a host tool's outcome.
func hostOutcomeResult(call types.ToolCall, o hostToolOutcome) types.ToolResult {
	r := types.ToolResult{ToolCallID: call.ID, ToolName: call.ToolName, Input: call.Arguments, Dynamic: call.Dynamic}
	if o.err != nil {
		r.Error = o.err
	} else {
		r.Result = o.output
	}
	return r
}

// publishToolExecutionEnd fires OnToolExecutionEnd and telemetry's tool end
// (ensuring the start events fired first). Mirrors TS
// `publishToolExecutionEnd`.
func (d *turnDriver) publishToolExecutionEnd(toolCallID, toolName string, call types.ToolCall, toolResult types.ToolResult) {
	// Host tool goroutines write toolExecMs under execMu.
	d.execMu.Lock()
	if d.endedToolExecutions[toolCallID] {
		d.execMu.Unlock()
		return
	}
	d.endedToolExecutions[toolCallID] = true
	execMs := d.toolExecMs[toolCallID]
	d.execMu.Unlock()

	// Always runs on the main goroutine, so reading d.telStepCtx live here
	// (rather than a pre-captured snapshot) is safe.
	d.ensureToolExecutionStarted(d.telStepCtx, call)

	if d.in.Callbacks.OnToolExecutionEnd != nil {
		d.in.Callbacks.OnToolExecutionEnd(d.ctx, ai.OnToolCallFinishEvent{
			CallID:     d.telCallID,
			ToolCallID: toolCallID, ToolName: toolName, ToolCall: call, ToolOutput: toolResult,
			ToolExecutionMs: execMs,
			RuntimeContext:  d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}
	d.telToolExecutionEnd(call, toolResult, execMs)
}

// completeStep flushes buffered tool-result chunks, fires the model-call-end
// and step-end callbacks from locally-tracked step data (see turnDriver
// doc), pushes the step boundary chunk, and advances stepNumber.
func (d *turnDriver) completeStep(finishReason FinishReason, usage Usage, metadata Metadata) {
	d.languageModelCallEnd(finishReason, usage, metadata)
	d.flushBufferedResultChunks()

	// The local view of the step, used if the result does not report one.
	local := types.StepResult{
		CallID:          d.telCallID,
		StepNumber:      d.stepNumber,
		Model:           types.StepModel{Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.respModelID},
		Text:            d.stepText,
		ReasoningText:   d.stepReasoning,
		ToolCalls:       append([]types.ToolCall(nil), d.stepToolCalls...),
		ToolResults:     append([]types.ToolResult(nil), d.stepToolResults...),
		FinishReason:    unifiedFinishReason(finishReason),
		RawFinishReason: finishReason.Raw,
		Usage:           harnessUsageToTypesUsageValue(usage),
	}
	if len(metadata) > 0 {
		local.ProviderMetadata = convertProviderMetadata(ProviderMetadata(metadata))
	}

	// TS: `result.finishStep()` enqueues the finish-step part and returns the
	// step; `lifecycle.stepEnd(step)` then reports that step.
	waiting := d.steps.expect()
	d.stream.push(provider.StreamChunk{
		Type:             provider.ChunkTypeFinishStep,
		FinishReason:     unifiedFinishReason(finishReason),
		RawFinishReason:  finishReason.Raw,
		Usage:            harnessUsageToTypesUsage(usage),
		ProviderMetadata: marshalMetadata(metadata),
	})
	step := local
	timer := time.NewTimer(stepResultTimeout)
	select {
	case step = <-waiting:
	case <-timer.C:
	}
	timer.Stop()

	if d.in.Callbacks.OnStepEnd != nil {
		d.in.Callbacks.OnStepEnd(d.ctx, stepFinishEvent(d.telCallID, step))
	}
	d.telStepEnd(step.StepNumber, step)

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
// `finishForHostInputPause` exactly: it never closes the open step (TS calls
// finishForHostInputPause directly at this call site); the step-closing pause
// is pauseAtHostInputBoundary.
func (d *turnDriver) pauseForHostInput() error {
	if err := d.joinOutstandingExecutions(); err != nil {
		return err
	}
	d.flushBufferedResultChunks()
	return d.finishForHostInputPause()
}

// pauseAtHostInputBoundary pauses a turn at the finish-step (fs; nil when the
// adapter closed the response without one) that follows a requested host
// input (approval or pending client tool result): joins outstanding host tool
// executions, closes the step with the boundary's usage and a tool-calls
// finish reason, then settles the result. Mirrors TS `pauseForHostInput`.
func (d *turnDriver) pauseAtHostInputBoundary(fs *FinishStepPart) error {
	if err := d.joinOutstandingExecutions(); err != nil {
		return err
	}
	d.closingResumedStep = false
	d.ensureStepOpen()
	var usage Usage
	if fs != nil {
		usage = fs.Usage
	}
	var metadata Metadata
	if fs != nil {
		metadata = fs.HarnessMetadata
	}
	d.completeStep(FinishReason{Unified: FinishReasonToolCalls}, usage, metadata)
	return d.finishForHostInputPause()
}

// finishForHostInputPause settles the local result of a pausing turn. Mirrors
// TS `finishForHostInputPause`'s tail.
func (d *turnDriver) finishForHostInputPause() error {
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
// FinishPart branch in consumeLoop). The telemetry span's turn total is the
// sum of every completed step's usage (TS #22259), not just the last
// step's.
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
func (d *turnDriver) suspendOrFinishNow(finishReason FinishReason) {
	suspended := false
	if d.in.OnStopConditionMet != nil {
		if _, err := d.in.OnStopConditionMet(d.ctx); err == nil {
			suspended = true
		}
	}
	// TS's StopConditions early-stop path ends the root telemetry span with
	// the usage of every completed step summed (TS #22259; previously only
	// the last step's), regardless of whether the underlying session turn
	// was suspended or hard-finished.
	var total types.Usage
	for _, step := range d.completedSteps {
		total = total.Add(step.Usage)
	}
	d.telEnd(d.completedSteps, total)
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
	// TS addTokenCounts: undefined only when both are; otherwise a missing
	// side counts as 0.
	if u.InputTokens.Total != nil || u.OutputTokens.Total != nil {
		var sum int64
		if u.InputTokens.Total != nil {
			sum += int64(*u.InputTokens.Total)
		}
		if u.OutputTokens.Total != nil {
			sum += int64(*u.OutputTokens.Total)
		}
		out.TotalTokens = &sum
	}
	if len(u.Raw) > 0 {
		out.Raw = u.Raw
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
	out := map[string]interface{}{"type": "execution-denied"}
	// TS leaves `reason` undefined (and so absent from the JSON) when the
	// tool has no denial reason.
	if reason != "" {
		out["reason"] = reason
	}
	return out
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
		return false, fmt.Errorf("Harness '%s' emitted approval request '%s' for unknown tool call '%s'.", d.in.Harness.HarnessID(), part.ApprovalID, part.ToolCallID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	raw := d.execToolCallsByID[part.ToolCallID]
	toolName := call.ToolName
	if raw != nil {
		toolName = raw.ToolName
	}
	if !IsBuiltinToolIncluded(toolName, d.in.BuiltinToolFiltering) {
		submitter, ok := d.control.(ToolApprovalSubmitter)
		if !ok {
			return false, fmt.Errorf("Harness '%s' emitted a built-in tool approval request but does not support approval responses.", d.in.Harness.HarnessID()) //nolint:staticcheck // matches TS SDK's exact error text
		}
		// The runtime may report a step boundary while it waits for this
		// decision; that boundary must not end the step before the denied
		// call's result arrives (TS #23260).
		d.markAwaitingEcho(part.ToolCallID)
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
			// TS: rawToolCall?.providerExecuted ?? true.
			providerExecuted = raw.ProviderExecuted || raw.ProviderExecutedOmitted
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
		if outcome == turnOutcomeAwaitingToolResult {
			return true, nil
		}
		// TS: `closingResumedStep = true` after a continued approval, so the
		// finish-step closing the resumed step is discarded.
		d.closingResumedStep = true
		return false, nil
	}

	if d.in.OnPendingToolApproval != nil {
		d.in.OnPendingToolApproval(*pending)
	}
	d.pushApprovalRequest(&types.ToolApprovalRequestContent{
		ApprovalID: pending.ApprovalID, ToolCallID: pending.ToolCallID, ToolCall: call,
	})
	d.hostInputPauseRequested = true
	return false, nil
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
		d.markAwaitingEcho(raw.ToolCallID)
		if err := d.submitToolResult(ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output}); err != nil {
			return false, err
		}
		return false, nil
	}

	if isClientExecutedBuiltin {
		d.recordPendingResult(raw)
		d.hostInputPauseRequested = true
		return false, nil
	}

	hostCall := d.validatedHostCallsByID[raw.ToolCallID]
	if hostCall == nil {
		return false, fmt.Errorf("Harness '%s' could not validate host tool '%s'.", d.in.Harness.HarnessID(), raw.ToolName) //nolint:staticcheck // matches TS SDK's exact error text
	}
	// Schema-invalid input is rejected here, before any approval decision is
	// made and without ever calling tool.Execute — mirrors TS's
	// `validatedHostToolCall.invalid` short-circuit, which runs ahead of
	// resolveCustomToolApproval.
	if hostCall.Invalid {
		return false, d.rejectInvalidHostToolCall(raw, call)
	}

	decision := ResolveCustomToolApproval(raw.ToolName, d.in.ToolApproval)
	if decision.Type == CustomToolApprovalDeny && decision.Reason == "" {
		decision.Reason = d.in.ToolApprovalReasons[raw.ToolName]
	}
	switch decision.Type {
	case CustomToolApprovalDeny:
		approvalID := generateApprovalID()
		d.pushApprovalRequest(&types.ToolApprovalRequestContent{
			ApprovalID: approvalID, ToolCallID: raw.ToolCallID, ToolCall: call, IsAutomatic: true,
		})
		d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{
			ApprovalID: approvalID, ToolCallID: raw.ToolCallID, ToolCall: call, Approved: false, Reason: decision.Reason,
		}})
		output := executionDeniedOutput(decision.Reason)
		// TS publishes the execution start before submitting the denial and
		// the end right after it, without waiting for the runtime's echo.
		d.ensureToolExecutionStarted(d.telStepCtx, call)
		d.markAwaitingEcho(raw.ToolCallID)
		if err := d.submitToolResult(ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output}); err != nil {
			return false, err
		}
		d.publishToolExecutionEnd(raw.ToolCallID, raw.ToolName, call, hostOutcomeResult(call, hostToolOutcome{output: output}))
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
			if outcome == turnOutcomeAwaitingToolResult {
				return true, nil
			}
			d.closingResumedStep = true
			return false, nil
		}
		if d.in.OnPendingToolApproval != nil {
			d.in.OnPendingToolApproval(*pending)
		}
		d.pushApprovalRequest(&types.ToolApprovalRequestContent{
			ApprovalID: pending.ApprovalID, ToolCallID: pending.ToolCallID, ToolCall: call,
		})
		d.hostInputPauseRequested = true
		return false, nil

	default: // allow (not-applicable / approved)
		tool, executable := d.in.ActiveTools[raw.ToolName]
		if !executable || tool.Execute == nil {
			d.recordPendingResult(raw)
			d.hostInputPauseRequested = true
			return false, nil
		}
		d.executeHostToolAsync(tool, raw, call, hostCall.Arguments, false)
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
			Error:      errors.New(invalidToolInputMessage), //nolint:staticcheck // invalidToolInputMessage/unclosedStepErrorMessage are exact TS parity strings
			Dynamic:    true,
		},
	}})
	return d.submitToolResult(ToolResultSubmission{
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
func (d *turnDriver) executeHostToolAsync(tool types.Tool, raw *ToolCallPart, call types.ToolCall, execArgs map[string]interface{}, inline bool) {
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
	// TS #21696: fire OnToolExecutionStart (and telemetry's own
	// toolExecutionStart) as execution actually begins, not buffered
	// behind this step's slower sibling tool executions. Called here —
	// synchronously, on the caller's (the main consumeLoop) goroutine,
	// before the exec goroutine is even spawned — rather than as the
	// first statement inside the goroutine below: a step's own step span
	// can legitimately end (telStepEnd, e.g. at this step's very next
	// StreamPart, FinishStepPart) before a newly-spawned goroutine gets
	// scheduled onto a thread, which would silently resolve the
	// telemetry integration's per-step span state to an already-ended,
	// non-recording span and drop the tool's span entirely — a real
	// ordering race, not merely a data race (genAIState's own map/fields
	// are already mutex-protected either way). Firing synchronously here
	// instead gives it the same happens-before guarantee relative to
	// subsequent StreamParts that TS gets for free from its
	// single-threaded, run-to-completion-until-the-first-await execution
	// model. See the telToolCtx field doc.
	d.ensureToolExecutionStarted(d.telStepCtx, call)
	// The runtime echoes this call's result back as a tool-result part; a
	// finish-step arriving before that is not the end of the step (TS #23260).
	if _, settled := d.settledHostIDs[raw.ToolCallID]; !settled {
		d.markAwaitingEcho(raw.ToolCallID)
	}
	d.execWG.Add(1)
	body := func() {
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
				// Context is host-only and can contain credentials: the detailed
				// error stays in the host-facing outcome and is never sent to
				// the runtime/model. The turn continues with a tool error (TS
				// maybeExecuteHostTool returns `{executed: true, ok: false}`).
				d.setHostOutcome(raw.ToolCallID, hostToolOutcome{err: providererrors.NewValidationErrorWithContext(
					normalizedToolCtx, err.Error(), err,
					&providererrors.ValidationContext{Field: "tool context", EntityName: raw.ToolName},
				)})
				d.submitHostToolResult(raw.ToolCallID, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": "Tool context validation failed."}, IsError: true})
				return
			}
			toolCtx = normalizedToolCtx
		}
		// Wrapped by telemetry.FireExecuteToolWithSettings so integrations
		// can create nested spans for tool -> generateText chains (TS
		// 59a2306's `wrappedExecuteTool`, matching pkg/ai/generate.go's own
		// tool-loop wrapping of the same call).
		runTool := func(execCtx context.Context, args map[string]interface{}) (res interface{}, execErr error) {
			// A throwing tool is a tool error in TS (the exception is caught
			// and submitted as `String(err)`); a Go panic must not take the
			// process down either.
			defer func() {
				if r := recover(); r != nil {
					res, execErr = nil, fmt.Errorf("tool %q panicked: %v", raw.ToolName, r)
				}
			}()
			return tool.Execute(execCtx, args, types.ToolExecutionOptions{
				ToolCallID: raw.ToolCallID, RuntimeContext: d.in.RuntimeContext, ToolContext: toolCtx,
				ExperimentalSandbox: d.in.SandboxSession,
				// Each value a generator-form tool yields before its last is
				// a preliminary tool-result part on the consumer stream (TS
				// maybeExecuteHostTool onPreliminaryResult). Preliminary
				// values never reach the runtime, so the work dir is
				// stripped here like the final result's projection.
				OnPreliminaryResult: func(prelim interface{}) {
					display := StripWorkDir(&ToolResultPart{ToolCallID: raw.ToolCallID, ToolName: raw.ToolName, Result: prelim}, d.in.SessionWorkDir)
					stripped, _ := display.(*ToolResultPart)
					if stripped == nil {
						return
					}
					d.stream.pushAsync(d.ctx, provider.StreamChunk{
						Type: provider.ChunkTypeToolResult,
						ToolResult: &types.ToolResult{
							ToolCallID: raw.ToolCallID, ToolName: raw.ToolName,
							Result: stripped.Result, Preliminary: true,
						},
					})
				},
			})
		}
		var output interface{}
		var err error
		if d.in.Telemetry == nil {
			// No telemetry settings: no dispatcher, so globally registered
			// integrations do not wrap the tool either (TS executeTool).
			output, err = runTool(execTelCtx, execArgs)
		} else {
			output, err = telemetry.FireExecuteToolWithSettings(execTelCtx, d.in.Telemetry, raw.ToolName, execArgs, runTool)
		}
		d.execMu.Lock()
		d.toolExecMs[raw.ToolCallID] = time.Since(start).Milliseconds()
		d.execMu.Unlock()
		if err != nil {
			d.setHostOutcome(raw.ToolCallID, hostToolOutcome{err: err})
			d.submitHostToolResult(raw.ToolCallID, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": toolErrorString(err)}, IsError: true})
			return
		}
		submission := ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: output}
		// Run toModelOutput() exactly once per submitted result (TS #22367):
		// the raw output stays on the consumer-facing result, and the
		// model-facing representation travels as ToolResult for the adapter
		// and is cached for ResponseMessages.
		if tool.ToModelOutput != nil {
			modelOutput, mErr := tool.ToModelOutput(d.ctx, types.ToModelOutputOptions{
				ToolCallID: raw.ToolCallID, Input: execArgs, Output: output, Result: output, //nolint:staticcheck // Result is the deprecated alias; set for older callbacks
			})
			if mErr != nil {
				d.setHostOutcome(raw.ToolCallID, hostToolOutcome{err: mErr})
				d.submitHostToolResult(raw.ToolCallID, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": toolErrorString(mErr)}, IsError: true})
				return
			}
			if modelOutput != nil {
				submission.ToolResult = newModelToolResult(raw.ToolCallID, raw.ToolName, *modelOutput)
			}
		}
		d.setHostOutcome(raw.ToolCallID, hostToolOutcome{output: output})
		if err := d.submitToolResult(submission); err != nil {
			// TS maybeExecuteHostTool submits inside its try block: a model
			// output the harness cannot deliver (or a failed submit) is the
			// tool's error, reported back to the runtime as such, not a
			// turn failure.
			d.setHostOutcome(raw.ToolCallID, hostToolOutcome{err: err})
			d.submitHostToolResult(raw.ToolCallID, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": toolErrorString(err)}, IsError: true})
		}
	}
	if inline {
		body()
		return
	}
	go body()
}

func (d *turnDriver) setHostOutcome(toolCallID string, o hostToolOutcome) {
	d.execMu.Lock()
	d.hostOutcomes[toolCallID] = o
	d.execMu.Unlock()
}

// takeHostOutcome removes and returns a host tool's recorded outcome.
func (d *turnDriver) takeHostOutcome(toolCallID string) (hostToolOutcome, bool) {
	d.execMu.Lock()
	defer d.execMu.Unlock()
	o, ok := d.hostOutcomes[toolCallID]
	delete(d.hostOutcomes, toolCallID)
	return o, ok
}

// submitHostToolResult submits a host tool's result to the runtime. A
// submission failure fails the turn at the next join, as a rejected
// execution does in TS.
func (d *turnDriver) submitHostToolResult(toolCallID string, submission ToolResultSubmission) {
	if err := d.submitToolResult(submission); err != nil {
		d.recordExecError(toolCallID, err)
	}
}

// submitToolResult is the single path every tool result takes back to the
// harness runtime. When the submission carries a model-facing tool result
// (from a tool's ToModelOutput or a client-supplied continuation), its
// output is normalized (text and inline images only) and cached so
// ResponseMessages reports it. Mirrors TS runPrompt's wrapped
// `submitToolResult` (TS #22367): an unsupported model output is an error,
// except for a client continuation, which falls back to the JSON text of the
// raw output.
func (d *turnDriver) submitToolResult(submission ToolResultSubmission) error {
	var output *types.ToolResultOutput
	switch tr := submission.ToolResult.(type) {
	case *ModelToolResult:
		if tr != nil {
			output = &tr.Output
		}
	case *types.ToolResultContent:
		if tr != nil {
			output = tr.Output
		}
	}
	if output != nil {
		normalized, err := NormalizeToolModelOutput(*output)
		if err != nil {
			_, hasContinuation := d.continuationsByToolCallID[submission.ToolCallID]
			if !hasContinuation || !IsCapabilityUnsupportedError(err) {
				return err
			}
			text, mErr := jsstr.Marshal(submission.Output)
			if mErr != nil || string(text) == "" {
				text = []byte("null")
			}
			normalized = types.ToolResultOutput{Type: types.ToolResultOutputText, Value: string(text)}
		}
		switch tr := submission.ToolResult.(type) {
		case *ModelToolResult:
			cp := *tr
			cp.Output = normalized
			submission.ToolResult = &cp
		case *types.ToolResultContent:
			cp := *tr
			cp.Output = &normalized
			submission.ToolResult = &cp
		}
		d.execMu.Lock()
		if d.toolModelOutputs == nil {
			d.toolModelOutputs = map[string]types.ToolResultOutput{}
		}
		d.toolModelOutputs[submission.ToolCallID] = normalized
		d.execMu.Unlock()
	}
	if d.suspending() {
		return d.recordSuspendedResult(submission)
	}
	return d.control.SubmitToolResult(d.ctx, submission)
}

// recordSuspendedResult stores a tool result that completed after the host
// suspended the turn. The runtime's stream is already closed, so instead of
// submitting it the result is reported as a completed pending tool result (a
// continuation submits it without re-running the tool) and, for a tool call
// this turn has already seen, queued to be published as a tool-result part.
// Mirrors the `isTurnSuspending` branch of TS runPrompt's `submitToolResult`.
func (d *turnDriver) recordSuspendedResult(submission ToolResultSubmission) error {
	d.execMu.Lock()
	info, ok := d.callInfo[submission.ToolCallID]
	d.execMu.Unlock()
	if !ok {
		return fmt.Errorf("Unknown suspended tool call '%s'.", submission.ToolCallID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	completed := &CompletedToolResult{Output: submission.Output, ToolResult: submission.ToolResult}
	if submission.IsError {
		isError := true
		completed.IsError = &isError
	}
	if d.in.OnPendingToolResult != nil {
		d.in.OnPendingToolResult(PendingToolResult{
			ToolCallID: submission.ToolCallID, ToolName: info.name, Input: info.input, CompletedResult: completed,
		})
	}
	// No echo will arrive from the closed runtime.
	d.clearAwaitingEcho(submission.ToolCallID)
	if info.parsed {
		d.execMu.Lock()
		d.suspendedOutcomes = append(d.suspendedOutcomes, &ToolResultPart{
			ToolCallID: submission.ToolCallID, ToolName: info.name, Result: submission.Output, IsError: submission.IsError,
		})
		d.execMu.Unlock()
	}
	return nil
}

// publishSuspendedOutcomes emits the tool-result parts for host tool
// outcomes that completed while the turn was suspending, skipping calls a
// continuation already settled. Runs on the driver goroutine.
func (d *turnDriver) publishSuspendedOutcomes() {
	d.execMu.Lock()
	outcomes := d.suspendedOutcomes
	d.suspendedOutcomes = nil
	d.execMu.Unlock()
	for _, tr := range outcomes {
		if d.isSettled(tr.ToolCallID) {
			continue
		}
		display, _ := StripWorkDir(tr, d.in.SessionWorkDir).(*ToolResultPart)
		if display == nil {
			display = tr
		}
		d.recordToolResult(display)
		for _, c := range TranslatePart(display, d.translateOpts()) {
			d.stream.push(c)
		}
	}
}

// discardCurrentStep drops the content of a step that never reached its
// finish-step. Mirrors TS `result.discardCurrentStepContent()`.
func (d *turnDriver) discardCurrentStep() {
	d.bufferedResultChunks = nil
	d.resetStepAccum()
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
			return turnOutcomeContinue, fmt.Errorf("Harness '%s' emitted a built-in tool approval request but does not support approval responses.", d.in.Harness.HarnessID()) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if err := submitter.SubmitToolApproval(d.ctx, ToolApprovalSubmission{ApprovalID: approval.ApprovalID, Approved: continuation.Approved, Reason: continuation.Reason}); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeContinue, nil
	}

	d.settledHostIDs[approval.ToolCallID] = struct{}{}
	if !continuation.Approved {
		output := executionDeniedOutput(continuation.Reason)
		if err := d.submitToolResult(ToolResultSubmission{ToolCallID: approval.ToolCallID, Output: output}); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeContinue, nil
	}

	if validatedHostToolCall == nil {
		return turnOutcomeContinue, fmt.Errorf("Harness '%s' could not validate approved host tool '%s'.", d.in.Harness.HarnessID(), approval.ToolName) //nolint:staticcheck // matches TS SDK's exact error text
	}
	// Revalidation rejected the input: settle as a tool-error, exactly like
	// a first-pass invalid host tool call (rejectInvalidHostToolCall),
	// without ever calling tool.Execute. Mirrors TS's
	// `validatedToolCall.invalid` branch here.
	if validatedHostToolCall.Invalid {
		if err := d.submitToolResult(ToolResultSubmission{
			ToolCallID: approval.ToolCallID,
			Output:     map[string]interface{}{"error": invalidToolInputMessage},
			IsError:    true,
		}); err != nil {
			return turnOutcomeContinue, err
		}
		// TS publishes this outcome right away (`publishToolExecutions`).
		d.stream.push(d.hostOutcomeChunk(approval.ToolCallID, approval.ToolName, call, hostToolOutcome{err: errors.New(invalidToolInputMessage)})) //nolint:staticcheck // invalidToolInputMessage/unclosedStepErrorMessage are exact TS parity strings
		return turnOutcomeContinue, nil
	}

	// TS `await lifecycle.start(input.model)` then `publishToolExecutionStart`
	// run before maybeExecuteHostTool finds out whether the tool can execute.
	d.ensureStarted()
	d.ensureToolExecutionStarted(d.telStepCtx, call)
	tool, executable := d.in.ActiveTools[approval.ToolName]
	if !executable || tool.Execute == nil {
		d.recordPendingResult(&ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input})
		if err := d.pauseForHostInput(); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeAwaitingToolResult, nil
	}
	// Run the approved tool to completion here, like TS (`await
	// maybeExecuteHostTool`): its outcome is surfaced as a continuation
	// tool-result/tool-error part, since the runtime's echo of a settled call
	// is skipped as a replay.
	d.executeHostToolAsync(tool, &ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input}, call, validatedHostToolCall.Arguments, true)
	if outcome, ok := d.takeHostOutcome(approval.ToolCallID); ok {
		d.publishToolExecutionEnd(approval.ToolCallID, approval.ToolName, call, hostOutcomeResult(call, outcome))
		d.stream.push(d.hostOutcomeChunk(approval.ToolCallID, approval.ToolName, call, outcome))
	}
	return turnOutcomeContinue, nil
}

// hostOutcomeChunk is the consumer-stream tool-result / tool-error part for a
// host tool outcome produced outside the runtime's echo. Mirrors TS
// `enqueueHostToolOutcome`: the output is work-dir-stripped like every
// forwarded result; an error keeps the original value. TS's
// enqueueHostToolOutcome sets no `dynamic` flag on either part (only the
// first-pass invalid-input tool-error built in the loop carries it).
func (d *turnDriver) hostOutcomeChunk(toolCallID, toolName string, call types.ToolCall, o hostToolOutcome) provider.StreamChunk {
	tr := &types.ToolResult{ToolCallID: toolCallID, ToolName: toolName, Input: call.Arguments}
	if o.err != nil {
		tr.Error = o.err
	} else {
		tr.Result = o.output
		if stripped, _ := StripWorkDir(&ToolResultPart{ToolCallID: toolCallID, ToolName: toolName, Result: o.output}, d.in.SessionWorkDir).(*ToolResultPart); stripped != nil {
			tr.Result = stripped.Result
		}
	}
	return provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: tr}
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
		// TS: `{...continuation, toolName: pendingResult.toolName,
		// providerOptions: pendingResult.providerOptions when the
		// continuation has none}`. The fallback carries adapter-private state
		// (e.g. ACP's native question request) stamped on the tool call.
		tr := *continuation
		tr.ToolName = pending.ToolName
		if tr.ProviderOptions == nil && len(pending.ProviderOptions) > 0 {
			tr.ProviderOptions = make(map[string]interface{}, len(pending.ProviderOptions))
			for k, v := range pending.ProviderOptions {
				tr.ProviderOptions[k] = v
			}
		}
		submission.ToolResult = &tr
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
	if err := d.submitToolResult(submission); err != nil {
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
		return executionDeniedOutput(result.Output.Reason), false
	case types.ToolResultOutputContent:
		return result.Output, false
	default:
		return result.Result, false
	}
}

func generateApprovalID() string { return newID() }

// toolErrorString mirrors JS `String(err)` for an Error, which is what the TS
// host sends back to the runtime for a failed host tool: "Error: <message>".
func toolErrorString(err error) string {
	return "Error: " + err.Error()
}
