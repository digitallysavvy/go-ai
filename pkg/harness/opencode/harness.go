package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// HarnessID is the stable harness-v1 identifier for this adapter.
const HarnessID = "opencode"

var clientApp = harnessutil.ClientApp(HarnessID, Version)

// Settings configures CreateOpenCode. Mirrors TS `OpenCodeHarnessSettings`.
type Settings struct {
	Auth                 harness.Authentication
	CredentialForwarding harness.CredentialForwarding
	// OpenCodeConfig is additional configuration passed through to OpenCode
	// as-is. Keys must use their native OpenCode names. Values managed by
	// this adapter take precedence over conflicting entries.
	OpenCodeConfig map[string]any
	// MCPServers are server definitions keyed by name. The name
	// "harness-tools" is reserved for HarnessAgent tools.
	MCPServers map[string]any
	Provider   string
	// ReasoningVariant is the OpenCode reasoning/thinking variant for
	// reasoning-capable models, e.g. "low", "medium", "high".
	ReasoningVariant string
	Port             int
	PortEndpoint     *harness.PortEndpoint
	StartupTimeout   time.Duration
	Reconnect        bridge.ReconnectOptions
	MintBridgeToken  harness.MintBridgeTokenCallback
}

type openCodeHarness struct {
	settings Settings
}

// CreateOpenCode returns the opencode harness-v1 adapter. Mirrors TS
// `createOpenCode`.
func CreateOpenCode(settings ...Settings) (harness.Harness, error) {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	if _, reserved := s.MCPServers["harness-tools"]; reserved {
		return nil, errors.New(`OpenCode MCP server name "harness-tools" is reserved for HarnessAgent tools.`)
	}
	return &openCodeHarness{settings: s}, nil
}

func (h *openCodeHarness) SpecificationVersion() string                 { return harness.SpecificationVersion }
func (h *openCodeHarness) HarnessID() string                            { return HarnessID }
func (h *openCodeHarness) SupportsBuiltinToolApprovals() bool           { return true }
func (h *openCodeHarness) BuiltinTools() map[string]harness.BuiltinTool { return BuiltinTools() }
func (h *openCodeHarness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return GetBootstrap(ctx)
}

type resumeStateData struct {
	OpenCodeSessionID            string            `json:"openCodeSessionId,omitempty"`
	Bridge                       *bridgeCoords     `json:"bridge,omitempty"`
	SandboxCredentialEnvironment map[string]string `json:"sandboxCredentialEnvironment,omitempty"`
}

type bridgeCoords struct {
	Port            int     `json:"port"`
	Token           string  `json:"token"`
	LastSeenEventID float64 `json:"lastSeenEventId"`
	SandboxID       string  `json:"sandboxId,omitempty"`
}

func (h *openCodeHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var v resumeStateData
	return json.Unmarshal(data, &v)
}

func unsupported(capability string) error {
	return harness.NewCapabilityUnsupportedError(
		fmt.Sprintf("Harness 'opencode' does not support %s yet.", capability), HarnessID, nil)
}

func (h *openCodeHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
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
			"The OpenCode harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.", HarnessID, nil)
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
	resumeSessionID := resumeData.OpenCodeSessionID
	coords := resumeData.Bridge

	processEnv := harnessutil.ProcessEnv()
	authenticationMode := resolveAuthenticationMode(settings.Auth, "", settings.Provider, processEnv)
	resolvedAuthEnvironment := resolveEnv(settings.Auth, "", settings.Provider, processEnv)
	resolvedOpenCodeConfig := settings.OpenCodeConfig
	sandboxAuthEnvironment := resolvedAuthEnvironment
	var sandboxCredentialEnvironment map[string]string
	credentialsBrokered := false

	if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
		if resumeData.SandboxCredentialEnvironment != nil {
			sandboxCredentialEnvironment = resumeData.SandboxCredentialEnvironment
		} else {
			var err error
			sandboxCredentialEnvironment, err = harnessutil.CreateSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
				Environment: resolvedAuthEnvironment, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
				CredentialForwarding: settings.CredentialForwarding,
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
		// Native (adapter-brokered) subscription auth is not ported (see
		// package doc); this always takes the plain credential-forwarding
		// path TS takes when `authentication.subscription == null`.
		transformations, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
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

	sandboxHomeDir, err := harnessutil.ResolveSandboxHomeDir(ctx, toolSafeSandboxSession)
	if err != nil {
		return nil, err
	}
	stateDir := harness.StateDirectoryPath(sandboxHomeDir)
	bootstrapDir := stateDir + "/" + BootstrapDir
	workDir := opts.SessionWorkDir
	skillsDir := sandboxHomeDir + "/.agents/skills"
	sessionDataDir := harness.SessionDataDirectoryPath(stateDir, opts.SessionID)
	bridgeStateDir := sessionDataDir + "/bridge"
	timeout := settings.StartupTimeout
	if timeout <= 0 {
		timeout = bridge.DefaultStartupTimeout
	}
	helloTimeout := timeout
	if helloTimeout > 5*time.Second {
		helloTimeout = 5 * time.Second
	}
	onBridgeErr := func(*harness.ErrorPart) {}

	sessionArgs := sessionParams{
		sessionID: opts.SessionID, provider: settings.Provider, reasoningVariant: settings.ReasoningVariant,
		openCodeConfig: resolvedOpenCodeConfig, mcpServers: settings.MCPServers, headers: opts.Headers,
		openCodeSessionID: resumeSessionID, sandboxID: sandboxID, sandboxCredentialEnvironment: sandboxCredentialEnvironment,
		debug: debugConfig(opts.Observability), permissionMode: opts.PermissionMode, builtinToolFiltering: opts.BuiltinToolFiltering,
		sandbox: toolSafeSandboxSession, sandboxHomeDir: sandboxHomeDir, reconnect: settings.Reconnect,
	}

	if coords != nil {
		if sess, ok := attachToRunningBridge(ctx, sandboxSession, settings, coords, isContinue, resumeSessionID, helloTimeout, onBridgeErr, sessionArgs); ok {
			return sess, nil
		}
	}

	var respawnStrategy string // "" | "replay" | "rerun"
	if isResume {
		respawnStrategy = "rerun"
	}
	if coords != nil && isContinue {
		log, _ := toolSafeSandboxSession.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: bridgeStateDir + "/event-log.ndjson"})
		if harnessutil.ClassifyDiskLog(log) == harnessutil.DiskLogReplay {
			respawnStrategy = "replay"
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
		if k == SubscriptionAccessTokenEnvironmentVariable {
			continue
		}
		env[k] = v
	}
	env["AI_SDK_HARNESS_CLIENT_APP"] = clientApp

	if respawnStrategy == "" {
		if _, err := toolSafeSandboxSession.Run(ctx, providerutils.SandboxProcessOptions{
			Command: "mkdir -p " + harnessutil.ShellQuote(workDir) + " " + harnessutil.ShellQuote(bridgeStateDir),
		}); err != nil {
			return nil, err
		}
	}

	command := "node " + harnessutil.ShellQuote(bootstrapDir+"/bridge.mjs") +
		" --workdir " + harnessutil.ShellQuote(workDir) +
		" --bridge-state-dir " + harnessutil.ShellQuote(bridgeStateDir) +
		" --bootstrap-dir " + harnessutil.ShellQuote(bootstrapDir) +
		" --skills-dir " + harnessutil.ShellQuote(skillsDir)

	launched, err := bridge.Launch(ctx, bridge.LaunchOptions{
		Label: "OpenCode bridge", Source: HarnessID, Sandbox: toolSafeSandboxSession,
		Command: command, Env: env, Port: port, Token: token, ReplayFromDisk: respawnStrategy == "replay",
		BridgeStateDir: bridgeStateDir, BridgeType: HarnessID, StartupTimeout: timeout,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return resolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort)
		},
	})
	if err != nil {
		return nil, err
	}

	var supportsUserMessageResponses bool
	channelOpts := bridge.ChannelOptions{
		Connect: bridge.NewConnectFunc(launched.Endpoint, bridge.DialOptions{
			Name: "OpenCode bridge", WaitForHello: true, HelloTimeout: helloTimeout,
			OnHello: func(h *bridge.Hello) {
				if h.Capabilities != nil && h.Capabilities.ExperimentalUserMessageResponses != nil {
					supportsUserMessageResponses = *h.Capabilities.ExperimentalUserMessageResponses
				}
			},
		}),
		Reconnect: settings.Reconnect, OnBridgeError: onBridgeErr,
	}
	if respawnStrategy == "replay" && coords != nil {
		channelOpts.InitialLastSeenEventID = coords.LastSeenEventID
	}
	channel := bridge.NewChannel(channelOpts)
	if err := channel.Open(ctx, respawnStrategy == "replay"); err != nil {
		return nil, err
	}

	sessionArgs.channel = channel
	sessionArgs.proc = launched.Proc
	sessionArgs.bridgePort = port
	sessionArgs.bridgeToken = token
	sessionArgs.isResume = respawnStrategy != ""
	sessionArgs.seedResumeSessionOnFirstPrompt = respawnStrategy != ""
	sessionArgs.rerunContinue = respawnStrategy == "rerun"
	sessionArgs.supportsUserMessageResponses = func() bool { return supportsUserMessageResponses }
	return newSession(sessionArgs), nil
}

func debugConfig(o *harness.Observability) *harness.DebugConfig {
	if o == nil {
		return nil
	}
	return o.Debug
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
			"The OpenCode harness requires an explicit `port` when using a basic sandbox session.", HarnessID, nil)
	}
	if portEndpoint == nil {
		return harness.NewCapabilityUnsupportedError(
			"The OpenCode harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
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
		"The OpenCode harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
}

func wrapPortError(err error) error {
	if err == bridge.ErrNoPort {
		return harness.NewCapabilityUnsupportedError(
			"The OpenCode harness needs a TCP port exposed by the sandbox. "+
				"Create the sandbox with `ports: [<port>]` or pass `createOpenCode({ port })`.", HarnessID, err)
	}
	return err
}
