// Package codemode is a Go port of the TypeScript AI SDK's @ai-sdk/code-mode
// package. It lets a model write JavaScript that calls host tools
// programmatically instead of one tool call per model turn ("code mode").
// The model's source runs in an isolated QuickJS sandbox compiled to
// WebAssembly and executed with wazero, via a vendored, locally-patched
// copy of github.com/fastschema/qjs v0.0.6 at
// pkg/internal/third_party/qjs (see its README.vendor.md), so no cgo is
// required.
//
// # Experimental
//
// This package is experimental, mirroring the TypeScript package's own
// experimental_ naming convention on every exported entry point (e.g.
// experimental_runCodeMode, experimental_createCodeModeTool,
// experimental_requestCodeModeInterrupt): its API may change in a minor
// version. Go names drop the experimental_ prefix (Go has no equivalent
// convention; see e.g. RunCodeMode, RequestCodeModeInterrupt) but every
// such entry point is still experimental in the same sense the TypeScript
// name flags.
//
// # Scope
//
// This package ports the TypeScript package's core execution path (source
// wrapping, execution limits, the host tool bridge, error translation, the
// TypeScript-signature prompt builder) plus its continuation/interrupt
// system:
//
//   - RequestCodeModeInterrupt (TS host-interrupt.ts) and
//     ContinueCodeModeInterrupt, GetCodeModeInterrupt, IsCodeModeInterrupt,
//     UnwrapCodeModeResult (TS interrupt-continuation.ts).
//   - ContinueCodeModeApproval, GetCodeModeApprovalResponse,
//     IsCodeModeApprovalInterrupt, ToCodeModeApprovalMessages (TS
//     approval-continuation.ts) -- these implement approval mode
//     ApprovalModeInterrupt.
//   - SetCodeModeContinuationSigningKey (TS continuation-capability.ts):
//     continuations are HMAC-SHA256 signed exactly as TypeScript signs them
//     (canonical JSON over the unsigned envelope + auth metadata minus the
//     signature, base64url-encoded digest) so that VerifyCodeModeContinuation
//     validates a TypeScript-signed envelope's signature and shape given the
//     same signing key. The Continuation.Token field itself, however, is
//     documented as opaque by both implementations (TypeScript's own doc
//     comment: "Applications should persist this value without reading or
//     modifying its fields"); this Go port's Token encodes its own replay
//     ledger (see "Deterministic replay" below) in a Go-only format, so a
//     continuation produced by the TypeScript package cannot actually be
//     *resumed* by this package (or vice versa) even though its envelope
//     will verify -- only same-implementation continuations round-trip
//     end-to-end.
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
// exceeded in practice, and -- the one behavior-visible consequence --
// `Promise.all([tools.a(x), tools.b(y)])` can only ever produce one pending
// Interrupt at a time (for whichever call is evaluated first), never a
// multi-item interruption batch the way TypeScript's genuinely concurrent
// dispatch can. Resolving that single interrupt resumes execution up to
// the next bridge call, which itself may interrupt again; from the
// caller's perspective the two tools still each require their own
// approval/resolution before executing, just via a chain of one-interrupt
// continuations instead of one two-item batch. RunInput.Continuation's
// PendingInterruptions/Resolutions therefore always hold at most one entry
// for continuations this package produces, though the code that consumes
// them handles an arbitrary-length batch generically (needed to resume a
// continuation produced by a hypothetical future concurrent
// implementation, or, structurally, one from TypeScript, even though its
// Token cannot be replayed here -- see above).
//
// # Deterministic replay
//
// TypeScript resumes an interrupted invocation by re-running the recorded
// source from the top in a fresh sandbox and short-circuiting every host
// bridge call whose result was already observed with that recorded result,
// instead of re-invoking the real host tool -- so side effects never repeat
// -- until execution reaches the call being resumed (which receives the
// resolution instead of interrupting again, via CodeModeInterrupt for a
// custom kind or a skipped approval check for the approval kind) and then,
// beyond that, genuinely fresh execution. This package implements the same
// model on top of the synchronous bridge described above: every
// invocation's toolBridge records each completed call's (tool name, input,
// output) in call order; Continuation.Token is that recorded list,
// JSON-encoded, for every call made before the (at most one, per above)
// pending interruption. Resuming decodes it back into a replay ledger and
// re-runs the identical source, short-circuiting calls 0..len(ledger)-1
// from the ledger, applying resume semantics to the calls covering the
// continuation's (possibly multi-entry, for a hypothetical future or
// foreign continuation) PendingInterruptions, and executing every call
// after that for real.
package codemode

import (
	"context"
	"time"

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
// or ApprovalDecision{Approved: false, Reason: "..."} instead). Also
// mirrors TypeScript's CodeModeApprovalResolution, an identical shape used
// for a continuation's recorded approval decision; JSON tags match its
// wire form (code-mode/src/types.ts).
type ApprovalDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
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

	// ApprovalModeInterrupt suspends execution the moment a tool call needs
	// approval and resumes later via a signed Continuation, mirroring
	// TypeScript's 'interrupt' mode: RunCodeMode/Run return an
	// *Interrupt (its Payload.Kind() == ToolApprovalKind) instead of
	// erroring or blocking on OnApprovalRequired. Resolve it with
	// ContinueCodeModeApproval or ContinueCodeModeInterrupt.
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

	// ContinuationSecurity overrides the signing key/max age used to sign
	// and verify this invocation's continuations. Defaults to the
	// process-wide key set by SetCodeModeContinuationSigningKey (a random
	// key generated at process start, if never called).
	ContinuationSecurity *ContinuationSecurityOptions

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

// RunInput is the input to Run. Mirrors TypeScript's RunCodeModeInput.
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

	// Continuation resumes a previously interrupted invocation. When set,
	// InterruptResolution must be set too, JS must equal the continuation's
	// JS, and Tools' names must equal its ToolNames. Mirrors TypeScript's
	// RunCodeModeInput.continuation.
	Continuation *Continuation

	// InterruptResolution supplies the resolution for the next pending
	// interruption in Continuation.PendingInterruptions (the one at index
	// len(Continuation.Resolutions)). Mirrors TypeScript's
	// RunCodeModeInput.interruptResolution.
	InterruptResolution *InterruptResolution
}

// ToolApprovalKind is the InterruptPayload.Kind() value used for an
// interruption raised because a host tool call needs approval under
// ApprovalModeInterrupt. Mirrors TypeScript's CODE_MODE_TOOL_APPROVAL_KIND
// (code-mode/src/approval.ts).
const ToolApprovalKind = "ai-sdk-code-mode/tool-approval"

// InterruptPayload is arbitrary interruption data carried by an Interrupt,
// always including a "kind" discriminator. Mirrors TypeScript's
// CodeModeInterruptPayload ({ kind: string; [key: string]: unknown }).
type InterruptPayload map[string]interface{}

// Kind returns the payload's "kind" discriminator, or "" if unset or not a
// string.
func (p InterruptPayload) Kind() string {
	if p == nil {
		return ""
	}
	k, _ := p["kind"].(string)
	return k
}

// InterruptExecutionContext is populated on
// types.ToolExecutionOptions.CodeModeInterrupt (as a
// *InterruptExecutionContext) when a host tool's Execute call is replaying
// after that same tool previously called RequestCodeModeInterrupt. Mirrors
// TypeScript's CodeModeInterruptExecutionContext.
type InterruptExecutionContext struct {
	InterruptID string
	Payload     InterruptPayload
	Resolution  interface{}
}

// InterruptResolution supplies the resolution for one pending interruption
// when resuming a Continuation. Mirrors TypeScript's
// CodeModeInterruptResolution.
type InterruptResolution struct {
	InterruptID string      `json:"interruptId"`
	Resolution  interface{} `json:"resolution"`
}

// ContinuationAuth is the signed authentication envelope on a Continuation.
// Mirrors TypeScript's CodeModeContinuationAuth.
type ContinuationAuth struct {
	Alg         string `json:"alg"`
	Nonce       string `json:"nonce"`
	IssuedAtMs  int64  `json:"issuedAtMs"`
	ExpiresAtMs int64  `json:"expiresAtMs"`
	Signature   string `json:"signature"`
}

// PendingInterruption is one authenticated, not-yet-resolved interruption
// recorded on a Continuation. Mirrors TypeScript's
// CodeModePendingInterruption.
type PendingInterruption struct {
	RunInterruptionID string           `json:"runInterruptionId"`
	InterruptID       string           `json:"interruptId"`
	ToolName          string           `json:"toolName"`
	ToolCallID        string           `json:"toolCallId"`
	Input             interface{}      `json:"input"`
	Payload           InterruptPayload `json:"payload"`
}

// PendingResolution is one resolution recorded on a Continuation while a
// batch of PendingInterruptions is exposed one at a time. Mirrors
// TypeScript's CodeModePendingResolution.
type PendingResolution struct {
	RunInterruptionID string      `json:"runInterruptionId"`
	Value             interface{} `json:"value"`
}

// Continuation is the opaque, HMAC-signed resume state for an interrupted
// code-mode invocation. Applications should persist it (e.g. as JSON)
// without reading or modifying its fields; use ContinueCodeModeInterrupt /
// ContinueCodeModeApproval to resume. Mirrors TypeScript's
// CodeModeContinuation. See the package doc's "Deterministic replay"
// section for what Token encodes in this Go port.
type Continuation struct {
	Version              int                   `json:"version"`
	JS                   string                `json:"js"`
	OuterToolCallID      string                `json:"outerToolCallId"`
	ToolNames            []string              `json:"toolNames"`
	Token                string                `json:"token"`
	PendingInterruptions []PendingInterruption `json:"pendingInterruptions"`
	Resolutions          []PendingResolution   `json:"resolutions"`
	Auth                 ContinuationAuth      `json:"auth"`
}

// ContinuationSecurityOptions configures continuation signing/verification.
// Mirrors TypeScript's CodeModeContinuationSecurityOptions.
type ContinuationSecurityOptions struct {
	// SigningKey overrides the process-wide default signing key for this
	// call. Empty/nil uses the default (see SetCodeModeContinuationSigningKey).
	SigningKey []byte

	// MaxAge bounds how long a signed continuation remains valid. Zero uses
	// the process-wide default (1 hour, unless changed by
	// SetCodeModeContinuationSigningKey).
	MaxAge time.Duration
}

// Interrupt is returned by RunCodeMode/Run (and threaded through
// Continuation.PendingInterruptions) in place of a completed result when
// execution paused on a host tool call. Mirrors TypeScript's
// CodeModeInterrupt.
type Interrupt struct {
	Type            string           `json:"type"`
	InterruptID     string           `json:"interruptId"`
	ToolName        string           `json:"toolName"`
	ToolCallID      string           `json:"toolCallId"`
	OuterToolCallID string           `json:"outerToolCallId"`
	Input           interface{}      `json:"input"`
	Payload         InterruptPayload `json:"payload"`
	Continuation    Continuation     `json:"continuation"`
}

// InterruptTypeValue is Interrupt.Type's only value. Mirrors TypeScript's
// literal type 'code-mode-interrupt'.
const InterruptTypeValue = "code-mode-interrupt"

// ApprovalInterrupt is an Interrupt whose Payload.Kind() == ToolApprovalKind.
// Mirrors TypeScript's CodeModeApprovalInterrupt
// (CodeModeInterrupt<CodeModeApprovalInterruptPayload>).
type ApprovalInterrupt = Interrupt

// ApprovalResponse resolves one ApprovalInterrupt via ContinueCodeModeApproval.
// Mirrors TypeScript's CodeModeApprovalResponse.
type ApprovalResponse struct {
	ApprovalID string `json:"approvalId"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"`
}

// UnwrappedResult is the outcome of UnwrapCodeModeResult: either a
// completed value or a pending Interrupt. Mirrors TypeScript's
// CodeModeUnwrappedResult.
type UnwrappedResult struct {
	// Status is "completed" or "interrupted".
	Status string

	// Output is the completed value. Only meaningful when Status ==
	// "completed".
	Output interface{}

	// Interrupt is the pending interrupt. Only non-nil when Status ==
	// "interrupted".
	Interrupt *Interrupt
}
