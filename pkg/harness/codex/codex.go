package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
// `VERSION` pinned to the ai@7.0.118 release this port targets.
const version = "1.0.130"

var _ harness.Harness = (*Harness)(nil)
var _ harness.BootstrapProvider = (*Harness)(nil)
var _ harness.LifecycleStateValidator = (*Harness)(nil)
var _ harness.BuiltinToolFilteringSupport = (*Harness)(nil)

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

// SupportsBuiltinToolFiltering returns true. Mirrors TS
// `createCodex().supportsBuiltinToolFiltering = true`: Codex app-server
// filters bash/webSearch/view_image via its `features` config and
// apply_patch via a trusted hook (see codex-tool-filtering{,-hook}.ts,
// bridge-internal — no host-side gating is needed beyond forwarding the
// caller's HarnessV1BuiltinToolFiltering on the `start` frame).
func (h *Harness) SupportsBuiltinToolFiltering() bool { return true }

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
	resolvedAuthEnv := resolveAuthenticationEnvironment(ctx, settings.Auth, harnessutil.ProcessEnv())

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
	timeout := settings.StartupTimeout
	if timeout <= 0 {
		timeout = bridge.DefaultStartupTimeout
	}

	if err := mkdirs(ctx, restricted, workDir, bridgeStateDir); err != nil {
		return nil, err
	}

	// Normalize each forwarded bridge diagnostics frame into the general
	// harness.Diagnostic and report it. Mirrors TS `codex-harness.ts`
	// `onDiagnostic` (report ? frame => report(harnessV1DiagnosticFromBridgeFrame(...)) : undefined).
	var onDiagnostic func(bridge.OutboundMessage)
	if opts.Observability != nil {
		onDiagnostic = bridge.ReportDiagnostic(opts.Observability.Report, opts.SessionID)
	}

	// Rung 1 — ATTACH. When lifecycle state carries live bridge coordinates,
	// try to reopen a socket to the still-running bridge instead of
	// respawning. No spawn, no fresh token. A continued (suspended) turn
	// requests replay of everything past the persisted cursor; a resumed
	// (parked) session just attaches and waits for the next start. If the
	// bridge is gone the open fails and DoStart falls through to a
	// spawn-based recovery. Mirrors TS `createCodex().doStart` rung 1.
	coords := resumeData.Bridge
	if coords != nil {
		if sess := h.tryAttach(ctx, attachOptions{
			coords: coords, sandboxSession: sandboxSession, settings: settings, timeout: timeout,
			sessionID: opts.SessionID, isContinue: opts.ContinueFrom != nil,
			reasoningEffort: settings.ReasoningEffort, webSearch: settings.WebSearch,
			builtinToolFiltering: opts.BuiltinToolFiltering,
			codexConfig:          settings.CodexConfig, mcpServers: settings.MCPServers, headers: opts.Headers,
			resumeThreadID:               resumeData.ThreadID,
			sandboxCredentialEnvironment: sandboxCredentialEnv,
			permissionMode:               opts.PermissionMode, sandbox: restricted, sandboxHomeDir: sandboxHomeDir,
			turnConfigurationFingerprint: resumeData.TurnConfigurationFingerprint,
			onDiagnostic:                 onDiagnostic,
		}); sess != nil {
			return sess, nil
		}
	}

	// Rungs 2/3 — REPLAY vs RERUN. Respawn the bridge. `replay` is only
	// sound for a continued (suspended) turn; a resumed (parked) session
	// always reruns via `codex.resumeThread(threadId)`. Mirrors TS `doStart`
	// rungs 2/3.
	respawnStrategy := ""
	if isResume {
		respawnStrategy = "rerun"
	}
	if coords != nil && opts.ContinueFrom != nil {
		logText, _ := restricted.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: bridgeStateDir + "/event-log.ndjson"})
		if harnessutil.ClassifyDiskLog(logText) == harnessutil.DiskLogReplay {
			respawnStrategy = "replay"
		}
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
		Command: fmt.Sprintf("node %s/bridge.mjs --workdir %s --bridge-state-dir %s",
			harnessutil.ShellQuote(bootstrapDir), harnessutil.ShellQuote(workDir), harnessutil.ShellQuote(bridgeStateDir)),
		Env: env, Port: port, Token: token, ReplayFromDisk: respawnStrategy == "replay",
		BridgeStateDir: bridgeStateDir, BridgeType: HarnessID, StartupTimeout: timeout,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return bridge.ResolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort)
		},
	})
	if err != nil {
		return nil, err
	}

	channelOpts := bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			return bridge.Dial(ctx, launched.Endpoint, bridge.DialOptions{Name: "codex bridge"})
		},
		Reconnect:    settings.Reconnect,
		OnDiagnostic: onDiagnostic,
	}
	replaying := respawnStrategy == "replay"
	if replaying {
		channelOpts.InitialLastSeenEventID = coords.LastSeenEventID
	}
	channel := bridge.NewChannel(channelOpts)
	var finishAttachment func()
	if replaying {
		finishAttachment = channel.BeginListenerAttachment()
	}
	err = channel.Open(ctx, replaying)
	if finishAttachment != nil {
		finishAttachment()
	}
	if err != nil {
		return nil, err
	}

	return newSession(sessionOptions{
		sessionID: opts.SessionID, channel: channel, proc: launched.Proc,
		model: DefaultModel, reasoningEffort: settings.ReasoningEffort, webSearch: settings.WebSearch,
		builtinToolFiltering: opts.BuiltinToolFiltering,
		codexConfig:          settings.CodexConfig, mcpServers: settings.MCPServers, headers: opts.Headers,
		isResume: isResume, seedResumeThreadOnFirstPrompt: isResume, rerunContinue: isResume,
		resumeThreadID: resumeData.ThreadID,
		bridgePort:     port, bridgeToken: token, sandboxID: sandboxID,
		sandboxCredentialEnvironment: sandboxCredentialEnv,
		permissionMode:               opts.PermissionMode, sandbox: restricted, sandboxHomeDir: sandboxHomeDir,
		turnConfigurationFingerprint: resumeData.TurnConfigurationFingerprint,
	}), nil
}

// attachOptions is the input of tryAttach.
type attachOptions struct {
	coords         *bridgeCoords
	sandboxSession providerutils.SandboxSession
	settings       Settings
	timeout        time.Duration
	sessionID      string
	isContinue     bool

	reasoningEffort              string
	webSearch                    *bool
	builtinToolFiltering         *harness.BuiltinToolFiltering
	codexConfig                  map[string]any
	mcpServers                   map[string]any
	headers                      map[string]string
	resumeThreadID               string
	sandboxCredentialEnvironment map[string]string
	permissionMode               harness.PermissionMode
	sandbox                      providerutils.SandboxSession
	sandboxHomeDir               string
	turnConfigurationFingerprint string
	onDiagnostic                 func(bridge.OutboundMessage)
}

// tryAttach reopens a socket to a still-running bridge using persisted
// coordinates, reusing the existing token (no new mint) and reusing the
// existing process (proc: nil). Returns nil when the bridge is unreachable,
// so the caller falls through to a spawn-based recovery. Mirrors TS rung 1
// of `doStart`.
func (h *Harness) tryAttach(ctx context.Context, opts attachOptions) *session {
	endpoint, err := bridge.ResolveBridgeEndpoint(ctx, opts.sandboxSession, opts.settings.PortEndpoint, opts.coords.Port)
	if err != nil {
		return nil
	}
	attachEndpoint, err := bridge.WithBridgeToken(endpoint, opts.coords.Token)
	if err != nil {
		return nil
	}
	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			return bridge.Dial(ctx, attachEndpoint, bridge.DialOptions{Name: "codex bridge"})
		},
		Reconnect:              opts.settings.Reconnect,
		InitialLastSeenEventID: opts.coords.LastSeenEventID,
		OnDiagnostic:           opts.onDiagnostic,
	})
	var finishAttachment func()
	if opts.isContinue {
		finishAttachment = channel.BeginListenerAttachment()
	}
	err = channel.Open(ctx, opts.isContinue)
	if finishAttachment != nil {
		finishAttachment()
	}
	if err != nil {
		return nil
	}
	return newSession(sessionOptions{
		sessionID: opts.sessionID, channel: channel, proc: nil,
		model: DefaultModel, reasoningEffort: opts.reasoningEffort, webSearch: opts.webSearch,
		builtinToolFiltering: opts.builtinToolFiltering,
		codexConfig:          opts.codexConfig, mcpServers: opts.mcpServers, headers: opts.headers,
		isResume: true, seedResumeThreadOnFirstPrompt: false, rerunContinue: false,
		resumeThreadID: opts.resumeThreadID,
		bridgePort:     opts.coords.Port, bridgeToken: opts.coords.Token, sandboxID: opts.coords.SandboxID,
		sandboxCredentialEnvironment: opts.sandboxCredentialEnvironment,
		permissionMode:               opts.permissionMode, sandbox: opts.sandbox, sandboxHomeDir: opts.sandboxHomeDir,
		turnConfigurationFingerprint: opts.turnConfigurationFingerprint,
	})
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
