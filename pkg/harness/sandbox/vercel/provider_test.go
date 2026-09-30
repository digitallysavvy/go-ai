package vercel

// Ports packages/sandbox-vercel/src/vercel-sandbox.test.ts and
// utils.test.ts against the fake HTTP server (see testserver_test.go)
// instead of a mocked native `Sandbox.create`/`get`/`getOrCreate`.

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func testCreds() Credentials { return Credentials{Token: "tok", TeamID: "team", ProjectID: "proj"} }

func TestCreateNetworkSandboxSessionBasic(t *testing.T) {
	f := newFakeServer()
	defer f.Close()

	session, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), SandboxID: "live-session", Runtime: "node24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID() != "live-session" {
		t.Fatalf("expected id 'live-session', got %q", session.ID())
	}
	if len(f.createRequests) != 1 || f.createRequests[0].Name != "live-session" || f.createRequests[0].Runtime != "node24" {
		t.Fatalf("unexpected create requests: %#v", f.createRequests)
	}
}

// TestCreateNetworkSandboxSessionForwardsFullCreateOptions confirms the
// deferred TS `BaseCreateSandboxParams` fields (resources, env, tags,
// region, failoverRegions, keepLastSnapshots) reach the create request body,
// matching TS's `VercelSandboxCreateParams` alias of `Sandbox.create`'s
// parameter type: it accepts these directly and spreads them straight
// through in `createVercelNetworkSandboxSession` -> `Sandbox.create` ->
// `APIClient.createSandbox`.
func TestCreateNetworkSandboxSessionForwardsFullCreateOptions(t *testing.T) {
	f := newFakeServer()
	defer f.Close()

	expiration := int64(3600_000)
	deleteEvicted := true
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), SandboxID: "full-options", Runtime: "node24",
		Resources:       &ResourcesParams{Vcpus: 4},
		Env:             map[string]string{"NODE_ENV": "production"},
		Tags:            map[string]string{"team": "infra"},
		Region:          "iad1",
		FailoverRegions: []string{"sfo1"},
		KeepLastSnapshots: &KeepLastSnapshotsParams{
			Count: 3, Expiration: &expiration, DeleteEvicted: &deleteEvicted,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) != 1 {
		t.Fatalf("expected 1 create request, got %d", len(f.createRequests))
	}
	req := f.createRequests[0]
	if req.Resources == nil || req.Resources.Vcpus != 4 {
		t.Errorf("Resources not forwarded: %#v", req.Resources)
	}
	if req.Env["NODE_ENV"] != "production" {
		t.Errorf("Env not forwarded: %#v", req.Env)
	}
	if req.Tags["team"] != "infra" {
		t.Errorf("Tags not forwarded: %#v", req.Tags)
	}
	if req.Region != "iad1" {
		t.Errorf("Region not forwarded: %q", req.Region)
	}
	if len(req.FailoverRegions) != 1 || req.FailoverRegions[0] != "sfo1" {
		t.Errorf("FailoverRegions not forwarded: %#v", req.FailoverRegions)
	}
	if req.KeepLastSnapshots == nil || req.KeepLastSnapshots.Count != 3 ||
		req.KeepLastSnapshots.Expiration == nil || *req.KeepLastSnapshots.Expiration != expiration ||
		req.KeepLastSnapshots.DeleteEvicted == nil || !*req.KeepLastSnapshots.DeleteEvicted {
		t.Errorf("KeepLastSnapshots not forwarded: %#v", req.KeepLastSnapshots)
	}
}

func TestCreateNetworkSandboxSessionRejectsConflictingNameAndID(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), Name: "native-name", SandboxID: "other-name",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(f.createRequests) != 0 {
		t.Fatalf("expected no create call, got %d", len(f.createRequests))
	}
}

func TestCreateNetworkSandboxSessionAllowsMatchingNameAndID(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), Name: "same-name", SandboxID: "same-name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) != 1 || f.createRequests[0].Name != "same-name" {
		t.Fatalf("unexpected create requests: %#v", f.createRequests)
	}
}

func TestResumeNetworkSandboxSessionReattaches(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	if _, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), SandboxID: "sbx_harness",
	}); err != nil {
		t.Fatal(err)
	}

	session, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), SandboxID: "sbx_harness",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID() != "sbx_harness" {
		t.Fatalf("expected id 'sbx_harness', got %q", session.ID())
	}
	if len(f.getRequests) != 1 || f.getRequests[0].Resume != "true" {
		t.Fatalf("expected one resume=true get, got %#v", f.getRequests)
	}
}

func TestResumeNetworkSandboxSessionRejectsMissing(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	_, err := ResumeNetworkSandboxSession(context.Background(), ResumeSessionOptions{
		Credentials: testCreds(), BaseURL: f.URL(), SandboxID: "missing",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.IsNotFound() {
		t.Fatalf("expected a 404 APIError, got %T: %v", err, err)
	}
}

func TestCreateNetworkSandboxSessionMissingCredentialsIsAuthError(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	f := newFakeServer()
	defer f.Close()
	_, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{BaseURL: f.URL()})
	if !harness.IsSandboxAuthenticationError(err) {
		t.Fatalf("expected a SandboxAuthenticationError, got %T: %v", err, err)
	}
}

func TestCreateNetworkSandboxSessionWithTemplateDerivesReusableSnapshot(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.snapshotOnStop = true

	var prepareCalls int
	template := &Template{Identity: "recipe-one", Prepare: func(ctx context.Context, session providerutils.SandboxSession) error {
		prepareCalls++
		return nil
	}}

	// Each call gets its own live sandbox name (a harness would normally stop
	// the previous session's sandbox before reusing a name; that lifecycle
	// concern is orthogonal to what this test exercises: that the *template*
	// snapshot, keyed by identity, is prepared once and reused).
	create := func(liveName, snapshotID, identity string) harness.NetworkSandboxSession {
		tmpl := *template
		tmpl.Identity = identity
		sess, err := CreateNetworkSandboxSession(context.Background(), CreateSessionOptions{
			Credentials: testCreds(), BaseURL: f.URL(),
			Source:    &SnapshotSource{Type: "snapshot", SnapshotID: snapshotID},
			SandboxID: liveName, Template: &tmpl,
		})
		if err != nil {
			t.Fatalf("create(%q,%q,%q): %v", liveName, snapshotID, identity, err)
		}
		return sess
	}

	create("live-1", "base", "recipe-one")
	create("live-2", "base", "recipe-one")
	create("live-3", "base", "other-base-identity") // different identity -> different template
	create("live-4", "base", "recipe-two")

	// Every live sandbox must fork from a snapshot.
	liveCreates := 0
	for _, req := range f.createRequests {
		if req.Name == "live-1" || req.Name == "live-2" || req.Name == "live-3" || req.Name == "live-4" {
			liveCreates++
			if req.Source == nil || req.Source.Type != "snapshot" {
				t.Fatalf("expected live sandbox %d to be created from a snapshot source, got %#v", liveCreates, req)
			}
		}
	}
	if liveCreates != 4 {
		t.Fatalf("expected 4 live sandbox creates, got %d", liveCreates)
	}
	// The template with identity "recipe-one" is prepared once and reused for
	// its second call; the other two identities get their own preparation.
	if prepareCalls != 3 {
		t.Fatalf("expected 3 template preparations (1 reused), got %d", prepareCalls)
	}
}

// Ports utils.test.ts "prepares and publishes a named template once when a
// cache is supplied": a template sandbox with no snapshot yet is stopped to
// produce one, and the result is cached by template name.
func TestEnsureTemplateSnapshotUsesStopFallbackAndCache(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.snapshotOnStop = true
	client := NewAPIClient(f.URL(), testCreds())
	cache := newSnapshotCache()

	var onCreateCalls int
	onCreate := func(ctx context.Context, sbx *Sandbox) error {
		onCreateCalls++
		return nil
	}
	params := CreateParams{Runtime: "node24", TimeoutMs: 60_000}

	id1, err := ensureTemplateSnapshot(context.Background(), client, params, "prepared-template", cache, onCreate)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := ensureTemplateSnapshot(context.Background(), client, params, "prepared-template", cache, onCreate)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" || id1 != id2 {
		t.Fatalf("expected the same non-empty snapshot id both times, got %q and %q", id1, id2)
	}
	if onCreateCalls != 1 {
		t.Fatalf("expected onCreate called once (second call served from cache), got %d", onCreateCalls)
	}
	if cached, ok := cache.get("prepared-template"); !ok || cached != id1 {
		t.Fatalf("expected the cache to hold %q, got %q (ok=%v)", id1, cached, ok)
	}
	if len(f.createRequests) != 1 {
		t.Fatalf("expected exactly one create request (cache skips the second GetOrCreate), got %d", len(f.createRequests))
	}
}

// Ports utils.test.ts "forks a live sandbox with the derived snapshot and its
// own name": runtime/image/source/persistent are stripped from baseParams.
func TestCreateLiveSandboxFromSnapshotStripsEnvironmentFields(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	persistent := true

	_, err := createLiveSandboxFromSnapshot(context.Background(), client, CreateParams{
		Image: "vercel/sandbox/universal", TimeoutMs: 60_000, Persistent: &persistent,
	}, "snap_derived", "live-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) != 1 {
		t.Fatalf("expected one create request, got %d", len(f.createRequests))
	}
	req := f.createRequests[0]
	if req.Image != "" || req.Runtime != "" || req.Persistent != nil {
		t.Fatalf("expected image/runtime/persistent stripped, got %#v", req)
	}
	if req.Source == nil || req.Source.Type != "snapshot" || req.Source.SnapshotID != "snap_derived" {
		t.Fatalf("expected snapshot source snap_derived, got %#v", req.Source)
	}
	if req.Name != "live-session" || req.Timeout != 60_000 {
		t.Fatalf("expected name/timeout preserved, got %#v", req)
	}
}

// TestCreateLiveSandboxFromSnapshotPreservesForwardedOptions confirms
// resources/env/tags/region/failoverRegions/keepLastSnapshots survive the
// runtime/image/persistent/source strip in createLiveSandboxFromSnapshot,
// matching TS `createLiveSandboxFromSnapshot`'s `...forkParams` spread
// (only `runtime`, `image`, `source`, and `persistent` are destructured out).
func TestCreateLiveSandboxFromSnapshotPreservesForwardedOptions(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	expiration := int64(1000)

	_, err := createLiveSandboxFromSnapshot(context.Background(), client, CreateParams{
		Image:           "vercel/sandbox/universal",
		Resources:       &ResourcesParams{Vcpus: 2},
		Env:             map[string]string{"FOO": "bar"},
		Tags:            map[string]string{"env": "staging"},
		Region:          "iad1",
		FailoverRegions: []string{"sfo1"},
		KeepLastSnapshots: &KeepLastSnapshotsParams{
			Count: 2, Expiration: &expiration,
		},
	}, "snap_derived", "live-session")
	if err != nil {
		t.Fatal(err)
	}
	req := f.createRequests[0]
	if req.Resources == nil || req.Resources.Vcpus != 2 {
		t.Errorf("Resources not preserved: %#v", req.Resources)
	}
	if req.Env["FOO"] != "bar" {
		t.Errorf("Env not preserved: %#v", req.Env)
	}
	if req.Tags["env"] != "staging" {
		t.Errorf("Tags not preserved: %#v", req.Tags)
	}
	if req.Region != "iad1" {
		t.Errorf("Region not preserved: %q", req.Region)
	}
	if len(req.FailoverRegions) != 1 || req.FailoverRegions[0] != "sfo1" {
		t.Errorf("FailoverRegions not preserved: %#v", req.FailoverRegions)
	}
	if req.KeepLastSnapshots == nil || req.KeepLastSnapshots.Count != 2 {
		t.Errorf("KeepLastSnapshots not preserved: %#v", req.KeepLastSnapshots)
	}
}

func TestDeriveTemplateNameIsDeterministicAndInjective(t *testing.T) {
	p := withDefaultSandboxSettings(CreateParams{Source: &SnapshotSource{Type: "snapshot", SnapshotID: "base"}})
	a := deriveTemplateName("recipe-one", p)
	b := deriveTemplateName("recipe-one", p)
	if a != b {
		t.Fatalf("expected deterministic name, got %q vs %q", a, b)
	}
	if got := deriveTemplateName("recipe-two", p); got == a {
		t.Fatalf("expected a different name for a different identity, got the same %q", got)
	}
	if len(a) <= len("ai-sdk-harness-v2-") {
		t.Fatalf("expected a hashed suffix, got %q", a)
	}
}
