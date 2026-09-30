package vercel

// Ports packages/sandbox-vercel/src/vercel-legacy-sandbox-provider.test.ts
// against the fake HTTP server (testserver_test.go) instead of a mocked
// native Sandbox.

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func createTestSession(t *testing.T, f *fakeServer, name string) *NetworkSession {
	t.Helper()
	sbx := newTestSandbox(t, f, name)
	return NewNetworkSession(sbx, true)
}

// --- wrap-existing-sandbox path ---

func TestLegacyWrapExistingPortsFromRoutes(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_harness")
	if err := sbx.Update(context.Background(), UpdateParams{Ports: []int{3000, 4000}}); err != nil {
		t.Fatal(err)
	}
	p := NewLegacyProvider(LegacySettings{Sandbox: sbx})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := session.Ports()
	if len(got) != 2 || got[0] != 3000 || got[1] != 4000 {
		t.Fatalf("expected [3000 4000], got %v", got)
	}
}

func TestLegacyWrapExistingRestrictedRuns(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(string, string, []string, string, map[string]string) CommandScript {
		return CommandScript{Stdout: "ok\n"}
	}
	sbx := newTestSandbox(t, f, "sbx_restricted")
	p := NewLegacyProvider(LegacySettings{Sandbox: sbx})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Restricted().Run(context.Background(), providerutils.SandboxProcessOptions{Command: "echo ok"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok\n" {
		t.Fatalf("expected 'ok\\n', got %q", result.Stdout)
	}
}

func TestLegacyWrapExistingStopIsNoOp(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_stopnoop")
	p := NewLegacyProvider(LegacySettings{Sandbox: sbx})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	stopped := f.byName["sbx_stopnoop"].stopped
	f.mu.Unlock()
	if stopped {
		t.Fatal("expected Stop() to be a no-op for a wrapped sandbox")
	}
}

func TestLegacyWrapExistingDestroyIsNoOp(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_destroynoop")
	p := NewLegacyProvider(LegacySettings{Sandbox: sbx})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	_, stillExists := f.byName["sbx_destroynoop"]
	f.mu.Unlock()
	if !stillExists {
		t.Fatal("expected Destroy() to be a no-op for a wrapped sandbox")
	}
}

func TestLegacyGetPortEndpointHTTPS(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_ep")
	if err := sbx.Update(context.Background(), UpdateParams{Ports: []int{3000}}); err != nil {
		t.Fatal(err)
	}
	session := NewNetworkSession(sbx, false)
	endpoint, err := session.GetPortEndpoint(context.Background(), harness.PortEndpointOptions{Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.URL == "" {
		t.Fatal("expected a non-empty URL")
	}
}

func TestLegacyGetPortEndpointWSUpgrade(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_ws")
	if err := sbx.Update(context.Background(), UpdateParams{Ports: []int{4000}}); err != nil {
		t.Fatal(err)
	}
	session := NewNetworkSession(sbx, false)
	endpoint, err := session.GetPortEndpoint(context.Background(), harness.PortEndpointOptions{Port: 4000, Protocol: harness.PortProtocolWS})
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoint.URL) < 3 || endpoint.URL[:3] != "wss" {
		t.Fatalf("expected a wss:// URL (domain is https), got %q", endpoint.URL)
	}
}

func TestLegacyGetPortEndpointNotExposed(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_notexposed")
	if err := sbx.Update(context.Background(), UpdateParams{Ports: []int{4000}}); err != nil {
		t.Fatal(err)
	}
	session := NewNetworkSession(sbx, false)
	_, err := session.GetPortEndpoint(context.Background(), harness.PortEndpointOptions{Port: 9999})
	if !harness.IsCapabilityUnsupportedError(err) {
		t.Fatalf("expected a CapabilityUnsupportedError, got %T: %v", err, err)
	}
}

func TestLegacyGetPortURLCompatibilityWrapper(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_geturl")
	if err := sbx.Update(context.Background(), UpdateParams{Ports: []int{4000}}); err != nil {
		t.Fatal(err)
	}
	session := NewNetworkSession(sbx, false)
	url, err := session.GetPortURL(context.Background(), harness.PortEndpointOptions{Port: 4000, Protocol: harness.PortProtocolWS})
	if err != nil {
		t.Fatal(err)
	}
	if len(url) < 3 || url[:3] != "wss" {
		t.Fatalf("expected a wss:// URL, got %q", url)
	}
}

// --- setNetworkPolicy mapping ---

func TestLegacySetNetworkPolicyMapsAllowAll(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	sbx := newTestSandbox(t, f, "sbx_np1")
	// Seed a non-default initial policy (deny-all), mirroring the TS test's
	// mock sandbox: otherwise "allow-all" already equals the implicit
	// default and the policy manager (correctly) skips the update.
	denyAll := DenyAllNetworkPolicy()
	if err := sbx.Update(context.Background(), UpdateParams{NetworkPolicy: &denyAll}); err != nil {
		t.Fatal(err)
	}
	session := NewNetworkSession(sbx, true)

	if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyAllowAll}); err != nil {
		t.Fatal(err)
	}
	last := f.updateRequests[len(f.updateRequests)-1]
	requirePolicyEqual(t, *last.Req.NetworkPolicy, AllowAllNetworkPolicy())
}

func TestLegacySetNetworkPolicyMapsDenyAll(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	session := createTestSession(t, f, "sbx_np2")
	if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyDenyAll}); err != nil {
		t.Fatal(err)
	}
	last := f.updateRequests[len(f.updateRequests)-1]
	requirePolicyEqual(t, *last.Req.NetworkPolicy, DenyAllNetworkPolicy())
}

func TestLegacySetNetworkPolicyMapsCustomAllowedHosts(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	session := createTestSession(t, f, "sbx_np3")
	if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"api.example.com", "*.npmjs.org"}}); err != nil {
		t.Fatal(err)
	}
	last := f.updateRequests[len(f.updateRequests)-1]
	requirePolicyEqual(t, *last.Req.NetworkPolicy, NetworkPolicy{AllowList: []string{"api.example.com", "*.npmjs.org"}})
}

func TestLegacySetNetworkPolicyMapsCustomCIDRs(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	session := createTestSession(t, f, "sbx_np4")
	if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedCIDRs: []string{"10.0.0.0/8"}, DeniedCIDRs: []string{"10.5.0.0/16"}}); err != nil {
		t.Fatal(err)
	}
	last := f.updateRequests[len(f.updateRequests)-1]
	requirePolicyEqual(t, *last.Req.NetworkPolicy, NetworkPolicy{Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}, Deny: []string{"10.5.0.0/16"}}})
}

// --- setPorts ---

func TestLegacySetPortsForwardsPortList(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	session := createTestSession(t, f, "sbx_ports")
	if err := session.SetPorts(context.Background(), []int{4000, 5000}); err != nil {
		t.Fatal(err)
	}
	last := f.updateRequests[len(f.updateRequests)-1]
	if len(last.Req.Ports) != 2 || last.Req.Ports[0] != 4000 || last.Req.Ports[1] != 5000 {
		t.Fatalf("expected [4000 5000], got %v", last.Req.Ports)
	}
}

// --- create-from-scratch: defaults, auth errors, snapshot templates ---

func TestLegacyCreateMissingCredentialsIsAuthError(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{BaseURL: f.URL()})
	_, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if !harness.IsSandboxAuthenticationError(err) {
		t.Fatalf("expected a SandboxAuthenticationError, got %T: %v", err, err)
	}
}

func TestLegacyCreatePreservesUnrelatedFailures(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.forceCreateError = &APIError{StatusCode: 503, Message: "Sandbox service unavailable"}
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL()})
	_, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if harness.IsSandboxAuthenticationError(err) {
		t.Fatalf("expected the unrelated failure to pass through, got a SandboxAuthenticationError: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
		t.Fatalf("expected the 503 APIError to pass through, got %T: %v", err, err)
	}
}

func TestLegacyCreatePreservesRuntimeAndTimeoutDefaults(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL()})
	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	req := f.createRequests[len(f.createRequests)-1]
	if req.Runtime != "node24" || req.Timeout != 30*60*1000 {
		t.Fatalf("expected runtime node24 and 30m timeout, got %#v", req)
	}
}

func TestLegacyCreateOmitsRuntimeWhenImageProvided(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL(), Image: "vercel/sandbox/universal"})
	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	req := f.createRequests[len(f.createRequests)-1]
	if req.Image != "vercel/sandbox/universal" || req.Runtime != "" {
		t.Fatalf("expected image set and runtime empty, got %#v", req)
	}
}

func TestLegacyCreateRespectsExplicitTimeout(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL(), TimeoutMs: 60_000})
	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	req := f.createRequests[len(f.createRequests)-1]
	if req.Timeout != 60_000 {
		t.Fatalf("expected timeout 60000, got %d", req.Timeout)
	}
}

func TestLegacyCreateWithTemplateCachesSnapshotAcrossSessions(t *testing.T) {
	ResetLegacySnapshotCache()
	defer ResetLegacySnapshotCache()
	f := newFakeServer()
	defer f.Close()
	f.snapshotOnStop = true
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL(), Name: "explicit-template"})
	var onFirstCreateCalls int
	onFirstCreate := func(ctx context.Context, session providerutils.SandboxSession) error {
		onFirstCreateCalls++
		return nil
	}

	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{
		SessionID: "first", Identity: "recipe", OnFirstCreate: onFirstCreate,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{
		SessionID: "second", Identity: "recipe", OnFirstCreate: onFirstCreate,
	}); err != nil {
		t.Fatal(err)
	}

	if onFirstCreateCalls != 1 {
		t.Fatalf("expected the template to be prepared once, got %d calls", onFirstCreateCalls)
	}
	var liveNames []string
	for _, req := range f.createRequests {
		if req.Name == "ai-sdk-harness-session-first" || req.Name == "ai-sdk-harness-session-second" {
			liveNames = append(liveNames, req.Name)
			if req.Source == nil || req.Source.Type != "snapshot" {
				t.Fatalf("expected %s to fork from a snapshot, got %#v", req.Name, req)
			}
		}
	}
	if len(liveNames) != 2 {
		t.Fatalf("expected 2 live sessions, got %v", liveNames)
	}
}

func TestLegacyDestroyStopsBeforeDeletingOwnedSandboxes(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL()})
	session, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := session.ID()
	if err := session.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	_, exists := f.byName[name]
	f.mu.Unlock()
	if exists {
		t.Fatal("expected the sandbox to be deleted")
	}
}

func TestLegacyResumeForwardsCredentials(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	p := NewLegacyProvider(LegacySettings{Credentials: testCreds(), BaseURL: f.URL()})
	if _, err := p.CreateSession(context.Background(), harness.CreateSandboxSessionOptions{SessionID: "session-123"}); err != nil {
		t.Fatal(err)
	}
	session, err := p.ResumeSession(context.Background(), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID() != "ai-sdk-harness-session-session-123" {
		t.Fatalf("unexpected id %q", session.ID())
	}
}
