package codex_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type mkdirCounter struct {
	*testSandbox
	mkdirs atomic.Int32
}

func (m *mkdirCounter) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if strings.HasPrefix(opts.Command, "mkdir -p ") {
		m.mkdirs.Add(1)
	}
	return m.testSandbox.Run(ctx, opts)
}

func (m *mkdirCounter) Restricted() providerutils.SandboxSession { return m }

// TS resolves the bridge port before the fresh-start mkdir, so a sandbox
// without a port fails before anything is created in it.
func TestR42PortResolvedBeforeMkdir(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := &mkdirCounter{testSandbox: newTestSandbox(srv)}
	sandbox.ports = nil
	h := codex.New(codex.Settings{StartupTimeout: time.Second})
	_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"})
	var unsupported *harness.CapabilityUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want the missing-port error", err)
	}
	if n := sandbox.mkdirs.Load(); n != 0 {
		t.Fatalf("mkdir ran %d time(s) before the port was resolved", n)
	}
}
