package bridge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// basicSandbox hides the network surface of a sandbox (TS `restricted()`).
type basicSandbox struct{ providerutils.SandboxSession }

func TestValidateBridgeSandboxSettings(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{})
	t.Cleanup(srv.Close)
	endpoint := &harness.PortEndpoint{URL: "ws://" + srv.Addr() + "/"}
	mint := func(id string) string { return id }
	basic := basicSandbox{}

	cases := []struct {
		name      string
		sandbox   providerutils.SandboxSession
		sandboxID string
		port      int
		endpoint  *harness.PortEndpoint
		mint      harness.MintBridgeTokenCallback
		want      string
	}{
		{"basic without port", basic, "", 0, endpoint, nil, "The harness requires an explicit `port` setting when using a basic sandbox session."},
		{"basic without endpoint", basic, "", 4319, nil, nil, "The harness requires an explicit `portEndpoint` setting when using a basic sandbox session."},
		{"mint without sandbox id", basic, "", 4319, endpoint, mint, "The harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id."},
		{"basic with explicit settings", basic, "", 4319, endpoint, nil, ""},
		{"mint with sandbox id", basic, "sbx", 4319, endpoint, mint, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bridge.ValidateBridgeSandboxSettings(tc.sandbox, tc.sandboxID, tc.port, tc.endpoint, tc.mint, "h")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			var cu *harness.CapabilityUnsupportedError
			if !errors.As(err, &cu) || err.Error() != tc.want || cu.HarnessID != "h" {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveBridgePortAndEndpointOrUnsupported(t *testing.T) {
	_, err := bridge.ResolveBridgePortOrUnsupported(basicSandbox{}, 0, "h")
	if want := "The harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or configure the harness `port` setting."; err == nil || err.Error() != want {
		t.Fatalf("port err = %v, want %q", err, want)
	}
	if port, err := bridge.ResolveBridgePortOrUnsupported(basicSandbox{}, 4319, "h"); err != nil || port != 4319 {
		t.Fatalf("port = %d, %v", port, err)
	}
	_, err = bridge.ResolveBridgeEndpointOrUnsupported(context.Background(), basicSandbox{}, nil, 4319, "h")
	if want := "The harness requires an explicit `portEndpoint` setting when using a basic sandbox session."; err == nil || err.Error() != want {
		t.Fatalf("endpoint err = %v, want %q", err, want)
	}
	override := &harness.PortEndpoint{URL: "ws://example/"}
	if got, err := bridge.ResolveBridgeEndpointOrUnsupported(context.Background(), basicSandbox{}, override, 1, "h"); err != nil || got.URL != override.URL {
		t.Fatalf("endpoint = %+v, %v", got, err)
	}
}
