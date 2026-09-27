package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// HarnessID is the harness-v1 adapter identifier. Mirrors TS `harnessId:
// 'claude-code'`.
const HarnessID = "claude-code"

var _ harness.Harness = (*Harness)(nil)
var _ harness.BootstrapProvider = (*Harness)(nil)
var _ harness.BuiltinToolApprovalSupport = (*Harness)(nil)
var _ harness.BuiltinToolFilteringSupport = (*Harness)(nil)
var _ harness.LifecycleStateValidator = (*Harness)(nil)

// Harness is the claude-code harness-v1 adapter.
type Harness struct {
	settings Settings
	tools    map[string]harness.BuiltinTool
}

// New builds the claude-code harness. Mirrors TS `createClaudeCode`.
func New(settings Settings) (*Harness, error) {
	if settings.MCPServers != nil {
		if _, reserved := settings.MCPServers["harness-tools"]; reserved {
			return nil, errors.New(`Claude Code MCP server name "harness-tools" is reserved for HarnessAgent tools.`)
		}
	}
	if settings.Thinking == nil {
		settings.Thinking = &ThinkingConfig{Type: "adaptive", Display: "summarized"}
	}
	return &Harness{settings: settings, tools: builtinTools()}, nil
}

// SpecificationVersion returns "harness-v1".
func (h *Harness) SpecificationVersion() string { return harness.SpecificationVersion }

// HarnessID returns "claude-code".
func (h *Harness) HarnessID() string { return HarnessID }

// BuiltinTools returns the complete Claude Code built-in tool table.
func (h *Harness) BuiltinTools() map[string]harness.BuiltinTool { return h.tools }

// SupportsBuiltinToolApprovals returns true.
func (h *Harness) SupportsBuiltinToolApprovals() bool { return true }

// SupportsBuiltinToolFiltering returns true.
func (h *Harness) SupportsBuiltinToolFiltering() bool { return true }

// GetBootstrap returns the embedded bridge bootstrap recipe.
func (h *Harness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return GetBootstrap(ctx)
}

// resumeStateData is the adapter-specific lifecycle `data` payload. Mirrors TS
// `claudeCodeResumeStateSchema` (a loose object: unknown keys are preserved by
// json.RawMessage round-tripping at the harness.ResumeSessionState/
// ContinueTurnState level, so ValidateLifecycleStateData only checks shape).
type resumeStateData struct {
	Bridge                       *bridgeCoords     `json:"bridge,omitempty"`
	SandboxCredentialEnvironment map[string]string `json:"sandboxCredentialEnvironment,omitempty"`
	ClaudeSessionID              string            `json:"claudeSessionId,omitempty"`
}

type bridgeCoords struct {
	Port            int     `json:"port"`
	Token           string  `json:"token"`
	LastSeenEventID float64 `json:"lastSeenEventId"`
	SandboxID       string  `json:"sandboxId,omitempty"`
}

// ValidateLifecycleStateData accepts any JSON object or null (TS
// `z.looseObject`).
func (h *Harness) ValidateLifecycleStateData(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("claude-code: invalid lifecycle state data: %w", err)
	}
	return nil
}

// DoStart starts a fresh session or respawns the bridge for a resumed one.
// Mirrors TS `createClaudeCode().doStart` (see the package doc for the
// deferred attach/replay rungs).
func (h *Harness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	settings := h.settings
	sandboxSession := opts.SandboxSession
	restricted := harnessutil.GetRestrictedSandboxSession(sandboxSession)
	networkSandbox, isNetwork := harness.AsNetworkSandboxSession(sandboxSession)
	var sandboxID string
	if isNetwork {
		sandboxID = networkSandbox.ID()
	}
	if !isNetwork {
		if settings.Port == 0 {
			return nil, harness.NewCapabilityUnsupportedError(
				"The Claude Code harness requires an explicit `port` when using a basic sandbox session.", HarnessID, nil)
		}
		if settings.PortEndpoint == nil {
			return nil, harness.NewCapabilityUnsupportedError(
				"The Claude Code harness requires an explicit `portEndpoint` when using a basic sandbox session.", HarnessID, nil)
		}
	}
	if settings.MintBridgeToken != nil && sandboxID == "" {
		return nil, harness.NewCapabilityUnsupportedError(
			"The Claude Code harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.", HarnessID, nil)
	}

	var resumeData resumeStateData
	isResume := opts.ContinueFrom != nil || opts.ResumeFrom != nil
	if opts.ContinueFrom != nil {
		_ = json.Unmarshal(opts.ContinueFrom.Data, &resumeData)
	} else if opts.ResumeFrom != nil {
		_ = json.Unmarshal(opts.ResumeFrom.Data, &resumeData)
	}

	authMode := resolveAuthenticationMode(settings.Auth, harnessutil.ProcessEnv())
	authEnv := resolveAuthenticationEnvironment(ctx, settings.Auth, harnessutil.ProcessEnv())
	claudeEnv := map[string]string{}
	for k, v := range authEnv {
		claudeEnv[k] = v
	}
	if authEnv["AI_GATEWAY_BASE_URL"] != "" {
		claudeEnv["CLAUDE_AGENT_SDK_CLIENT_APP"] = harnessutil.ClientApp(HarnessID, version)
	}
	for k, v := range settings.Env {
		claudeEnv[k] = v
	}
	if len(opts.Headers) > 0 {
		var lines []string
		for name, value := range opts.Headers {
			lines = append(lines, fmt.Sprintf("%s: %s", name, value))
		}
		claudeEnv["ANTHROPIC_CUSTOM_HEADERS"] = joinLines(lines)
	}

	sandboxClaudeEnv := claudeEnv
	var sandboxCredentialEnv map[string]string
	if adder, ok := sandboxSession.(harness.RequestTransformationAdder); ok {
		var err error
		sandboxCredentialEnv = resumeData.SandboxCredentialEnvironment
		if sandboxCredentialEnv == nil {
			sandboxCredentialEnv, err = harnessutil.CreateSandboxCredentialEnvironment(ctx, harnessutil.CredentialForwardingOptions{
				Environment: claudeEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
				CredentialForwarding: settings.CredentialForwarding,
			})
			if err != nil {
				return nil, err
			}
		}
		sandboxClaudeEnv = map[string]string{}
		for k, v := range claudeEnv {
			sandboxClaudeEnv[k] = v
		}
		for k, v := range sandboxCredentialEnv {
			sandboxClaudeEnv[k] = v
		}
		transformations, err := requestTransformations(claudeEnv, sandboxClaudeEnv, authMode)
		if err != nil {
			return nil, err
		}
		if len(transformations) > 0 {
			if err := adder.AddRequestTransformations(ctx, transformations); err != nil {
				return nil, err
			}
		}
	} else {
		var err error
		sandboxClaudeEnv, err = harnessutil.ApplyCredentialForwarding(ctx, harnessutil.CredentialForwardingOptions{
			Environment: sandboxClaudeEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
			CredentialForwarding: settings.CredentialForwarding,
		})
		if err != nil {
			return nil, err
		}
		harnessutil.WarnCredentialBrokeringUnavailable(harnessutil.WarnCredentialBrokeringUnavailableOptions{
			Environment: claudeEnv, ForwardedEnvironment: sandboxClaudeEnv, CredentialEnvironmentVariables: CredentialEnvironmentVariables,
		})
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

	// Rung 1 — ATTACH. When lifecycle state carries live bridge coordinates,
	// try to reopen a socket to the still-running bridge instead of
	// respawning. No spawn, no fresh token (the existing bridge still
	// authorizes the persisted one). A continued (suspended) turn requests
	// replay of everything past the persisted cursor; a resumed (parked)
	// session just attaches and waits for the next start. If the bridge is
	// gone the open fails and DoStart falls through to a spawn-based
	// recovery. Mirrors TS `createClaudeCode().doStart` rung 1.
	coords := resumeData.Bridge
	if coords != nil {
		if sess := h.tryAttach(ctx, attachOptions{
			coords: coords, sandboxSession: sandboxSession, settings: settings, timeout: timeout,
			sessionID: opts.SessionID, isContinue: opts.ContinueFrom != nil,
			maxTurns: settings.MaxTurns, env: sandboxClaudeEnv, thinking: *settings.Thinking, effort: settings.Effort,
			resumeSessionID:              resumeData.ClaudeSessionID,
			sandboxCredentialEnvironment: sandboxCredentialEnv,
			permissionMode:               opts.PermissionMode, builtinToolFiltering: opts.BuiltinToolFiltering,
			mcpServers: settings.MCPServers, sandbox: restricted, sandboxHomeDir: sandboxHomeDir,
		}); sess != nil {
			return sess, nil
		}
	}

	// Rungs 2/3 — REPLAY vs RERUN. Respawn the bridge. `replay` is only
	// sound for a continued (suspended) turn: those coordinates carry the
	// cursor the on-disk log is replayed *from*. A resumed (parked) session
	// always reruns — even carrying bridge coordinates, replaying the
	// previous turn would re-deliver stale events into the next one; the CLI
	// continues its own thread from the workdir snapshot via
	// resumeSessionId/continue instead. Mirrors TS `doStart` rungs 2/3.
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
			"The claude-code harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or pass `New(Settings{Port: ...})`.", HarnessID, err)
	}
	token := bridge.CreateBridgeToken()
	if settings.MintBridgeToken != nil {
		token = settings.MintBridgeToken(sandboxID)
	}

	launched, err := bridge.Launch(ctx, bridge.LaunchOptions{
		Label: "claude-code bridge", Source: HarnessID, Sandbox: restricted,
		Command:        fmt.Sprintf("node %s/bridge.mjs --workdir %s --bridge-state-dir %s", harnessutil.ShellQuote(bootstrapDir), harnessutil.ShellQuote(workDir), harnessutil.ShellQuote(bridgeStateDir)),
		Port:           port,
		Token:          token,
		ReplayFromDisk: respawnStrategy == "replay",
		BridgeStateDir: bridgeStateDir,
		BridgeType:     HarnessID,
		StartupTimeout: timeout,
		ResolveEndpoint: func(ctx context.Context, boundPort int) (harness.PortEndpoint, error) {
			return bridge.ResolveBridgeEndpoint(ctx, sandboxSession, settings.PortEndpoint, boundPort)
		},
	})
	if err != nil {
		return nil, err
	}

	var supportsUserMessages atomic.Bool
	channelOpts := bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			return bridge.OpenBridgeWebSocket(ctx, launched.Endpoint, bridge.OpenOptions{
				Name: "claude-code bridge", Timeout: timeout,
				OnHello: func(h *bridge.Hello) {
					if h.SupportsUserMessageResponses() {
						supportsUserMessages.Store(true)
					}
				},
			})
		},
		Reconnect: settings.Reconnect,
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
		maxTurns: settings.MaxTurns, env: sandboxClaudeEnv, thinking: *settings.Thinking, effort: settings.Effort,
		isResume: isResume, continueOnFirstPrompt: isResume, rerunContinue: isResume,
		resumeSessionID: resumeData.ClaudeSessionID,
		bridgePort:      port, bridgeToken: token, sandboxID: sandboxID,
		sandboxCredentialEnvironment: sandboxCredentialEnv,
		permissionMode:               opts.PermissionMode, builtinToolFiltering: opts.BuiltinToolFiltering,
		mcpServers: settings.MCPServers, sandbox: restricted, sandboxHomeDir: sandboxHomeDir,
		supportsUserMessages: supportsUserMessages.Load(),
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

	maxTurns                     int
	env                          map[string]string
	thinking                     ThinkingConfig
	effort                       string
	resumeSessionID              string
	sandboxCredentialEnvironment map[string]string
	permissionMode               harness.PermissionMode
	builtinToolFiltering         *harness.BuiltinToolFiltering
	mcpServers                   map[string]any
	sandbox                      providerutils.SandboxSession
	sandboxHomeDir               string
}

// tryAttach reopens a socket to a still-running bridge using persisted
// coordinates, reusing the existing token (no new mint) and reusing the
// existing process (proc: nil — DoDestroy/DoStop then only close the
// channel). Returns nil when the bridge is unreachable, so the caller falls
// through to a spawn-based recovery. Mirrors TS rung 1 of `doStart`.
func (h *Harness) tryAttach(ctx context.Context, opts attachOptions) *session {
	endpoint, err := bridge.ResolveBridgeEndpoint(ctx, opts.sandboxSession, opts.settings.PortEndpoint, opts.coords.Port)
	if err != nil {
		return nil
	}
	attachEndpoint, err := bridge.WithBridgeToken(endpoint, opts.coords.Token)
	if err != nil {
		return nil
	}
	var supportsUserMessages atomic.Bool
	channel := bridge.NewChannel(bridge.ChannelOptions{
		Connect: func(ctx context.Context) (bridge.Conn, error) {
			return bridge.OpenBridgeWebSocket(ctx, attachEndpoint, bridge.OpenOptions{
				Name: "claude-code bridge", Timeout: opts.timeout,
				OnHello: func(h *bridge.Hello) {
					if h.SupportsUserMessageResponses() {
						supportsUserMessages.Store(true)
					}
				},
			})
		},
		Reconnect:              opts.settings.Reconnect,
		InitialLastSeenEventID: opts.coords.LastSeenEventID,
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
		maxTurns: opts.maxTurns, env: opts.env, thinking: opts.thinking, effort: opts.effort,
		isResume: true, continueOnFirstPrompt: false, rerunContinue: false,
		resumeSessionID: opts.resumeSessionID,
		bridgePort:      opts.coords.Port, bridgeToken: opts.coords.Token, sandboxID: opts.coords.SandboxID,
		sandboxCredentialEnvironment: opts.sandboxCredentialEnvironment,
		supportsUserMessages:         supportsUserMessages.Load(),
		permissionMode:               opts.permissionMode, builtinToolFiltering: opts.builtinToolFiltering,
		mcpServers: opts.mcpServers, sandbox: opts.sandbox, sandboxHomeDir: opts.sandboxHomeDir,
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

func joinLines(lines []string) string { return strings.Join(lines, "\n") }
