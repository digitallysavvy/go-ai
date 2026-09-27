// Package codemode is a Go port of the TypeScript AI SDK's @ai-sdk/code-mode
// package. It lets a model write JavaScript that calls host tools
// programmatically instead of one tool call per model turn ("code mode").
// The model's source runs in an isolated QuickJS sandbox compiled to
// WebAssembly and executed with wazero (github.com/fastschema/qjs), so no
// cgo is required.
//
// # Scope
//
// This package ports the TypeScript package's core execution path: source
// wrapping, execution limits, the host tool bridge (including approval),
// error translation, and the TypeScript-signature prompt builder. It does
// NOT yet port the continuation/interrupt system (CodeModeContinuation,
// experimental_runCodeMode's continuation/interruptResolution inputs, the
// approval "interrupt" mode, and experimental_requestCodeModeInterrupt and
// friends). See the package README/PRD tracking notes for the precise list
// of deferred TypeScript surface. Approval mode "callback" (the default) is
// fully supported; approval mode "interrupt" returns a clear error.
//
// # Host tool bridge dispatch
//
// The TypeScript implementation runs the sandbox in a worker thread and
// bridges `tools.x(input)` calls to the host across a postMessage protocol,
// which allows several bridge calls to be genuinely in flight at once (up
// to CodeModeExecutionPolicy.MaxInFlightBridgeRequests). This Go port runs
// QuickJS in-process (no worker) and dispatches each `tools.x(input)` call
// as a synchronous Go host-function call: the sandbox blocks until the Go
// tool's Execute function returns. This is semantically compatible
// (Promise.all still resolves once every call completes) but not carbon
// copy: it never actually executes bridge calls in parallel, so
// MaxInFlightBridgeRequests is accepted for API/policy parity but is never
// exceeded in practice.
package codemode

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSet is the set of host tools exposed to sandboxed code through the
// global `tools` object. Mirrors TypeScript's CodeModeToolSet.
type ToolSet = map[string]types.Tool

// ApprovalRequest describes a host tool call that requires approval before
// it executes. Mirrors TypeScript's CodeModeApprovalRequest.
type ApprovalRequest struct {
	ToolName   string
	Input      interface{}
	ToolCallID string
}

// ApprovalDecision is the result of an OnApprovalRequired callback. Mirrors
// TypeScript's ApprovalDecision (which also accepts the bare strings
// "approved"/"denied"; Go callers return ApprovalDecision{Approved: true}
// or ApprovalDecision{Approved: false, Reason: "..."} instead).
type ApprovalDecision struct {
	Approved bool
	Reason   string
}

// Approved returns an approved ApprovalDecision, for convenience.
func Approved() ApprovalDecision { return ApprovalDecision{Approved: true} }

// Denied returns a denied ApprovalDecision with an optional reason, for
// convenience.
func Denied(reason string) ApprovalDecision {
	return ApprovalDecision{Approved: false, Reason: reason}
}

// ApprovalMode selects how a host tool's approval requirement is resolved.
type ApprovalMode string

const (
	// ApprovalModeCallback resolves approval synchronously through
	// ApprovalOptions.OnApprovalRequired. This is the default and the only
	// mode implemented in this Go port; see the package doc.
	ApprovalModeCallback ApprovalMode = "callback"

	// ApprovalModeInterrupt would suspend execution and resume later via a
	// signed continuation, mirroring TypeScript's 'interrupt' mode. It is
	// not implemented in this Go port (see package doc); selecting it
	// returns a *ProtocolError when a tool requires approval.
	ApprovalModeInterrupt ApprovalMode = "interrupt"
)

// OnApprovalRequiredFunc resolves an approval request. Mirrors TypeScript's
// options.approval.onApprovalRequired.
type OnApprovalRequiredFunc func(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error)

// ApprovalOptions configures how host tool calls that require approval
// (Tool.NeedsApproval / Tool.ToolApproval) are resolved. Mirrors
// TypeScript's CodeModeOptions['approval'].
type ApprovalOptions struct {
	// Mode selects how approval requirements are resolved. Defaults to
	// ApprovalModeCallback.
	Mode ApprovalMode

	// OnApprovalRequired is invoked for each host tool call that requires
	// approval when Mode is ApprovalModeCallback. When nil, a required
	// approval fails with *ToolApprovalRequiredError.
	OnApprovalRequired OnApprovalRequiredFunc
}

// Options configures a code-mode invocation. Mirrors TypeScript's
// CodeModeOptions.
type Options struct {
	// ExecutionPolicy overrides the default sandbox limits.
	ExecutionPolicy *ExecutionPolicy

	// Approval configures host tool approval handling.
	Approval *ApprovalOptions
}

// ToolDiscovery controls how host tools are presented to the model.
type ToolDiscovery string

const (
	// ToolDiscoveryDescription includes host-tool TypeScript signatures in
	// the provider-visible code-mode tool description. This is the default.
	ToolDiscoveryDescription ToolDiscovery = "description"

	// ToolDiscoveryConversation keeps the code-mode tool definition stable
	// and announces the current host-tool catalog in a conversation
	// message instead (TS toolDiscovery: 'conversation').
	ToolDiscoveryConversation ToolDiscovery = "conversation"
)

// ToolCallerOptions configures the code-mode tool caller created by
// CodeModeTool for use with ai.ExperimentalToolCallers. Mirrors
// TypeScript's CodeModeToolOptions.
type ToolCallerOptions struct {
	Options

	// ToolDiscovery controls how bound host tools are presented to the
	// model. Defaults to ToolDiscoveryDescription.
	ToolDiscovery ToolDiscovery
}

// RunInput is the input to Run. Mirrors TypeScript's RunCodeModeInput
// (continuation/interruptResolution are omitted; see package doc).
type RunInput struct {
	// JS is the JavaScript (or type-stripped TypeScript) source to
	// execute. It runs as the body of an async function, so top-level
	// `await`/`return` are supported.
	JS string

	// Tools are the host tools reachable from the sandbox as
	// `tools.<name>(input)`.
	Tools ToolSet

	// ToolExecutionOptions is forwarded to each host tool's Execute call,
	// except for ToolCallID which Run assigns per nested call. May be nil.
	ToolExecutionOptions *types.ToolExecutionOptions

	// Options configures execution limits and approval handling.
	Options *Options
}
