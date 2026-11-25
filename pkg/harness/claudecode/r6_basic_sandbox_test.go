package claudecode_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type basicSandbox struct{ providerutils.SandboxSession }

// TS Claude Code harness tests: a basic (non-network) sandbox session needs an
// explicit port and portEndpoint. The deprecated mintBridgeToken setting is
// ignored (TS c849d37dac removed its sandbox-id check).
func TestBasicSandboxSessionRequiresExplicitBridgeSettings(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: claudecode.R91carryTestKeys.PublicKey})
	t.Cleanup(srv.Close)
	endpoint := &harness.PortEndpoint{URL: "ws://" + srv.Addr() + "/"}
	for _, tc := range []struct {
		name string
		mut  func(*claudecode.Settings)
		want string
	}{
		{"no port", func(s *claudecode.Settings) { s.PortEndpoint = endpoint }, "The harness requires an explicit `port` setting when using a basic sandbox session."},
		{"no endpoint", func(s *claudecode.Settings) { s.Port = 4319 }, "The harness requires an explicit `portEndpoint` setting when using a basic sandbox session."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s claudecode.Settings
			tc.mut(&s)
			h := mustNew(t, s)
			_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: basicSandbox{newTestSandbox(srv)}})
			var cu *harness.CapabilityUnsupportedError
			if !errors.As(err, &cu) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func mustNew(t *testing.T, s claudecode.Settings) *claudecode.Harness {
	t.Helper()
	h, err := claudecode.New(s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
