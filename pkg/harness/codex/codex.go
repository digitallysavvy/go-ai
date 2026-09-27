package codex

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// HarnessID is the harness-v1 adapter identifier. Mirrors TS `harnessId:
// 'codex'`.
const HarnessID = "codex"

// version is used in the client-app attribution string, mirroring TS
// `VERSION` pinned to the ai@7.0.113 release this port targets.
const version = "1.0.125"

// cliShimFilename mirrors TS `CLI_SHIM_FILENAME` (harness-codex/src/bridge/cli-relay.ts).
const cliShimFilename = "harness-tool.mjs"

var _ harness.Harness = (*Harness)(nil)
var _ harness.BootstrapProvider = (*Harness)(nil)
var _ harness.LifecycleStateValidator = (*Harness)(nil)

// Harness is the codex harness-v1 adapter.
type Harness struct {
	settings Settings
	tools    map[string]harness.BuiltinTool
}

// New builds the codex harness. Mirrors TS `createCodex`.
func New(settings Settings) *Harness {
	return &Harness{settings: settings, tools: builtinTools()}
}

// SpecificationVersion returns "harness-v1".
func (h *Harness) SpecificationVersion() string { return harness.SpecificationVersion }

// HarnessID returns "codex".
func (h *Harness) HarnessID() string { return HarnessID }

// BuiltinTools returns Codex's model-callable built-in tools.
func (h *Harness) BuiltinTools() map[string]harness.BuiltinTool { return h.tools }

// GetBootstrap returns the embedded bridge bootstrap recipe.
func (h *Harness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return GetBootstrap(ctx)
}

// resumeStateData is the adapter-specific lifecycle `data` payload. Mirrors
// TS `codexResumeStateSchema`.
type resumeStateData struct {
	ThreadID                     string            `json:"threadId,omitempty"`
	TurnConfigurationFingerprint string            `json:"turnConfigurationFingerprint,omitempty"`
	Bridge                       *bridgeCoords     `json:"bridge,omitempty"`
	SandboxCredentialEnvironment map[string]string `json:"sandboxCredentialEnvironment,omitempty"`
}

type bridgeCoords struct {
	Port            int     `json:"port"`
	Token           string  `json:"token"`
	LastSeenEventID float64 `json:"lastSeenEventId"`
	SandboxID       string  `json:"sandboxId,omitempty"`
}

// ValidateLifecycleStateData accepts any JSON object (TS `z.object`, all
// fields optional).
func (h *Harness) ValidateLifecycleStateData(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("codex: invalid lifecycle state data: %w", err)
	}
	return nil
}

// DoStart starts a fresh session or respawns the bridge for a resumed one.
// Mirrors TS `createCodex().doStart` (see the package doc for the deferred
// attach/replay rungs).
func (h *Harness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	settings := h.settings
	if opts.BuiltinToolFiltering != nil {
		return nil, harness.NewCapabilityUnsupportedError("Harness 'codex' does not support built-in tool filtering controls.", HarnessID, nil)
	}
	if opts.PermissionMode != "" && opts.PermissionMode != harness.PermissionModeAllowAll {
		return nil, harness.NewCapabilityUnsupportedError("Harness 'codex' does not support built-in tool approval requests; use permissionMode: 'allow-all'.", HarnessID, nil)
	}

	sandboxSession := opts.SandboxSession
	restricted := harnessutil.GetRestrictedSandboxSession(sandboxSession)
	networkSandbox, isNetwork := harness.AsNetworkSandboxSession(sandboxSession)
	var sandboxID string
	if isNetwork {
		sandboxID = networkSandbox.ID()
	}
	if !isNetwork {
		if settings.Port == 0 {
			return nil, harness.NewCapabilityUnsupportedError("The codex harness requires an explicit `port` when using a basic sandbox session.", HarnessID, nil)
		}
		if settings.PortEndpoint == nil {
			return nil, harness.NewCapabilityUnsupportedError("The codex harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
		}
	}
	if settings.MintBridgeToken != nil && sandboxID == "" {
		return nil, harness.NewCapabilityUnsupportedError("The codex harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.", HarnessID, nil)
	}

	var resumeData resumeStateData
	isResume := opts.ContinueFrom != nil || opts.ResumeFrom != nil
	if opts.ContinueFrom != nil {
		_ = json.Unmarshal(opts.ContinueFrom.Data, &resumeData)
	} else if opts.ResumeFrom != nil {
		_ = json.Unmarshal(opts.ResumeFrom.Data, &resumeData)
	}

	authMode := resolveAuthenticationMode(settings.Auth, harnessutil.ProcessEnv())
	resolvedAuthEnv := resolveAuthenticationEnvironment(settings.Auth, harnessutil.ProcessEnv())

	sandboxAuthEnv := resolvedAuthEnv
	var sandboxCredentialEnv map[string]string
	credentialsBrokered := false
	if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
		var err error
		sandboxCredentialEnv = resumeData.SandboxCredentialEnvironment
		if sandboxCredentialEnv == nil {
			sandboxCredentialEnv, err = harnessutil.CreateSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
				Environment: resolvedAuthEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
				CredentialForwarding: settings.CredentialForwarding,
			})
			if err != nil {
				return nil, err
			}
		}
		sandboxAuthEnv = map[string]string{}
		for k, v := range resolvedAuthEnv {
			sandboxAuthEnv[k] = v
		}
		for k, v := range sandboxCredentialEnv {
			sandboxAuthEnv[k] = v
		}
		transformations, err := requestTransformations(resolvedAuthEnv, sandboxAuthEnv, authMode)
		if err != nil {
			return nil, err
		}
		if len(transformations) > 0 {
			if err := adder.AddRequestTransformations(ctx, transformations); err != nil {
				return nil, err
			}
			if authMode == AuthModeDirect && resolvedAuthEnv["OPENAI_BASE_URL"] == "" {
				sandboxAuthEnv["OPENAI_BASE_URL"] = DefaultOpenAIBaseURL
			}
		}
		credentialsBrokered = true
	}

	sandboxHomeDir, err := harnessutil.ResolveSandboxHomeDir(ctx, restricted)
	if err != nil {
		return nil, err
	}
	stateDir := harness.StateDirectoryPath(sandboxHomeDir)
	bootstrapDir := posixpath.Resolve(stateDir, BootstrapDir)
	workDir := opts.SessionWorkDir
	sessionDataDir := harness.SessionDataDirectoryPath(stateDir, opts.SessionID)
	bridgeStateDir := sessionDataDir + "/bridge"
	cliShimDir := sessionDataDir + "/codex"
	cliShimPath := cliShimDir + "/" + cliShimFilename
	timeout := settings.StartupTimeout
	if timeout <= 0 {
		timeout = bridge.DefaultStartupTimeout
	}

	if err := mkdirs(ctx, restricted, workDir, bridgeStateDir); err != nil {
		return nil, err
	}

	port, err := bridge.ResolveBridgePort(sandboxSession, settings.Port)
	if err != nil {
		return nil, harness.NewCapabilityUnsupportedError(
			"The codex harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or pass `New(Settings{Port: ...})`.", HarnessID, err)
	}
	token := bridge.CreateBridgeToken()
	if settings.MintBridgeToken != nil {
		token = settings.MintBridgeToken(sandboxID)
	}

	forwardedAuthEnv := sandboxAuthEnv
	if !credentialsBrokered {
		forwardedAuthEnv, err = harnessutil.ApplyCredentialForwarding(ctx, harnessutil.CredentialForwardingOptions{
			Environment: sandboxAuthEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
			CredentialForwarding: settings.CredentialForwarding,
		})
		if err != nil {
			return nil, err
		}
		harnessutil.WarnCredentialBrokeringUnavailable(harnessutil.WarnCredentialBrokeringUnavailableOptions{
			Environment: resolvedAuthEnv, ForwardedEnvironment: forwardedAuthEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
		})
	}
	env := map[string]string{}
	for k, v := range forwardedAuthEnv {
		env[k] = v
	}
	env["AI_SDK_HARNESS_CLIENT_APP"] = harnessutil.ClientApp(HarnessID, version)

	launched, err := bridge.Launch(ctx, bridge.LaunchOptions{
		Label: "codex bridge", Source: HarnessID, Sandbox: restricted,
		Command: fmt.Sprintf("node %s/bridge.mjs --workdir %s --bridge-state-dir %s --cli-shim-dir %s",
			harnessutil.ShellQuote(bootstrapDir), harnessutil.ShellQuote(workDir), harnessutil.ShellQuote(bridgeStateDir), harnessutil.ShellQuote(cliShimDir)),
		Env: env, Port: port, Token: token, BridgeStateDir: bridgeStateDir, BridgeType: HarnessID, StartupTimeout: timeout,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return bridge.ResolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort)
		},
	})
	if err != nil {
		return nil, err
	}

	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			return bridge.Dial(ctx, launched.Endpoint, bridge.DialOptions{Name: "codex bridge"})
		},
		Reconnect: settings.Reconnect,
	})
	if err := channel.Open(ctx, false); err != nil {
		return nil, err
	}

	return newSession(sessionOptions{
		sessionID: opts.SessionID, channel: channel, proc: launched.Proc, cliShimPath: cliShimPath,
		model: DefaultModel, reasoningEffort: settings.ReasoningEffort, webSearch: settings.WebSearch,
		codexConfig: settings.CodexConfig, mcpServers: settings.MCPServers, headers: opts.Headers,
		isResume: isResume, seedResumeThreadOnFirstPrompt: isResume, rerunContinue: isResume,
		resumeThreadID: resumeData.ThreadID,
		bridgePort:     port, bridgeToken: token, sandboxID: sandboxID,
		sandboxCredentialEnvironment: sandboxCredentialEnv,
		permissionMode:               opts.PermissionMode, sandbox: restricted, sandboxHomeDir: sandboxHomeDir,
		turnConfigurationFingerprint: resumeData.TurnConfigurationFingerprint,
	}), nil
}

func mkdirs(ctx context.Context, sandbox providerutils.SandboxSession, dirs ...string) error {
	for _, dir := range dirs {
		if _, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
			Command: "mkdir -p " + harnessutil.ShellQuote(dir),
		}); err != nil {
			return err
		}
	}
	return nil
}
