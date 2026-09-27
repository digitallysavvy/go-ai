package acp

import (
	"context"
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
	PackageJSON       string
	PnpmLockYAML      string
	PnpmWorkspaceYAML string // optional

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
// explicit no-op for that mode). Mirrors TS `ACPPermissionModeMapping`; Go's
// three named fields make it inherently "complete" once constructed, unlike
// TS's runtime completeness check on an optional record.
type PermissionModeMapping struct {
	AllowReads *PermissionModeTarget
	AllowEdits *PermissionModeTarget
	AllowAll   *PermissionModeTarget
}

// Get returns the target configured for mode (nil if mode is unmapped or
// explicitly a no-op).
func (m *PermissionModeMapping) Get(mode harness.PermissionMode) *PermissionModeTarget {
	if m == nil {
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
	Type ModelMappingType
	Path string
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
	MethodID           string
	Meta               map[string]any
	ClientCapabilities map[string]any
}

// CredentialBrokering builds request transformations from resolved
// credential environments. Mirrors TS `ACPCredentialBrokering`.
type CredentialBrokering func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation

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
	// request is not (or no longer) an askUserQuestions request.
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

	// ClientApp overrides the default "ai-sdk/harness-acp"/Version identity
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
	ForwardEnv           []string
	CredentialEnv        []string
	CredentialBrokering  CredentialBrokering
	AuthenticationFiles  AuthenticationFilesFunc
	CredentialForwarding harness.CredentialForwarding
	Env                  map[string]string

	Authentication         *Authentication
	ClientCapabilities     map[string]any
	ProviderAuthentication *ProviderAuthentication
	ModelMapping           ModelMapping
	SkillsDirectory        string
	InstructionMapping     *InstructionMapping
	OutputSchemaMapping    *OutputSchemaMapping
	HostToolMCPTransport   HostToolMCPTransport
	AskUserQuestions       *AskUserQuestionsSettings
	PermissionModeMapping  *PermissionModeMapping
	SessionMeta            map[string]any
	MintBridgeToken        harness.MintBridgeTokenCallback
}

// ClientApp identifies the harness adapter to the ACP implementation and
// participates in the implementation identity hash. Mirrors TS `ACPClientApp`.
type ClientApp struct {
	Name    string
	Version string
}

// DefaultClientApp is used when Settings.ClientApp is the zero value.
var DefaultClientApp = ClientApp{Name: "ai-sdk/harness-acp", Version: Version}
