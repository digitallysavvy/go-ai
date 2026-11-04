package codemode

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// invocationCounter mirrors TypeScript's module-level `invocationCounter`
// (code-mode/src/run-code-mode.ts), which is only ever incremented lazily
// via “ `code-mode-${++invocationCounter}` “ as the last fallback in the
// outerToolCallId `??` chain -- i.e. only for an invocation that supplies
// neither toolExecutionOptions.toolCallId nor a continuation -- so every
// such anonymous invocation in the process gets a distinct default id
// instead of every one colliding on the same literal "code-mode-1".
var invocationCounter int64

// RunCodeMode runs code-mode JavaScript directly, without wrapping it as an
// AI SDK tool. The source runs as the body of an async function, so
// top-level `await`/`return` are supported; the source is wrapped in a
// fresh QuickJS sandbox with input.Options' execution limits applied.
//
// When input.Continuation is set (resuming a prior *Interrupt), RunCodeMode
// deterministically replays input.JS instead of a fresh evaluation -- see
// the package doc's "Deterministic replay" section.
//
// Returns the sandboxed program's JSON-decoded return value on completion,
// a *Interrupt (as the returned interface{}, with a nil error) if execution
// paused on a host tool call, or an error.
//
// Mirrors TypeScript's experimental_runCodeMode (code-mode/src/
// run-code-mode.ts).
func RunCodeMode(ctx context.Context, input RunInput) (interface{}, error) {
	var options *Options
	if input.Options != nil {
		options = input.Options
	}
	var policyInput *ExecutionPolicy
	if options != nil {
		policyInput = options.ExecutionPolicy
	}
	// TS resolves the policy's defaults first and leaves validating it to
	// the runner, after the source-size check and prepareContinuation (a
	// partially resolved continuation returns its next interrupt without
	// running, so it never validates).
	policy := defaultExecutionPolicy(policyInput)

	if err := assertSourceSize(input.JS, policy.MaxSourceBytes); err != nil {
		return nil, err
	}

	toolNames := make([]string, 0, len(input.Tools))
	for name := range input.Tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	prepared, perr2 := prepareContinuation(input, toolNames, policy.MaxToolOutputBytes)
	if perr2 != nil {
		return nil, perr2
	}
	if prepared.nextInterrupt != nil {
		return prepared.nextInterrupt, nil
	}
	if verr := validateExecutionPolicy(policy); verr != nil {
		return nil, verr
	}

	var outerToolCall string
	switch {
	case input.ToolExecutionOptions != nil && (input.ToolExecutionOptions.ToolCallID != "" || input.Continuation == nil):
		// TS `??` keeps an explicit empty toolCallId. A Go caller that
		// resumes with options carrying no ID (only a context, say) still
		// resumes under the continuation's ID.
		outerToolCall = input.ToolExecutionOptions.ToolCallID
	case input.Continuation != nil:
		outerToolCall = input.Continuation.OuterToolCallID
	default:
		// Mirrors TypeScript's lazy `` `code-mode-${++invocationCounter}` ``
		// fallback: only incremented for an invocation that supplies neither
		// of the above, so every such anonymous invocation in the process
		// gets a distinct default id.
		outerToolCall = fmt.Sprintf("code-mode-%d", atomic.AddInt64(&invocationCounter, 1))
	}

	// One CPU budget per invocation: a continuation resume starts fresh. The
	// bridge pauses it while a host tool runs (time waiting on tools does not
	// count) and runInSandbox runs it.
	ctx = withCPUBudget(ctx, newCPUBudget(policy.CPUTimeoutMs))
	bridge := newToolBridge(ctx, input, options, policy, outerToolCall, prepared.replayLedger, prepared.resumePendings, prepared.resumeResolutions)
	// Mirrors TypeScript's stripSnippetTypes (run package,
	// dist/utils/source-cache.js), which catches *any* stripper error and
	// falls back to the snippet unmodified rather than rejecting it: any
	// TypeScript syntax the stripper can't erase (or genuinely can't parse)
	// is handed to the JavaScript engine as-is, where it fails -- if it
	// truly isn't valid JavaScript -- with an ordinary engine syntax error
	// instead of a stripper-specific one. stripTypeScriptAnnotations already
	// returns the original source unmodified on error, so the error itself
	// is never surfaced here.
	strippedJS, _ := stripTypeScriptAnnotations(input.JS)
	// Reject dynamic import() up front -- see assertNoDynamicImport's doc
	// comment (sandbox_hardening.go) for why this, paired with
	// installRuntimeHardening blocking eval/Function (engine.go), is the
	// fix for the qjs:std/qjs:os/qjs:bjson native-module sandbox-escape
	// gap: stripSandboxGlobals alone does not stop
	// `await import('qjs:std')` from reaching the same host-escape
	// primitives independently of the global object.
	if ierr := assertNoDynamicImport(strippedJS); ierr != nil {
		return nil, ierr
	}
	source := wrapCodeModeSource(strippedJS)
	// Host calls run on their own goroutines (toolBridge.start); whatever
	// is still running when the invocation ends is cancelled and waited for.
	defer bridge.shutdown()

	resultJSON, isUndefined, interrupted, err := runInSandboxCtx(ctx, policy, func(sandboxCtx context.Context, jsCtx *qjs.Context) (string, bool, bool, error) {
		if berr := bindCodeModeDispatch(jsCtx, bridge); berr != nil {
			return "", false, false, berr
		}
		return driveCodeModeExecution(sandboxCtx, jsCtx, bridge, source)
	})

	committed := bridge.committedSnapshot()

	if interrupted {
		// Quiescence was reached (driveCodeModeExecution) with one or more
		// host calls newly pending and none in flight -- every entry
		// collected in the same wave (e.g. a Promise.all([...]) where more
		// than one call needed approval) surfaces together as a single
		// multi-item Continuation. Mirrors TypeScript's run requiring the
		// complete interruption batch to be resolved together, from the one
		// quiescence point where `run`'s manager.js collects them.
		batch := bridge.batchSnapshot()
		if len(batch) == 0 {
			return nil, NewProtocolError("Code mode reported an interruption with no pending interruptions recorded.", nil)
		}
		return buildInterruptResult(input, options, outerToolCall, toolNames, committed, batch)
	}

	// TS run (manager.js handleReadyMessage): a resumed script that
	// finished, successfully or not, before making every call the
	// continuation recorded is a protocol error.
	if bridge.finishedScript() && input.Continuation != nil && bridge.requestTotal() < bridge.ledgerLength() {
		return nil, NewProtocolError("Continuation completed before replaying the full host function ledger.", nil)
	}

	if err != nil {
		// Prefer a preserved *typed* CodeModeError a bridge call raised
		// over the generic JavaScript exception text that comes back once
		// it round-trips through the sandbox (e.g. a script that catches a
		// rejected tool call's error and rethrows, or lets it propagate
		// unchanged) -- mirrors TypeScript's findPreservedCodeModeError.
		// Only the script's own rejection can be such an error; an engine
		// failure (timeout, abort, CPU budget) is reported as itself.
		var rejected *guestRejection
		if errors.As(err, &rejected) {
			if rejected.hasMessage {
				if codeModeErr := bridge.preservedCodeModeErr(rejected.message); codeModeErr != nil {
					return nil, codeModeErr
				}
			}
			return nil, rejected.err
		}
		return nil, err
	}

	if isUndefined {
		return nil, nil
	}

	if serr := assertJSONPayloadSize(resultJSON, policy.MaxResultBytes, "Code mode result"); serr != nil {
		return nil, serr
	}
	return fromJSONPayload(resultJSON)
}

// assertSourceSize mirrors TS assertSourceSize: an invalid limit is left for
// the policy validation to report.
func assertSourceSize(source string, maxBytes int) error {
	if !isValidRunLimit(maxBytes) {
		return nil
	}
	bytes := len([]byte(source))
	if bytes > maxBytes {
		return NewSourceTooLargeError(bytes, maxBytes)
	}
	return nil
}

// preparedContinuation is the result of prepareContinuation.
type preparedContinuation struct {
	// nextInterrupt, when non-nil, is returned by RunCodeMode directly: the
	// continuation's pending-interruption batch isn't fully resolved yet
	// (see the package doc; this package itself only ever produces
	// single-entry batches, but resuming a foreign or future continuation
	// with more than one entry is still handled correctly).
	nextInterrupt *Interrupt

	// replayLedger/resumePendings/resumeResolutions feed newToolBridge when
	// nextInterrupt is nil and (for a continuation input) every pending
	// interruption has a resolution: see toolBridge's doc comment. Keyed
	// by call index (see replayRecord's doc comment).
	replayLedger      map[int]replayRecord
	resumePendings    map[int]PendingInterruption
	resumeResolutions map[int]interface{}
}

// prepareContinuation validates and advances input.Continuation, mirroring
// TypeScript's prepareContinuation (code-mode/src/run-code-mode.ts). Returns
// a zero preparedContinuation (no nextInterrupt, no replay data) when
// input.Continuation is nil -- a fresh invocation.
func prepareContinuation(input RunInput, toolNames []string, maxToolOutputBytes int) (preparedContinuation, error) {
	if input.Continuation == nil {
		if input.InterruptResolution != nil {
			return preparedContinuation{}, NewProtocolError(
				"A code-mode interrupt resolution was provided without continuation state.", nil,
			)
		}
		return preparedContinuation{}, nil
	}

	continuation := *input.Continuation
	var security ContinuationSecurityOptions
	if input.Options != nil && input.Options.ContinuationSecurity != nil {
		security = *input.Options.ContinuationSecurity
	}
	if err := verifyContinuation(continuation, security); err != nil {
		return preparedContinuation{}, err
	}
	if continuation.JS != input.JS {
		return preparedContinuation{}, NewProtocolError(
			"Code mode continuation source does not match the resumed source.", nil,
		)
	}
	if !stringSlicesEqual(continuation.ToolNames, toolNames) {
		return preparedContinuation{}, NewProtocolError(
			"Code mode continuation tool names do not match the resumed tools.", nil,
		)
	}
	if input.InterruptResolution == nil {
		return preparedContinuation{}, NewProtocolError(
			"A code-mode continuation requires an interrupt resolution.", nil,
		)
	}

	// TS reports a fully resolved continuation (no pending entry at the
	// next index) with the same mismatch error.
	resolutionIndex := len(continuation.Resolutions)
	if resolutionIndex >= len(continuation.PendingInterruptions) ||
		continuation.PendingInterruptions[resolutionIndex].InterruptID != input.InterruptResolution.InterruptID {
		return preparedContinuation{}, NewProtocolError(
			"Interrupt resolution does not match the next pending code-mode interruption.",
			map[string]interface{}{"interruptId": input.InterruptResolution.InterruptID},
		)
	}
	pending := continuation.PendingInterruptions[resolutionIndex]

	resolutionValue, err := normalizeResolutionForPending(pending, input.InterruptResolution.Resolution, maxToolOutputBytes)
	if err != nil {
		return preparedContinuation{}, err
	}
	resolutions := append(append([]PendingResolution(nil), continuation.Resolutions...), PendingResolution{
		RunInterruptionID: pending.RunInterruptionID,
		Value:             resolutionValue,
	})

	if len(resolutions) < len(continuation.PendingInterruptions) {
		resolvedSecurity, serr := resolveContinuationSecurity(security)
		if serr != nil {
			return preparedContinuation{}, serr
		}
		unsigned := continuation
		unsigned.Resolutions = resolutions
		signed, serr := signContinuation(unsigned, resolvedSecurity)
		if serr != nil {
			return preparedContinuation{}, serr
		}
		next, ierr := toCodeModeInterrupt(signed, len(resolutions))
		if ierr != nil {
			return preparedContinuation{}, ierr
		}
		return preparedContinuation{nextInterrupt: next}, nil
	}

	if err := assertNoDeniedApproval(continuation.PendingInterruptions, resolutions); err != nil {
		return preparedContinuation{}, err
	}

	ledger, derr := decodeReplayLedger(continuation.Token)
	if derr != nil {
		return preparedContinuation{}, derr
	}
	// Index resumePendings/resumeResolutions by each pending interruption's
	// own call index (not its position in the batch -- see
	// callIndexForPending/replayRecord's doc comment): concurrent batching
	// means the batch's entries are not necessarily contiguous with the
	// replay ledger, or with each other.
	resumePendings := make(map[int]PendingInterruption, len(continuation.PendingInterruptions))
	resumeResolutions := make(map[int]interface{}, len(resolutions))
	for i, pending := range continuation.PendingInterruptions {
		callIdx, cerr := callIndexForPending(pending)
		if cerr != nil {
			return preparedContinuation{}, cerr
		}
		resumePendings[callIdx] = pending
		if i < len(resolutions) {
			resumeResolutions[callIdx] = resolutions[i].Value
		}
	}
	return preparedContinuation{
		replayLedger:      ledger,
		resumePendings:    resumePendings,
		resumeResolutions: resumeResolutions,
	}, nil
}

func normalizeResolutionForPending(pending PendingInterruption, resolution interface{}, maxToolOutputBytes int) (interface{}, error) {
	if pending.Payload.Kind() == ToolApprovalKind {
		decision, err := normalizeApprovalResolution(resolution)
		if err != nil {
			return nil, err
		}
		return decision, nil
	}
	outJSON, err := toJSONPayload(resolution, maxToolOutputBytes, "Resolution \""+pending.InterruptID+"\"")
	if err != nil {
		return nil, err
	}
	return fromJSONPayload(outJSON)
}

func assertNoDeniedApproval(pendingInterruptions []PendingInterruption, resolutions []PendingResolution) error {
	for i, pending := range pendingInterruptions {
		if pending.Payload.Kind() != ToolApprovalKind {
			continue
		}
		var value interface{}
		if i < len(resolutions) {
			value = resolutions[i].Value
		}
		decision, err := normalizeApprovalResolution(value)
		if err != nil {
			return err
		}
		if !decision.Approved {
			return NewToolApprovalDeniedError(pending.ToolName, pending.Input, pending.ToolCallID, decision.Reason)
		}
	}
	return nil
}

// buildInterruptResult signs a new Continuation covering every call
// completed so far (committed) plus the newly pending interruption batch
// (one or more entries collected together at the same job-queue-quiescence
// point -- see driveCodeModeExecution/toolBridge.pendingBatch), and
// returns an *Interrupt for the first of them. Mirrors the tail of
// TypeScript's runCodeMode (the catch-free success path that builds a
// fresh continuation from result.interruptions, which is likewise
// potentially multi-entry). Resolving the returned Interrupt advances
// through the rest of batch via the existing single-sandbox-call-free path
// in prepareContinuation (run_code_mode.go's prepareContinuation/
// toCodeModeInterrupt), exactly as it already does for a hypothetical
// multi-entry continuation from any source.
func buildInterruptResult(input RunInput, options *Options, outerToolCall string, toolNames []string, committed map[int]replayRecord, batch []PendingInterruption) (interface{}, error) {
	token, err := encodeReplayLedger(committed)
	if err != nil {
		return nil, err
	}
	unsigned := Continuation{
		Version:              2,
		JS:                   input.JS,
		OuterToolCallID:      outerToolCall,
		ToolNames:            toolNames,
		Token:                token,
		PendingInterruptions: batch,
		Resolutions:          []PendingResolution{},
	}

	var securityOpts ContinuationSecurityOptions
	if options != nil && options.ContinuationSecurity != nil {
		securityOpts = *options.ContinuationSecurity
	}
	security, serr := resolveContinuationSecurity(securityOpts)
	if serr != nil {
		return nil, serr
	}
	signed, serr2 := signContinuation(unsigned, security)
	if serr2 != nil {
		return nil, serr2
	}
	return toCodeModeInterrupt(signed, 0)
}

// toCodeModeInterrupt mirrors TypeScript's toCodeModeInterrupt.
func toCodeModeInterrupt(continuation Continuation, index int) (*Interrupt, error) {
	if index < 0 || index >= len(continuation.PendingInterruptions) {
		return nil, NewProtocolError("Code mode continuation has no pending interruption at the requested index.", nil)
	}
	pending := continuation.PendingInterruptions[index]
	return &Interrupt{
		Type:            InterruptTypeValue,
		InterruptID:     pending.InterruptID,
		ToolName:        pending.ToolName,
		ToolCallID:      pending.ToolCallID,
		OuterToolCallID: continuation.OuterToolCallID,
		Input:           pending.Input,
		Payload:         pending.Payload,
		Continuation:    continuation,
	}, nil
}

// guestRejection is the sandboxed script's own failure (its top-level
// promise rejected). message is the rejection's `message` when it is an
// Error, used to find the bridge error it carries (preservedCodeModeErr).
type guestRejection struct {
	err        error
	message    string
	hasMessage bool
}

func (e *guestRejection) Error() string { return e.err.Error() }
func (e *guestRejection) Unwrap() error { return e.err }

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// wrapCodeModeSource wraps (type-stripped) user JS as the body of an async
// IIFE, binding `tools` to the bridge proxy codeModeGuestSource installed
// (bindCodeModeDispatch). Mirrors TypeScript's createCodeModeSource; the
// proxy routes every name through the same bridge, and the Go host resolves
// an unknown name into "Unknown tool: x" (toolBridge.invoke), as TS's
// `__codeMode.missing` does.
func wrapCodeModeSource(js string) string {
	return "(async()=>{\n" +
		"const tools=__codeModeTools;\n" +
		js + "\n" +
		"})()"
}

// driveCodeModeExecution evaluates source (already wrapped by
// wrapCodeModeSource) via qjs.Context.EvalNoAutoAwait -- which, unlike
// plain Eval/QJS_Eval, never blocks draining the job queue waiting for a
// promise to settle (see EvalNoAutoAwait's doc comment) -- and drives it to
// a completed result or to the first point where the job queue is idle, no
// host call is in flight, and one or more calls are pending an interruption
// (interrupted return value). runInSandboxCtx (engine.go) calls this as its
// `drive` callback.
//
// Mirrors `run`'s worker and manager.js event loop: the job queue is drained
// completely (qjs.Context.RunPendingJobs) before the host delivers one
// settled call to the guest, in settlement order (toolBridge.nextResponse),
// and drains it again; with nothing to deliver and calls still in flight it
// waits for one to finish (the CPU budget does not run while it waits); with
// none in flight and interruptions pending it stops
// (settleInterruptionsIfQuiescent). Once the script's promise is fulfilled,
// its value is serialized and the guest checks for detached calls
// (serializeValuePayload / __runAssertNoDetachedBridgeCalls).
func driveCodeModeExecution(ctx context.Context, jsCtx *qjs.Context, bridge *toolBridge, source string) (resultJSON string, isUndefined bool, interrupted bool, err error) {
	value, eerr := jsCtx.EvalNoAutoAwait("code-mode.js", qjs.Code(source))
	if eerr != nil {
		bridge.markFinished()
		return "", false, false, eerr
	}

	for {
		if fatal := bridge.fatal(); fatal != nil {
			return "", false, false, fatal
		}

		if !value.IsPromise() {
			break
		}

		switch value.PromiseState() {
		case qjs.PromiseStateFulfilled:
			value = value.PromiseResult()
			continue
		case qjs.PromiseStateRejected:
			bridge.markFinished()
			reason := value.PromiseResult()
			rejected := &guestRejection{err: reason.Exception()}
			if reason.IsError() {
				msg := reason.GetPropertyStr("message")
				if msg.IsString() {
					rejected.message, rejected.hasMessage = msg.String(), true
				}
			}
			return "", false, false, rejected
		default: // qjs.PromiseStatePending
			ran, rerr := jsCtx.RunPendingJobs()
			if rerr != nil {
				return "", false, false, rerr
			}
			if ran > 0 {
				// Something settled or ran further; re-check fatal/state
				// from the top with fresh information.
				continue
			}

			// The job queue is idle. Take finished host calls, then hand
			// the guest the next settlement, one per drained queue.
			for bridge.takeCompletion() {
			}
			if fatal := bridge.fatal(); fatal != nil {
				return "", false, false, fatal
			}
			if resp, ok := bridge.nextResponse(); ok {
				if serr := settleGuest(jsCtx, bridge.guest, resp); serr != nil {
					return "", false, false, serr
				}
				continue
			}
			if bridge.inFlightCount() > 0 {
				budget := cpuBudgetFrom(bridge.ctx)
				if budget != nil {
					budget.pause()
				}
				werr := bridge.waitCompletion(ctx)
				if budget != nil {
					budget.resume()
				}
				if werr != nil {
					return "", false, false, werr
				}
				continue
			}
			if len(bridge.batchSnapshot()) > 0 {
				return "", false, true, nil
			}
			// Nothing pending, nothing fatal, yet the top-level promise
			// never settled and nothing more can run: the script itself
			// awaited a promise tied to no host call at all (e.g. `await
			// new Promise(() => {})`), not anything the bridge produced.
			return "", false, false, NewProtocolError(
				"Code mode script awaited a promise that no pending host call will ever settle.",
				nil,
			)
		}
	}

	bridge.markFinished()
	var js string
	undefined := value.IsUndefined()
	if !undefined {
		var jerr error
		js, jerr = value.JSONStringify()
		if jerr != nil {
			return "", false, false, jerr
		}
	}
	if derr := guestDetachedCheck(bridge.guest, bridge.toolNames); derr != nil {
		return "", false, false, derr
	}
	return js, undefined, false, nil
}
