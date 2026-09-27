// Package harness is the Go port of the TypeScript `@ai-sdk/harness` package
// (pinned to ai@7.0.113).
//
// It contains the harness-v1 specification types that adapters implement
// (TS `HarnessV1*`), the stream-part vocabulary, JSON lifecycle state that is
// persisted by callers, the sandbox abstraction for harness sessions, the
// bootstrap recipe machinery and the harness error types.
//
// All JSON shapes are wire-compatible with the TypeScript zod schemas so that a
// Go host can talk to the unchanged TS in-sandbox bridge and resume sessions
// created by a TS host (and vice versa).
//
// Naming: TS prefixes spec types with `HarnessV1` and exposes consumer aliases
// prefixed `HarnessAgent` (d77bed4). In Go the package name provides the
// namespace, so spec types drop the prefix (harness.Session is
// HarnessV1Session). Consumer-facing aliases live in agent_types.go.
package harness

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpecificationVersion is the literal spec version implemented by adapters.
const SpecificationVersion = "harness-v1"

// Harness is the versioned specification for a harness adapter: the
// integration point for one third-party coding-agent runtime (Claude Code,
// Codex, ...). Mirrors TS `HarnessV1`.
//
// Optional TS members are expressed as optional interfaces a Harness may also
// implement: BootstrapProvider (getBootstrap), BuiltinToolApprovalSupport
// (supportsBuiltinToolApprovals), BuiltinToolFilteringSupport
// (supportsBuiltinToolFiltering) and LifecycleStateValidator
// (lifecycleStateSchema).
type Harness interface {
	// SpecificationVersion always returns "harness-v1".
	SpecificationVersion() string

	// HarnessID is the stable identifier for this harness, used as the key in
	// Metadata objects (e.g. "claude-code", "codex").
	HarnessID() string

	// BuiltinTools returns the tools the adapter's runtime exposes natively,
	// keyed by what the bridge emits on tool-call events
	// (commonName ?? nativeName).
	BuiltinTools() map[string]BuiltinTool

	// DoStart starts a fresh session, resumes a parked session via
	// ResumeFrom, or resumes a suspended turn via ContinueFrom.
	DoStart(ctx context.Context, opts StartOptions) (Session, error)
}

// BootstrapProvider is implemented by adapters that declare a bootstrap
// recipe (TS `getBootstrap`).
type BootstrapProvider interface {
	GetBootstrap(ctx context.Context) (*Bootstrap, error)
}

// BuiltinToolApprovalSupport is implemented by adapters that can emit approval
// requests for built-in tools (TS `supportsBuiltinToolApprovals`).
type BuiltinToolApprovalSupport interface {
	SupportsBuiltinToolApprovals() bool
}

// BuiltinToolFilteringSupport is implemented by adapters that can prevent their
// runtime from seeing inactive built-in tools (TS
// `supportsBuiltinToolFiltering`).
type BuiltinToolFilteringSupport interface {
	SupportsBuiltinToolFiltering() bool
}

// LifecycleStateValidator is implemented by adapters that validate the
// adapter-defined `data` payload of lifecycle state (TS
// `lifecycleStateSchema`).
type LifecycleStateValidator interface {
	ValidateLifecycleStateData(data json.RawMessage) error
}

// GetBootstrap returns the adapter's bootstrap recipe, or nil when the adapter
// does not declare one.
func GetBootstrap(ctx context.Context, h Harness) (*Bootstrap, error) {
	if bp, ok := h.(BootstrapProvider); ok {
		return bp.GetBootstrap(ctx)
	}
	return nil, nil
}

// SupportsBuiltinToolApprovals reports TS `supportsBuiltinToolApprovals`.
func SupportsBuiltinToolApprovals(h Harness) bool {
	s, ok := h.(BuiltinToolApprovalSupport)
	return ok && s.SupportsBuiltinToolApprovals()
}

// SupportsBuiltinToolFiltering reports TS `supportsBuiltinToolFiltering`.
func SupportsBuiltinToolFiltering(h Harness) bool {
	s, ok := h.(BuiltinToolFilteringSupport)
	return ok && s.SupportsBuiltinToolFiltering()
}

// StartOptions are passed to Harness.DoStart. Cancellation (TS abortSignal) is
// carried by the context passed to DoStart. Mirrors TS `HarnessV1StartOptions`.
type StartOptions struct {
	// Headers are additional normalized HTTP headers to send with model
	// requests.
	Headers map[string]string

	// SessionID is the stable identifier for this harness session.
	SessionID string

	// ResumeFrom is the resume payload from a prior lifecycle method.
	ResumeFrom *ResumeSessionState

	// ContinueFrom is the continuation payload from DoSuspendTurn, or nested
	// in ResumeFrom.
	ContinueFrom *ContinueTurnState

	// PermissionMode is the approval policy for built-in adapter-native tools.
	PermissionMode PermissionMode

	// BuiltinToolFiltering lists adapter-native built-in tools available for
	// this session.
	BuiltinToolFiltering *BuiltinToolFiltering

	// Observability is diagnostics wiring; nil when diagnostics are disabled.
	Observability *Observability

	// SandboxSession is the sandbox the adapter operates against. It is either
	// a NetworkSandboxSession or a caller-provided plain
	// providerutils.SandboxSession (filesystem + process only). Adapters must
	// not stop or destroy the sandbox themselves.
	SandboxSession providerutils.SandboxSession

	// SessionWorkDir is the absolute path the adapter runs the agent in.
	SessionWorkDir string
}

// Prompt is the fresh input for one turn: either plain text or a single user
// message (TS `HarnessV1Prompt` = string | UserModelMessage). Exactly one of
// Text or Message should be set.
type Prompt struct {
	Text    string
	Message *types.Message
}

// TextPrompt returns a plain-text Prompt.
func TextPrompt(text string) Prompt { return Prompt{Text: text} }

// MarshalJSON encodes the prompt as a JSON string or a user message object.
func (p Prompt) MarshalJSON() ([]byte, error) {
	if p.Message != nil {
		return json.Marshal(p.Message)
	}
	return json.Marshal(p.Text)
}

// UnmarshalJSON accepts a JSON string or a message object.
func (p *Prompt) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*p = Prompt{Text: text}
		return nil
	}
	var msg types.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("harness prompt must be a string or a user message: %w", err)
	}
	*p = Prompt{Message: &msg}
	return nil
}

// EmitFunc receives every stream part an adapter produces during a turn.
type EmitFunc func(part StreamPart)

// PromptTurnOptions are passed to Session.DoPromptTurn. Cancellation (TS
// abortSignal) is carried by the context. Mirrors TS
// `HarnessV1PromptTurnOptions` (= TurnSettings & {...}).
type PromptTurnOptions struct {
	TurnSettings

	// Prompt is the fresh input for this turn.
	Prompt Prompt

	// ResponseFormat requested for this turn. Adapters that cannot honor a
	// JSON response format must return a CapabilityUnsupportedError.
	ResponseFormat *ResponseFormat

	// Emit is invoked once for each event the adapter produces.
	Emit EmitFunc
}

// ContinueTurnOptions are passed to Session.DoContinueTurn. Mirrors TS
// `HarnessV1ContinueTurnOptions`.
type ContinueTurnOptions struct {
	TurnSettings

	// ResponseFormat of the in-flight turn.
	ResponseFormat *ResponseFormat

	// Emit is invoked once for each event the adapter produces.
	Emit EmitFunc
}

// Session is an active harness session returned by Harness.DoStart. Mirrors TS
// `HarnessV1Session`.
type Session interface {
	// SessionID is the same value passed in via StartOptions.SessionID.
	SessionID() string

	// IsResume reports whether the session was created from ResumeFrom or
	// ContinueFrom.
	IsResume() bool

	// DoPromptTurn runs one prompt turn and returns its control handle.
	DoPromptTurn(ctx context.Context, opts PromptTurnOptions) (PromptControl, error)

	// DoCompact requests that the runtime compact its context. Runtimes that
	// cannot honour it return a CapabilityUnsupportedError.
	DoCompact(ctx context.Context, customInstructions string) error

	// DoContinueTurn continues the in-flight turn without a new prompt.
	DoContinueTurn(ctx context.Context, opts ContinueTurnOptions) (PromptControl, error)

	// DoSuspendTurn freezes the active turn at a precise cursor while keeping
	// the runtime alive and returns the continuation payload.
	DoSuspendTurn(ctx context.Context) (*ContinueTurnState, error)

	// DoDetach detaches from the runtime without tearing it down.
	DoDetach(ctx context.Context) (*ResumeSessionState, error)

	// DoStop persists enough state to resume later, then stops the runtime.
	DoStop(ctx context.Context) (*ResumeSessionState, error)

	// DoDestroy stops the runtime without returning lifecycle state.
	DoDestroy(ctx context.Context) error
}

// ToolResultSubmission is the input of PromptControl.SubmitToolResult.
type ToolResultSubmission struct {
	ToolCallID string
	Output     any
	IsError    bool
	// ToolResult optionally carries the full tool-result part (TS
	// `ToolResultPart`) for adapters that forward it verbatim.
	ToolResult any
}

// ToolApprovalSubmission is the input of ToolApprovalSubmitter.SubmitToolApproval.
type ToolApprovalSubmission struct {
	ApprovalID string
	Approved   bool
	Reason     string
}

// PromptControl is the bidirectional control surface returned by
// DoPromptTurn / DoContinueTurn. Mirrors TS `HarnessV1PromptControl`.
//
// The optional TS methods submitToolApproval and submitUserMessage are the
// optional interfaces ToolApprovalSubmitter and UserMessageSubmitter.
type PromptControl interface {
	// SubmitToolResult provides a result for a tool-call the adapter emitted.
	SubmitToolResult(ctx context.Context, result ToolResultSubmission) error

	// Done is closed when the adapter has finished the turn (success or
	// failure).
	Done() <-chan struct{}

	// Err returns the turn error after Done is closed (nil on success).
	Err() error
}

// ToolApprovalSubmitter is implemented by prompt controls that accept
// approval responses.
type ToolApprovalSubmitter interface {
	SubmitToolApproval(ctx context.Context, approval ToolApprovalSubmission) error
}

// UserMessageSubmitter is implemented by prompt controls that accept mid-turn
// user messages (steering).
type UserMessageSubmitter interface {
	SubmitUserMessage(ctx context.Context, text string) error
}

// CheckpointPinner is an optional capability of a PromptControl (or a
// Session, for a bridge-backed adapter whose control is stateless): pin the
// adapter's current replay checkpoint, deferring garbage-collection of
// already-delivered bridge events until the returned release func runs.
//
// Used opportunistically by run_prompt.go's StopWhen early-stop path
// (consumeLoop's pendingStopBoundary handling / suspendOrFinishNow), which
// must decide — based on stream parts it hasn't received yet (the "one event
// of lookahead" refinement) — whether a completed step boundary that
// satisfies a StopCondition should suspend the turn. A bridge-backed adapter
// that implements this can safely let its connection keep advancing while
// that decision is made without risking the frozen cursor falling out of the
// replay buffer; an adapter that does not implement it (any adapter's own
// test double, or one with no live event buffer to protect) simply skips the
// optimization with no change in correctness. Mirrors TS
// `pinSandboxChannelEventCheckpoint` (harness/utils/sandbox-channel.ts),
// whose per-event symbol is attached at decode time for the same reason
// pkg/harness/bridge.CheckpointRecorder.Record captures a seq at dispatch
// time rather than reading the channel's live cursor when Pin is later
// called — see that type's doc. Implemented by every bridge-backed adapter's
// promptControl (claudecode, codex, opencode, deepagents, acp), each
// delegating to its own pkg/harness/bridge.CheckpointRecorder.
type CheckpointPinner interface {
	PinCheckpoint() (release func())
}

// PermissionMode is the baseline permission mode for adapter-native built-in
// tools. Mirrors TS `HarnessV1PermissionMode`.
type PermissionMode string

const (
	PermissionModeAllowReads PermissionMode = "allow-reads"
	PermissionModeAllowEdits PermissionMode = "allow-edits"
	PermissionModeAllowAll   PermissionMode = "allow-all"
)

// Valid reports whether m is one of the defined modes.
func (m PermissionMode) Valid() bool {
	switch m {
	case PermissionModeAllowReads, PermissionModeAllowEdits, PermissionModeAllowAll:
		return true
	}
	return false
}

// BuiltinToolFilteringMode discriminates BuiltinToolFiltering.
type BuiltinToolFilteringMode string

const (
	BuiltinToolFilteringAllow BuiltinToolFilteringMode = "allow"
	BuiltinToolFilteringDeny  BuiltinToolFilteringMode = "deny"
)

// BuiltinToolFiltering selects the adapter-native built-in tools available for
// a session. Mirrors TS `HarnessV1BuiltinToolFiltering`.
type BuiltinToolFiltering struct {
	Mode      BuiltinToolFilteringMode `json:"mode"`
	ToolNames []string                 `json:"toolNames"`
}

// MarshalJSON always emits toolNames as an array.
func (f BuiltinToolFiltering) MarshalJSON() ([]byte, error) {
	type alias BuiltinToolFiltering
	a := alias(f)
	if a.ToolNames == nil {
		a.ToolNames = []string{}
	}
	return json.Marshal(a)
}

// IsBuiltinToolIncluded mirrors TS `isHarnessV1BuiltinToolIncluded`.
func IsBuiltinToolIncluded(toolName string, filtering *BuiltinToolFiltering) bool {
	if filtering == nil {
		return true
	}
	contains := false
	for _, name := range filtering.ToolNames {
		if name == toolName {
			contains = true
			break
		}
	}
	if filtering.Mode == BuiltinToolFilteringAllow {
		return contains
	}
	return !contains
}

// BuiltinToolFilteringDenialReason mirrors TS
// `getHarnessV1BuiltinToolFilteringDenialReason`.
func BuiltinToolFilteringDenialReason(toolName string) string {
	return fmt.Sprintf("Tool '%s' is inactive due to the HarnessAgent tool filtering policy.", toolName)
}

// ResponseFormatType discriminates ResponseFormat.
type ResponseFormatType string

const (
	ResponseFormatText ResponseFormatType = "text"
	ResponseFormatJSON ResponseFormatType = "json"
)

// ResponseFormat is the requested response format for one harness turn.
// Mirrors TS `HarnessV1ResponseFormat`.
type ResponseFormat struct {
	Type        ResponseFormatType `json:"type"`
	Schema      map[string]any     `json:"schema,omitempty"`
	Name        string             `json:"name,omitempty"`
	Description string             `json:"description,omitempty"`
}

// ToolSpec describes a host-defined tool made available to the runtime.
// Mirrors TS `HarnessV1ToolSpec`; it is also the bridge `start.tools[]` wire
// shape (`harnessV1BridgeToolWireSchema`).
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// InputSchema is a JSON Schema 7 document.
	InputSchema any `json:"inputSchema,omitempty"`
}

// Skill is a self-contained instruction bundle the runtime can load. Mirrors
// TS `HarnessV1Skill`.
type Skill struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Content     string      `json:"content"`
	Files       []SkillFile `json:"files,omitempty"`
}

// SkillFile is an additional file belonging to a Skill. Path is skill-relative
// POSIX.
type SkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Metadata is adapter-namespaced opaque data attached to harness events, keyed
// by harness id. Mirrors TS `HarnessV1Metadata`.
type Metadata map[string]map[string]any

// CallWarningType discriminates CallWarning.
type CallWarningType string

const (
	CallWarningUnsupportedSetting CallWarningType = "unsupported-setting"
	CallWarningUnsupportedTool    CallWarningType = "unsupported-tool"
	CallWarningOther              CallWarningType = "other"
)

// CallWarning is a non-fatal warning emitted by an adapter. Mirrors TS
// `HarnessV1CallWarning`.
type CallWarning struct {
	Type    CallWarningType `json:"type"`
	Setting string          `json:"setting,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Details string          `json:"details,omitempty"`
	Message string          `json:"message,omitempty"`
}

// MarshalJSON emits only the fields belonging to the warning variant.
func (w CallWarning) MarshalJSON() ([]byte, error) {
	switch w.Type {
	case CallWarningUnsupportedSetting:
		return json.Marshal(struct {
			Type    CallWarningType `json:"type"`
			Setting string          `json:"setting"`
			Details string          `json:"details,omitempty"`
		}{w.Type, w.Setting, w.Details})
	case CallWarningUnsupportedTool:
		return json.Marshal(struct {
			Type    CallWarningType `json:"type"`
			Tool    string          `json:"tool"`
			Details string          `json:"details,omitempty"`
		}{w.Type, w.Tool, w.Details})
	default:
		return json.Marshal(struct {
			Type    CallWarningType `json:"type"`
			Message string          `json:"message"`
		}{w.Type, w.Message})
	}
}

// Observability is diagnostics wiring handed to DoStart. Mirrors TS
// `HarnessV1Observability`.
type Observability struct {
	Debug  *DebugConfig
	Report func(Diagnostic)
}

// DebugLevel is a diagnostic severity, ordered most to least severe.
type DebugLevel string

const (
	DebugLevelError DebugLevel = "error"
	DebugLevelWarn  DebugLevel = "warn"
	DebugLevelInfo  DebugLevel = "info"
	DebugLevelDebug DebugLevel = "debug"
	DebugLevelTrace DebugLevel = "trace"
)

// Valid reports whether l is a defined level.
func (l DebugLevel) Valid() bool {
	switch l {
	case DebugLevelError, DebugLevelWarn, DebugLevelInfo, DebugLevelDebug, DebugLevelTrace:
		return true
	}
	return false
}

// DebugConfig is the per-session diagnostics configuration (also sent on the
// bridge `start.debug` field). Mirrors TS `HarnessV1DebugConfig`.
type DebugConfig struct {
	Enabled    *bool      `json:"enabled,omitempty"`
	Level      DebugLevel `json:"level,omitempty"`
	Subsystems []string   `json:"subsystems,omitempty"`
}

// DiagnosticError is the error payload of a structured diagnostic.
type DiagnosticError struct {
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
}

// Diagnostic is a diagnostic emitted by an adapter. Mirrors TS
// `HarnessV1Diagnostic`.
type Diagnostic struct {
	Level     DebugLevel       `json:"level"`
	Message   string           `json:"message"`
	Subsystem string           `json:"subsystem"`
	Kind      string           `json:"kind"` // "log" | "event"
	Source    string           `json:"source,omitempty"`
	Stream    string           `json:"stream,omitempty"` // "stdout" | "stderr"
	Attrs     map[string]any   `json:"attrs,omitempty"`
	Error     *DiagnosticError `json:"error,omitempty"`
	SessionID string           `json:"sessionId,omitempty"`
	// Timestamp is the emission time in epoch milliseconds.
	Timestamp int64 `json:"timestamp"`
}

// CredentialForwardingOptions is the input of a CredentialForwarding callback.
type CredentialForwardingOptions struct {
	// Credential is the value the adapter would otherwise forward: a generated
	// sandbox placeholder when brokering is available, the real credential
	// otherwise.
	Credential string
	// EnvironmentVariableName is the variable used to expose the value.
	EnvironmentVariableName string
}

// CredentialForwarding customizes a credential value immediately before an
// adapter forwards it into a sandbox process. Mirrors TS
// `HarnessV1CredentialForwarding`.
type CredentialForwarding func(ctx context.Context, opts CredentialForwardingOptions) (string, error)

// MintBridgeTokenCallback creates the bridge channel token for one session.
// Mirrors TS `HarnessV1MintBridgeTokenCallback`.
type MintBridgeTokenCallback func(sandboxID string) string
