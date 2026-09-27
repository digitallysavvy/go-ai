package codex_test

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// testSandbox is a fake harness.NetworkSandboxSession backed by
// bridgetest.Sandbox, used to drive Harness.DoStart against a bridgetest
// fake bridge server without any real Node process or CLI.
type testSandbox struct {
	*bridgetest.Sandbox
	home       string
	id         string
	workDir    string
	endpoint   harness.PortEndpoint
	ports      []int
	spawnCount int32
}

// SpawnCount returns how many times Spawn has been called, so a test can
// assert an attach rung skipped (or a fallback rung required) a respawn.
func (t *testSandbox) SpawnCount() int { return int(atomic.LoadInt32(&t.spawnCount)) }

func newTestSandbox(srv *bridgetest.Server) *testSandbox {
	return &testSandbox{
		Sandbox: bridgetest.NewSandbox(),
		home:    "/home/agent", id: "test-sandbox", workDir: "/workdir",
		endpoint: srv.Endpoint(), ports: []int{4319},
	}
}

// Run intercepts the `printf "%s" "$HOME"` probe ResolveSandboxHomeDir sends;
// every other command (mkdir -p, ...) succeeds via the embedded fake.
func (t *testSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if opts.Command == `printf "%s" "$HOME"` {
		return providerutils.SandboxRunResult{Stdout: t.home}, nil
	}
	return t.Sandbox.Run(ctx, opts)
}

func (t *testSandbox) ID() string                      { return t.id }
func (t *testSandbox) DefaultWorkingDirectory() string { return t.workDir }
func (t *testSandbox) Ports() []int                    { return t.ports }

// GetPortEndpoint returns the live fake bridge server's endpoint for the
// declared port; any other port (used to simulate stale, unreachable
// persisted bridge coordinates in an attach-failure test) resolves to a
// closed local port so the connection is refused immediately instead of
// hanging.
func (t *testSandbox) GetPortEndpoint(_ context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	if len(t.ports) > 0 && opts.Port != t.ports[0] {
		return harness.PortEndpoint{URL: "ws://127.0.0.1:1/"}, nil
	}
	return t.endpoint, nil
}
func (t *testSandbox) GetPortURL(context.Context, harness.PortEndpointOptions) (string, error) {
	return t.endpoint.URL, nil
}
func (t *testSandbox) Stop(context.Context) error               { return nil }
func (t *testSandbox) Destroy(context.Context) error            { return nil }
func (t *testSandbox) Restricted() providerutils.SandboxSession { return t }

var _ harness.NetworkSandboxSession = (*testSandbox)(nil)

// wireSpawn configures sandbox to answer every Spawn call with a fresh fake
// process that immediately reports itself ready and "exits" shortly after so
// a test's Session.DoDestroy/DoStop teardown does not sit through the full 5s
// exit-wait budget.
func wireSpawn(sandbox *testSandbox) {
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		atomic.AddInt32(&sandbox.spawnCount, 1)
		proc := bridgetest.NewProcess()
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
		go func() {
			time.Sleep(50 * time.Millisecond)
			proc.Exit(0)
		}()
		return proc, nil
	})
}
