package claudecode_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS a54f0fd8c6 "passes the host disconnect timeout to the bridge": the
// spawn command carries --host-disconnect-timeout-ms only when it is set,
// and never as an environment variable.
func TestR88carryHostDisconnectTimeoutSpawn(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{time.Minute, " --host-disconnect-timeout-ms '60000'"}, {0, ""}} {
		srv, _ := newReplayServer()
		sandbox := newRecSandbox(srv)
		h := replayHarness(t, claudecode.Settings{HostDisconnectTimeout: tc.d})
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/vercel/sandbox/claude-code-s1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(sandbox.spawns) != 1 {
			t.Fatalf("spawns = %q", sandbox.spawns)
		}
		cmd := sandbox.spawns[0]
		if tc.want == "" && strings.Contains(cmd, "host-disconnect") || tc.want != "" && !strings.HasSuffix(cmd, tc.want) {
			t.Errorf("spawn = %q, want suffix %q", cmd, tc.want)
		}
		_ = sess.DoDestroy(context.Background())
		srv.Close()
	}
}
