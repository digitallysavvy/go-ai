package deepagents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// HarnessID is the stable harness-v1 identifier for this adapter.
const HarnessID = "deepagents"

// clientApp is the value sent as AI_SDK_HARNESS_CLIENT_APP / User-Agent.
var clientApp = harnessutil.ClientApp(HarnessID, Version)

// skillsSourcePath is written under $HOME (out of the work dir so it cannot
// clash with code cloned into the work dir) and also discovered from
// <workDir> for repo-provided skills. Mirrors TS `SKILLS_SOURCE_PATH`.
const skillsSourcePath = "/.agents/skills"

// Settings configures CreateDeepAgents. Mirrors TS `DeepAgentsHarnessSettings`.
type Settings struct {
	Auth harness.Authentication
	// CredentialForwarding customizes each credential value before it is
	// forwarded into a sandbox process.
	CredentialForwarding harness.CredentialForwarding
	// Thinking controls Anthropic extended thinking for the Deep Agents
	// model. Nil preserves the Deep Agents runtime default.
	Thinking *ThinkingConfig
	// Effort controls how much effort Claude applies when adaptive thinking
	// is enabled: "low"|"medium"|"high"|"xhigh"|"max".
	Effort string
	// Port overrides the bridge port; defaults to the sandbox's first
	// declared port.
	Port int
	// PortEndpoint overrides the host endpoint used to connect to the
	// sandbox bridge. Required together with Port for a basic sandbox
	// session.
	PortEndpoint *harness.PortEndpoint
	// StartupTimeout is the maximum time to wait for the bridge to advertise
	// its port. Zero defaults to bridge.DefaultStartupTimeout.
	StartupTimeout time.Duration
	// Reconnect configures reconnection after an established bridge
	// connection drops.
	Reconnect bridge.ReconnectOptions
	// MintBridgeToken creates the bridge channel token for one session.
	// Defaults to a random 32-byte hex token.
	MintBridgeToken harness.MintBridgeTokenCallback
	// RecursionLimit is the maximum LangGraph super-steps per turn before it
	// errors. Nil applies the Deep Agents default.
	RecursionLimit *int
	// MCPServers are server definitions keyed by name, in the underlying
	// runtime's native MCP server configuration format.
	MCPServers map[string]any
}

type deepAgentsHarness struct {
	settings Settings
}

// CreateDeepAgents returns the deepagents harness-v1 adapter. Mirrors TS
// `createDeepAgents`.
func CreateDeepAgents(settings ...Settings) harness.Harness {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	return &deepAgentsHarness{settings: s}
}

func (h *deepAgentsHarness) SpecificationVersion() string { return harness.SpecificationVersion }
func (h *deepAgentsHarness) HarnessID() string            { return HarnessID }

// SupportsBuiltinToolApprovals is true: built-in tool approvals are gated
// in-bridge via DeepAgents' interruptOn (HITL) middleware.
func (h *deepAgentsHarness) SupportsBuiltinToolApprovals() bool { return true }

func (h *deepAgentsHarness) BuiltinTools() map[string]harness.BuiltinTool { return BuiltinTools() }

func (h *deepAgentsHarness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return GetBootstrap(ctx)
}

// resumeStateData is the adapter-defined `data` payload of lifecycle state.
// Mirrors TS `deepAgentsResumeStateSchema`.
type resumeStateData struct {
	Bridge                       *bridgeCoords     `json:"bridge,omitempty"`
	SandboxCredentialEnvironment map[string]string `json:"sandboxCredentialEnvironment,omitempty"`
}

// bridgeCoords are the live bridge coordinates returned by doDetach/
// doSuspendTurn so a later process can reattach. Mirrors TS
// `deepAgentsBridgeCoordsSchema`.
type bridgeCoords struct {
	Port            int     `json:"port"`
	Token           string  `json:"token"`
	LastSeenEventID float64 `json:"lastSeenEventId"`
	SandboxID       string  `json:"sandboxId,omitempty"`
}

// ValidateLifecycleStateData mirrors TS `lifecycleStateSchema`.
func (h *deepAgentsHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var v resumeStateData
	return json.Unmarshal(data, &v)
}

func unsupported(capability string) error {
	return harness.NewCapabilityUnsupportedError(
		fmt.Sprintf("Harness 'deepagents' does not support %s yet.", capability), HarnessID, nil)
}

func (h *deepAgentsHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	settings := h.settings
	sandboxSession := opts.SandboxSession
	toolSafeSandboxSession := harness.GetRestrictedSandboxSession(sandboxSession)
	networkSession, isNetwork := harness.AsNetworkSandboxSession(sandboxSession)
	var sandboxID string
	if isNetwork {
		sandboxID = networkSession.ID()
	}
	if err := validateBasicSandboxSettings(sandboxSession, settings.Port, settings.PortEndpoint); err != nil {
		return nil, err
	}
	if settings.MintBridgeToken != nil && sandboxID == "" {
		return nil, harness.NewCapabilityUnsupportedError(
			"The deepagents harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.",
			HarnessID, nil)
	}

	isContinue := opts.ContinueFrom != nil
	isResume := isContinue || opts.ResumeFrom != nil
	var lifecycleData json.RawMessage
	switch {
	case opts.ContinueFrom != nil:
		lifecycleData = opts.ContinueFrom.Data
	case opts.ResumeFrom != nil:
		lifecycleData = opts.ResumeFrom.Data
	}
	var resumeData resumeStateData
	if len(lifecycleData) > 0 {
		_ = json.Unmarshal(lifecycleData, &resumeData)
	}
	coords := resumeData.Bridge

	authenticationMode := resolveAuthenticationMode(settings.Auth, harnessutil.ProcessEnv())
	resolvedAuthEnvironment := resolveEnv(settings.Auth, harnessutil.ProcessEnv())
	sandboxAuthEnvironment := resolvedAuthEnvironment
	var sandboxCredentialEnvironment map[string]string
	credentialsBrokered := false

	if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
		if resumeData.SandboxCredentialEnvironment != nil {
			sandboxCredentialEnvironment = resumeData.SandboxCredentialEnvironment
		} else {
			var err error
			sandboxCredentialEnvironment, err = harnessutil.CreateSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
				Environment:                    resolvedAuthEnvironment,
				CredentialEnvironmentVariables: CredentialEnvironmentVariables,
				CredentialForwarding:           settings.CredentialForwarding,
			})
			if err != nil {
				return nil, err
			}
		}
		merged := map[string]string{}
		for k, v := range resolvedAuthEnvironment {
			merged[k] = v
		}
		for k, v := range sandboxCredentialEnvironment {
			merged[k] = v
		}
		sandboxAuthEnvironment = merged
		transformations, err := createDeepAgentsRequestTransformations(createRequestTransformationsInput{
			Env: resolvedAuthEnvironment, SandboxEnv: sandboxAuthEnvironment, Auth: authenticationMode,
		})
		if err != nil {
			return nil, err
		}
		if len(transformations) > 0 {
			if err := adder.AddRequestTransformations(ctx, transformations); err != nil {
				return nil, err
			}
		}
		credentialsBrokered = true
	}

	// Harness SDK state (bootstrap, per-session runs) always lives under the
	// sandbox's own HOME, never the working directory.
	homeDir, err := harnessutil.ResolveSandboxHomeDir(ctx, toolSafeSandboxSession)
	if err != nil {
		return nil, err
	}
	stateDir := harness.StateDirectoryPath(homeDir)
	bootstrapDir := stateDir + "/" + BootstrapDir

	workDir := opts.SessionWorkDir
	homeSkillsRoot := homeDir + skillsSourcePath
	skillsPaths := []string{workDir + skillsSourcePath, homeSkillsRoot}
	sessionDataDir := harness.SessionDataDirectoryPath(stateDir, opts.SessionID)
	bridgeStateDir := sessionDataDir + "/bridge"
	timeout := settings.StartupTimeout
	if timeout <= 0 {
		timeout = bridge.DefaultStartupTimeout
	}

	onBridgeErr := func(*harness.ErrorPart) {}
	var onDiagnostic func(bridge.OutboundMessage)
	if opts.Observability != nil {
		onDiagnostic = bridge.ReportDiagnostic(opts.Observability.Report, opts.SessionID)
	}

	sessionArgs := sessionParams{
		sessionID: opts.SessionID, thinking: settings.Thinking, effort: settings.Effort,
		sandboxID: sandboxID, sandboxCredentialEnvironment: sandboxCredentialEnvironment,
		sandbox: toolSafeSandboxSession, homeDir: homeDir, skillsPaths: skillsPaths,
		permissionMode: opts.PermissionMode, builtinToolFiltering: opts.BuiltinToolFiltering,
		recursionLimit: settings.RecursionLimit, mcpServers: settings.MCPServers, headers: opts.Headers,
		reconnect: settings.Reconnect,
	}

	// Attach to the still-running bridge (continueFrom replays past the
	// cursor); on failure fall through to a fresh spawn.
	if coords != nil {
		if sess, ok := attachToRunningBridge(ctx, sandboxSession, settings, coords, isContinue, onBridgeErr, onDiagnostic, sessionArgs); ok {
			return sess, nil
		}
	}

	port, err := bridge.ResolveBridgePort(sandboxSession, settings.Port)
	if err != nil {
		return nil, wrapPortError(err)
	}
	token := mintBridgeToken(settings.MintBridgeToken, sandboxID)

	forwardedAuthEnvironment := sandboxAuthEnvironment
	if !credentialsBrokered {
		forwardedAuthEnvironment, err = harnessutil.ApplyCredentialForwarding(ctx, harnessutil.CredentialForwardingOptions{
			Environment: sandboxAuthEnvironment, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
			CredentialForwarding: settings.CredentialForwarding,
		})
		if err != nil {
			return nil, err
		}
		harnessutil.WarnCredentialBrokeringUnavailable(harnessutil.WarnCredentialBrokeringUnavailableOptions{
			Environment: resolvedAuthEnvironment, ForwardedEnvironment: forwardedAuthEnvironment,
			CredentialEnvironmentVariables: CredentialEnvironmentVariables,
		})
	}
	env := map[string]string{}
	for k, v := range forwardedAuthEnvironment {
		env[k] = v
	}
	env["AI_SDK_HARNESS_CLIENT_APP"] = clientApp

	if _, err := toolSafeSandboxSession.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "mkdir -p " + harnessutil.ShellQuote(workDir) + " " + harnessutil.ShellQuote(bridgeStateDir),
	}); err != nil {
		return nil, err
	}

	command := "node " + harnessutil.ShellQuote(bootstrapDir+"/bridge.mjs") +
		" --workdir " + harnessutil.ShellQuote(workDir) +
		" --bridge-state-dir " + harnessutil.ShellQuote(bridgeStateDir) +
		" --bootstrap-dir " + harnessutil.ShellQuote(bootstrapDir)
	if isResume {
		command += " --resume true"
	}

	launched, err := bridge.Launch(ctx, bridge.LaunchOptions{
		Label: "deepagents bridge", Source: HarnessID, Sandbox: toolSafeSandboxSession,
		Command: command, Env: env, Port: port, Token: token, ReplayFromDisk: isResume,
		BridgeStateDir: bridgeStateDir, BridgeType: HarnessID, StartupTimeout: timeout,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort)
		},
	})
	if err != nil {
		return nil, err
	}

	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect:       bridge.NewConnectFunc(launched.Endpoint, bridge.DialOptions{Name: "deepagents bridge"}),
		Reconnect:     settings.Reconnect,
		OnBridgeError: onBridgeErr,
		OnDiagnostic:  onDiagnostic,
	})
	if err := channel.Open(ctx, false); err != nil {
		return nil, err
	}

	sessionArgs.channel = channel
	sessionArgs.proc = launched.Proc
	sessionArgs.bridgePort = port
	sessionArgs.bridgeToken = token
	sessionArgs.isResume = isResume
	return newSession(sessionArgs), nil
}

func mintBridgeToken(cb harness.MintBridgeTokenCallback, sandboxID string) string {
	if cb == nil {
		return bridge.CreateBridgeToken()
	}
	return cb(sandboxID)
}

func validateBasicSandboxSettings(sandboxSession providerutils.SandboxSession, port int, portEndpoint *harness.PortEndpoint) error {
	if _, ok := harness.AsNetworkSandboxSession(sandboxSession); ok {
		return nil
	}
	if port == 0 {
		return harness.NewCapabilityUnsupportedError(
			"The deepagents harness requires an explicit `port` when using a basic sandbox session.", HarnessID, nil)
	}
	if portEndpoint == nil {
		return harness.NewCapabilityUnsupportedError(
			"The deepagents harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
	}
	return nil
}

func resolveBridgeEndpoint(ctx context.Context, sandboxSession providerutils.SandboxSession, override *harness.PortEndpoint, port int) (harness.PortEndpoint, error) {
	if override != nil {
		return *override, nil
	}
	if n, ok := harness.AsNetworkSandboxSession(sandboxSession); ok {
		return n.GetPortEndpoint(ctx, harness.PortEndpointOptions{Port: port, Protocol: harness.PortProtocolWS})
	}
	return harness.PortEndpoint{}, harness.NewCapabilityUnsupportedError(
		"The deepagents harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
}

func wrapPortError(err error) error {
	if err == bridge.ErrNoPort {
		return harness.NewCapabilityUnsupportedError(
			"The deepagents harness needs a TCP port exposed by the sandbox. "+
				"Create the sandbox with `ports: [<port>]` or pass `createDeepAgents({ port })`.", HarnessID, err)
	}
	return err
}
