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

	// replayLedger holds calls 0..len(replayLedger)-1 from a prior
	// invocation of the identical source, in call order; invoke
	// short-circuits them with their recorded output instead of calling
	// the real host tool. nil on a fresh (non-continuation) invocation.
	replayLedger []replayRecord

	// resumePendings/resumeResolutions cover calls
	// len(replayLedger)..len(replayLedger)+len(resumePendings)-1: each is
	// resumed with its resolution (skipping approval, for the approval
	// kind, or populating types.ToolExecutionOptions.CodeModeInterrupt,
	// for any other kind) instead of interrupting again. Parallel slices;
	// nil/empty on a fresh invocation. Calls beyond this range execute for
	// real, exactly like a fresh invocation.
	resumePendings    []PendingInterruption
	resumeResolutions []interface{}

	mu           sync.Mutex
	requestCount int

	// committed accumulates every call resolved so far, in order --
	// short-circuited replay calls and resumed/fresh real calls alike --
	// so a *new* interrupt raised later in this same invocation can encode
	// the next continuation's replay ledger from it (see
	// buildInterruptResult in run_code_mode.go).
	committed []replayRecord

	// pendingNew/pendingNewIndex record the most recent call that decided
	// to interrupt (approval under ApprovalModeInterrupt, or a host tool
	// calling RequestCodeModeInterrupt), together with the 0-based call
	// index it happened at. RunCodeMode inspects this after the sandbox
	// returns to tell a genuine escaping interrupt (sandbox failed, and
	// this is the most recent failure -- see lastCodeModeErrIndex) from a
	// *DetachedBridgeRequestError (sandbox nonetheless "succeeded",
	// meaning the sandboxed script caught and discarded the interrupt
	// instead of letting it unwind -- see the package doc).
	pendingNew      *PendingInterruption
	pendingNewIndex int

	// lastCodeModeErr/lastCodeModeErrIndex record the most recent
	// CodeModeError produced by a bridge call, so RunCodeMode can
	// re-surface the original typed error instead of the generic
	// JavaScript exception text that comes back once it round-trips
	// through the sandbox (mirrors TypeScript's codeModeErrors
	// accumulator / findPreservedCodeModeError). The index lets RunCodeMode
	// prefer whichever of this or pendingNew happened later, when a script
	// catches an earlier failure of one kind and a later one of the other
	// kind escapes uncaught.
	lastCodeModeErr      CodeModeError
	lastCodeModeErrIndex int
}

func newToolBridge(ctx context.Context, input RunInput, options *Options, policy resolvedPolicy, outerToolCall string, replayLedger []replayRecord, resumePendings []PendingInterruption, resumeResolutions []interface{}) *toolBridge {
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
	}
}

// invoke handles one `tools.<name>(input)` call. inputJSON is "" when the
// sandbox called the tool with no arguments.
func (b *toolBridge) invoke(toolName, inputJSON string) (outputJSON string, err error) {
	defer func() {
		if err != nil {
			if cmErr, ok := err.(CodeModeError); ok {
				b.mu.Lock()
				b.lastCodeModeErr = cmErr
				b.lastCodeModeErrIndex = b.requestCount
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
	// deterministic replay of a resumed run.
	if idx < len(b.replayLedger) {
		rec := b.replayLedger[idx]
		b.mu.Lock()
		b.committed = append(b.committed, rec)
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
	// interruption (see the replayLedger/resumePendings doc above).
	skipApproval := false
	resumeOffset := idx - len(b.replayLedger)
	if resumeOffset >= 0 && resumeOffset < len(b.resumePendings) {
		pending := b.resumePendings[resumeOffset]
		resolution := b.resumeResolutions[resumeOffset]
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
	b.committed = append(b.committed, replayRecord{ToolName: toolName, InputJSON: inputJSON, OutputJSON: outJSON})
	b.mu.Unlock()
	return outJSON, nil
}

// raiseInterrupt records the call at index n-1 (1-based n, matching
// invoke's toolCallID numbering) as the invocation's pending interruption
// and returns the sentinel error that unwinds the sandboxed script.
// RunCodeMode inspects toolBridge.pendingNew after the sandbox returns to
// build the resulting Interrupt/Continuation (see buildInterruptResult).
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
	b.pendingNew = &pending
	b.pendingNewIndex = n
	b.mu.Unlock()
	return &interruptUnwind{pending: pending}
}

// interruptUnwind is the sentinel error returned by invoke when a call
// raises a pending interruption, propagated as a JS exception the way any
// other bridge error is (see bindCodeModeDispatch in run_code_mode.go) so
// it unwinds the sandboxed script -- unless the script itself catches it,
// in which case RunCodeMode reports *DetachedBridgeRequestError (see the
// package doc).
type interruptUnwind struct {
	pending PendingInterruption
}

func (e *interruptUnwind) Error() string {
	return fmt.Sprintf("Code mode execution paused: tool %q requested an interruption (kind=%q).", e.pending.ToolName, e.pending.Payload.Kind())
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
