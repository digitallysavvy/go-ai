package bridge

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Environment variables read by the in-sandbox bridge (`runBridge`).
const (
	EnvChannelToken   = "BRIDGE_CHANNEL_TOKEN"
	EnvWSPort         = "BRIDGE_WS_PORT"
	EnvReplayFromDisk = "BRIDGE_REPLAY_FROM_DISK"
)

// DefaultStartupTimeout is the bridge startup budget used by every TS bridge
// adapter (`startupTimeoutMs ?? 120_000`).
const DefaultStartupTimeout = 120 * time.Second

// ErrNoPortEndpoint is returned by ResolveBridgeEndpoint for a basic sandbox
// session without an explicit endpoint; adapters wrap it in their own
// harness.CapabilityUnsupportedError message.
var ErrNoPortEndpoint = errors.New("harness bridge: the sandbox session exposes no port endpoint and no explicit portEndpoint was given")

// ErrNoPort is returned by ResolveBridgePort when neither an explicit port
// nor a sandbox-exposed port is available.
var ErrNoPort = errors.New("harness bridge: no TCP port exposed by the sandbox and no explicit port was given")

// ResolveBridgePort returns override when non-zero, else the first port the
// network sandbox exposes.
func ResolveBridgePort(session providerutils.SandboxSession, override int) (int, error) {
	if override != 0 {
		return override, nil
	}
	if n, ok := session.(harness.NetworkSandboxSession); ok {
		if ports := n.Ports(); len(ports) > 0 {
			return ports[0], nil
		}
	}
	return 0, ErrNoPort
}

// ResolveBridgeEndpoint returns override when set, else the network sandbox's
// `ws` endpoint for port.
func ResolveBridgeEndpoint(ctx context.Context, session providerutils.SandboxSession, override *harness.PortEndpoint, port int) (harness.PortEndpoint, error) {
	if override != nil {
		return *override, nil
	}
	if n, ok := session.(harness.NetworkSandboxSession); ok {
		return n.GetPortEndpoint(ctx, harness.PortEndpointOptions{Port: port, Protocol: harness.PortProtocolWS})
	}
	return harness.PortEndpoint{}, ErrNoPortEndpoint
}

// BridgeEnvironment returns the spawn environment every bridge reads: the
// channel token, the port and, for disk replay, BRIDGE_REPLAY_FROM_DISK=1.
func BridgeEnvironment(token string, port int, replayFromDisk bool) map[string]string {
	env := map[string]string{EnvChannelToken: token, EnvWSPort: strconv.Itoa(port)}
	if replayFromDisk {
		env[EnvReplayFromDisk] = "1"
	}
	return env
}

// LaunchOptions configures Launch.
type LaunchOptions struct {
	// Label prefixes startup errors, e.g. "claude-code bridge" ->
	// "claude-code bridge did not become ready in time.".
	Label string
	// Source labels forwarded stderr lines (`[harness:<source>:stderr]`),
	// normally the harness id.
	Source string
	// Sandbox spawns the bridge and serves bridge-meta.json (normally the
	// restricted, tool-safe view).
	Sandbox providerutils.SandboxSession
	// Command is the full spawn command (`node <dir>/bridge.mjs --workdir ...`).
	Command string
	// Env is merged under the bridge variables (token/port/replay win).
	Env map[string]string
	// Port is the port the bridge should bind (BRIDGE_WS_PORT).
	Port int
	// Token authorizes host connections (CreateBridgeToken or the adapter's
	// MintBridgeToken result).
	Token string
	// ReplayFromDisk asks a respawned bridge to reload its event log.
	ReplayFromDisk bool
	BridgeStateDir string
	BridgeType     string
	// StartupTimeout defaults to DefaultStartupTimeout.
	StartupTimeout time.Duration
	// PollInterval of the bridge-meta.json fallback.
	PollInterval time.Duration
	// ResolveEndpoint maps the bound port to an endpoint (see
	// ResolveBridgeEndpoint).
	ResolveEndpoint func(ctx context.Context, port int) (harness.PortEndpoint, error)
	// StderrWrite replaces the os.Stderr sink for forwarded stderr lines.
	StderrWrite func(line string)
}

// Launched is a started bridge process.
type Launched struct {
	Proc providerutils.SandboxProcess
	// Port is the port the bridge bound.
	Port int
	// Endpoint is the resolved endpoint with the token applied.
	Endpoint    harness.PortEndpoint
	ReadySource ReadySource
	StdoutTail  []string
	StderrTail  *LineTail
	// StderrDone is closed when the bridge's stderr ends.
	StderrDone <-chan struct{}
}

// Launch runs the host-side startup sequence shared by every TS bridge
// adapter: mark bridge-meta.json "starting", spawn the bridge with its token
// and port, forward stderr (keeping a tail), wait for readiness (stdout line
// or metadata fallback) with startup errors that carry the exit code and
// output tails, drain stdout, resolve the endpoint and apply the token.
// Callers then build a Channel with NewConnectFunc(launched.Endpoint, ...).
func Launch(ctx context.Context, opts LaunchOptions) (*Launched, error) {
	timeout := opts.StartupTimeout
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	label := opts.Label
	if label == "" {
		label = "bridge"
	}

	MarkBridgeStarting(ctx, opts.Sandbox, opts.BridgeStateDir, opts.BridgeType)

	env := map[string]string{}
	for k, v := range opts.Env {
		env[k] = v
	}
	for k, v := range BridgeEnvironment(opts.Token, opts.Port, opts.ReplayFromDisk) {
		env[k] = v
	}
	proc, err := opts.Sandbox.Spawn(ctx, providerutils.SandboxProcessOptions{Command: opts.Command, Env: env})
	if err != nil {
		return nil, err
	}

	stderrTail := NewLineTail(DefaultTailLimit)
	stderrDone := ForwardBridgeProcessStream(proc.Stderr(), ForwardOptions{
		StreamName: "stderr",
		Source:     opts.Source,
		Tail:       stderrTail,
		Write:      opts.StderrWrite,
	})
	startupError := func(message string) func(ReadyErrorContext) error {
		return func(rc ReadyErrorContext) error {
			return CreateBridgeStartupError(StartupErrorOptions{
				Message:    message,
				Proc:       rc.Proc,
				StdoutTail: rc.StdoutTail,
				StderrTail: stderrTail,
				StderrDone: stderrDone,
			})
		}
	}
	ready, err := WaitForBridgeReady(ctx, WaitForBridgeReadyOptions{
		Proc:               proc,
		Sandbox:            opts.Sandbox,
		BridgeStateDir:     opts.BridgeStateDir,
		BridgeType:         opts.BridgeType,
		Timeout:            timeout,
		PollInterval:       opts.PollInterval,
		CreateTimeoutError: startupError(label + " did not become ready in time."),
		CreateExitError:    startupError(label + " exited before becoming ready."),
	})
	if err != nil {
		return nil, err
	}

	resolve := opts.ResolveEndpoint
	if resolve == nil {
		resolve = func(ctx context.Context, port int) (harness.PortEndpoint, error) {
			return ResolveBridgeEndpoint(ctx, opts.Sandbox, nil, port)
		}
	}
	endpoint, err := resolve(ctx, ready.Port)
	if err != nil {
		return nil, err
	}
	endpoint, err = WithBridgeToken(endpoint, opts.Token)
	if err != nil {
		return nil, err
	}
	return &Launched{
		Proc:        proc,
		Port:        ready.Port,
		Endpoint:    endpoint,
		ReadySource: ready.Source,
		StdoutTail:  ready.StdoutTail,
		StderrTail:  stderrTail,
		StderrDone:  stderrDone,
	}, nil
}
