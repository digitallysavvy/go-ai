package bridge_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// fixedResolver builds a LaunchOptions.ResolveEndpoint that turns the bound
// port into a loopback ws:// URL, standing in for a real
// NetworkSandboxSession.GetPortEndpoint.
func fixedResolver(_ int) func(context.Context, int) (harness.PortEndpoint, error) {
	return func(_ context.Context, port int) (harness.PortEndpoint, error) {
		return harness.PortEndpoint{URL: "ws://127.0.0.1:" + strconv.Itoa(port) + "/"}, nil
	}
}

// TS: Launch resolves once the bridge announces readiness on stdout, and the
// returned endpoint carries the bridge token.
func TestLaunchResolvesFromStdoutReadyLine(t *testing.T) {
	sandbox := bridgetest.NewSandbox()
	proc := bridgetest.NewProcess()
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		return proc, nil
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
	}()

	launched, err := bridge.Launch(context.Background(), bridge.LaunchOptions{
		Label:           "claude-code bridge",
		Source:          "claude-code",
		Sandbox:         sandbox,
		Command:         "node bridge.mjs",
		Port:            4319,
		Token:           "tok",
		BridgeStateDir:  "/state",
		BridgeType:      "claude-code",
		StartupTimeout:  time.Second,
		ResolveEndpoint: fixedResolver(4319),
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if launched.Port != 4319 {
		t.Fatalf("Port = %d, want 4319", launched.Port)
	}
	if launched.ReadySource != bridge.ReadySourceStdout {
		t.Fatalf("ReadySource = %v, want stdout", launched.ReadySource)
	}
	if !strings.Contains(launched.Endpoint.URL, "agent_bridge_token=tok") {
		t.Fatalf("Endpoint.URL = %q, want it to carry the bridge token", launched.Endpoint.URL)
	}
	if launched.Proc != proc {
		t.Fatal("Launched.Proc is not the spawned process")
	}
}

// TS/aae0138: Launch also resolves from the bridge-meta.json "waiting"
// fallback when stdout never delivers (e.g. a Bun sandbox runtime).
func TestLaunchResolvesFromMetadataFallback(t *testing.T) {
	sandbox := bridgetest.NewSandbox()
	proc := bridgetest.NewProcess() // stdout never writes anything
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		return proc, nil
	})
	go func() {
		time.Sleep(20 * time.Millisecond)
		meta, _ := json.Marshal(map[string]any{"type": "claude-code", "port": 4319, "state": "waiting"})
		sandbox.SetFile(bridge.BridgeMetaPath("/state"), string(meta))
	}()

	launched, err := bridge.Launch(context.Background(), bridge.LaunchOptions{
		Label:           "claude-code bridge",
		Source:          "claude-code",
		Sandbox:         sandbox,
		Command:         "node bridge.mjs",
		Port:            4319,
		Token:           "tok",
		BridgeStateDir:  "/state",
		BridgeType:      "claude-code",
		StartupTimeout:  2 * time.Second,
		PollInterval:    10 * time.Millisecond,
		ResolveEndpoint: fixedResolver(4319),
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if launched.ReadySource != bridge.ReadySourceMetadata {
		t.Fatalf("ReadySource = %v, want metadata", launched.ReadySource)
	}

	// MarkBridgeStarting must have written the "starting" marker before the
	// test's own "waiting" write landed.
	if _, ok := sandbox.File(bridge.BridgeMetaPath("/state")); !ok {
		t.Fatal("bridge-meta.json was never written")
	}
}

// TS/39c8276: an exit before readiness reports the exit code and the
// stdout/stderr tails: "<label> exited before becoming ready. Exit code:
// N.\n\nstdout:\n…\n\nstderr:\n…".
func TestLaunchExitBeforeReadyReportsCodeAndTails(t *testing.T) {
	sandbox := bridgetest.NewSandbox()
	proc := bridgetest.NewProcess()
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		return proc, nil
	})
	proc.WriteStdout("starting up\n")
	proc.WriteStderr("boom\n")
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.Exit(7)
	}()

	_, err := bridge.Launch(context.Background(), bridge.LaunchOptions{
		Label:           "claude-code bridge",
		Source:          "claude-code",
		Sandbox:         sandbox,
		Command:         "node bridge.mjs",
		Port:            4319,
		Token:           "tok",
		BridgeStateDir:  "/state",
		BridgeType:      "claude-code",
		StartupTimeout:  time.Second,
		ResolveEndpoint: fixedResolver(4319),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{
		"claude-code bridge exited before becoming ready. Exit code: 7.",
		"stdout:\nstarting up",
		"stderr:\nboom",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want it to contain %q", msg, want)
		}
	}
	if !proc.Killed() {
		// Already exited on its own; Kill() need not be called, but it must
		// not hang or panic if it is.
		_ = proc.Kill()
	}
}

// The startup-timeout path ("<label> did not become ready in time.") also
// kills the process and reports the code once known.
func TestLaunchStartupTimeout(t *testing.T) {
	sandbox := bridgetest.NewSandbox()
	proc := bridgetest.NewProcess() // never becomes ready
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		return proc, nil
	})

	_, err := bridge.Launch(context.Background(), bridge.LaunchOptions{
		Label:           "claude-code bridge",
		Source:          "claude-code",
		Sandbox:         sandbox,
		Command:         "node bridge.mjs",
		Port:            4319,
		Token:           "tok",
		BridgeStateDir:  "/state",
		BridgeType:      "claude-code",
		StartupTimeout:  30 * time.Millisecond,
		PollInterval:    5 * time.Millisecond,
		ResolveEndpoint: fixedResolver(4319),
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "claude-code bridge did not become ready in time.") {
		t.Fatalf("error = %q", err.Error())
	}
	if !proc.Killed() {
		t.Fatal("WaitForBridgeReady must kill the process on timeout")
	}
}
