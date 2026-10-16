package acp

import (
	"context"
	"errors"
	"strings"
	"sync"
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

// TS forwards the thrown Error object itself in the `error` part and rejects
// the turn with it, so a typed error from isMcpToolCall survives both.
func TestToolCallClassificationErrorKeepsTypeInForwardedErrorPart(t *testing.T) {
	const token = "cls-token"
	sentinel := errors.New("classifier failed")
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{
			"type": "acp-tool-call-candidate", "requestId": "req-1",
			"toolCall": map[string]any{"toolCallId": "tc-1", "title": "Run"},
		})
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "tc-1", "toolName": "run", "input": "{}"})
	})
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.IsMcpToolCall = func(ToolCall) bool { panic(sentinel) }
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv),
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	var mu sync.Mutex
	var forwarded error
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("hi"),
		Emit: func(p harness.StreamPart) {
			if ep, ok := p.(*harness.ErrorPart); ok {
				mu.Lock()
				forwarded, _ = ep.Error.(error)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-control.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not settle")
	}
	if !errors.Is(control.Err(), sentinel) {
		t.Fatalf("control.Err() = %v, want the sentinel", control.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(forwarded, sentinel) {
		t.Fatalf("forwarded error part = %v, want the sentinel error value", forwarded)
	}
}

// A turn that settled early on a callback error is still finished by the
// bridge after its abort; TS's session-level markTurnFinished listeners clear
// the in-flight flag then, so the session can start its next prompt.
func TestTurnInFlightClearsWhenBridgeFinishesAfterEarlySettle(t *testing.T) {
	const token = "late-finish-token"
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{
			"type": "acp-tool-call-candidate", "requestId": "req-1",
			"toolCall": map[string]any{"toolCallId": "tc-1", "title": "Run"},
		})
		turn.Emit(map[string]any{"type": "tool-call", "toolCallId": "tc-1", "toolName": "run", "input": "{}"})
		time.Sleep(50 * time.Millisecond)
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
		})
	})
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return token }
		s.IsMcpToolCall = func(ToolCall) bool { panic("classifier failed") }
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s", SessionWorkDir: "/w", SandboxSession: newFakeSandbox(srv),
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi")})
	if err != nil {
		t.Fatal(err)
	}
	<-control.Done()
	s := sess.(*session)
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		inFlight := s.turnInFlight
		s.mu.Unlock()
		if !inFlight {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("turnInFlight stayed true after the bridge finished the aborted turn")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TS parses the persisted turnStartConfig / coldSession with strict zod
// schemas, so a malformed one is rejected before it can drive recovery.
func TestValidateLifecycleStateDataChecksStartConfigShape(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	v := h.(*acpHarness)
	good := `{"version":1,"configurationFingerprint":"f","prompt":[{"type":"text","text":"hi"}],"permissionMode":"allow-all"}`
	if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","turnStartConfig":` + good + `}`)); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
	for name, cfg := range map[string]string{
		"empty object":        `{}`,
		"wrong version":       `{"version":2,"configurationFingerprint":"f","prompt":[],"permissionMode":"allow-all"}`,
		"missing fingerprint": `{"version":1,"prompt":[],"permissionMode":"allow-all"}`,
		"bad permission mode": `{"version":1,"configurationFingerprint":"f","prompt":[],"permissionMode":"root"}`,
		"missing prompt":      `{"version":1,"configurationFingerprint":"f","permissionMode":"allow-all"}`,
		"non-text block":      `{"version":1,"configurationFingerprint":"f","prompt":[{"type":"image"}],"permissionMode":"allow-all"}`,
		"null tools":          `{"version":1,"configurationFingerprint":"f","prompt":[],"permissionMode":"allow-all","tools":null}`,
	} {
		if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","turnStartConfig":` + cfg + `}`)); err == nil {
			t.Errorf("%s: turnStartConfig accepted", name)
		}
	}
	cold := `{"version":1,"configurationFingerprint":"f","permissionMode":"allow-all"}`
	if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","coldSession":` + cold + `}`)); err != nil {
		t.Fatalf("valid coldSession rejected: %v", err)
	}
	if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","coldSession":{"version":1,"permissionMode":"allow-all"}}`)); err == nil {
		t.Error("coldSession without a fingerprint accepted")
	}
}
