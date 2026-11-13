package vercel

import (
	"encoding/json"
	"testing"
)

// @vercel/sandbox parses the API's networkPolicy with its zod schema, which
// matches object keys exactly: `Subnets` / `ForwardURL`-cased variants are
// not the fields (review round 95, guard extension to pkg/harness/sandbox).
func TestR95integNetworkPolicyKeysMatchExactly(t *testing.T) {
	var p NetworkPolicy
	if err := json.Unmarshal([]byte(`{"allow":{"a.com":[{"forwardurl":"https://x"}]},"Subnets":{"allow":["10.0.0.0/8"]}}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Subnets != nil || len(p.AllowMap["a.com"]) != 1 || p.AllowMap["a.com"][0].ForwardURL != "" {
		t.Fatalf("policy = %+v, want only the exact keys", p)
	}
}
