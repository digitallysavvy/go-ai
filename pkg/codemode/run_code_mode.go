package codemode

import (
	"context"
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
	policy, perr := resolveExecutionPolicy(policyInput)
	if perr != nil {
		return nil, perr
	}

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

	var outerToolCall string
	switch {
	case input.ToolExecutionOptions != nil && input.ToolExecutionOptions.ToolCallID != "":
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

	bridge := newToolBridge(ctx, input, options, policy, outerToolCall, prepared.replayLedger, prepared.resumePendings, prepared.resumeResolutions)
	source := wrapCodeModeSource(stripTypeScriptAnnotations(input.JS))

	resultJSON, isUndefined, interrupted, err := runInSandbox(ctx, policy, func(jsCtx *qjs.Context) (string, bool, bool, error) {
		if berr := bindCodeModeDispatch(jsCtx, bridge); berr != nil {
			return "", false, false, berr
		}
		return driveCodeModeExecution(jsCtx, bridge, source)
	})

	committed := bridge.committedSnapshot()

	if interrupted {
		// Quiescence was reached (runInSandbox's poll loop; see
		// driveCodeModeExecution) with one or more host calls newly
		// pending -- every entry collected in the same wave (e.g. a
		// Promise.all([...]) where more than one call needed approval)
		// surfaces together as a single multi-item Continuation. Mirrors
		// TypeScript's run requiring the complete interruption batch to be
		// resolved together, from the one job-queue-quiescence point where
		// `run`'s manager.js collects them.
		batch := bridge.batchSnapshot()
		if len(batch) == 0 {
			return nil, NewProtocolError("Code mode reported an interruption with no pending interruptions recorded.", nil)
		}
		return buildInterruptResult(input, options, outerToolCall, toolNames, committed, batch)
	}

	if err != nil {
		// Prefer a preserved *typed* CodeModeError a bridge call raised
		// over the generic JavaScript exception text that comes back once
		// it round-trips through the sandbox (e.g. a script that catches a
		// rejected tool call's error and rethrows, or lets it propagate
		// unchanged) -- mirrors TypeScript's findPreservedCodeModeError.
		if codeModeErr := bridge.lastCodeModeErr(); codeModeErr != nil {
			return nil, codeModeErr
		}
		return nil, err
	}

	if batch := bridge.batchSnapshot(); len(batch) > 0 {
		// The sandboxed script never awaited (or otherwise observed) one
		// or more deliberately-unresolved interrupt Promises and completed
		// anyway. Mirrors TypeScript's CodeModeDetachedBridgeRequestError
		// (code-mode/src/errors.ts), translated from the underlying `run`
		// package's RUN_DETACHED_BRIDGE_REQUEST in toCodeModeRuntimeError.
		first := batch[0]
		return nil, NewDetachedBridgeRequestError(
			"Code mode requested a host bridge interruption that was never observed: the sandboxed script must not let the interruption go undetected and complete anyway.",
			map[string]interface{}{"toolName": first.ToolName, "toolCallId": first.ToolCallID},
		)
	}

	if isUndefined {
		return nil, nil
	}

	if serr := assertJSONPayloadSize(resultJSON, policy.MaxResultBytes, "Code mode result"); serr != nil {
		return nil, serr
	}
	return fromJSONPayload(resultJSON)
}

func assertSourceSize(source string, maxBytes int) error {
	bytes := len([]byte(source))
	if maxBytes > 0 && bytes > maxBytes {
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

	resolutionIndex := len(continuation.Resolutions)
	if resolutionIndex >= len(continuation.PendingInterruptions) {
		return preparedContinuation{}, NewProtocolError(
			"Code mode continuation has no further pending interruptions to resolve.", nil,
		)
	}
	pending := continuation.PendingInterruptions[resolutionIndex]
	if pending.InterruptID != input.InterruptResolution.InterruptID {
		return preparedContinuation{}, NewProtocolError(
			"Interrupt resolution does not match the next pending code-mode interruption.",
			map[string]interface{}{"interruptId": input.InterruptResolution.InterruptID},
		)
	}

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
// IIFE, exposing `tools` as a Proxy that forwards every property access to
// the single __codeModeDispatch host function bound by
// bindCodeModeDispatch. Mirrors TypeScript's createCodeModeSource; this
// port uses one dispatch function plus a real JS Proxy instead of TS's
// numbered per-tool-name bindings, since the Go host-function bridge below
// already resolves unknown tool names into a proper "Unknown tool: x"
// CodeModeError (see toolBridge.invoke) without needing to know the tool
// set up front.
func wrapCodeModeSource(js string) string {
	return "(async()=>{\n" +
		"const tools=new Proxy(Object.create(null),{get(_t,name){return (input)=>__codeModeDispatch(String(name),input);}});\n" +
		js + "\n" +
		"})()"
}

// bindCodeModeDispatch installs the single global host function that every
// `tools.<name>(input)` call is routed through by the Proxy in
// wrapCodeModeSource, as a genuine async host function (isAsync=true):
// calling it returns a real JS Promise immediately, synchronously, without
// blocking on bridge.invoke's outcome -- this is what lets a guest
// `Promise.all([tools.a(x), tools.b(y)])` dispatch both calls before
// either one's Promise settles, so Go can collect every call that decided
// to interrupt in the same wave together (see
// toolBridge.pendingBatch/driveCodeModeExecution) instead of surfacing
// only the first one. See This.Promise's doc comment
// (pkg/internal/third_party/qjs/value.go) and QJS_CreateFunctionProxy's
// generated JS wrapper (the inline `async function QJS_AsyncFunctionProxy`
// source in qjswasm/function.c) for how the underlying
// async-function-returns-immediately-with-a-pending-promise mechanism
// works.
func bindCodeModeDispatch(jsCtx *qjs.Context, bridge *toolBridge) error {
	dispatch := jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) == 0 {
			return nil, NewProtocolError("code-mode dispatch called without a tool name.", nil)
		}
		toolName := args[0].String()

		inputJSON := ""
		if len(args) > 1 && !args[1].IsUndefined() {
			js, err := args[1].JSONStringify()
			if err != nil {
				return nil, err
			}
			inputJSON = js
		}

		promise := this.Promise()

		outJSON, ierr := bridge.invoke(toolName, inputJSON)
		if ierr != nil {
			return settleDispatchFailure(jsCtx, bridge, promise, ierr)
		}

		var resultVal *qjs.Value
		if outJSON == "" {
			resultVal = jsCtx.NewUndefined()
		} else {
			resultVal = jsCtx.ParseJSON(outJSON)
		}
		if rerr := promise.Resolve(resultVal); rerr != nil {
			return nil, rerr
		}
		return nil, nil
	}, true)
	jsCtx.Global().SetPropertyStr("__codeModeDispatch", dispatch)
	return nil
}

// settleDispatchFailure decides how one failed `tools.x(input)` dispatch
// affects the Promise __codeModeDispatch already returned for it,
// mirroring the three-way split TypeScript's genuinely concurrent dispatch
// gets for free by running each call as its own independent async host
// function (code-mode/src/run-code-mode.ts's invokeCodeModeTool):
//
//   - *interruptUnwind (toolBridge.raiseInterrupt: approval needed, or a
//     host tool called RequestCodeModeInterrupt): leave the Promise
//     deliberately unresolved so the sandboxed script keeps making
//     synchronous progress -- e.g. the rest of a concurrent Promise.all
//     array -- until nothing more can run
//     (driveCodeModeExecution's poll loop is what notices that and
//     collects every such call from the same wave together).
//   - *AbortedError/*BridgeLimitError: fatal to the whole invocation, not
//     just this call -- mirrors TypeScript's failTerminal, which aborts
//     the entire worker run for exactly these two conditions outside the
//     per-call promise machinery entirely (run-code-mode.ts's
//     markWorkerRequest, called before a call's promise even exists).
//     Recording it on the bridge (toolBridge.setFatal) rather than
//     rejecting the Promise is what lets driveCodeModeExecution notice it
//     immediately even if the sandboxed script would otherwise catch -- or
//     never even observe -- a plain rejection.
//   - anything else (unknown tool, a failing tool.Execute, a denied
//     callback-mode approval, an oversized payload, ...): reject this
//     call's own Promise and let the sandbox's own Promise.all/await
//     semantics decide what happens next, exactly as TypeScript's
//     invokeCodeModeTool throwing inside one `__codeMode.toolN` async host
//     function only rejects that one call's Promise.
func settleDispatchFailure(jsCtx *qjs.Context, bridge *toolBridge, promise *qjs.Value, ierr error) (*qjs.Value, error) {
	if _, ok := ierr.(*interruptUnwind); ok {
		return nil, nil
	}
	switch fatal := ierr.(type) {
	case *AbortedError:
		bridge.setFatal(fatal)
		return nil, nil
	case *BridgeLimitError:
		bridge.setFatal(fatal)
		return nil, nil
	}
	errVal := jsCtx.NewError(ierr)
	if rerr := promise.Reject(errVal); rerr != nil {
		return nil, rerr
	}
	return nil, nil
}

// driveCodeModeExecution evaluates source (already wrapped by
// wrapCodeModeSource) via qjs.Context.EvalNoAutoAwait -- which, unlike
// plain Eval/QJS_Eval, never blocks draining the job queue waiting for a
// promise to settle (see EvalNoAutoAwait's doc comment) -- and drives it
// either to a completed result or to the first point where the job queue
// goes quiescent with one or more host calls newly pending (interrupted
// return value), using qjs.Context.RunPendingJobs/Value.PromiseState/
// Value.PromiseResult instead of Value.Await. runInSandbox
// (pkg/codemode/engine.go) calls this as its `drive` callback.
//
// Mirrors, at a much smaller scale, `run`'s own manager.js: its
// settleInterruptionsIfQuiescent (pendingInterruptionIndexes.size > 0 &&
// inFlightBridgeRequests === 0) is the same "nothing more can run, and at
// least one call is pending" condition this checks once RunPendingJobs
// reports no more jobs ran.
func driveCodeModeExecution(jsCtx *qjs.Context, bridge *toolBridge, source string) (resultJSON string, isUndefined bool, interrupted bool, err error) {
	value, eerr := jsCtx.EvalNoAutoAwait("code-mode.js", qjs.Code(source))
	if eerr != nil {
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
			reason := value.PromiseResult()
			return "", false, false, reason.Exception()
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

			// The job queue is quiescent: nothing more can run right now
			// without one of the deliberately-unresolved interrupt
			// Promises settling.
			if fatal := bridge.fatal(); fatal != nil {
				return "", false, false, fatal
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

	if value.IsUndefined() {
		return "", true, false, nil
	}
	js, jerr := value.JSONStringify()
	if jerr != nil {
		return "", false, false, jerr
	}
	return js, false, false, nil
}
