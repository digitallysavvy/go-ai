package acp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SourceType discriminates Source. Mirrors TS `ACPSource`.
type SourceType string

const (
	SourceNPMSimple      SourceType = "npm-simple"
	SourceNPMLocked      SourceType = "npm-locked"
	SourceInstallCommand SourceType = "install-command"
)

// Source is where the ACP implementation binary comes from. Exactly the
// fields matching Type are used. Mirrors TS `ACPSource`.
type Source struct {
	Type SourceType

	// npm-simple
	PackageName    string
	PackageVersion string // optional exact semver; empty means "latest"

	// npm-locked
	PackageJSON  string
	PnpmLockYAML string
	// PnpmWorkspaceYAML is optional. An empty string means "not provided"
	// unless PnpmWorkspaceYAMLSet is true, in which case an empty value is
	// rejected like TS (`pnpmWorkspaceYaml?.length === 0` throws).
	PnpmWorkspaceYAMLSet bool
	PnpmWorkspaceYAML    string

	// install-command
	Command string
}

// PermissionModeTargetType discriminates PermissionModeTarget.
type PermissionModeTargetType string

const (
	PermissionTargetSessionMode         PermissionModeTargetType = "session-mode"
	PermissionTargetSessionConfigOption PermissionModeTargetType = "session-config-option"
)

// PermissionModeTarget is where a harness permission mode routes in the ACP
// implementation. Mirrors TS `ACPPermissionModeTarget`.
type PermissionModeTarget struct {
	Type PermissionModeTargetType
	// session-mode
	ModeID string
	// session-config-option
	ConfigID string
	Value    any // string or bool
}

// PermissionModeMapping maps every harness.PermissionMode to a target (nil =
// explicit no-op for that mode). Mirrors TS `ACPPermissionModeMapping`.
//
// TS also accepts a partial record (a key left out). Such a mapping is part of
// the implementation identity but is otherwise ignored: it is never sent to the
// bridge. Go expresses a left-out key by setting its Unset<Mode> flag; the zero
// value (no flag set) is a complete mapping, so existing callers are unchanged.
type PermissionModeMapping struct {
	AllowReads *PermissionModeTarget
	AllowEdits *PermissionModeTarget
	AllowAll   *PermissionModeTarget
	// UnsetAllowReads, UnsetAllowEdits and UnsetAllowAll mark a mode whose key
	// is absent (TS `undefined`); the target field of a marked mode is
	// ignored. Flags (not a slice) keep the struct comparable with ==.
	UnsetAllowReads bool
	UnsetAllowEdits bool
	UnsetAllowAll   bool
}

// IsComplete reports whether every mode has an entry (a target or an explicit
// nil no-op). Mirrors TS `isCompletePermissionModeMapping`; a nil mapping is
// not complete.
func (m *PermissionModeMapping) IsComplete() bool {
	return m != nil &&
		!m.UnsetAllowReads && !m.UnsetAllowEdits && !m.UnsetAllowAll &&
		validPermissionModeTarget(m.AllowReads) &&
		validPermissionModeTarget(m.AllowEdits) &&
		validPermissionModeTarget(m.AllowAll)
}

// validPermissionModeTarget mirrors TS `isPermissionModeMappingValue`: nil (an
// explicit no-op) or a well-formed target. A session-config-option value must
// be a string or bool.
func validPermissionModeTarget(t *PermissionModeTarget) bool {
	if t == nil {
		return true
	}
	switch t.Type {
	case PermissionTargetSessionMode:
		return true
	case PermissionTargetSessionConfigOption:
		switch t.Value.(type) {
		case string, bool:
			return true
		}
	}
	return false
}

func (m *PermissionModeMapping) isUnset(mode harness.PermissionMode) bool {
	switch mode {
	case harness.PermissionModeAllowReads:
		return m.UnsetAllowReads
	case harness.PermissionModeAllowEdits:
		return m.UnsetAllowEdits
	case harness.PermissionModeAllowAll:
		return m.UnsetAllowAll
	}
	return false
}

// Get returns the target configured for mode (nil if mode is unmapped or
// explicitly a no-op).
func (m *PermissionModeMapping) Get(mode harness.PermissionMode) *PermissionModeTarget {
	if m == nil || m.isUnset(mode) {
		return nil
	}
	switch mode {
	case harness.PermissionModeAllowReads:
		return m.AllowReads
	case harness.PermissionModeAllowEdits:
		return m.AllowEdits
	case harness.PermissionModeAllowAll:
		return m.AllowAll
	}
	return nil
}

// InstructionMappingType discriminates InstructionMapping.
type InstructionMappingType string

const (
	InstructionMappingSessionMeta   InstructionMappingType = "session-meta"
	InstructionMappingLaunchEnvJSON InstructionMappingType = "launch-env-json"
	InstructionMappingFilesystem    InstructionMappingType = "filesystem"
)

// InstructionMapping describes where an ACP implementation accepts native
// session instructions. Mirrors TS `ACPInstructionMapping`.
type InstructionMapping struct {
	Type InstructionMappingType
	// session-meta / launch-env-json
	Path []string
	// launch-env-json
	Variable string
	// filesystem
	FilePath string
}

// OutputSchemaMapping maps structured output JSON Schema to a path below the
// ACP session prompt's `_meta` field. Mirrors TS `ACPOutputSchemaMapping`.
type OutputSchemaMapping struct {
	Path []string
}

// ModelMappingType discriminates ModelMapping.
type ModelMappingType string

const (
	ModelMappingSessionConfigOption ModelMappingType = "session-config-option"
	ModelMappingSessionModel        ModelMappingType = "session-model"
)

// ModelMapping maps the HarnessAgent model identifier to an ACP session
// operation. Mirrors TS `ACPModelMapping`.
type ModelMapping struct {
	Type ModelMappingType `json:"type"`
	Path string           `json:"path"`
}

// ProviderAuthentication configures the ACP implementation to authenticate
// through AI Gateway using a profile of environment values the in-sandbox
// bridge resolves (magic `$source` tokens such as "gateway-api-key" are
// resolved by the unchanged embedded bridge, never on the host). Mirrors TS
// `ACPProviderAuthentication`.
type ProviderAuthentication struct {
	GatewayEnv map[string]any
}

// Authentication identifies the ACP authentication method already selected
// for the implementation (methodId from `authenticate`). Mirrors TS
// `ACPAuthentication`.
type Authentication struct {
	MethodID           string         `json:"methodId"`
	Meta               map[string]any `json:"meta,omitempty"`
	ClientCapabilities map[string]any `json:"clientCapabilities,omitempty"`
}

// MarshalJSON keeps a non-nil empty Meta or ClientCapabilities on the wire:
// TS emits `_meta` whenever `authentication.meta` is not null, so an empty
// record is sent as `{}`.
func (a Authentication) MarshalJSON() ([]byte, error) {
	type wire struct {
		MethodID           string          `json:"methodId"`
		Meta               *map[string]any `json:"meta,omitempty"`
		ClientCapabilities *map[string]any `json:"clientCapabilities,omitempty"`
	}
	w := wire{MethodID: a.MethodID}
	if a.Meta != nil {
		w.Meta = &a.Meta
	}
	if a.ClientCapabilities != nil {
		w.ClientCapabilities = &a.ClientCapabilities
	}
	return json.Marshal(w)
}

// CredentialBrokering builds request transformations from resolved
// credential environments. Mirrors TS `ACPCredentialBrokering`.
type CredentialBrokering func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation

// CredentialBrokeringErr is CredentialBrokering for implementations whose
// transformation construction can fail (for example an invalid base URL in the
// environment). TS `createCredentialRequestTransformation` throws there and
// the error fails the session start; this form surfaces it the same way.
type CredentialBrokeringErr func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error)

// AuthenticationFile is one file materialized under the ACP implementation's
// private home directory before it starts. Mirrors TS `ACPAuthenticationFile`.
type AuthenticationFile struct {
	Path    string
	Content string
}

// AuthenticationFilesFunc mirrors TS `ACPAuthenticationFiles`.
type AuthenticationFilesFunc func(env, sandboxEnv map[string]string, credentialBrokeringAvailable bool) []AuthenticationFile

// HostToolMCPTransport selects the transport for the harness-owned MCP
// server that exposes host tools to the ACP implementation. Mirrors TS
// `ACPHostToolMCPTransport`.
type HostToolMCPTransport string

const (
	HostToolMCPStdio HostToolMCPTransport = "stdio"
	HostToolMCPHTTP  HostToolMCPTransport = "http"
)

// NonTextContentType is a kind of non-text tool model output (produced by a
// tool's toModelOutput transformation) an ACP implementation can deliver to
// its model. Text is always supported. Mirrors the elements of TS
// `nonTextContentTypes` (`'image'`).
type NonTextContentType string

// NonTextContentImage declares that the implementation accepts inline images.
const NonTextContentImage NonTextContentType = "image"

// AskUserQuestionsSettings normalizes an ACP implementation's native
// clarifying-question mechanism into the harness askUserQuestions built-in
// tool. Mirrors TS `ACPAskUserQuestionsSettings`.
type AskUserQuestionsSettings struct {
	RequestMethod string
	// IsNativeToolCall reports whether a native tool-call candidate the
	// bridge could not resolve is actually this implementation's native
	// question mechanism (so it should be suppressed from the harness tool
	// stream rather than surfaced as an unknown dynamic tool call).
	IsNativeToolCall func(nativeToolCall ToolCall) bool
	// FromNativeRequest converts one native question request into a
	// client-executed askUserQuestions tool-call StreamPart, or nil if this
	// request is not (or no longer) an askUserQuestions request. As in TS,
	// the returned call must carry an explicit client-executed flag: set
	// ProviderExecutedOmitted to model an undefined `providerExecuted`,
	// which is rejected.
	FromNativeRequest func(nativeRequest any, nativeToolCall *ToolCall) *harness.ToolCallPart
	// ToNativeResponse converts the harness askUserQuestions tool result
	// back into the implementation's native response shape.
	ToNativeResponse func(nativeRequest any, toolResult types.ToolResultContent) any
	// MatchesNativeRequest correlates a buffered result (submitted before
	// the native request arrived) with a newly observed native request.
	MatchesNativeRequest func(previousNativeRequest, nativeRequest any) bool
}

// Settings configures CreateACP. Mirrors TS `ACPHarnessSettings` (which
// flattens `ACPV1Settings`).
type Settings struct {
	// BuiltinTools are the tools the ACP implementation exposes natively.
	// Unlike DeepAgents/OpenCode, ACP has no fixed built-in tool table: it is
	// entirely supplied by the caller (the concrete per-implementation
	// wrapper — Cursor, fx, GitHub Copilot, Grok Build — which is WG12, out
	// of this package's scope).
	BuiltinTools map[string]harness.BuiltinTool

	MCPServers    map[string]any
	IsMcpToolCall func(ToolCall) bool

	Port           int
	PortEndpoint   *harness.PortEndpoint
	StartupTimeout time.Duration
	Reconnect      bridge.ReconnectOptions

	// ClientApp overrides the default "ai-sdk-harness-acp"/Version identity
	// sent to the ACP implementation and used in the implementation identity
	// hash. Concrete wrapper adapters normally set their own.
	ClientApp ClientApp

	// HarnessID must be a stable kebab-case identifier (e.g. "cursor").
	HarnessID string

	Auth                             harness.Authentication
	ResolveAuthenticationEnvironment func(ctx context.Context, auth harness.Authentication, env map[string]string) (map[string]string, error)

	Source     Source
	Executable string
	Args       []string
	// ForwardEnv/CredentialEnv/Env must not overlap (validated).
	ForwardEnv          []string
	CredentialEnv       []string
	CredentialBrokering CredentialBrokering
	// CredentialBrokeringErr is the error-returning form of
	// CredentialBrokering; when set it is used instead, and its error fails
	// the session start. Configure at most one of the two.
	CredentialBrokeringErr CredentialBrokeringErr
	AuthenticationFiles    AuthenticationFilesFunc
	CredentialForwarding   harness.CredentialForwarding
	Env                    map[string]string

	Authentication         *Authentication
	ClientCapabilities     map[string]any
	ProviderAuthentication *ProviderAuthentication
	ModelMapping           ModelMapping
	SkillsDirectory        string
	InstructionMapping     *InstructionMapping
	OutputSchemaMapping    *OutputSchemaMapping
	HostToolMCPTransport   HostToolMCPTransport
	// StreamStartFallbackModelID is reported as the model of every
	// stream-start part that carries none. TS wraps the GitHub Copilot
	// harness for this (`withGitHubCopilotGatewayModel`): Copilot's BYOK mode
	// exposes no model over ACP. Empty leaves stream-start parts unchanged.
	StreamStartFallbackModelID string
	// NonTextContentTypes lists the non-text tool model output the ACP
	// implementation can deliver to its model. Text is always supported;
	// undeclared image content is omitted with a bridge stderr warning.
	// Declaring NonTextContentImage also enables image prompts when the ACP
	// agent does not advertise image prompt support. Defaults to none.
	// Mirrors TS `nonTextContentTypes`.
	NonTextContentTypes   []NonTextContentType
	AskUserQuestions      *AskUserQuestionsSettings
	PermissionModeMapping *PermissionModeMapping
	SessionMeta           map[string]any
	MintBridgeToken       harness.MintBridgeTokenCallback
}

// ClientApp identifies the harness adapter to the ACP implementation and
// participates in the implementation identity hash. Mirrors TS `ACPClientApp`.
type ClientApp struct {
	Name    string
	Version string
}

// DefaultClientApp is used when Settings.ClientApp is the zero value.
var DefaultClientApp = ClientApp{Name: "ai-sdk-harness-acp", Version: Version}
