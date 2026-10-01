package vercel

// Ports packages/sandbox-vercel/src/vercel-network-policy-manager.test.ts
// (VercelNetworkPolicyManager) test-for-test. The TS suite mocks the native
// `Sandbox`'s `currentSession().networkPolicy` getter and `update` method
// directly (not HTTP); fakeSandbox below plays the same role for
// *PolicyManager, which only depends on the PolicySandbox interface.

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

type fakeSandbox struct {
	mu     sync.Mutex
	policy NetworkPolicy
	calls  []NetworkPolicy
	fn     func(NetworkPolicy) error // optional per-call hook (errors/blocking)
}

func (f *fakeSandbox) CurrentNetworkPolicy() NetworkPolicy {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.policy
}

func (f *fakeSandbox) UpdateNetworkPolicy(_ context.Context, policy NetworkPolicy) error {
	f.mu.Lock()
	f.calls = append(f.calls, policy)
	fn := f.fn
	f.mu.Unlock()
	if fn != nil {
		return fn(policy)
	}
	return nil
}

func (f *fakeSandbox) lastCall() (NetworkPolicy, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return NetworkPolicy{}, false
	}
	return f.calls[len(f.calls)-1], true
}

func (f *fakeSandbox) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var credentialTransformation = harness.RequestTransformation{
	Match: harness.RequestTransformationMatch{
		Host:   "api.example.com",
		Method: []string{"POST"},
		Path:   &harness.StringMatcher{StartsWith: "/v1/"},
	},
	Transform: harness.RequestTransformationTransform{
		Headers: map[string]string{"authorization": "Bearer managed-secret"},
	},
}

var secondTransformation = harness.RequestTransformation{
	Match: harness.RequestTransformationMatch{
		Host: "api.example.com",
		Path: &harness.StringMatcher{Exact: "/models"},
	},
	Transform: harness.RequestTransformationTransform{
		Headers: map[string]string{"x-api-key": "second-secret"},
	},
}

func requirePolicyEqual(t *testing.T, got NetworkPolicy, want NetworkPolicy) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestPolicyManagerReplacesRequestTransformations(t *testing.T) {
	sbx := &fakeSandbox{}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	if err := m.SetRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRequestTransformations(ctx, []harness.RequestTransformation{secondTransformation}); err != nil {
		t.Fatal(err)
	}

	last, ok := sbx.lastCall()
	if !ok {
		t.Fatal("expected update to have been called")
	}
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"*": {},
		"api.example.com": {
			{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{Exact: "/models"}}, Transform: []PolicyTransform{{Headers: map[string]string{"x-api-key": "second-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerAddsToTransformationsPreviouslySetOrAdded(t *testing.T) {
	sbx := &fakeSandbox{}
	m := NewPolicyManager(sbx)
	ctx := context.Background()
	third := harness.RequestTransformation{
		Match:     harness.RequestTransformationMatch{Host: "api.example.com", Path: &harness.StringMatcher{Exact: "/usage"}},
		Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-usage-key": "third-secret"}},
	}

	must(t, m.SetRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{secondTransformation}))
	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{third}))

	last, _ := sbx.lastCall()
	if len(last.AllowMap["api.example.com"]) != 3 {
		t.Fatalf("expected 3 rules for api.example.com, got %d: %#v", len(last.AllowMap["api.example.com"]), last.AllowMap["api.example.com"])
	}
}

func TestPolicyManagerReplacesManagedTransformationWithSameMatchAndHeaders(t *testing.T) {
	sbx := &fakeSandbox{}
	m := NewPolicyManager(sbx)
	ctx := context.Background()
	refreshed := credentialTransformation
	refreshed.Transform = harness.RequestTransformationTransform{Headers: map[string]string{"authorization": "Bearer refreshed-secret"}}

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{refreshed}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"*": {},
		"api.example.com": {
			{
				Match:     &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}},
				Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer refreshed-secret"}}},
			},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerNoUpdateWhenAddingSameTransformationTwice(t *testing.T) {
	sbx := &fakeSandbox{}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))

	if got := sbx.callCount(); got != 1 {
		t.Fatalf("expected update called once, got %d", got)
	}
}

func TestPolicyManagerPreservesForwardingRulesAndMergesSameHost(t *testing.T) {
	sbx := &fakeSandbox{policy: NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{StartsWith: "/proxy/"}}, ForwardURL: "https://proxy.example.com"}},
	}}}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
			{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{StartsWith: "/proxy/"}}, ForwardURL: "https://proxy.example.com"},
		},
	}}
	requirePolicyEqual(t, last, want)

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"api.example.com", "registry.npmjs.org"}}))
	must(t, m.SetRequestTransformations(ctx, []harness.RequestTransformation{secondTransformation}))

	last, _ = sbx.lastCall()
	want = NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{Exact: "/models"}}, Transform: []PolicyTransform{{Headers: map[string]string{"x-api-key": "second-secret"}}}},
			{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{StartsWith: "/proxy/"}}, ForwardURL: "https://proxy.example.com"},
		},
		"registry.npmjs.org": {},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerKeepsTransformationsPendingUntilAccessAllows(t *testing.T) {
	sbx := &fakeSandbox{policy: DenyAllNetworkPolicy()}
	m := NewPolicyManager(sbx)
	ctx := context.Background()
	registryTransformation := harness.RequestTransformation{
		Match:     harness.RequestTransformationMatch{Host: "registry.npmjs.org"},
		Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-registry-key": "registry-secret"}},
	}

	must(t, m.SetRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{registryTransformation}))
	if got := sbx.callCount(); got != 0 {
		t.Fatalf("expected no update while access denies all, got %d calls", got)
	}

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"api.example.com"}}))
	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyAllowAll}))
	last, _ = sbx.lastCall()
	if last.Mode != "" || last.AllowMap == nil {
		t.Fatalf("expected an allow-map policy, got %#v", last)
	}
	if _, ok := last.AllowMap["*"]; !ok {
		t.Fatalf("expected '*' key, got %#v", last.AllowMap)
	}
	if _, ok := last.AllowMap["api.example.com"]; !ok {
		t.Fatalf("expected api.example.com key, got %#v", last.AllowMap)
	}
	if _, ok := last.AllowMap["registry.npmjs.org"]; !ok {
		t.Fatalf("expected registry.npmjs.org key, got %#v", last.AllowMap)
	}

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyDenyAll}))
	last, _ = sbx.lastCall()
	requirePolicyEqual(t, last, DenyAllNetworkPolicy())

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"registry.npmjs.org"}}))
	last, _ = sbx.lastCall()
	want = NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"registry.npmjs.org": {
			{Transform: []PolicyTransform{{Headers: map[string]string{"x-registry-key": "registry-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerIntersectsExactAndWildcardHosts(t *testing.T) {
	sbx := &fakeSandbox{}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	must(t, m.SetRequestTransformations(ctx, []harness.RequestTransformation{
		credentialTransformation,
		{Match: harness.RequestTransformationMatch{Host: "*.sub.example.com"}, Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-sub-key": "sub-secret"}}},
		{Match: harness.RequestTransformationMatch{Host: "outside.example.net"}, Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-outside-key": "outside-secret"}}},
	}))

	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"*.example.com"}}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"*.example.com": {},
		"api.example.com": {
			{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
		},
		"*.sub.example.com": {
			{Transform: []PolicyTransform{{Headers: map[string]string{"x-sub-key": "sub-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerCIDROnlyAccessDoesNotActivateHostnameRules(t *testing.T) {
	sbx := &fakeSandbox{policy: DenyAllNetworkPolicy()}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))
	must(t, m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedCIDRs: []string{"10.0.0.0/8"}, DeniedCIDRs: []string{"10.5.0.0/16"}}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}, Deny: []string{"10.5.0.0/16"}}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerReconstructsInitialHostnameAndSubnetPolicy(t *testing.T) {
	sbx := &fakeSandbox{policy: NetworkPolicy{
		AllowList: []string{"api.example.com"},
		Subnets:   &PolicySubnets{Allow: []string{"10.0.0.0/8"}, Deny: []string{"10.5.0.0/16"}},
	}}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	must(t, m.SetRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{
		AllowMap: map[string][]PolicyRule{
			"api.example.com": {
				{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
			},
		},
		Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}, Deny: []string{"10.5.0.0/16"}},
	}
	requirePolicyEqual(t, last, want)
}

func makeRedactedPolicy(count int, forwardURL string) NetworkPolicy {
	rules := make([]PolicyRule, 0, count+1)
	for i := 0; i < count; i++ {
		rules = append(rules, PolicyRule{
			Match:     &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}},
			Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "<redacted>"}}},
		})
	}
	if forwardURL != "" {
		rules = append(rules, PolicyRule{ForwardURL: forwardURL})
	}
	return NetworkPolicy{AllowMap: map[string][]PolicyRule{"api.example.com": rules}}
}

func TestPolicyManagerRehydratesMatchingRedactedTransformations(t *testing.T) {
	sbx := &fakeSandbox{policy: makeRedactedPolicy(1, "")}
	m := NewPolicyManager(sbx)
	ctx := context.Background()
	pending := harness.RequestTransformation{
		Match:     harness.RequestTransformationMatch{Host: "pending.example.com"},
		Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-pending-key": "pending-secret"}},
	}

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{credentialTransformation, pending}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerRehydratesWildcardUnderNarrowerHost(t *testing.T) {
	sbx := &fakeSandbox{policy: makeRedactedPolicy(1, "")}
	m := NewPolicyManager(sbx)
	ctx := context.Background()
	wildcard := credentialTransformation
	wildcard.Match.Host = "*.example.com"

	must(t, m.AddRequestTransformations(ctx, []harness.RequestTransformation{wildcard}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Method: []string{"POST"}, Path: &PolicyRuleMatcher{StartsWith: "/v1/"}}, Transform: []PolicyTransform{{Headers: map[string]string{"authorization": "Bearer managed-secret"}}}},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerRehydratesSameHostDespiteUnmatchedDetails(t *testing.T) {
	cases := []struct {
		name            string
		policy          NetworkPolicy
		transformations []harness.RequestTransformation
	}{
		{
			name:   "matcher",
			policy: makeRedactedPolicy(1, ""),
			transformations: []harness.RequestTransformation{{
				Match:     harness.RequestTransformationMatch{Host: credentialTransformation.Match.Host, Method: credentialTransformation.Match.Method, Path: &harness.StringMatcher{Exact: "/v1/chat"}},
				Transform: credentialTransformation.Transform,
			}},
		},
		{
			name:   "header name",
			policy: makeRedactedPolicy(1, ""),
			transformations: []harness.RequestTransformation{{
				Match:     credentialTransformation.Match,
				Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-api-key": "secret"}},
			}},
		},
		{
			name:            "multiplicity",
			policy:          makeRedactedPolicy(2, ""),
			transformations: []harness.RequestTransformation{credentialTransformation},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sbx := &fakeSandbox{policy: tc.policy}
			m := NewPolicyManager(sbx)
			if err := m.AddRequestTransformations(context.Background(), tc.transformations); err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if got := sbx.callCount(); got != 1 {
				t.Fatalf("expected update called once, got %d", got)
			}
		})
	}
}

func TestPolicyManagerRejectsUnattributedRedactedTransformations(t *testing.T) {
	sbx := &fakeSandbox{policy: makeRedactedPolicy(1, "")}
	m := NewPolicyManager(sbx)
	other := credentialTransformation
	other.Match.Host = "other.test"

	err := m.AddRequestTransformations(context.Background(), []harness.RequestTransformation{other})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !harness.IsCapabilityUnsupportedError(err) {
		t.Fatalf("expected a CapabilityUnsupportedError, got %T: %v", err, err)
	}
	if got := sbx.callCount(); got != 0 {
		t.Fatalf("expected no update, got %d calls", got)
	}
}

func TestPolicyManagerBlocksInitialPolicyChangeWithRedactedTransformations(t *testing.T) {
	sbx := &fakeSandbox{policy: makeRedactedPolicy(1, "")}
	m := NewPolicyManager(sbx)

	err := m.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyAllowAll})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := sbx.callCount(); got != 0 {
		t.Fatalf("expected no update, got %d calls", got)
	}
}

func TestPolicyManagerSetRequestTransformationsReplacesRedacted(t *testing.T) {
	sbx := &fakeSandbox{policy: makeRedactedPolicy(1, "https://proxy.example.com")}
	m := NewPolicyManager(sbx)

	must(t, m.SetRequestTransformations(context.Background(), []harness.RequestTransformation{secondTransformation}))

	last, _ := sbx.lastCall()
	want := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {
			{Match: &PolicyRuleMatch{Path: &PolicyRuleMatcher{Exact: "/models"}}, Transform: []PolicyTransform{{Headers: map[string]string{"x-api-key": "second-secret"}}}},
			{ForwardURL: "https://proxy.example.com"},
		},
	}}
	requirePolicyEqual(t, last, want)
}

func TestPolicyManagerKeepsQueuedOperationsInFIFOOrder(t *testing.T) {
	sbx := &fakeSandbox{}
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	sbx.fn = func(NetworkPolicy) error {
		if first {
			first = false
			close(started)
			<-release
		}
		return nil
	}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)
	go func() {
		firstErr <- m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"first.example.com"}})
	}()
	<-started
	go func() {
		secondErr <- m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"second.example.com"}})
	}()
	close(release)

	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if err := <-secondErr; err != nil {
		t.Fatal(err)
	}

	sbx.mu.Lock()
	calls := append([]NetworkPolicy(nil), sbx.calls...)
	sbx.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	requirePolicyEqual(t, calls[0], NetworkPolicy{AllowList: []string{"first.example.com"}})
	requirePolicyEqual(t, calls[1], NetworkPolicy{AllowList: []string{"second.example.com"}})
}

func TestPolicyManagerDoesNotCommitFailedUpdates(t *testing.T) {
	sbx := &fakeSandbox{}
	calls := 0
	sbx.fn = func(NetworkPolicy) error {
		calls++
		if calls == 1 {
			return errors.New("update failed")
		}
		return nil
	}
	m := NewPolicyManager(sbx)
	ctx := context.Background()

	err1 := m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"first.example.com"}})
	if err1 == nil {
		t.Fatal("expected first call to fail")
	}
	err2 := m.SetNetworkPolicy(ctx, harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"second.example.com"}})
	if err2 != nil {
		t.Fatalf("expected second call to succeed, got %v", err2)
	}

	if got := sbx.callCount(); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
	last, _ := sbx.lastCall()
	requirePolicyEqual(t, last, NetworkPolicy{AllowList: []string{"second.example.com"}})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
