package deepagents

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS "reuses a caller-minted token and passes endpoint headers when
// attaching" (deepagents-harness.test.ts): DoDetach returns bridge
// coordinates carrying the caller-minted token, and reattaching (a second
// DoStart with that ResumeSessionState) must reuse the same token — no
// second MintBridgeToken call — and must not respawn a fresh bridge process,
// since live coordinates route it through the attach rung instead.
func TestDoDetach_ReattachReusesTokenAndReconnect(t *testing.T) {
	const token = "detach-reattach-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)

	var mintMu sync.Mutex
	var mintCalls []string
	reconnect := bridge.ReconnectOptions{MaxElapsed: 120 * time.Second, InitialDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second}
	h := CreateDeepAgents(Settings{
		MintBridgeToken: func(sandboxID string) string {
			mintMu.Lock()
			mintCalls = append(mintCalls, sandboxID)
			mintMu.Unlock()
			return token
		},
		Reconnect: reconnect,
	})

	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart (initial): %v", err)
	}

	mintMu.Lock()
	if len(mintCalls) != 1 || mintCalls[0] != "test-sandbox" {
		t.Fatalf("mintCalls after initial start = %v, want exactly [test-sandbox]", mintCalls)
	}
	mintMu.Unlock()

	resumeFrom, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatalf("DoDetach: %v", err)
	}
	var resumeData resumeStateData
	if err := json.Unmarshal(resumeFrom.Data, &resumeData); err != nil {
		t.Fatalf("unmarshal resumeFrom.Data: %v", err)
	}
	if resumeData.Bridge == nil || resumeData.Bridge.Token != token {
		t.Fatalf("resumeFrom bridge coords = %+v, want token %q", resumeData.Bridge, token)
	}

	attachedSess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session",
		SandboxSession: sandbox, ResumeFrom: resumeFrom,
	})
	if err != nil {
		t.Fatalf("DoStart (reattach): %v", err)
	}
	t.Cleanup(func() { _ = attachedSess.DoDestroy(context.Background()) })

	mintMu.Lock()
	defer mintMu.Unlock()
	if len(mintCalls) != 1 {
		t.Fatalf("mintCalls after reattach = %v, want still exactly 1 (the token must be reused, not re-minted)", mintCalls)
	}

	// The reattach must go through attachToRunningBridge (rung 1), not a
	// fresh spawn — so the sandbox must still show exactly the one spawn
	// from the initial DoStart.
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	if len(sandbox.spawnCommands) != 1 {
		t.Fatalf("spawnCommands = %v, want exactly 1 (reattach must not respawn a fresh bridge process)", sandbox.spawnCommands)
	}
}

// basicNetworkSandbox wraps a *fakeSandbox but deliberately does NOT expose
// AddRequestTransformations (unlike embedding, a named field does not
// promote it), so `sandboxSession.(harness.RequestTransformationAdder)`
// fails and DoStart falls back to the credential-forwarding path that warns
// when brokering is unavailable.
type basicNetworkSandbox struct{ inner *fakeSandbox }

func (b *basicNetworkSandbox) Description() string { return b.inner.Description() }
func (b *basicNetworkSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	return b.inner.Run(ctx, opts)
}
func (b *basicNetworkSandbox) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return b.inner.Spawn(ctx, opts)
}
func (b *basicNetworkSandbox) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	return b.inner.ReadFile(ctx, path)
}
func (b *basicNetworkSandbox) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	return b.inner.ReadBinaryFile(ctx, path)
}
func (b *basicNetworkSandbox) ReadTextFile(ctx context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	return b.inner.ReadTextFile(ctx, opts)
}
func (b *basicNetworkSandbox) WriteFile(ctx context.Context, path string, content io.Reader) error {
	return b.inner.WriteFile(ctx, path, content)
}
func (b *basicNetworkSandbox) WriteBinaryFile(ctx context.Context, path string, content []byte) error {
	return b.inner.WriteBinaryFile(ctx, path, content)
}
func (b *basicNetworkSandbox) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	return b.inner.WriteTextFile(ctx, opts)
}
func (b *basicNetworkSandbox) ID() string { return b.inner.ID() }
func (b *basicNetworkSandbox) DefaultWorkingDirectory() string {
	return b.inner.DefaultWorkingDirectory()
}
func (b *basicNetworkSandbox) Ports() []int { return b.inner.Ports() }
func (b *basicNetworkSandbox) GetPortEndpoint(ctx context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return b.inner.GetPortEndpoint(ctx, opts)
}
func (b *basicNetworkSandbox) GetPortURL(ctx context.Context, opts harness.PortEndpointOptions) (string, error) {
	return b.inner.GetPortURL(ctx, opts)
}
func (b *basicNetworkSandbox) Stop(ctx context.Context) error           { return b.inner.Stop(ctx) }
func (b *basicNetworkSandbox) Destroy(ctx context.Context) error        { return b.inner.Destroy(ctx) }
func (b *basicNetworkSandbox) Restricted() providerutils.SandboxSession { return b }

var _ harness.NetworkSandboxSession = (*basicNetworkSandbox)(nil)

func assertNotRequestTransformationAdder(t *testing.T, s providerutils.SandboxSession) {
	t.Helper()
	if _, ok := s.(harness.RequestTransformationAdder); ok {
		t.Fatal("test bug: basicNetworkSandbox must not implement harness.RequestTransformationAdder")
	}
}

// TS `0c37bf0` / harnessutil "warns when credential brokering is
// unavailable": when the sandbox does not support additive request
// transformations and no CredentialForwarding callback replaces the real
// credential, DoStart must fall back to forwarding it directly and emit the
// brokering-unavailable warning (not silently leak it without any signal).
func TestDoStart_WarnsWhenCredentialBrokeringUnavailable(t *testing.T) {
	const token = "no-broker-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := &basicNetworkSandbox{inner: newFakeSandbox(srv)}
	assertNotRequestTransformationAdder(t, sandbox)

	var warnings []string
	orig := harnessutil.Warn
	harnessutil.Warn = func(m string) { warnings = append(warnings, m) }
	t.Cleanup(func() { harnessutil.Warn = orig })

	h := CreateDeepAgents(Settings{
		MintBridgeToken: func(string) string { return token },
		Auth: harness.AuthEnvironment(map[string]string{
			"ANTHROPIC_API_KEY": "anthropic-secret",
		}),
	})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "test-session", SessionWorkDir: "/vercel/sandbox/deepagents-test-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	if len(warnings) != 1 || warnings[0] != harnessutil.CredentialBrokeringUnavailableWarning {
		t.Fatalf("warnings = %v, want exactly one CredentialBrokeringUnavailableWarning", warnings)
	}

	// The real credential must still reach the spawned process (fallback
	// forwarding, not brokering) — the warning documents reduced security,
	// it does not withhold the credential.
	sandbox.inner.mu.Lock()
	defer sandbox.inner.mu.Unlock()
	if len(sandbox.inner.spawnEnvs) == 0 || sandbox.inner.spawnEnvs[0]["ANTHROPIC_API_KEY"] != "anthropic-secret" {
		t.Fatalf("spawn env = %v, want ANTHROPIC_API_KEY=anthropic-secret", sandbox.inner.spawnEnvs)
	}
}

// Port of TS `lifecycleStateSchema` coverage: ValidateLifecycleStateData
// must accept empty/absent state and well-formed resume data, and reject
// malformed JSON and type-mismatched fields.
func TestValidateLifecycleStateData(t *testing.T) {
	h := CreateDeepAgents(Settings{})
	validator, ok := h.(harness.LifecycleStateValidator)
	if !ok {
		t.Fatal("deepagents harness does not implement harness.LifecycleStateValidator")
	}

	cases := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"empty", "", false},
		{"empty object", "{}", false},
		{"well-formed bridge coords", `{"bridge":{"port":4319,"token":"tok","lastSeenEventId":3}}`, false},
		{"well-formed with sandboxCredentialEnvironment", `{"sandboxCredentialEnvironment":{"ANTHROPIC_API_KEY":"placeholder"}}`, false},
		{"malformed json", `{not json`, true},
		{"bridge.port wrong type", `{"bridge":{"port":"not-a-number","token":"tok","lastSeenEventId":3}}`, true},
		{"bridge.token wrong type", `{"bridge":{"port":4319,"token":123,"lastSeenEventId":3}}`, true},
		{"top-level array instead of object", `[1,2,3]`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var data json.RawMessage
			if tc.data != "" {
				data = json.RawMessage(tc.data)
			}
			err := validator.ValidateLifecycleStateData(data)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateLifecycleStateData(%s): expected an error, got none", tc.data)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateLifecycleStateData(%s): unexpected error: %v", tc.data, err)
			}
		})
	}
}
