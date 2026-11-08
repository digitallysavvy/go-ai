package codex_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS a54f0fd8c6 codex-harness.test.ts "passes hostDisconnectTimeoutMs %s to
// the bridge" (60000 and unset).
func TestR88toolsCodexHostDisconnectTimeout(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		want    string
	}{{60 * time.Second, " --host-disconnect-timeout-ms '60000'"}, {0, ""}} {
		srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
		t.Cleanup(srv.Close)
		sandbox := newTestSandbox(srv)
		var mu sync.Mutex
		var commands []string
		sandbox.SetSpawn(func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
			mu.Lock()
			commands = append(commands, opts.Command)
			mu.Unlock()
			proc := bridgetest.NewProcess()
			proc.WriteStdout("{\"type\":\"bridge-ready\",\"port\":4319}\n")
			go func() { time.Sleep(20 * time.Millisecond); proc.Exit(0) }()
			return proc, nil
		})
		sess, err := codex.New(codex.Settings{HostDisconnectTimeout: tc.timeout}).DoStart(context.Background(), harness.StartOptions{
			SessionID: "s", SessionWorkDir: "/workdir/s", SandboxSession: sandbox,
		})
		if err == nil {
			_ = sess.DoDestroy(context.Background())
		}
		mu.Lock()
		if len(commands) == 0 {
			mu.Unlock()
			t.Fatalf("no spawn (DoStart: %v)", err)
		}
		cmd := commands[0]
		mu.Unlock()
		if tc.want != "" && !strings.HasSuffix(cmd, tc.want) {
			t.Fatalf("command = %q, want suffix %q", cmd, tc.want)
		}
		if tc.want == "" && strings.Contains(cmd, "host-disconnect") {
			t.Fatalf("command = %q carries the flag", cmd)
		}
	}
}
