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
)

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
	if d.ctx.Err() != nil {
		d.abort(err)
		return
	}
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
	result := ai.NewStreamTextResultFromParts(ctx, stream, ai.ExternalStreamOptions{
		Provider: "harness:" + in.Harness.HarnessID(),
		ModelID:  in.Model,
	})

	done := make(chan struct{})
	d := &turnDriver{ctx: ctx, in: in, stream: stream}
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
	// for approvals, classification and callbacks.
	execToolCallsByID map[string]*ToolCallPart
	toolCallsByID     map[string]types.ToolCall
	providerExecByID  map[string]bool

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
}

func (d *turnDriver) run() {
	d.execToolCallsByID = map[string]*ToolCallPart{}
	d.toolCallsByID = map[string]types.ToolCall{}
	d.providerExecByID = map[string]bool{}
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
// (pauseForHostInput, finishNow) already pushed the closing chunk(s) and
// closed d.stream itself — the caller (run) must not touch d.stream again in
// that case, and must not fire OnTurnFinished/OnTurnFailed (whichever
// settled it already did, if appropriate: a pause fires neither, matching
// TS's finishForHostInputPause).
func (d *turnDriver) consumeLoop(partsCh <-chan StreamPart) (finished bool, alreadySettled bool, err error) {
	for {
		var part StreamPart
		var ok bool
		select {
		case part, ok = <-partsCh:
		case <-d.ctx.Done():
			return false, false, d.ctx.Err()
		}
		if !ok {
			if !finished {
				// The adapter ended the turn (closed its Done channel)
				// without ever emitting `finish`. Mirrors TS's fallback
				// `else { input.onTurnFailed?.(); }` branch for a reader
				// loop that ends without reaching a terminal `finish`.
				return false, false, errors.New("harness: adapter ended the turn without emitting `finish`")
			}
			return true, false, nil
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
			d.execToolCallsByID[tc.ToolCallID] = part.(*ToolCallPart)
			validated := d.validateToolCall(tc)
			d.toolCallsByID[tc.ToolCallID] = validated
			d.providerExecByID[tc.ToolCallID] = tc.ProviderExecuted
			d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &validated})
			d.stepToolCalls = append(d.stepToolCalls, validated)
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
			if reason := d.evaluateStopConditions(); reason != "" {
				d.finishNow(fs.FinishReason)
				return false, true, nil
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
			d.stream.push(provider.StreamChunk{
				Type:            provider.ChunkTypeFinish,
				FinishReason:    unifiedFinishReason(fp.FinishReason),
				RawFinishReason: fp.FinishReason.Raw,
				Usage:           usage,
			})
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

// validateToolCall parses a harness tool-call event against the merged tool
// set, mirroring TS `validateToolCall` (schema/existence validation; async in
// TS only because of dynamic schema imports, synchronous here).
func (d *turnDriver) validateToolCall(tc *ToolCallPart) types.ToolCall {
	args, parseErr := parseJSONObject(tc.Input)
	call := types.ToolCall{
		ID: tc.ToolCallID, ToolName: tc.ToolName, Arguments: args, RawArguments: tc.Input,
		ProviderExecuted: tc.ProviderExecuted, Dynamic: tc.Dynamic,
		ProviderMetadata: convertProviderMetadata(ProviderMetadata(tc.ProviderMetadata)),
	}
	if _, known := d.in.Tools[tc.ToolName]; !known {
		call.Dynamic = true
		call.Invalid = true
		names := make([]string, 0, len(d.in.Tools))
		for n := range d.in.Tools {
			names = append(names, n)
		}
		call.Error = &ai.NoSuchToolError{ToolName: tc.ToolName, AvailableTools: names}
		return call
	}
	if parseErr != nil {
		call.Invalid = true
		call.Error = fmt.Errorf("invalid tool input JSON for %q: %w", tc.ToolName, parseErr)
	}
	return call
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
	d.flushBufferedResultChunks()

	step := types.StepResult{
		StepNumber:   d.stepNumber,
		Model:        types.StepModel{Provider: "harness:" + d.in.Harness.HarnessID(), ModelID: d.in.Model},
		Text:         d.stepText,
		ToolCalls:    append([]types.ToolCall(nil), d.stepToolCalls...),
		ToolResults:  append([]types.ToolResult(nil), d.stepToolResults...),
		FinishReason: unifiedFinishReason(finishReason),
		Usage:        harnessUsageToTypesUsageValue(usage),
	}
	if d.in.Callbacks.OnStepEnd != nil {
		d.in.Callbacks.OnStepEnd(d.ctx, ai.OnStepFinishEvent{
			StepNumber: step.StepNumber, Model: step.Model, Text: step.Text,
			ToolCalls: step.ToolCalls, ToolResults: step.ToolResults,
			FinishReason: step.FinishReason, Usage: step.Usage,
			RuntimeContext: d.in.RuntimeContext, ToolsContext: d.in.ToolsContext,
		})
	}

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
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeFinish})
	d.stream.closeOK()
	return nil
}

// finishNow settles the turn as fully complete right now, ahead of any later
// `finish` the adapter might still emit — used only by the StopConditions
// early-stop path. It fires OnTurnFinished (unlike pauseForHostInput, this
// really is "done" from the session's point of view: WG4 does not yet keep
// the underlying harness Session turn reachable afterward for a later
// resume, so — pending WG13's workflow-harness suspend/continue slicing —
// treating it as finished is more useful than leaving the session
// permanently stuck as "running"). finishReason is the last completed
// step's; usage is deliberately left unset so the locally-summed total
// stands, since this is not a real bridge-reported total the way a genuine
// terminal `finish` carries one (contrast pauseForHostInput/the FinishPart
// branch in consumeLoop).
func (d *turnDriver) finishNow(finishReason FinishReason) {
	d.stream.push(provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: unifiedFinishReason(finishReason), RawFinishReason: finishReason.Raw})
	if d.in.OnTurnFinished != nil {
		d.in.OnTurnFinished()
	}
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
		if err := d.pauseForHostInput(); err != nil {
			return false, err
		}
		return true, nil
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
		if err := d.pauseForHostInput(); err != nil {
			return false, err
		}
		return true, nil

	default: // allow (not-applicable / approved)
		tool, executable := d.in.ActiveTools[raw.ToolName]
		if !executable || tool.Execute == nil {
			d.recordPendingResult(raw)
			if err := d.pauseForHostInput(); err != nil {
				return false, err
			}
			return true, nil
		}
		d.executeHostToolAsync(tool, raw, call)
		return false, nil
	}
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
// Joined at the next step boundary via joinOutstandingExecutions.
func (d *turnDriver) executeHostToolAsync(tool types.Tool, raw *ToolCallPart, call types.ToolCall) {
	d.execWG.Add(1)
	go func() {
		defer d.execWG.Done()
		start := time.Now()
		var toolCtx interface{}
		if ctxValue, ok := d.in.ToolsContext[raw.ToolName]; ok {
			toolCtx = ctxValue
		}
		if tool.ContextSchema != nil {
			if err := tool.ContextSchema.Validator().Validate(toolCtx); err != nil {
				_ = d.control.SubmitToolResult(d.ctx, ToolResultSubmission{ToolCallID: raw.ToolCallID, Output: map[string]interface{}{"error": "Tool context validation failed."}, IsError: true})
				d.recordExecError(raw.ToolCallID, err)
				return
			}
		}
		output, err := tool.Execute(d.ctx, call.Arguments, types.ToolExecutionOptions{
			ToolCallID: raw.ToolCallID, RuntimeContext: d.in.RuntimeContext, ToolContext: toolCtx,
			ExperimentalSandbox: d.in.SandboxSession,
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

// processApprovalContinuation submits a resolved approval decision back to
// the harness (builtin via PromptControl.SubmitToolApproval, custom via a
// denial/execution result), mirroring TS `processPendingApprovalContinuation`.
func (d *turnDriver) processApprovalContinuation(approval PendingToolApproval, continuation types.ToolApprovalResponseContent) (turnOutcome, error) {
	call, ok := d.toolCallsByID[approval.ToolCallID]
	if !ok {
		args, _ := parseJSONObject(approval.Input)
		providerExecuted := false
		if approval.ProviderExecuted != nil {
			providerExecuted = *approval.ProviderExecuted
		}
		call = types.ToolCall{ID: approval.ToolCallID, ToolName: approval.ToolName, Arguments: args, RawArguments: approval.Input, ProviderExecuted: providerExecuted}
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

	tool, executable := d.in.ActiveTools[approval.ToolName]
	if !executable || tool.Execute == nil {
		d.recordPendingResult(&ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input})
		if err := d.pauseForHostInput(); err != nil {
			return turnOutcomeContinue, err
		}
		return turnOutcomeAwaitingToolResult, nil
	}
	d.executeHostToolAsync(tool, &ToolCallPart{ToolCallID: approval.ToolCallID, ToolName: approval.ToolName, Input: approval.Input}, call)
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
