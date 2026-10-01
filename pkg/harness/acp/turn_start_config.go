package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// BuiltinToolMapping is one entry the bridge uses to resolve an ACP native
// tool call it cannot name programmatically. Mirrors TS
// `ACPBuiltinToolMapping`.
type BuiltinToolMapping struct {
	ToolName    string         `json:"toolName"`
	NativeName  string         `json:"nativeName,omitempty"`
	Title       string         `json:"title,omitempty"`
	ToolUseKind string         `json:"toolUseKind,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// TurnStartConfig is the versioned, fingerprinted non-secret turn
// configuration sent with every `start` frame. Mirrors TS
// `ACPTurnStartConfig`.
type TurnStartConfig struct {
	Version                  int                     `json:"version"`
	ConfigurationFingerprint string                  `json:"configurationFingerprint"`
	Prompt                   []TextContentBlock      `json:"prompt"`
	Tools                    []harness.ToolSpec      `json:"tools"`
	BuiltinTools             []BuiltinToolMapping    `json:"builtinTools"`
	PermissionMode           harness.PermissionMode  `json:"permissionMode"`
	PermissionModeMapping    *PermissionModeMapping  `json:"permissionModeMapping,omitempty"`
	Model                    string                  `json:"model,omitempty"`
	ModelMapping             *ModelMapping           `json:"modelMapping,omitempty"`
	Debug                    *harness.DebugConfig    `json:"debug,omitempty"`
	ResponseFormat           *harness.ResponseFormat `json:"responseFormat,omitempty"`
	OutputSchemaMapping      *OutputSchemaMapping    `json:"outputSchemaMapping,omitempty"`
}

// ColdSessionState is the subset of TurnStartConfig persisted for a cold ACP
// session restore. Mirrors TS `ACPColdSessionState`. This port never
// produces or consumes cold-restore lifecycle data (see the package doc);
// the type exists so lifecycle JSON round-trips without data loss.
type ColdSessionState struct {
	Version                  int                     `json:"version"`
	ConfigurationFingerprint string                  `json:"configurationFingerprint"`
	Tools                    []harness.ToolSpec      `json:"tools"`
	BuiltinTools             []BuiltinToolMapping    `json:"builtinTools"`
	PermissionMode           harness.PermissionMode  `json:"permissionMode"`
	ResponseFormat           *harness.ResponseFormat `json:"responseFormat,omitempty"`
	OutputSchemaMapping      *OutputSchemaMapping    `json:"outputSchemaMapping,omitempty"`
	PermissionModeMapping    *PermissionModeMapping  `json:"permissionModeMapping,omitempty"`
}

// createTurnStartConfigInput is the input of createTurnStartConfig.
type createTurnStartConfigInput struct {
	Prompt                []TextContentBlock
	Tools                 []harness.ToolSpec
	BuiltinTools          []BuiltinToolMapping
	PermissionMode        harness.PermissionMode
	PermissionModeMapping *PermissionModeMapping
	MCPServers            map[string]any
	Debug                 *harness.DebugConfig
	AuthenticationProfile authenticationProfileIdentity
	SessionMeta           map[string]any
	InstructionMapping    *InstructionMapping
	ResponseFormat        *harness.ResponseFormat
	OutputSchemaMapping   *OutputSchemaMapping
	Model                 string
	ModelMapping          ModelMapping
}

// createTurnStartConfig mirrors TS `createACPTurnStartConfig`.
func createTurnStartConfig(in createTurnStartConfigInput) TurnStartConfig {
	fingerprintPayload := map[string]any{
		"authenticationProfile": in.AuthenticationProfile,
		"sessionMeta":           in.SessionMeta,
		"modelMapping":          in.ModelMapping,
		"builtinTools":          in.BuiltinTools,
		"permissionModeMapping": in.PermissionModeMapping,
		"mcpServers":            in.MCPServers,
	}
	if in.InstructionMapping != nil {
		fingerprintPayload["instructionMapping"] = in.InstructionMapping
	}
	if in.OutputSchemaMapping != nil {
		fingerprintPayload["outputSchemaMapping"] = in.OutputSchemaMapping
	}
	sum := sha256.Sum256([]byte(stableStringify(fingerprintPayload)))

	cfg := TurnStartConfig{
		Version: 1, ConfigurationFingerprint: hex.EncodeToString(sum[:]),
		Prompt:         nonNilBlocks(in.Prompt),
		Tools:          nonNilToolSpecs(in.Tools),
		BuiltinTools:   nonNilBuiltinTools(in.BuiltinTools),
		PermissionMode: in.PermissionMode, PermissionModeMapping: in.PermissionModeMapping,
		ResponseFormat: in.ResponseFormat, OutputSchemaMapping: in.OutputSchemaMapping, Debug: in.Debug,
	}
	if in.Model != "" {
		cfg.Model = in.Model
		m := in.ModelMapping
		cfg.ModelMapping = &m
	}
	return cfg
}

// createColdSessionState mirrors TS `createACPColdSessionState`.
func createColdSessionState(cfg TurnStartConfig) ColdSessionState {
	return ColdSessionState{
		Version: cfg.Version, ConfigurationFingerprint: cfg.ConfigurationFingerprint,
		Tools: cfg.Tools, BuiltinTools: cfg.BuiltinTools, PermissionMode: cfg.PermissionMode,
		ResponseFormat: cfg.ResponseFormat, OutputSchemaMapping: cfg.OutputSchemaMapping,
		PermissionModeMapping: cfg.PermissionModeMapping,
	}
}

// validateTurnStartConfigInput is the current (non-persisted) configuration
// validateTurnStartConfig recomputes a fingerprint from, to compare against
// a persisted TurnStartConfig. Mirrors TS `validateACPTurnStartConfig`'s
// parameter object.
type validateTurnStartConfigInput struct {
	AuthenticationProfile authenticationProfileIdentity
	SessionMeta           map[string]any
	InstructionMapping    *InstructionMapping
	OutputSchemaMapping   *OutputSchemaMapping
	ModelMapping          ModelMapping
	BuiltinTools          []BuiltinToolMapping
	PermissionModeMapping *PermissionModeMapping
	MCPServers            map[string]any
}

// validateTurnStartConfig mirrors TS `validateACPTurnStartConfig`: it
// recomputes the non-secret configuration fingerprint from the CURRENT
// session settings (not the persisted ones) and rejects a lossy rerun if it
// no longer matches, since the resumed turn must not silently change tool
// catalogs, permission mapping, auth, etc.
func validateTurnStartConfig(turnStartConfig TurnStartConfig, in validateTurnStartConfigInput) error {
	current := createTurnStartConfig(createTurnStartConfigInput{
		Prompt: turnStartConfig.Prompt, Tools: turnStartConfig.Tools, BuiltinTools: in.BuiltinTools,
		PermissionMode: turnStartConfig.PermissionMode, PermissionModeMapping: in.PermissionModeMapping,
		MCPServers: in.MCPServers, Debug: turnStartConfig.Debug, AuthenticationProfile: in.AuthenticationProfile,
		SessionMeta: in.SessionMeta, InstructionMapping: in.InstructionMapping, ResponseFormat: turnStartConfig.ResponseFormat,
		OutputSchemaMapping: in.OutputSchemaMapping, Model: turnStartConfig.Model, ModelMapping: in.ModelMapping,
	})
	if current.ConfigurationFingerprint != turnStartConfig.ConfigurationFingerprint {
		return fmt.Errorf("The persisted ACP turn start configuration is incompatible with the current non-secret start configuration.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// validateColdSessionConfigurationInput mirrors
// validateTurnStartConfigInput, plus the fields a cold restore also needs
// (permissionMode and debug are not part of the persisted ColdSessionState
// itself but must match the CURRENT turn's settings).
type validateColdSessionConfigurationInput struct {
	PermissionMode        harness.PermissionMode
	AuthenticationProfile authenticationProfileIdentity
	SessionMeta           map[string]any
	InstructionMapping    *InstructionMapping
	OutputSchemaMapping   *OutputSchemaMapping
	ModelMapping          ModelMapping
	BuiltinTools          []BuiltinToolMapping
	PermissionModeMapping *PermissionModeMapping
	MCPServers            map[string]any
	Debug                 *harness.DebugConfig
}

// validateColdSessionConfiguration mirrors TS
// `validateACPColdSessionConfiguration`: it recomputes a turn start config
// with an empty prompt (a cold restore carries no prompt) and the current
// session settings, and rejects the restore if either the fingerprint or
// the permission mode changed. Returns the freshly computed config, which
// becomes the respawned session's turnStartConfig (TS's `current` return
// value — used verbatim as the cold-restore `start` frame's turnStartConfig
// and as the session's remembered config for a future rerun/cold-restore).
func validateColdSessionConfiguration(coldSession ColdSessionState, in validateColdSessionConfigurationInput) (TurnStartConfig, error) {
	current := createTurnStartConfig(createTurnStartConfigInput{
		Prompt: nil, Tools: coldSession.Tools, BuiltinTools: in.BuiltinTools,
		PermissionMode: in.PermissionMode, PermissionModeMapping: in.PermissionModeMapping,
		MCPServers: in.MCPServers, Debug: in.Debug, AuthenticationProfile: in.AuthenticationProfile,
		SessionMeta: in.SessionMeta, InstructionMapping: in.InstructionMapping, ResponseFormat: coldSession.ResponseFormat,
		OutputSchemaMapping: in.OutputSchemaMapping, Model: "", ModelMapping: in.ModelMapping,
	})
	if current.ConfigurationFingerprint != coldSession.ConfigurationFingerprint || coldSession.PermissionMode != in.PermissionMode {
		return TurnStartConfig{}, fmt.Errorf("ACP cold-session state is incompatible with the current non-secret session configuration.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return current, nil
}

// assertRecoveryToolCatalog mirrors TS `assertRecoveryToolCatalog`: a lossy
// rerun must use the exact same active host tool catalog as the turn it is
// replacing, since the fresh process negotiates tool availability once, at
// start.
func assertRecoveryToolCatalog(persisted, current []harness.ToolSpec) error {
	if fingerprintValue(persisted) != fingerprintValue(current) {
		return fmt.Errorf("ACP lossy rerun requires the same active host tool catalog as the original turn.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// fingerprintValue mirrors TS `fingerprintValue`.
func fingerprintValue(value any) string {
	sum := sha256.Sum256([]byte(stableStringify(value)))
	return hex.EncodeToString(sum[:])
}

// nonNilBlocks/nonNilToolSpecs/nonNilBuiltinTools guarantee a non-nil
// (possibly empty) slice: the embedded bridge's zod schemas require the
// `prompt`/`tools`/`builtinTools` wire fields to always be JSON arrays,
// never `null`, matching TS's `z.array(...)` (with `.default([])` for the
// two array fields that are optional at the call site).
func nonNilBlocks(v []TextContentBlock) []TextContentBlock {
	if v == nil {
		return []TextContentBlock{}
	}
	return append([]TextContentBlock(nil), v...)
}

func nonNilToolSpecs(v []harness.ToolSpec) []harness.ToolSpec {
	if v == nil {
		return []harness.ToolSpec{}
	}
	return append([]harness.ToolSpec(nil), v...)
}

func nonNilBuiltinTools(v []BuiltinToolMapping) []BuiltinToolMapping {
	if v == nil {
		return []BuiltinToolMapping{}
	}
	return append([]BuiltinToolMapping(nil), v...)
}
