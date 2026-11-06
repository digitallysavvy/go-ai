package claudecode_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS persists the port the bridge reported ready on (boundPort) in the
// lifecycle bridge coordinates, not the requested one: the sandbox exposes
// 4000 but the fake bridge reports 4319.
func TestDetachPersistsBoundBridgePort(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newTestSandbox(srv)
	sandbox.ports = []int{4000}
	wireSpawn(sandbox)
	h, err := claudecode.New(claudecode.Settings{MintBridgeToken: func(string) string { return "tok" }, StartupTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir",
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	state, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	var data struct {
		Bridge *struct {
			Port int `json:"port"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(state.Data, &data); err != nil || data.Bridge == nil {
		t.Fatalf("state data = %s, %v", state.Data, err)
	}
	if data.Bridge.Port != 4319 {
		t.Fatalf("bridge.port = %d, want the bound port 4319", data.Bridge.Port)
	}
}
