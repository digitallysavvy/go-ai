package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
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
		return nil, errors.New(`OpenCode MCP server name "harness-tools" is reserved for HarnessAgent tools.`) //nolint:staticcheck // matches TS SDK exact error text
	}
	return &openCodeHarness{settings: s}, nil
}

func (h *openCodeHarness) SpecificationVersion() string                 { return harness.SpecificationVersion }
func (h *openCodeHarness) HarnessID() string                            { return HarnessID }
func (h *openCodeHarness) SupportsBuiltinToolApprovals() bool           { return true }
func (h *openCodeHarness) SupportsBuiltinToolFiltering() bool           { return true }
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
	return harnessutil.ValidateLifecycleStateObject(data, &resumeStateData{})
}

func (h *openCodeHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	settings := h.settings
	if err := validateBuiltinToolFiltering(opts.BuiltinToolFiltering); err != nil {
		return nil, err
	}
	sandboxSession := opts.SandboxSession
	toolSafeSandboxSession := harness.GetRestrictedSandboxSession(sandboxSession)
	networkSession, isNetwork := harness.AsNetworkSandboxSession(sandboxSession)
	var sandboxID string
	if isNetwork {
		sandboxID = networkSession.ID()
	}
	if err := bridge.ValidateBridgeSandboxSettings(sandboxSession, sandboxID, settings.Port, settings.PortEndpoint, settings.MintBridgeToken, HarnessID); err != nil {
		return nil, err
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
	resolved, err := resolveOpenCodeAuthentication(ctx, settings.Auth, "", settings.Provider, processEnv, nil)
	if err != nil {
		return nil, err
	}
	authenticationMode := resolved.AuthenticationMode
	resolvedAuthEnvironment := resolved.Environment
	resolvedOpenCodeConfig := settings.OpenCodeConfig
	var transformModel func(string) (string, error)
	gitLabSubscriptionBrokered := false
	sandboxAuthEnvironment := resolvedAuthEnvironment
	var sandboxCredentialEnvironment map[string]string
	credentialsBrokered := false

	if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
		sandboxCredentialEnvironment, err = harnessutil.ResolveSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
			Environment: resolvedAuthEnvironment, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
			CredentialForwarding:                 settings.CredentialForwarding,
			PreviousSandboxCredentialEnvironment: resumeData.SandboxCredentialEnvironment,
		})
		if err != nil {
			return nil, err
		}
		merged := map[string]string{}
		for k, v := range resolvedAuthEnvironment {
			merged[k] = v
		}
		for k, v := range sandboxCredentialEnvironment {
			merged[k] = v
		}
		sandboxAuthEnvironment = merged

		sandboxSubscriptionAccessToken := sandboxAuthEnvironment[SubscriptionAccessTokenEnvironmentVariable]
		var transformations []harness.RequestTransformation
		if resolved.Subscription != nil && resolved.Subscription.ProviderID == SubscriptionGitLab && sandboxSubscriptionAccessToken != "" {
			directAccess, err := requestOpenCodeGitLabDirectAccess(ctx, nil,
				resolved.Subscription.AccessToken, resolved.Subscription.EnterpriseURL, processEnv["GITLAB_AI_GATEWAY_URL"])
			if err != nil {
				return nil, err
			}
			transformations, err = createOpenCodeGitLabSubscriptionRequestTransformations(directAccess, sandboxSubscriptionAccessToken)
			if err != nil {
				return nil, err
			}
			resolvedOpenCodeConfig = createOpenCodeGitLabSubscriptionConfig(settings.OpenCodeConfig, sandboxSubscriptionAccessToken, directAccess.AIGatewayURL, opts.Headers)
			provider := settings.Provider
			transformModel = func(model string) (string, error) {
				return resolveOpenCodeGitLabSubscriptionModel(model, provider)
			}
			gitLabSubscriptionBrokered = true
		} else if resolved.Subscription == nil || sandboxSubscriptionAccessToken == "" {
			transformations, err = createOpenCodeRequestTransformations(createRequestTransformationsInput{
				Env: resolvedAuthEnvironment, SandboxEnv: sandboxAuthEnvironment, Auth: authenticationMode,
			})
			if err != nil {
				return nil, err
			}
		} else {
			transformations, err = createOpenCodeSubscriptionRequestTransformations(resolved.Subscription, sandboxSubscriptionAccessToken)
			if err != nil {
				return nil, err
			}
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
	onBridgeErr := bridge.CreateBridgeErrorHandler(HarnessID, opts.SessionID)
	var onDiagnostic func(bridge.OutboundMessage)
	if opts.Observability != nil {
		onDiagnostic = bridge.ReportDiagnostic(opts.Observability.Report, opts.SessionID)
	}

	sessionArgs := sessionParams{
		sessionID: opts.SessionID, provider: settings.Provider, reasoningVariant: settings.ReasoningVariant,
		openCodeConfig: resolvedOpenCodeConfig, mcpServers: settings.MCPServers, headers: opts.Headers,
		openCodeSessionID: resumeSessionID, sandboxID: sandboxID, sandboxCredentialEnvironment: sandboxCredentialEnvironment,
		debug: debugConfig(opts.Observability), permissionMode: opts.PermissionMode, builtinToolFiltering: opts.BuiltinToolFiltering,
		sandbox: toolSafeSandboxSession, sandboxHomeDir: sandboxHomeDir, reconnect: settings.Reconnect,
		transformModel: transformModel,
	}

	if coords != nil {
		if sess, ok := attachToRunningBridge(ctx, sandboxSession, settings, coords, isContinue, resumeSessionID, helloTimeout, onBridgeErr, onDiagnostic, sessionArgs); ok {
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

	port, err := bridge.ResolveBridgePortOrUnsupported(sandboxSession, settings.Port, HarnessID)
	if err != nil {
		return nil, err
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
	subscriptionAccessToken := forwardedAuthEnvironment[SubscriptionAccessTokenEnvironmentVariable]
	env := map[string]string{}
	for k, v := range forwardedAuthEnvironment {
		if k == SubscriptionAccessTokenEnvironmentVariable {
			continue
		}
		env[k] = v
	}
	if !gitLabSubscriptionBrokered && resolved.Subscription != nil && subscriptionAccessToken != "" {
		content, err := createOpenCodeSubscriptionAuthContent(resolved.Subscription, subscriptionAccessToken)
		if err != nil {
			return nil, err
		}
		env["OPENCODE_AUTH_CONTENT"] = content
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
			return bridge.ResolveBridgeEndpointOrUnsupported(ctx, sandboxSession, settings.PortEndpoint, boundPort, HarnessID)
		},
	})
	if err != nil {
		return nil, err
	}

	var supportsUserMessageResponses atomic.Bool
	channelOpts := bridge.ChannelOptions{
		Connect: bridge.NewBridgeConnectFunc(launched.Endpoint, bridge.BridgeConnectOptions{
			Name: "OpenCode bridge", OpenRetryTimeout: timeout, StartContext: ctx,
			OnHello: func(h *bridge.Hello) {
				if h.Capabilities != nil && h.Capabilities.ExperimentalUserMessageResponses != nil {
					supportsUserMessageResponses.Store(*h.Capabilities.ExperimentalUserMessageResponses)
				}
			},
		}),
		Reconnect: settings.Reconnect, OnBridgeError: onBridgeErr, OnDiagnostic: onDiagnostic,
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
	sessionArgs.bridgePort = launched.Port
	sessionArgs.bridgeToken = token
	sessionArgs.isResume = respawnStrategy != ""
	sessionArgs.seedResumeSessionOnFirstPrompt = respawnStrategy != ""
	sessionArgs.rerunContinue = respawnStrategy == "rerun"
	sessionArgs.supportsUserMessageResponses = supportsUserMessageResponses.Load
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

// validateBuiltinToolFiltering mirrors TS `validateBuiltinToolFiltering`
// (TS #23342): OpenCode implements `write` and `edit` through one native
// permission, so they must be both active or both inactive.
func validateBuiltinToolFiltering(filtering *harness.BuiltinToolFiltering) error {
	if filtering == nil {
		return nil
	}
	if harness.IsBuiltinToolIncluded("write", filtering) != harness.IsBuiltinToolIncluded("edit", filtering) {
		return harness.NewCapabilityUnsupportedError(
			"OpenCode built-in filtering requires 'write' and 'edit' to either both be active or both be inactive.", HarnessID, nil)
	}
	return nil
}

// resolveInactiveBuiltinToolNames mirrors TS `resolveInactiveBuiltinToolNames`.
func resolveInactiveBuiltinToolNames(filtering *harness.BuiltinToolFiltering) *[]string {
	if filtering == nil {
		return nil
	}
	inactive := []string{}
	for _, name := range builtinToolNames {
		if !harness.IsBuiltinToolIncluded(name, filtering) {
			inactive = append(inactive, name)
		}
	}
	return &inactive
}
