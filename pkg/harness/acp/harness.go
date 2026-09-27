package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var harnessIDRegexp = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ReservedMCPServerName is reserved for HarnessAgent tools; Settings.MCPServers
// must not define a server with this name. Mirrors TS
// `"ai-sdk-harness-tools"`.
const ReservedMCPServerName = "ai-sdk-harness-tools"

type acpHarness struct {
	settings  Settings
	impl      implementation
	clientApp ClientApp
	bootstrap *bootstrapCache
	permMap   *PermissionModeMapping
}

// CreateACP returns an ACP harness-v1 adapter wrapping the ACP
// implementation described by settings. Mirrors TS `createACP`
// (`acp-harness.ts`), minus the concrete per-implementation configs (Cursor,
// fx, GitHub Copilot, Grok Build — WG12).
func CreateACP(settings Settings) (harness.Harness, error) {
	if (settings.CredentialEnv == nil) != (settings.CredentialBrokering == nil) {
		return nil, fmt.Errorf("ACP credentialEnv and credentialBrokering must be configured together.")
	}
	if _, reserved := settings.MCPServers[ReservedMCPServerName]; reserved {
		return nil, fmt.Errorf("ACP MCP server name %q is reserved for HarnessAgent tools.", ReservedMCPServerName)
	}
	if !harnessIDRegexp.MatchString(settings.HarnessID) {
		return nil, fmt.Errorf("ACP harnessId must be a stable kebab-case identifier; received %q.", settings.HarnessID)
	}
	impl := newImplementation(settings)
	if err := validateImplementation(impl); err != nil {
		return nil, err
	}
	clientApp := settings.ClientApp
	if clientApp == (ClientApp{}) {
		clientApp = DefaultClientApp
	}
	builtinTools := settings.BuiltinTools
	if settings.AskUserQuestions != nil {
		merged := map[string]harness.BuiltinTool{}
		for k, v := range builtinTools {
			merged[k] = v
		}
		std := harness.StandardBuiltinTools()
		merged[string(harness.BuiltinToolAskUserQuestions)] = harness.BuiltinTool{
			Tool: std[harness.BuiltinToolAskUserQuestions], CommonName: harness.BuiltinToolAskUserQuestions,
		}
		builtinTools = merged
	}
	h := &acpHarness{
		settings: settings, impl: impl, clientApp: clientApp, bootstrap: &bootstrapCache{},
		permMap: settings.PermissionModeMapping,
	}
	h.settings.BuiltinTools = builtinTools
	return h, nil
}

func (h *acpHarness) SpecificationVersion() string { return harness.SpecificationVersion }
func (h *acpHarness) HarnessID() string            { return h.settings.HarnessID }

// SupportsBuiltinToolApprovals is true for every ACP implementation.
func (h *acpHarness) SupportsBuiltinToolApprovals() bool { return true }

// SupportsBuiltinToolFiltering is false: ACP built-in tool filtering is not
// available in this initial ACP v1 implementation (matches TS).
func (h *acpHarness) SupportsBuiltinToolFiltering() bool { return false }

func (h *acpHarness) BuiltinTools() map[string]harness.BuiltinTool { return h.settings.BuiltinTools }

func (h *acpHarness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return h.bootstrap.getBootstrap(ctx, h.settings.HarnessID, h.impl)
}

// resumeStateData is the adapter-defined `data` payload of lifecycle state.
// Mirrors TS `ACPLifecycleData` (`acp-v1-lifecycle.ts`).
type resumeStateData struct {
	ImplementationIdentity       string                         `json:"implementationIdentity"`
	AuthenticationProfile        *authenticationProfileIdentity `json:"authenticationProfile,omitempty"`
	SandboxCredentialEnvironment map[string]string              `json:"sandboxCredentialEnvironment,omitempty"`
	ACPSessionID                 string                         `json:"acpSessionId,omitempty"`
	Bridge                       *bridgeCoords                  `json:"bridge,omitempty"`
	ColdSession                  *ColdSessionState              `json:"coldSession,omitempty"`
	TurnStartConfig              *TurnStartConfig               `json:"turnStartConfig,omitempty"`
	Recovery                     *recoveryInfo                  `json:"recovery,omitempty"`
	Restoration                  *restorationInfo               `json:"restoration,omitempty"`
	InitialGuidanceApplied       bool                           `json:"initialGuidanceApplied,omitempty"`
	InstructionsFingerprint      string                         `json:"instructionsFingerprint,omitempty"`
	SkillsDirectory              string                         `json:"skillsDirectory,omitempty"`
}

type bridgeCoords struct {
	Port            int     `json:"port"`
	Token           string  `json:"token"`
	LastSeenEventID float64 `json:"lastSeenEventId"`
	SandboxID       string  `json:"sandboxId,omitempty"`
	StateDir        string  `json:"stateDir,omitempty"`
}

func (h *acpHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var v resumeStateData
	return json.Unmarshal(data, &v)
}

func unsupported(harnessID, message string) error {
	return harness.NewCapabilityUnsupportedError(message, harnessID, nil)
}

func unsupportedCause(harnessID, message string, cause error) error {
	return harness.NewCapabilityUnsupportedError(message, harnessID, cause)
}

// acpRespawnStrategy mirrors TS `ACPRespawnStrategy`: the process-loss
// recovery tier chosen when a live attach to a persisted bridge coordinate
// fails (or no coordinate was persisted). Exactly one of disk-replay,
// lossy-rerun or cold-restore applies.
type acpRespawnStrategy struct {
	mode acpRecoveryMode
	// reason (disk-replay/lossy-rerun only): recorded on the resumeStateData
	// for the NEXT lifecycle snapshot's `recovery.reason`, and, for
	// lossy-rerun, sent as the bridge's `recoveryMode.reason`.
	reason string
	// afterSeq (disk-replay only): the cursor a respawned bridge should
	// resume its channel replay from.
	afterSeq float64
	// turnStartConfig (lossy-rerun/cold-restore only): the persisted,
	// fingerprint-validated configuration to restart the turn/session with.
	turnStartConfig *TurnStartConfig
	// acpSessionID (lossy-rerun/cold-restore only): the native ACP session
	// id the respawned bridge should resume or load.
	acpSessionID string
}

type acpRecoveryMode string

const (
	acpRecoveryDiskReplay  acpRecoveryMode = "disk-replay"
	acpRecoveryLossyRerun  acpRecoveryMode = "lossy-rerun"
	acpRecoveryColdRestore acpRecoveryMode = "cold-restore"
)

func (h *acpHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	settings := h.settings
	if opts.BuiltinToolFiltering != nil {
		return nil, unsupported(settings.HarnessID, "ACP built-in tool filtering is not available in this initial ACP v1 implementation.")
	}
	permissionMode := opts.PermissionMode
	if permissionMode == "" {
		permissionMode = harness.PermissionModeAllowAll
	}
	processEnv := harnessutil.ProcessEnv()

	var authenticationEnvironment map[string]string
	if settings.ResolveAuthenticationEnvironment != nil {
		var err error
		authenticationEnvironment, err = settings.ResolveAuthenticationEnvironment(ctx, settings.Auth, processEnv)
		if err != nil {
			return nil, err
		}
	} else {
		authenticationEnvironment = resolveAuthenticationEnvironment(settings.Auth, processEnv)
	}

	compat := resolveProviderAuthenticationCompatibility(settings.Auth, settings.ProviderAuthentication, authenticationEnvironment)
	implementationIdentity := createImplementationIdentity(implementationIdentityInput{
		HarnessID: settings.HarnessID, Implementation: h.impl, ClientApp: h.clientApp,
		ClientCapabilities: settings.ClientCapabilities, ModelMapping: settings.ModelMapping,
		ProviderAuthentication: compat, PermissionModeMapping: h.permMap,
	})
	authProfile := createAuthenticationProfileIdentity(settings.Authentication, compat)
	resolvedProviderAuth, err := resolveProviderAuthentication(settings.Auth, settings.ProviderAuthentication, h.clientApp, authenticationEnvironment, compat)
	if err != nil {
		return nil, err
	}

	sandboxSession := opts.SandboxSession
	toolSafeSandboxSession := harness.GetRestrictedSandboxSession(sandboxSession)
	networkSession, isNetwork := harness.AsNetworkSandboxSession(sandboxSession)
	var sandboxID string
	if isNetwork {
		sandboxID = networkSession.ID()
	}
	if err := validateBasicSandboxSettings(sandboxSession, settings.Port, settings.PortEndpoint, settings.HarnessID); err != nil {
		return nil, err
	}
	if settings.MintBridgeToken != nil && sandboxID == "" {
		return nil, unsupported(settings.HarnessID, fmt.Sprintf(
			"The %s ACP harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.", settings.HarnessID))
	}

	isContinue := opts.ContinueFrom != nil
	isResume := isContinue || opts.ResumeFrom != nil
	var lifecycleData json.RawMessage
	var lifecycleHarnessID string
	switch {
	case opts.ContinueFrom != nil:
		lifecycleData, lifecycleHarnessID = opts.ContinueFrom.Data, opts.ContinueFrom.HarnessID
	case opts.ResumeFrom != nil:
		lifecycleData, lifecycleHarnessID = opts.ResumeFrom.Data, opts.ResumeFrom.HarnessID
	}
	var resumeData resumeStateData
	if len(lifecycleData) > 0 {
		if err := json.Unmarshal(lifecycleData, &resumeData); err != nil {
			return nil, fmt.Errorf("ACP lifecycle state data is invalid: %w", err)
		}
		if lifecycleHarnessID != settings.HarnessID {
			return nil, fmt.Errorf("ACP lifecycle state was produced by harness %q, but this harness is %q.", lifecycleHarnessID, settings.HarnessID)
		}
		if resumeData.ImplementationIdentity != implementationIdentity {
			return nil, fmt.Errorf("ACP lifecycle state is incompatible with the configured implementation.")
		}
		if resumeData.AuthenticationProfile != nil && resumeData.AuthenticationProfile.Digest != authProfile.Digest {
			return nil, fmt.Errorf("ACP lifecycle state is incompatible with the configured authentication profile.")
		}
		if coords := resumeData.Bridge; coords != nil && coords.SandboxID != "" && sandboxID != "" && coords.SandboxID != sandboxID {
			return nil, fmt.Errorf("ACP lifecycle state belongs to sandbox %q, not %q.", coords.SandboxID, sandboxID)
		}
	}

	implEnv := map[string]string{}
	for k, v := range processEnv {
		implEnv[k] = v
	}
	for k, v := range authenticationEnvironment {
		implEnv[k] = v
	}
	implementationEnvironment := resolveImplementationEnvironment(h.impl, implEnv, authenticationEnvironment)
	sandboxImplementationEnvironment := implementationEnvironment
	sandboxProviderAuthEnvironment := resolvedProviderAuth.Env
	var sandboxProviderEnvironment map[string]string
	credentialEnvironmentVariables := uniqueStrings(append(append([]string{}, settings.CredentialEnv...), "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN"))
	var sandboxCredentialEnvironment map[string]string
	credentialsBrokered := false

	if settings.CredentialBrokering != nil {
		if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
			brokeringEnvironment := map[string]string{}
			for k, v := range implementationEnvironment {
				brokeringEnvironment[k] = v
			}
			if resumeData.SandboxCredentialEnvironment != nil {
				sandboxCredentialEnvironment = resumeData.SandboxCredentialEnvironment
			} else {
				sandboxCredentialEnvironment, err = harnessutil.CreateSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
					Environment: brokeringEnvironment, CredentialEnvironmentVariables: credentialEnvironmentVariables,
					CredentialForwarding: settings.CredentialForwarding,
				})
				if err != nil {
					return nil, err
				}
			}
			merged := map[string]string{}
			for k, v := range brokeringEnvironment {
				merged[k] = v
			}
			for k, v := range sandboxCredentialEnvironment {
				merged[k] = v
			}
			sandboxImplementationEnvironment = merged
			headers := opts.Headers
			transformations := settings.CredentialBrokering(brokeringEnvironment, sandboxImplementationEnvironment, headers)
			if len(transformations) > 0 {
				if err := adder.AddRequestTransformations(ctx, transformations); err != nil {
					return nil, err
				}
			}
			credentialsBrokered = true
			if resolvedProviderAuth.Type == "ai-gateway" {
				sandboxProviderEnvironment = map[string]string{}
			}
		}
	}
	if settings.CredentialForwarding != nil && sandboxProviderEnvironment == nil && resolvedProviderAuth.Type == "ai-gateway" {
		sandboxProviderEnvironment = map[string]string{}
	}
	if sandboxProviderEnvironment != nil {
		for k, v := range sandboxProviderEnvironment {
			sandboxImplementationEnvironment[k] = v
		}
		filtered := map[string]string{}
		for k, v := range resolvedProviderAuth.Env {
			if k != "AI_SDK_ACP_GATEWAY_API_KEY" && k != "AI_SDK_ACP_GATEWAY_BASE_URL" {
				filtered[k] = v
			}
		}
		sandboxProviderAuthEnvironment = filtered
	}

	sandboxHomeDir, err := harnessutil.ResolveSandboxHomeDir(ctx, toolSafeSandboxSession)
	if err != nil {
		return nil, err
	}
	stateDirectory := harness.StateDirectoryPath(sandboxHomeDir)
	resolvedBridgeDir := stateDirectory + "/" + bootstrapDir(settings.HarnessID)
	resolvedImplementationDir := resolvedBridgeDir + "/implementation"
	workDir := opts.SessionWorkDir
	privateSessionDir := resolvePrivateSessionDirectory(stateDirectory, opts.SessionID)
	implementationHomeDir := sandboxHomeDir
	if h.impl.Source.Type == SourceInstallCommand {
		implementationHomeDir = resolvedImplementationDir + "/home"
	}
	skillsDir := settings.SkillsDirectory
	if skillsDir == "" {
		skillsDir = DefaultSkillsDirectory
	}
	skillsDirectory, err := resolveSkillsDirectory(implementationHomeDir, settings.SkillsDirectory)
	if err != nil {
		return nil, err
	}
	bridgeStateDir := privateSessionDir + "/bridge"

	builtinToolCatalog := serializeBuiltinTools(settings.BuiltinTools)
	onBridgeErr := func(*harness.ErrorPart) {}

	sessionArgs := sessionParams{
		sessionID: opts.SessionID, harnessID: settings.HarnessID,
		modelMapping: settings.ModelMapping, sessionMeta: settings.SessionMeta,
		instructionMapping: settings.InstructionMapping, outputSchemaMapping: settings.OutputSchemaMapping,
		askUserQuestions: settings.AskUserQuestions, implementationIdentity: implementationIdentity,
		authenticationProfile: authProfile, builtinTools: builtinToolCatalog,
		permissionMode: permissionMode, permissionModeMapping: h.permMap, mcpServers: settings.MCPServers,
		isMcpToolCall: settings.IsMcpToolCall, sandbox: toolSafeSandboxSession,
		homePath: implementationHomeDir, skillsDir: skillsDir, skillsDirectory: skillsDirectory,
		sandboxID: sandboxID, sandboxCredentialEnvironment: sandboxCredentialEnvironment,
		reconnect: settings.Reconnect, bridgeStateDir: bridgeStateDir,
	}

	// Attach to a still-running bridge when coordinates are known (TS's
	// first recovery attempt). On failure, fall through to process-loss
	// recovery: disk-replay/lossy-rerun for a continued turn, cold-restore
	// for a plain resume. Mirrors TS's `isResume` block in `doStart`.
	var respawnStrategy *acpRespawnStrategy
	if isResume {
		if len(lifecycleData) == 0 {
			return nil, fmt.Errorf("ACP lifecycle state data is missing.")
		}
		coords := resumeData.Bridge
		if coords == nil && isContinue {
			return nil, unsupported(settings.HarnessID,
				"ACP continuation state does not contain bridge coordinates required for replay or process-loss rerun.")
		}
		if coords != nil {
			sess, attachErr := attachToRunningBridge(ctx, sandboxSession, settings, coords, isContinue, resumeData, onBridgeErr, sessionArgs)
			if attachErr == nil {
				return sess, nil
			}
			if isContinue {
				logText, _ := toolSafeSandboxSession.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: bridgeStateDir + "/event-log.ndjson"})
				if harnessutil.ClassifyDiskLog(logText) == harnessutil.DiskLogReplay {
					respawnStrategy = &acpRespawnStrategy{mode: acpRecoveryDiskReplay, reason: "completed coherent event log", afterSeq: coords.LastSeenEventID}
				} else {
					if resumeData.TurnStartConfig == nil || resumeData.ACPSessionID == "" {
						return nil, unsupportedCause(settings.HarnessID,
							"ACP process-loss recovery is unavailable because the lifecycle state does not contain the persisted turn start configuration and ACP session identifier.",
							attachErr)
					}
					if err := validateTurnStartConfig(*resumeData.TurnStartConfig, validateTurnStartConfigInput{
						AuthenticationProfile: authProfile, SessionMeta: settings.SessionMeta,
						InstructionMapping: settings.InstructionMapping, OutputSchemaMapping: settings.OutputSchemaMapping,
						ModelMapping: settings.ModelMapping, BuiltinTools: builtinToolCatalog,
						PermissionModeMapping: h.permMap, MCPServers: settings.MCPServers,
					}); err != nil {
						return nil, err
					}
					respawnStrategy = &acpRespawnStrategy{
						mode: acpRecoveryLossyRerun, reason: "event log not replayable",
						turnStartConfig: resumeData.TurnStartConfig, acpSessionID: resumeData.ACPSessionID,
					}
				}
			}
		}
		if !isContinue {
			if resumeData.ColdSession == nil || resumeData.ACPSessionID == "" {
				return nil, unsupported(settings.HarnessID,
					"Cold ACP session restoration requires persisted cold-session configuration and an ACP session identifier.")
			}
			cfg, err := validateColdSessionConfiguration(*resumeData.ColdSession, validateColdSessionConfigurationInput{
				PermissionMode: permissionMode, AuthenticationProfile: authProfile, SessionMeta: settings.SessionMeta,
				InstructionMapping: settings.InstructionMapping, OutputSchemaMapping: settings.OutputSchemaMapping,
				ModelMapping: settings.ModelMapping, BuiltinTools: builtinToolCatalog,
				PermissionModeMapping: h.permMap, MCPServers: settings.MCPServers, Debug: debugConfig(opts.Observability),
			})
			if err != nil {
				return nil, err
			}
			respawnStrategy = &acpRespawnStrategy{mode: acpRecoveryColdRestore, turnStartConfig: &cfg, acpSessionID: resumeData.ACPSessionID}
		}
	}

	forwardedImplementationEnvironment := sandboxImplementationEnvironment
	if sandboxCredentialEnvironment == nil {
		forwardedImplementationEnvironment, err = harnessutil.ApplyCredentialForwarding(ctx, harnessutil.CredentialForwardingOptions{
			Environment: sandboxImplementationEnvironment, CredentialEnvironmentVariables: credentialEnvironmentVariables,
			CredentialForwarding: settings.CredentialForwarding,
		})
		if err != nil {
			return nil, err
		}
	}
	if settings.CredentialBrokering != nil && sandboxCredentialEnvironment == nil {
		env := map[string]string{}
		for k, v := range sandboxImplementationEnvironment {
			env[k] = v
		}
		for k, v := range resolvedProviderAuth.Env {
			env[k] = v
		}
		forwarded := map[string]string{}
		for k, v := range forwardedImplementationEnvironment {
			forwarded[k] = v
		}
		for k, v := range sandboxProviderAuthEnvironment {
			forwarded[k] = v
		}
		harnessutil.WarnCredentialBrokeringUnavailable(harnessutil.WarnCredentialBrokeringUnavailableOptions{
			Environment: env, ForwardedEnvironment: forwarded,
			CredentialEnvironmentVariables: append(append([]string{}, credentialEnvironmentVariables...), "AI_SDK_ACP_GATEWAY_API_KEY"),
		})
	}
	_ = credentialsBrokered

	if settings.AuthenticationFiles != nil {
		files := settings.AuthenticationFiles(implementationEnvironment, forwardedImplementationEnvironment, sandboxCredentialEnvironment != nil)
		if err := writeAuthenticationFiles(ctx, toolSafeSandboxSession, implementationHomeDir, files); err != nil {
			return nil, err
		}
	}

	port, err := bridge.ResolveBridgePort(sandboxSession, settings.Port)
	if err != nil {
		return nil, wrapPortError(err, settings.HarnessID)
	}
	token := mintBridgeToken(settings.MintBridgeToken, sandboxID)

	if _, err := toolSafeSandboxSession.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "mkdir -p " + harnessutil.ShellQuote(workDir) + " " + harnessutil.ShellQuote(bridgeStateDir),
	}); err != nil {
		return nil, err
	}

	env := map[string]string{}
	for k, v := range forwardedImplementationEnvironment {
		env[k] = v
	}
	var bridgeProviderAuthentication *bridgeProviderAuth
	if resolvedProviderAuth.Type != "" {
		bridgeProviderAuthentication = &bridgeProviderAuth{Type: resolvedProviderAuth.Type, Env: resolvedProviderAuth.GatewayEnv}
	}
	var providerEnvironment map[string]string
	if sandboxProviderEnvironment != nil {
		providerEnvironment = map[string]string{}
	}
	for k, v := range createBridgeEnvironment(bridgeConfiguration{
		Authentication: settings.Authentication, ProviderAuthentication: bridgeProviderAuthentication,
		ProviderEnvironment: providerEnvironment, SessionMeta: settings.SessionMeta,
		ClientCapabilities: settings.ClientCapabilities, AskUserQuestionsRequestMethod: askUserQuestionsRequestMethod(settings.AskUserQuestions),
		HostToolMCPTransport: settings.HostToolMCPTransport,
	}) {
		env[k] = v
	}
	for k, v := range sandboxProviderAuthEnvironment {
		env[k] = v
	}
	env[bridge.EnvChannelToken] = token
	env[bridge.EnvWSPort] = fmt.Sprint(port)

	command := "node " + harnessutil.ShellQuote(resolvedBridgeDir+"/bridge.mjs") +
		" --workdir " + harnessutil.ShellQuote(workDir) +
		" --bridge-state-dir " + harnessutil.ShellQuote(bridgeStateDir) +
		" --implementation-dir " + harnessutil.ShellQuote(resolvedImplementationDir) +
		" --bridge-type " + harnessutil.ShellQuote(settings.HarnessID)

	timeout := settings.StartupTimeout
	if timeout <= 0 {
		timeout = bridge.DefaultStartupTimeout
	}
	launched, err := bridge.Launch(ctx, bridge.LaunchOptions{
		Label: settings.HarnessID + " ACP bridge", Source: settings.HarnessID, Sandbox: toolSafeSandboxSession,
		Command: command, Env: env, Port: port, Token: token,
		BridgeStateDir: bridgeStateDir, BridgeType: settings.HarnessID, StartupTimeout: timeout,
		ReplayFromDisk: respawnStrategy != nil && respawnStrategy.mode == acpRecoveryDiskReplay,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort, settings.HarnessID)
		},
	})
	if err != nil {
		return nil, err
	}

	channelOpts := bridge.ChannelOptions{
		Connect:       bridge.NewConnectFunc(launched.Endpoint, bridge.DialOptions{Name: settings.HarnessID + " ACP bridge"}),
		Decode:        decodeOutbound,
		Reconnect:     settings.Reconnect,
		OnBridgeError: onBridgeErr,
	}
	resumeChannel := false
	if respawnStrategy != nil && respawnStrategy.mode == acpRecoveryDiskReplay {
		channelOpts.InitialLastSeenEventID = respawnStrategy.afterSeq
		resumeChannel = true
	}
	channel := bridge.NewChannel(channelOpts)
	if err := channel.Open(ctx, resumeChannel); err != nil {
		return nil, err
	}

	var coldRestoration string
	if respawnStrategy != nil && respawnStrategy.mode == acpRecoveryColdRestore {
		startMsg := StartMessage{
			StartBase: bridge.StartBase{
				Tools: respawnStrategy.turnStartConfig.Tools, PermissionMode: permissionMode,
				Debug: debugConfig(opts.Observability),
			},
			Prompt: []TextContentBlock{}, BuiltinTools: builtinToolCatalog, PermissionModeMapping: h.permMap,
			TurnStartConfig: *respawnStrategy.turnStartConfig,
			RecoveryMode:    &RecoveryMode{Type: "cold-restore", ACPSessionID: respawnStrategy.acpSessionID},
		}
		if settings.InstructionMapping != nil {
			startMsg.InstructionMapping = settings.InstructionMapping
		}
		if settings.MCPServers != nil {
			startMsg.MCPServers = settings.MCPServers
		}
		method, err := restoreColdACPSession(ctx, channel, settings.HarnessID, startMsg)
		if err != nil {
			channel.BeginClose()
			if !channel.IsClosed() {
				_ = channel.Send(bridge.DestroyCommand{})
			}
			_ = launched.Proc.Kill()
			channel.Close()
			return nil, err
		}
		coldRestoration = method
	}

	sessionArgs.channel = channel
	sessionArgs.proc = launched.Proc
	sessionArgs.bridgePort = port
	sessionArgs.bridgeToken = token
	sessionArgs.isResume = isResume
	sessionArgs.acpSessionID = resumeData.ACPSessionID
	switch {
	case respawnStrategy != nil && (respawnStrategy.mode == acpRecoveryLossyRerun || respawnStrategy.mode == acpRecoveryColdRestore):
		sessionArgs.turnStartConfig = respawnStrategy.turnStartConfig
	default:
		sessionArgs.turnStartConfig = resumeData.TurnStartConfig
	}
	sessionArgs.turnInFlight = respawnStrategy != nil && (respawnStrategy.mode == acpRecoveryDiskReplay || respawnStrategy.mode == acpRecoveryLossyRerun)
	if respawnStrategy != nil && (respawnStrategy.mode == acpRecoveryDiskReplay || respawnStrategy.mode == acpRecoveryLossyRerun) {
		sessionArgs.recoveryStatus = &recoveryInfo{Mode: string(respawnStrategy.mode), Reason: respawnStrategy.reason}
	} else if resumeData.Recovery != nil {
		sessionArgs.recoveryStatus = &recoveryInfo{Mode: resumeData.Recovery.Mode, Reason: resumeData.Recovery.Reason}
	}
	if coldRestoration != "" {
		sessionArgs.restoration = &restorationInfo{Method: coldRestoration}
	} else if resumeData.Restoration != nil {
		sessionArgs.restoration = &restorationInfo{Method: resumeData.Restoration.Method}
	}
	sessionArgs.replayOnly = respawnStrategy != nil && respawnStrategy.mode == acpRecoveryDiskReplay
	sessionArgs.lossyRerun = respawnStrategy != nil && respawnStrategy.mode == acpRecoveryLossyRerun
	return newSession(sessionArgs), nil
}

func debugConfig(o *harness.Observability) *harness.DebugConfig {
	if o == nil {
		return nil
	}
	return o.Debug
}

func askUserQuestionsRequestMethod(s *AskUserQuestionsSettings) string {
	if s == nil {
		return ""
	}
	return s.RequestMethod
}

func mintBridgeToken(cb harness.MintBridgeTokenCallback, sandboxID string) string {
	if cb == nil {
		return bridge.CreateBridgeToken()
	}
	return cb(sandboxID)
}

func validateBasicSandboxSettings(sandboxSession providerutils.SandboxSession, port int, portEndpoint *harness.PortEndpoint, harnessID string) error {
	if _, ok := harness.AsNetworkSandboxSession(sandboxSession); ok {
		return nil
	}
	if port == 0 {
		return unsupported(harnessID, fmt.Sprintf("The %s ACP harness requires an explicit `port` when using a basic sandbox session.", harnessID))
	}
	if portEndpoint == nil {
		return unsupported(harnessID, fmt.Sprintf("The %s ACP harness requires an explicit `portEndpoint` when using a basic sandbox session.", harnessID))
	}
	return nil
}

func resolveBridgeEndpoint(ctx context.Context, sandboxSession providerutils.SandboxSession, override *harness.PortEndpoint, port int, harnessID string) (harness.PortEndpoint, error) {
	if override != nil {
		return *override, nil
	}
	if n, ok := harness.AsNetworkSandboxSession(sandboxSession); ok {
		return n.GetPortEndpoint(ctx, harness.PortEndpointOptions{Port: port, Protocol: harness.PortProtocolWS})
	}
	return harness.PortEndpoint{}, unsupported(harnessID, fmt.Sprintf("The %s ACP harness requires an explicit `portEndpoint` when using a basic sandbox session.", harnessID))
}

func wrapPortError(err error, harnessID string) error {
	if err == bridge.ErrNoPort {
		return unsupported(harnessID, fmt.Sprintf(
			"The %s ACP harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or pass `port` in settings.", harnessID))
	}
	return err
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func writeAuthenticationFiles(ctx context.Context, sandbox providerutils.SandboxSession, homePath string, files []AuthenticationFile) error {
	for _, f := range files {
		if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{
			Path: posixpath.Join(homePath, f.Path), Content: f.Content,
		}); err != nil {
			return err
		}
	}
	return nil
}

// serializeBuiltinTools mirrors TS `serializeBuiltinTools`. Iterates in
// sorted tool-name order for deterministic wire output (Go map iteration is
// randomized; TS `Object.entries` follows insertion order, which callers
// cannot observe here anyway once tools live in a Go map).
func serializeBuiltinTools(tools map[string]harness.BuiltinTool) []BuiltinToolMapping {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]BuiltinToolMapping, 0, len(tools))
	for _, name := range names {
		tool := tools[name]
		m := BuiltinToolMapping{ToolName: name, NativeName: tool.NativeName, Title: tool.Title, ToolUseKind: string(tool.ToolUseKind)}
		if schema, ok := tool.Parameters.(map[string]any); ok {
			m.InputSchema = schema
		}
		out = append(out, m)
	}
	return out
}
