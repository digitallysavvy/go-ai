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
// from run-code-mode.ts's createHostFunctions/invokeCodeModeTool.
type toolBridge struct {
	ctx           context.Context
	tools         ToolSet
	baseOptions   types.ToolExecutionOptions
	options       *Options
	policy        resolvedPolicy
	outerToolCall string

	mu           sync.Mutex
	requestCount int
	// lastCodeModeErr records the most recent CodeModeError produced by a
	// bridge call, so RunCodeMode can re-surface the original typed error
	// instead of the generic JavaScript exception text that comes back
	// once it round-trips through the sandbox (mirrors TypeScript's
	// codeModeErrors accumulator / findPreservedCodeModeError).
	lastCodeModeErr CodeModeError
}

func newToolBridge(ctx context.Context, input RunInput, options *Options, policy resolvedPolicy, outerToolCall string) *toolBridge {
	base := types.ToolExecutionOptions{}
	if input.ToolExecutionOptions != nil {
		base = *input.ToolExecutionOptions
	}
	return &toolBridge{
		ctx:           ctx,
		tools:         input.Tools,
		baseOptions:   base,
		options:       options,
		policy:        policy,
		outerToolCall: outerToolCall,
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

	needsApproval, aerr := raceAgainstAbort(b.ctx, func() (bool, error) {
		return resolveNeedsApproval(b.ctx, tool, input, execOptions)
	})
	if aerr != nil {
		return "", aerr
	}
	if needsApproval {
		if err := b.resolveApproval(tool, toolName, input, toolCallID); err != nil {
			return "", err
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
		// Mirrors TypeScript's invokeCodeModeTool: any non-CodeModeError
		// thrown by a host tool is sanitized to a generic message so raw
		// tool internals never leak into the sandbox.
		return "", NewToolError("Host tool failed.", map[string]interface{}{"toolName": toolName, "cause": oerr.Error()})
	}

	return toJSONPayload(output, b.policy.MaxToolOutputBytes, fmt.Sprintf("Tool %q output", toolName))
}

func (b *toolBridge) resolveApproval(tool types.Tool, toolName string, input map[string]interface{}, toolCallID string) error {
	mode := ApprovalModeCallback
	var onApprovalRequired OnApprovalRequiredFunc
	if b.options != nil && b.options.Approval != nil {
		if b.options.Approval.Mode != "" {
			mode = b.options.Approval.Mode
		}
		onApprovalRequired = b.options.Approval.OnApprovalRequired
	}

	if mode == ApprovalModeInterrupt {
		return NewProtocolError(
			"code-mode approval mode \"interrupt\" is not implemented in this Go port; use \"callback\" mode (options.Approval.OnApprovalRequired) instead. See the pkg/codemode package doc.",
			map[string]interface{}{"toolName": toolName, "toolCallId": toolCallID},
		)
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
