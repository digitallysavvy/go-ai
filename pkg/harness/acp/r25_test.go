package acp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func TestR25EmptyPnpmWorkspaceYAMLRejected(t *testing.T) {
	i := tsLocked()
	i.Source.PnpmWorkspaceYAMLSet = true
	err := validateImplementation(i)
	if err == nil || !strings.Contains(err.Error(), "ACP source.pnpmWorkspaceYaml must not be empty.") {
		t.Fatalf("err = %v", err)
	}
	i.Source.PnpmWorkspaceYAMLSet = false
	if err := validateImplementation(i); err != nil {
		t.Fatal(err)
	}
	i.Source.PnpmWorkspaceYAMLSet = true
	i.Source.PnpmWorkspaceYAML = "x: 1\n"
	if err := validateImplementation(i); err != nil {
		t.Fatal(err)
	}
}

func TestR25CompatibilityBaseURLEmptyKept(t *testing.T) {
	pa := &ProviderAuthentication{GatewayEnv: tsGatewayEnv}
	c := resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa, map[string]string{"AI_GATEWAY_API_KEY": "k", "AI_GATEWAY_BASE_URL": ""})
	if c.BaseURL != "" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	c = resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa, map[string]string{"AI_GATEWAY_API_KEY": "k"})
	if c.BaseURL != defaultAIGatewayBaseURL {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

// A non-marker bridge error is rejected as the raw wire object in TS; its text
// is JSON.stringify of the object, not Go's map formatting.
func TestDeserializeBridgeErrorRendersObjectAsJSON(t *testing.T) {
	err := deserializeBridgeError(map[string]any{"name": "Error", "message": "boom"}, "h")
	if got := err.Error(); got != `{"message":"boom","name":"Error"}` {
		t.Fatalf("error text = %q", got)
	}
}

// Error text quotes values with JSON.stringify, not Go %q: a control character
// is \u0001 (not \x01) and U+2028 stays literal.
func TestImplementationErrorQuotesLikeJSONStringify(t *testing.T) {
	err := validateImplementation(implementation{
		Source:     Source{Type: SourceInstallCommand, Command: "x"},
		Executable: "a\x01 ",
	})
	want := "ACP executable must be a bare command name without a path; received \"a\\u0001 \"."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// boundPortSandbox reports a bridge-ready port different from the requested
// one and resolves every port to the live bridge.
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

func (s boundPortSandbox) GetPortEndpoint(_ context.Context, _ harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return harness.PortEndpoint{URL: "ws://" + s.bridgeAddr + "/"}, nil
}

// TS persists the port the bridge reported ready on (`boundPort`), not the
// port that was requested, in the lifecycle bridge coordinates.
func TestDoStartPersistsBoundBridgePort(t *testing.T) {
	const token = "bound-port-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/w", SandboxSession: boundPortSandbox{newFakeSandbox(srv)},
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	if got := sess.(*session).p.bridgePort; got != 5555 {
		t.Fatalf("bridgePort = %d, want the bound port 5555", got)
	}
}

func (s boundPortSandbox) Restricted() providerutils.SandboxSession { return s }

func TestWrapPortErrorMessageMatchesTS(t *testing.T) {
	err := wrapPortError(bridge.ErrNoPort, "h")
	want := "The h ACP harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or configure the harness `port`."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
