package deepagents

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// boundPortSandbox makes the bridge report ready on 5555, a port other than
// the one requested (4319).
type boundPortSandbox struct{ *fakeSandbox }

func (s boundPortSandbox) Spawn(_ context.Context, _ providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	proc := bridgetest.NewProcess()
	go func() {
		time.Sleep(2 * time.Millisecond)
		proc.WriteStdout(`{"type":"bridge-ready","port":5555}` + "\n")
		time.Sleep(5 * time.Millisecond)
		proc.Exit(0)
	}()
	return proc, nil
}
func (s boundPortSandbox) Restricted() providerutils.SandboxSession { return s }

// TS persists the port the bridge reported ready on (boundPort), not the
// requested one, in the lifecycle bridge coordinates.
func TestDoStartPersistsBoundBridgePort(t *testing.T) {
	const token = "bound-port-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	h := CreateDeepAgents(Settings{MintBridgeToken: func(string) string { return token }})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: boundPortSandbox{newFakeSandbox(srv)},
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	if got := sess.(*session).p.bridgePort; got != 5555 {
		t.Fatalf("bridgePort = %d, want the bound port 5555", got)
	}
}
