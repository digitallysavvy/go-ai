package claudecode_test

import (
	"context"
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
	home     string
	id       string
	workDir  string
	endpoint harness.PortEndpoint
	ports    []int
}

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
func (t *testSandbox) GetPortEndpoint(context.Context, harness.PortEndpointOptions) (harness.PortEndpoint, error) {
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
// process that immediately reports itself ready (the real bridge.mjs's
// stdout handshake line; the actual port is irrelevant since the test's
// PortEndpoint override always wins). The process "exits" shortly after so a
// test's Session.DoDestroy/DoStop teardown does not have to sit through the
// full 5s exit-wait budget waiting on a process nothing will ever kill in
// this fake (no real OS process is involved).
func wireSpawn(sandbox *testSandbox) {
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		proc := bridgetest.NewProcess()
		proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
		go func() {
			time.Sleep(50 * time.Millisecond)
			proc.Exit(0)
		}()
		return proc, nil
	})
}
