package acp

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS 04a8f3837e: keepAliveMs reaches both the fresh and the attached bridge
// channel; unset is the 20s default, a negative value disables the heartbeat.
func TestPort2toolsACPKeepAliveForwarded(t *testing.T) {
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
			srv := newServer(t, "", func(*bridgetest.Turn, map[string]any) {})
			sandbox := newFakeSandbox(srv)
			cfg := testSettings()
			cfg.KeepAlive = tc.set
			h, err := CreateACP(cfg)
			if err != nil {
				t.Fatal(err)
			}
			opts := harness.StartOptions{SessionID: "s1", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox}
			first, err := h.DoStart(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := first.(*session).p.channel.KeepAlive(); got != tc.want {
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
			if got := attached.(*session).p.channel.KeepAlive(); got != tc.want {
				t.Fatalf("attached channel keepalive = %v, want %v", got, tc.want)
			}
		})
	}
}
