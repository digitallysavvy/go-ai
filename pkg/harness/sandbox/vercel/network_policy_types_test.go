package vercel

import (
	"encoding/json"
	"testing"
)

func TestNetworkPolicyMarshalBareMode(t *testing.T) {
	b, err := json.Marshal(AllowAllNetworkPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"allow-all"` {
		t.Fatalf("expected bare string, got %s", b)
	}
	b, err = json.Marshal(DenyAllNetworkPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"deny-all"` {
		t.Fatalf("expected bare string, got %s", b)
	}
}

func TestNetworkPolicyRoundTripAllowListAndSubnets(t *testing.T) {
	in := NetworkPolicy{AllowList: []string{"api.example.com"}, Subnets: &PolicySubnets{Allow: []string{"10.0.0.0/8"}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out NetworkPolicy
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !in.Equal(out) {
		t.Fatalf("round trip mismatch: %#v vs %#v", in, out)
	}
}

func TestNetworkPolicyRoundTripAllowMap(t *testing.T) {
	in := NetworkPolicy{AllowMap: map[string][]PolicyRule{
		"api.example.com": {{ForwardURL: "https://proxy.example.com"}},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out NetworkPolicy
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !in.Equal(out) {
		t.Fatalf("round trip mismatch: %#v vs %#v", in, out)
	}
}

func TestNetworkPolicyEqualIgnoresKeyOrder(t *testing.T) {
	a := NetworkPolicy{AllowMap: map[string][]PolicyRule{"a": {}, "b": {}}}
	b := NetworkPolicy{AllowMap: map[string][]PolicyRule{"b": {}, "a": {}}}
	if !a.Equal(b) {
		t.Fatal("expected maps with different insertion order to be equal")
	}
}

func TestNetworkPolicyNotEqualDifferentContent(t *testing.T) {
	a := AllowAllNetworkPolicy()
	b := DenyAllNetworkPolicy()
	if a.Equal(b) {
		t.Fatal("expected allow-all != deny-all")
	}
}
