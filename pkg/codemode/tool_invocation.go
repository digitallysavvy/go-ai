package codemode

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// toolBridge dispatches `tools.<name>(input)` calls made from sandboxed
// JavaScript to the corresponding host tool. One bridge is created per
// Run/RunCodeMode invocation. Mirrors TypeScript's invokeHostTool
// (code-mode/src/tool-invocation.ts) plus the per-invocation bookkeeping
// from run-code-mode.ts's createHostFunctions/invokeCodeModeTool, and (see
// the package doc's "Deterministic replay" section) the replay/resume
// machinery from run-code-mode.ts's context.resume handling.
type toolBridge struct {
	ctx           context.Context
	tools         ToolSet
	baseOptions   types.ToolExecutionOptions
	options       *Options
	policy        resolvedPolicy
	outerToolCall string

	// replayLedger holds every call from a prior invocation of the
	// identical source that had already completed, keyed by its 0-based
	// call index (see replayRecord's doc comment for why this is sparse,
	// not a "calls 0..N-1" prefix, since CM3's concurrent batching); invoke
	// short-circuits an index present here with its recorded output
	// instead of calling the real host tool. nil on a fresh
	// (non-continuation) invocation.
	replayLedger map[int]replayRecord

	// resumePendings/resumeResolutions cover exactly the call indices of
	// the continuation's PendingInterruptions (recovered from each one's
	// RunInterruptionID via callIndexForPending, not from their position
	// in the batch -- see prepareContinuation in run_code_mode.go): each
	// is resumed with its resolution (skipping approval, for the approval
	// kind, or populating types.ToolExecutionOptions.CodeModeInterrupt,
	// for any other kind) instead of interrupting again. Parallel maps,
	// keyed by call index; nil/empty on a fresh invocation. A call index
	// present in neither replayLedger nor here executes for real, exactly
	// like a fresh invocation.
	resumePendings    map[int]PendingInterruption
	resumeResolutions map[int]interface{}

	mu           sync.Mutex
	requestCount int

	// committed accumulates every call resolved so far this invocation,
	// keyed by its 0-based call index (see replayRecord's doc comment for
	// why this must be index-keyed rather than append-ordered: a
	// later-dispatched call can settle before an earlier-dispatched one
	// still pending approval in the same batch) -- short-circuited replay
	// calls and resumed/fresh real calls alike -- so a *new* interrupt
	// raised later in this same invocation can encode the next
	// continuation's replay ledger from it (see buildInterruptResult in
	// run_code_mode.go).
	committed map[int]replayRecord

	// pendingBatch accumulates every call that decided to interrupt
	// (approval under ApprovalModeInterrupt, or a host tool calling
	// RequestCodeModeInterrupt) during the current invocation, in the call
	// order each one was dispatched. A dispatch that decides to interrupt
	// never resolves or rejects its own Promise (see invoke's doc comment
	// and bindCodeModeDispatch in run_code_mode.go): the sandboxed script
	// keeps running -- issuing further concurrent dispatches, e.g. the
	// rest of a `Promise.all([...])` array -- until the job queue goes
	// quiescent (runInSandbox's poll loop, driven by
	// qjs.Context.RunPendingJobs), at which point every entry collected
	// here so far is surfaced together as one multi-item Interrupt batch.
	// This is what lets Promise.all([tools.a(x), tools.b(y)]), where both
	// need approval, produce a single two-item Continuation instead of two
	// single-item ones chained together.
	pendingBatch []PendingInterruption

	// fatalErr, once set, wins over everything else once runInSandbox next
	// polls the bridge: an aborted context or an exceeded bridge-request
	// limit must stop the whole invocation immediately, not just the one
	// call that observed it (mirrors TypeScript's failTerminal, which
	// aborts the entire worker run for exactly these two conditions,
	// outside the per-call promise machinery entirely -- see
	// run-code-mode.ts's markWorkerRequest). Every other bridge failure
	// (an unknown tool, a failing tool.Execute, a denied callback-mode
	// approval, an oversized payload, ...) only rejects its own call's
	// Promise, exactly as TypeScript's invokeCodeModeTool throwing inside
	// one `__codeMode.toolN` async host function only rejects that one
	// call.
	fatalErr CodeModeError

	// lastTypedErr/lastTypedErrIndex record the most recent CodeModeError
	// produced by a bridge call, so RunCodeMode can re-surface the
	// original typed error (via the lastCodeModeErr getter) instead of the
	// generic JavaScript exception text that comes back once it
	// round-trips through the sandbox (mirrors TypeScript's
	// codeModeErrors accumulator / findPreservedCodeModeError).
	lastTypedErr      CodeModeError
	lastTypedErrIndex int
}

func newToolBridge(ctx context.Context, input RunInput, options *Options, policy resolvedPolicy, outerToolCall string, replayLedger map[int]replayRecord, resumePendings map[int]PendingInterruption, resumeResolutions map[int]interface{}) *toolBridge {
	base := types.ToolExecutionOptions{}
	if input.ToolExecutionOptions != nil {
		base = *input.ToolExecutionOptions
	}
	return &toolBridge{
		ctx:               ctx,
		tools:             input.Tools,
		baseOptions:       base,
		options:           options,
		policy:            policy,
		outerToolCall:     outerToolCall,
		replayLedger:      replayLedger,
		resumePendings:    resumePendings,
		resumeResolutions: resumeResolutions,
		committed:         make(map[int]replayRecord),
	}
}

// invoke handles one `tools.<name>(input)` call. inputJSON is "" when the
// sandbox called the tool with no arguments.
func (b *toolBridge) invoke(toolName, inputJSON string) (outputJSON string, err error) {
	defer func() {
		if err != nil {
			if cmErr, ok := err.(CodeModeError); ok {
				b.mu.Lock()
				b.lastTypedErr = cmErr
				b.lastTypedErrIndex = b.requestCount
				b.mu.Unlock()
			}
		}
	}()

	if cErr := b.ctx.Err(); cErr != nil {
		return "", NewAbortedError()
	}

	b.mu.Lock()
	b.requestCount++
	n := b.requestCount
	b.mu.Unlock()
	if b.policy.MaxBridgeRequests > 0 && n > b.policy.MaxBridgeRequests {
		return "", NewBridgeLimitError(
			fmt.Sprintf("Code mode exceeded the %d bridge request limit.", b.policy.MaxBridgeRequests),
			map[string]interface{}{"maxBridgeRequests": b.policy.MaxBridgeRequests},
		)
	}
	idx := n - 1 // 0-based call index, aligned with replayLedger/resumePendings.

	// Short-circuit a call already resolved by a prior invocation of this
	// same source: return its recorded result without touching the real
	// host tool, so side effects never repeat. Mirrors TypeScript's
	// deterministic replay of a resumed run. Keyed by call index, not
	// position (see replayLedger's doc comment): a later-dispatched call
	// can have settled, and so be present here, while an earlier-dispatched
	// one is not.
	if rec, ok := b.replayLedger[idx]; ok {
		b.mu.Lock()
		b.committed[idx] = rec
		b.mu.Unlock()
		return rec.OutputJSON, nil
	}

	tool, ok := b.tools[toolName]
	if !ok {
		names := make([]string, 0, len(b.tools))
		for name := range b.tools {
			names = append(names, name)
		}
		return "", NewToolError(fmt.Sprintf("Unknown tool: %s", toolName), map[string]interface{}{
			"toolName":       toolName,
			"availableTools": names,
		})
	}
	if tool.Execute == nil {
		return "", NewToolError(fmt.Sprintf("Tool %q does not have execute().", toolName), map[string]interface{}{"toolName": toolName})
	}

	if err := assertJSONPayloadSize(inputJSON, b.policy.MaxToolInputBytes, fmt.Sprintf("Tool %q input", toolName)); err != nil {
		return "", err
	}
	inputVal, ferr := fromJSONPayload(inputJSON)
	if ferr != nil {
		return "", ferr
	}
	input, _ := inputVal.(map[string]interface{})
	if input == nil {
		input = map[string]interface{}{}
	}

	if validator := toolInputValidator(tool); validator != nil {
		// TS's validateToolInput uses asSchema(...).validate, which for a
		// zod schema fills .default() values as part of validation and
		// passes validation.value (not the raw input) on to execute. Apply
		// defaults before validating for the same effect (schema.Validate
		// alone does not fill defaults).
		input = applyToolInputDefaults(input, validator)
		if verr := validator.Validate(input); verr != nil {
			return "", NewToolError(
				fmt.Sprintf("Invalid input for tool %q: %s", toolName, verr.Error()),
				map[string]interface{}{"toolName": toolName, "input": input, "cause": verr.Error()},
			)
		}
	}

	toolCallID := fmt.Sprintf("%s:tool-%d", b.outerToolCall, n)
	execOptions := b.baseOptions
	execOptions.ToolCallID = toolCallID

	// Determine whether this call is resuming a previously pending
	// interruption (see the replayLedger/resumePendings doc above), keyed
	// by call index directly.
	skipApproval := false
	if pending, ok := b.resumePendings[idx]; ok {
		resolution := b.resumeResolutions[idx]
		if pending.Payload.Kind() == ToolApprovalKind {
			decision, derr := normalizeApprovalResolution(resolution)
			if derr != nil {
				return "", derr
			}
			if !decision.Approved {
				return "", NewToolApprovalDeniedError(pending.ToolName, pending.Input, pending.ToolCallID, decision.Reason)
			}
			skipApproval = true
		} else {
			execOptions.CodeModeInterrupt = &InterruptExecutionContext{
				InterruptID: pending.InterruptID,
				Payload:     pending.Payload,
				Resolution:  resolution,
			}
		}
	}

	if !skipApproval {
		needsApproval, aerr := raceAgainstAbort(b.ctx, func() (bool, error) {
			return resolveNeedsApproval(b.ctx, tool, input, execOptions)
		})
		if aerr != nil {
			return "", aerr
		}
		if needsApproval {
			if err := b.resolveApproval(tool, toolName, input, toolCallID, n); err != nil {
				return "", err
			}
		}
	}

	output, oerr := raceAgainstAbort(b.ctx, func() (interface{}, error) {
		return tool.Execute(b.ctx, input, execOptions)
	})
	if oerr != nil {
		if cmErr, ok := oerr.(CodeModeError); ok && cmErr.ErrorCode() == "CODE_MODE_ABORTED" {
			// Mirrors TypeScript's raceAgainstAbort: cancellation wins
			// the race and propagates as-is, unsanitized (it is not a
			// tool-thrown error).
			return "", oerr
		}
		if sig, ok := oerr.(*interruptSignal); ok {
			// The tool called RequestCodeModeInterrupt: pause here
			// instead of sanitizing this as a tool failure.
			return "", b.raiseInterrupt(toolName, input, toolCallID, n, sig.payload)
		}
		// Mirrors TypeScript's invokeCodeModeTool: any non-CodeModeError
		// thrown by a host tool is sanitized to a generic message so raw
		// tool internals never leak into the sandbox.
		return "", NewToolError("Host tool failed.", map[string]interface{}{"toolName": toolName, "cause": oerr.Error()})
	}

	outJSON, jerr := toJSONPayload(output, b.policy.MaxToolOutputBytes, fmt.Sprintf("Tool %q output", toolName))
	if jerr != nil {
		return "", jerr
	}
	b.mu.Lock()
	b.committed[idx] = replayRecord{ToolName: toolName, InputJSON: inputJSON, OutputJSON: outJSON}
	b.mu.Unlock()
	return outJSON, nil
}

// raiseInterrupt records the call at index n-1 (1-based n, matching
// invoke's toolCallID numbering) as one of the invocation's pending
// interruptions (see pendingBatch's doc comment) and returns the sentinel
// error bindCodeModeDispatch recognizes to leave this call's Promise
// deliberately unresolved instead of rejecting it, so the sandboxed script
// keeps making synchronous progress (e.g. the rest of a concurrent
// Promise.all array) until nothing more can run.
//
// Enforces policy.MaxInFlightBridgeRequests against the batch collected so
// far: TypeScript enforces the same limit (run-code-mode.ts's
// maxInFlightBridgeRequests -> run's RunLimits.maxInFlightBridgeRequests)
// against however many host calls are concurrently dispatched-but-not-yet-
// settled; in this port that count is exactly len(pendingBatch), since
// every non-interrupting call already resolves synchronously before the
// next dispatch happens (see invoke).
func (b *toolBridge) raiseInterrupt(toolName string, input interface{}, toolCallID string, n int, payload InterruptPayload) error {
	pending := PendingInterruption{
		RunInterruptionID: fmt.Sprintf("interrupt-%d", n),
		InterruptID:       toolCallID + ":interrupt",
		ToolName:          toolName,
		ToolCallID:        toolCallID,
		Input:             input,
		Payload:           payload,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.policy.MaxInFlightBridgeRequests > 0 && len(b.pendingBatch) >= b.policy.MaxInFlightBridgeRequests {
		b.fatalErr = NewBridgeLimitError(
			fmt.Sprintf("Code mode exceeded the %d in-flight bridge request limit.", b.policy.MaxInFlightBridgeRequests),
			map[string]interface{}{"maxInFlightBridgeRequests": b.policy.MaxInFlightBridgeRequests},
		)
		return b.fatalErr
	}
	b.pendingBatch = append(b.pendingBatch, pending)
	return &interruptUnwind{pending: pending}
}

// interruptUnwind is the sentinel error returned by invoke when a call
// raises a pending interruption. bindCodeModeDispatch (run_code_mode.go)
// recognizes it by type and leaves the call's Promise deliberately
// unresolved instead of rejecting it -- unless the sandboxed script
// finishes (or itself settles) without ever awaiting that Promise, in
// which case RunCodeMode reports *DetachedBridgeRequestError (see the
// package doc).
type interruptUnwind struct {
	pending PendingInterruption
}

func (e *interruptUnwind) Error() string {
	return fmt.Sprintf("Code mode execution paused: tool %q requested an interruption (kind=%q).", e.pending.ToolName, e.pending.Payload.Kind())
}

// batchSnapshot returns a copy of the interruptions collected so far (see
// pendingBatch's doc comment).
func (b *toolBridge) batchSnapshot() []PendingInterruption {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pendingBatch) == 0 {
		return nil
	}
	return append([]PendingInterruption(nil), b.pendingBatch...)
}

// committedSnapshot returns a copy of every call resolved so far, keyed by
// call index (see the committed field's doc comment).
func (b *toolBridge) committedSnapshot() map[int]replayRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	snap := make(map[int]replayRecord, len(b.committed))
	for idx, rec := range b.committed {
		snap[idx] = rec
	}
	return snap
}

// lastCodeModeErr returns the most recent typed CodeModeError a bridge
// call produced, if any (see lastTypedErr's doc comment).
func (b *toolBridge) lastCodeModeErr() CodeModeError {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastTypedErr
}

// fatal returns the invocation's fatal error, if any (see fatalErr's doc
// comment).
func (b *toolBridge) fatal() CodeModeError {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fatalErr
}

// setFatal records err as the invocation's fatal error unless one is
// already recorded (first one wins, matching TypeScript's failTerminal
// being a no-op once terminalReached is set).
func (b *toolBridge) setFatal(err CodeModeError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fatalErr == nil {
		b.fatalErr = err
	}
}

func (b *toolBridge) resolveApproval(tool types.Tool, toolName string, input map[string]interface{}, toolCallID string, n int) error {
	mode := ApprovalModeCallback
	var onApprovalRequired OnApprovalRequiredFunc
	if b.options != nil && b.options.Approval != nil {
		if b.options.Approval.Mode != "" {
			mode = b.options.Approval.Mode
		}
		onApprovalRequired = b.options.Approval.OnApprovalRequired
	}

	if mode == ApprovalModeInterrupt {
		return b.raiseInterrupt(toolName, input, toolCallID, n, InterruptPayload{"kind": ToolApprovalKind})
	}

	if onApprovalRequired == nil {
		return NewToolApprovalRequiredError(toolName, input, toolCallID)
	}

	decision, derr := raceAgainstAbort(b.ctx, func() (ApprovalDecision, error) {
		return onApprovalRequired(b.ctx, ApprovalRequest{ToolName: toolName, Input: input, ToolCallID: toolCallID})
	})
	if derr != nil {
		return derr
	}
	if !decision.Approved {
		return NewToolApprovalDeniedError(toolName, input, toolCallID, decision.Reason)
	}
	return nil
}

// resolveNeedsApproval evaluates a host tool's approval requirement.
// Mirrors TypeScript's requiresApproval, which only recognizes
// tool.needsApproval as a boolean or a function (unlike the richer
// tool-approval status system used by the outer AI SDK step loop).
func resolveNeedsApproval(ctx context.Context, tool types.Tool, input map[string]interface{}, options types.ToolExecutionOptions) (bool, error) {
	setting := tool.ToolApproval
	if setting == nil {
		setting = tool.NeedsApproval
	}
	switch v := setting.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case types.NeedsApprovalFunc:
		return v(ctx, input), nil
	case types.ToolNeedsApprovalFunc:
		return v(ctx, input, types.ToolNeedsApprovalOptions{
			ToolCallID: options.ToolCallID,
			Messages:   options.Messages,
			Context:    options.ToolContext,
		}), nil
	default:
		return false, nil
	}
}

// applyToolInputDefaults fills any JSON-Schema "default" values missing from
// input. Mirrors pkg/ai/tool_call_pipeline.go's applyToolCallInputDefaults;
// never mutates input, and returns it unchanged if the defaulted result
// can't be represented as an object.
func applyToolInputDefaults(input map[string]interface{}, validator schema.Validator) map[string]interface{} {
	defaulted := schema.ApplyDefaults(input, schema.NewSimpleJSONSchema(validator.JSONSchema()))
	if obj, ok := defaulted.(map[string]interface{}); ok {
		return obj
	}
	return input
}

// toolInputValidator returns a JSON-schema validator for the tool's input,
// or nil when the tool has no JSON schema. Mirrors
// pkg/ai/tool_call_pipeline.go's toolCallInputValidator.
func toolInputValidator(tool types.Tool) schema.Validator {
	switch s := tool.Parameters.(type) {
	case map[string]interface{}:
		if len(s) == 0 {
			return nil
		}
		return schema.NewJSONSchema(s)
	case schema.Schema:
		if s == nil {
			return nil
		}
		return s.Validator()
	}
	return nil
}

// raceAgainstAbort runs fn in its own goroutine and returns its result,
// unless ctx is done first -- in which case it returns immediately with
// *AbortedError without waiting for fn to finish. If fn never returns (a
// misbehaving host tool that ignores context cancellation), its goroutine
// is abandoned; this is the same accepted trade-off runInSandbox makes for
// a timed-out sandbox invocation (see engine.go's doc comment) -- Go has
// no way to forcibly stop a goroutine, so the alternative is to hang
// indefinitely, which is worse.
//
// Mirrors TypeScript's raceAgainstAbort (code-mode/src/tool-invocation.ts),
// which races every nested host-tool step (needsApproval, the approval
// callback, execute) against the outer AbortSignal so cancellation takes
// effect immediately instead of only being observed between bridge calls.
func raceAgainstAbort[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, NewAbortedError()
	}

	type outcome struct {
		v   T
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		v, err := fn()
		ch <- outcome{v, err}
	}()

	select {
	case out := <-ch:
		return out.v, out.err
	case <-ctx.Done():
		return zero, NewAbortedError()
	}
}
