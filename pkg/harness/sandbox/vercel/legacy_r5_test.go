package vercel

// Remaining cases of packages/sandbox-vercel/src/vercel-legacy-sandbox-provider.test.ts
// (setNetworkPolicy / setRequestTransformations / addRequestTransformations).

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

var legacyCredentialTransformation = harness.RequestTransformation{
	Match: harness.RequestTransformationMatch{
		Host: "ai-gateway.vercel.sh", Method: []string{"POST"}, Path: &harness.StringMatcher{StartsWith: "/v1/"},
	},
	Transform: harness.RequestTransformationTransform{Headers: map[string]string{"authorization": "Bearer host-only-credential"}},
}

func lastPolicy(t *testing.T, f *fakeServer) NetworkPolicy {
	t.Helper()
	if len(f.updateRequests) == 0 || f.updateRequests[len(f.updateRequests)-1].Req.NetworkPolicy == nil {
		t.Fatal("no network policy update was sent")
	}
	return *f.updateRequests[len(f.updateRequests)-1].Req.NetworkPolicy
}

func TestLegacySetNetworkPolicyCombinedShapes(t *testing.T) {
	cases := []struct {
		name   string
		policy harness.NetworkPolicy
		want   NetworkPolicy
	}{
		{"allowedHosts + deniedCIDRs", harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"api.example.com"}, DeniedCIDRs: []string{"169.254.169.254/32"}},
			NetworkPolicy{AllowList: []string{"api.example.com"}, Subnets: &PolicySubnets{Deny: []string{"169.254.169.254/32"}}}},
		{"allowedCIDRs only", harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedCIDRs: []string{"10.0.0.0/8"}},
			NetworkPolicy{Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			defer f.Close()
			session := createTestSession(t, f, "sbx_combined")
			if err := session.SetNetworkPolicy(context.Background(), tc.policy); err != nil {
				t.Fatal(err)
			}
			requirePolicyEqual(t, lastPolicy(t, f), tc.want)
		})
	}
}

func TestLegacySetRequestTransformations(t *testing.T) {
	t.Run("preserves allow-all when only request transformations are configured", func(t *testing.T) {
		f := newFakeServer()
		defer f.Close()
		session := createTestSession(t, f, "sbx_rt1")
		if err := session.SetRequestTransformations(context.Background(), []harness.RequestTransformation{legacyCredentialTransformation}); err != nil {
			t.Fatal(err)
		}
		got := lastPolicy(t, f)
		if got.AllowMap == nil || len(got.AllowMap["*"]) != 0 {
			t.Fatalf("policy = %#v, want a wildcard host with no rules", got)
		}
		rules := got.AllowMap["ai-gateway.vercel.sh"]
		if len(rules) != 1 || rules[0].Match == nil || rules[0].Match.Path == nil || rules[0].Match.Path.StartsWith != "/v1/" ||
			!reflect.DeepEqual(rules[0].Match.Method, []string{"POST"}) || len(rules[0].Transform) != 1 ||
			rules[0].Transform[0].Headers["authorization"] != "Bearer host-only-credential" {
			t.Fatalf("rules = %#v", rules)
		}
	})
	t.Run("groups several request transformations for the same host", func(t *testing.T) {
		f := newFakeServer()
		defer f.Close()
		session := createTestSession(t, f, "sbx_rt2")
		second := harness.RequestTransformation{
			Match:     harness.RequestTransformationMatch{Host: "ai-gateway.vercel.sh", Path: &harness.StringMatcher{Exact: "/models"}},
			Transform: harness.RequestTransformationTransform{Headers: map[string]string{"x-api-key": "second-credential"}},
		}
		if err := session.SetRequestTransformations(context.Background(), []harness.RequestTransformation{legacyCredentialTransformation, second}); err != nil {
			t.Fatal(err)
		}
		if n := len(lastPolicy(t, f).AllowMap["ai-gateway.vercel.sh"]); n != 2 {
			t.Fatalf("rules for the host = %d, want 2", n)
		}
	})
	t.Run("keeps the access policy authoritative regardless of setter order", func(t *testing.T) {
		f := newFakeServer()
		defer f.Close()
		session := createTestSession(t, f, "sbx_rt3")
		if err := session.SetRequestTransformations(context.Background(), []harness.RequestTransformation{legacyCredentialTransformation}); err != nil {
			t.Fatal(err)
		}
		if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"registry.npmjs.org"}, DeniedCIDRs: []string{"169.254.169.254/32"}}); err != nil {
			t.Fatal(err)
		}
		requirePolicyEqual(t, lastPolicy(t, f), NetworkPolicy{AllowList: []string{"registry.npmjs.org"}, Subnets: &PolicySubnets{Deny: []string{"169.254.169.254/32"}}})
	})
	t.Run("restores the access policy when request transformations are cleared", func(t *testing.T) {
		f := newFakeServer()
		defer f.Close()
		session := createTestSession(t, f, "sbx_rt4")
		if err := session.SetNetworkPolicy(context.Background(), harness.NetworkPolicy{Mode: harness.NetworkPolicyCustom, AllowedHosts: []string{"registry.npmjs.org"}}); err != nil {
			t.Fatal(err)
		}
		if err := session.SetRequestTransformations(context.Background(), []harness.RequestTransformation{legacyCredentialTransformation}); err != nil {
			t.Fatal(err)
		}
		if err := session.SetRequestTransformations(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		requirePolicyEqual(t, lastPolicy(t, f), NetworkPolicy{AllowList: []string{"registry.npmjs.org"}})
	})
	t.Run("delegates additive transformations to the policy manager", func(t *testing.T) {
		f := newFakeServer()
		defer f.Close()
		session := createTestSession(t, f, "sbx_rt5")
		add := harness.RequestTransformation{
			Match:     harness.RequestTransformationMatch{Host: "api.example.com"},
			Transform: harness.RequestTransformationTransform{Headers: map[string]string{"authorization": "Bearer secret"}},
		}
		if err := session.AddRequestTransformations(context.Background(), []harness.RequestTransformation{add}); err != nil {
			t.Fatal(err)
		}
		got := lastPolicy(t, f)
		rules := got.AllowMap["api.example.com"]
		if got.AllowMap == nil || len(got.AllowMap["*"]) != 0 || len(rules) != 1 || rules[0].Match != nil || len(rules[0].Transform) != 1 ||
			rules[0].Transform[0].Headers["authorization"] != "Bearer secret" {
			t.Fatalf("policy = %#v", got)
		}
	})
}
