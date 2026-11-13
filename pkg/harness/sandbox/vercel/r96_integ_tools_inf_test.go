package vercel

import (
	"encoding/json"
	"math"
	"testing"
)

// A response rule read back from the API with an out-of-range number: TS
// JSON.parse gives Infinity and stableSerialize / JSON.stringify write null;
// Go failed the decode, and then Equal and MarshalJSON.
func TestR96integToolsNetworkPolicyInfinity(t *testing.T) {
	var p NetworkPolicy
	if err := json.Unmarshal([]byte(`{"allow":{"a.com":[{"response":{"statusCode":1e400}}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.AllowMap["a.com"][0].Response["statusCode"] != math.Inf(1) {
		t.Fatalf("response = %#v, want +Inf", p.AllowMap["a.com"][0].Response)
	}
	out, err := p.MarshalJSON()
	if err != nil || string(out) != `{"allow":{"a.com":[{"response":{"statusCode":null}}]}}` {
		t.Fatalf("marshal = %s, %v", out, err)
	}
	if !p.Equal(p) {
		t.Fatal("Equal(p, p) = false")
	}
}
