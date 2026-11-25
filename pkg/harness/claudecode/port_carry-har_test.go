package claudecode_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
)

// TS 04a8f3837e (claude-code-harness.test.ts): keepAliveMs reaches both the
// fresh and the attached channel; unset is the 20s default, a negative value
// disables the heartbeat.
func TestPortcarryharKeepAliveForwardedToChannels(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  time.Duration
		want time.Duration
	}{
		{"set", 100 * time.Millisecond, 100 * time.Millisecond},
		{"default", 0, bridge.DefaultKeepAlive},
		{"disabled", -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey})
			t.Cleanup(srv.Close)
			sandbox := newTestSandbox(srv)
			wireSpawn(sandbox)
			h, err := claudecode.New(claudecode.Settings{StartupTimeout: 2 * time.Second, KeepAlive: tc.set})
			if err != nil {
				t.Fatal(err)
			}
			opts := harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/workdir"}
			first, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := claudecode.PortcarryharChannelKeepAlive(first); got != tc.want {
				t.Fatalf("fresh channel keepalive = %v, want %v", got, tc.want)
			}
			opts.ResumeFrom, err = first.DoDetach(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			attached, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
			if got := claudecode.PortcarryharChannelKeepAlive(attached); got != tc.want {
				t.Fatalf("attached channel keepalive = %v, want %v", got, tc.want)
			}
		})
	}
}
