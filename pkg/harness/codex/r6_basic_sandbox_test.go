package codex_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/bridgeauthkeys"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type basicSandbox struct{ providerutils.SandboxSession }

// TS codex harness tests: a basic (non-network) sandbox session needs an
// explicit port and portEndpoint. (The mintBridgeToken-without-id error is
// gone since TS c849d37dac: the setting is ignored.)
func TestBasicSandboxSessionRequiresExplicitBridgeSettings(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{PublicKey: bridgeauthkeys.Testing().PublicKey})
	t.Cleanup(srv.Close)
	endpoint := &harness.PortEndpoint{URL: "ws://" + srv.Addr() + "/"}
	for _, tc := range []struct {
		name string
		mut  func(*codex.Settings)
		want string
	}{
		{"no port", func(s *codex.Settings) { s.PortEndpoint = endpoint }, "The harness requires an explicit `port` setting when using a basic sandbox session."},
		{"no endpoint", func(s *codex.Settings) { s.Port = 4319 }, "The harness requires an explicit `portEndpoint` setting when using a basic sandbox session."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s codex.Settings
			tc.mut(&s)
			h := codex.New(s)
			_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: basicSandbox{newTestSandbox(srv)}})
			var cu *harness.CapabilityUnsupportedError
			if !errors.As(err, &cu) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
