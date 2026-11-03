package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Ports TS vercel-network-policy-response-rules.test.ts (44a4261e52) and
// its fixture network-policy-response-rules-readback.json (a live
// @vercel/sandbox 3.9.0 create response).
const (
	r85toolsRequestedPolicy = `{"allow":{"registry.npmjs.org":[{"match":{"method":["PUT","POST","DELETE"]},"response":{"statusCode":403}}],"example.com":[]}}`
	r85toolsCreateResponse  = `{"mode":"custom","allowedDomains":["registry.npmjs.org","example.com"],"responseRules":[{"domain":"registry.npmjs.org","statusCode":403,"matchDimensions":["method"]}]}`
)

var r85toolsTransformations = []harness.RequestTransformation{{
	Match:     harness.RequestTransformationMatch{Host: "example.com"},
	Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-example": "1"}},
}}

type r85toolsMutator interface {
	SetNetworkPolicy(context.Context, harness.NetworkPolicy) error
	SetRequestTransformations(context.Context, []harness.RequestTransformation) error
	AddRequestTransformations(context.Context, []harness.RequestTransformation) error
}

var r85toolsOperations = []string{"addRequestTransformations", "setRequestTransformations", "setNetworkPolicy"}

func r85toolsMutate(m r85toolsMutator, op string) error {
	ctx := context.Background()
	switch op {
	case "setNetworkPolicy":
		return m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"registry.npmjs.org", "example.com"}})
	case "setRequestTransformations":
		return m.SetRequestTransformations(ctx, r85toolsTransformations)
	}
	return m.AddRequestTransformations(ctx, r85toolsTransformations)
}

type r85toolsSandbox struct {
	fakeSandbox
	metadata     json.RawMessage
	metadataErr  error
	metadataHits int
}

func (s *r85toolsSandbox) CurrentSessionNetworkPolicyMetadata(context.Context) (json.RawMessage, error) {
	s.metadataHits++
	if err := s.metadataErr; err != nil {
		s.metadataErr = nil
		return nil, err
	}
	return s.metadata, nil
}

func r85toolsPolicy(t *testing.T, raw string) NetworkPolicy {
	t.Helper()
	var p NetworkPolicy
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func r85toolsIsCapabilityError(err error) bool {
	var ce *harness.CapabilityUnsupportedError
	return errors.As(err, &ce)
}

func TestR85toolsRejectsResponseRulesInCurrentPolicy(t *testing.T) {
	for _, op := range r85toolsOperations {
		sb := &r85toolsSandbox{fakeSandbox: fakeSandbox{policy: r85toolsPolicy(t, r85toolsRequestedPolicy)}, metadata: json.RawMessage(r85toolsCreateResponse)}
		err := r85toolsMutate(NewPolicyManager(sb), op)
		if !r85toolsIsCapabilityError(err) || !strings.Contains(err.Error(), "response rules") {
			t.Fatalf("%s: err = %v", op, err)
		}
		if sb.callCount() != 0 || sb.metadataHits != 0 {
			t.Fatalf("%s: updates = %d, metadata reads = %d; want 0, 0", op, sb.callCount(), sb.metadataHits)
		}
	}
	// The rule survives a decode/encode round trip.
	got, _ := json.Marshal(r85toolsPolicy(t, r85toolsRequestedPolicy))
	if !strings.Contains(string(got), `"response":{"statusCode":403}`) {
		t.Fatalf("response rule lost on round trip: %s", got)
	}
}

// "rejects response rules omitted by native SDK readback": the session
// comes from a real API response whose networkPolicy is the API shape, and
// no request is sent by any mutation.
func TestR85toolsRejectsResponseRulesOmittedByReadback(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	body := `{"sandbox":{"name":"response-rule-sandbox","persistent":false,"currentSessionId":"session","status":"running"},` +
		`"session":{"id":"session","cwd":"/vercel/sandbox","status":"running","timeout":120000,"networkPolicy":` + r85toolsCreateResponse + `},"routes":[]}`
	for _, op := range r85toolsOperations {
		var resp sandboxAndSessionResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		sbx := newSandboxFromResponse(NewAPIClient(srv.URL, Credentials{Token: "t", TeamID: "team", ProjectID: "p"}), &resp)
		want := NetworkPolicy{AllowList: []string{"registry.npmjs.org", "example.com"}}
		if got := sbx.CurrentNetworkPolicy(); !got.Equal(want) {
			t.Fatalf("readback = %+v, want the TS nativeReadback {allow: [...]}", got)
		}
		err := r85toolsMutate(NewNetworkSession(sbx, false), op)
		if !r85toolsIsCapabilityError(err) || !strings.Contains(err.Error(), "response rules that cannot be preserved") {
			t.Fatalf("%s: err = %v", op, err)
		}
	}
	if n := atomic.LoadInt32(&requests); n != 0 {
		t.Fatalf("%d requests sent; want none", n)
	}
}

func TestR85toolsRejectsUninspectableSessionPolicy(t *testing.T) {
	for _, metadata := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		sb := &r85toolsSandbox{fakeSandbox: fakeSandbox{policy: r85toolsPolicy(t, `{"allow":["registry.npmjs.org","example.com"]}`)}, metadata: metadata}
		err := NewPolicyManager(sb).AddRequestTransformations(context.Background(), r85toolsTransformations)
		if err == nil || !strings.Contains(err.Error(), "could not be inspected") || sb.callCount() != 0 {
			t.Fatalf("metadata %s: err = %v, updates = %d", metadata, err, sb.callCount())
		}
	}
	// A PolicySandbox without the inspector fails closed for object policies.
	plain := &fakeSandbox{policy: r85toolsPolicy(t, `{"allow":["example.com"]}`)}
	var noInspector struct{ PolicySandbox }
	noInspector.PolicySandbox = plain
	if err := NewPolicyManager(noInspector).AddRequestTransformations(context.Background(), r85toolsTransformations); err == nil || !strings.Contains(err.Error(), "could not be inspected") {
		t.Fatalf("no inspector: err = %v", err)
	}
	// ...but not for allow-all / deny-all, which TS never inspects.
	allow := struct{ PolicySandbox }{&fakeSandbox{policy: AllowAllNetworkPolicy()}}
	if err := NewPolicyManager(allow).AddRequestTransformations(context.Background(), r85toolsTransformations); err != nil {
		t.Fatalf("allow-all: err = %v", err)
	}
}

func TestR85toolsRejectsUnrecognizedResponseRules(t *testing.T) {
	sb := &r85toolsSandbox{fakeSandbox: fakeSandbox{policy: r85toolsPolicy(t, `{"allow":["registry.npmjs.org","example.com"]}`)}, metadata: json.RawMessage(`{"mode":"custom","responseRules":{}}`)}
	err := NewPolicyManager(sb).AddRequestTransformations(context.Background(), r85toolsTransformations)
	if err == nil || !strings.Contains(err.Error(), "response rules") || sb.callCount() != 0 {
		t.Fatalf("err = %v, updates = %d", err, sb.callCount())
	}
}

func TestR85toolsInspectionFailureDoesNotCommitAndRetries(t *testing.T) {
	sb := &r85toolsSandbox{
		fakeSandbox: fakeSandbox{policy: r85toolsPolicy(t, `{"allow":["registry.npmjs.org","example.com"]}`)},
		metadata:    json.RawMessage(`{"mode":"custom","responseRules":[]}`),
		metadataErr: errors.New("inspection failed"),
	}
	m := NewPolicyManager(sb)
	if err := m.AddRequestTransformations(context.Background(), r85toolsTransformations); err == nil || err.Error() != "inspection failed" {
		t.Fatalf("first err = %v", err)
	}
	if sb.callCount() != 0 {
		t.Fatal("updated after a failed inspection")
	}
	if err := m.AddRequestTransformations(context.Background(), r85toolsTransformations); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRequestTransformations(context.Background(), r85toolsTransformations); err != nil {
		t.Fatal(err)
	}
	if sb.metadataHits != 2 || sb.callCount() != 1 {
		t.Fatalf("metadata reads = %d, updates = %d; want 2, 1", sb.metadataHits, sb.callCount())
	}
	got, _ := sb.lastCall()
	requirePolicyEqual(t, got, r85toolsPolicy(t, `{"allow":{"registry.npmjs.org":[],"example.com":[{"transform":[{"headers":{"x-example":"1"}}]}]}}`))
}

// The API session shape decodes to the public shape; members it cannot
// carry make a recomposition fail closed instead of being dropped.
func TestR85toolsAPIShapedPolicyDecode(t *testing.T) {
	for raw, want := range map[string]NetworkPolicy{
		`{"mode":"allow-all"}`: AllowAllNetworkPolicy(),
		`{"mode":"deny-all"}`:  DenyAllNetworkPolicy(),
		`{"mode":"custom","allowedDomains":["a.com"],"allowedCIDRs":["10.0.0.0/8"],"deniedCIDRs":["1.2.3.4/32"]}`: {AllowList: []string{"a.com"}, Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}, Deny: []string{"1.2.3.4/32"}}},
	} {
		if got := r85toolsPolicy(t, raw); !got.Equal(want) || got.IsZero() {
			t.Errorf("%s decoded to %+v, want %+v", raw, got, want)
		}
	}
	policy := r85toolsPolicy(t, `{"mode":"custom","allowedDomains":["a.com"],"injectionRules":[{"domain":"a.com"}]}`)
	sb := &r85toolsSandbox{fakeSandbox: fakeSandbox{policy: policy}, metadata: json.RawMessage(`{"mode":"custom"}`)}
	if err := NewPolicyManager(sb).SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyAllowAll}); err == nil || !strings.Contains(err.Error(), "could not be inspected") || sb.callCount() != 0 {
		t.Fatalf("unmapped member: err = %v, updates = %d", err, sb.callCount())
	}
}

// Ports TS utils.test.ts "keeps persistent: false on the live sandbox so
// stopping it writes no snapshot" (ea39523ce9).
func TestR85toolsLiveSandboxKeepsPersistentFalse(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	persistent := false
	if _, err := createLiveSandboxFromSnapshot(context.Background(), NewAPIClient(f.URL(), testCreds()), CreateParams{Runtime: "node24", TimeoutMs: 60_000, Persistent: &persistent}, "snap_derived", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.createRequests) != 1 {
		t.Fatalf("create requests = %d", len(f.createRequests))
	}
	body, _ := json.Marshal(f.createRequests[0])
	if !strings.Contains(string(body), `"persistent":false`) || strings.Contains(string(body), `"runtime"`) {
		t.Fatalf("create body = %s", body)
	}
}
