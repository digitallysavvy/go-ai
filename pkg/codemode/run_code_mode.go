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

	resultJSON, isUndefined, err := runInSandbox(ctx, policy, source, func(jsCtx *qjs.Context) error {
		return bindCodeModeDispatch(jsCtx, bridge)
	})

	bridge.mu.Lock()
	pendingNew, pendingNewIndex := bridge.pendingNew, bridge.pendingNewIndex
	codeModeErr, codeModeErrIndex := bridge.lastCodeModeErr, bridge.lastCodeModeErrIndex
	committed := append([]replayRecord(nil), bridge.committed...)
	bridge.mu.Unlock()

	if err != nil {
		// A genuine interrupt escaped the sandbox as a JS exception (see
		// toolBridge.raiseInterrupt) and is the most recent bridge failure
		// -- prefer it over a stale CodeModeError an earlier call in this
		// same invocation already caught and the script continued past.
		if pendingNew != nil && (codeModeErr == nil || pendingNewIndex >= codeModeErrIndex) {
			return buildInterruptResult(input, options, outerToolCall, toolNames, committed, *pendingNew)
		}
		if codeModeErr != nil {
			return nil, codeModeErr
		}
		return nil, err
	}

	if pendingNew != nil {
		// The sandboxed script caught the interrupt's exception (see
		// raiseInterrupt) and completed anyway instead of letting it
		// unwind: the pending host bridge work was started but never
		// surfaced to the caller for resolution. Mirrors TypeScript's
		// CodeModeDetachedBridgeRequestError (code-mode/src/errors.ts),
		// translated from the underlying `run` package's
		// RUN_DETACHED_BRIDGE_REQUEST in toCodeModeRuntimeError.
		return nil, NewDetachedBridgeRequestError(
			"Code mode requested a host bridge interruption that was never observed: the sandboxed script must not catch the interruption and continue.",
			map[string]interface{}{"toolName": pendingNew.ToolName, "toolCallId": pendingNew.ToolCallID},
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
	// interruption has a resolution: see toolBridge's doc comment.
	replayLedger      []replayRecord
	resumePendings    []PendingInterruption
	resumeResolutions []interface{}
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
	resolutionValues := make([]interface{}, len(resolutions))
	for i, r := range resolutions {
		resolutionValues[i] = r.Value
	}
	return preparedContinuation{
		replayLedger:      ledger,
		resumePendings:    continuation.PendingInterruptions,
		resumeResolutions: resolutionValues,
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
// completed so far (committed) plus the newly pending interruption, and
// returns the resulting *Interrupt. Mirrors the tail of TypeScript's
// runCodeMode (the catch-free success path that builds a fresh
// continuation from result.interruptions).
func buildInterruptResult(input RunInput, options *Options, outerToolCall string, toolNames []string, committed []replayRecord, pending PendingInterruption) (interface{}, error) {
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
		PendingInterruptions: []PendingInterruption{pending},
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
// wrapCodeModeSource.
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

		outJSON, ierr := bridge.invoke(toolName, inputJSON)
		if ierr != nil {
			return nil, ierr
		}
		if outJSON == "" {
			return jsCtx.NewUndefined(), nil
		}
		return jsCtx.ParseJSON(outJSON), nil
	})
	jsCtx.Global().SetPropertyStr("__codeModeDispatch", dispatch)
	return nil
}
