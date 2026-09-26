package bridge

import (
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port of harness/src/utils/bridge-token.test.ts.

var hexToken64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TS: "creates unique random 32-byte hexadecimal tokens"
func TestCreateBridgeTokenUniqueHex(t *testing.T) {
	first := CreateBridgeToken()
	second := CreateBridgeToken()

	if !hexToken64.MatchString(first) {
		t.Fatalf("first token %q does not match /^[0-9a-f]{64}$/", first)
	}
	if !hexToken64.MatchString(second) {
		t.Fatalf("second token %q does not match /^[0-9a-f]{64}$/", second)
	}
	if second == first {
		t.Fatalf("expected distinct tokens, got the same value twice: %q", first)
	}
}

// TS: "adds an encoded bridge token while preserving the endpoint"
func TestWithBridgeTokenAddsEncodedToken(t *testing.T) {
	endpoint := harness.PortEndpoint{
		URL:     "wss://sandbox.example/bridge?existing=value",
		Headers: map[string]string{"x-access-token": "traffic-token"},
	}

	out, err := WithBridgeToken(endpoint, "token with special characters &?")
	if err != nil {
		t.Fatalf("WithBridgeToken: %v", err)
	}
	wantURL := "wss://sandbox.example/bridge?existing=value&agent_bridge_token=token+with+special+characters+%26%3F"
	if out.URL != wantURL {
		t.Fatalf("URL = %q, want %q", out.URL, wantURL)
	}
	if out.Headers["x-access-token"] != "traffic-token" {
		t.Fatalf("headers not preserved: %+v", out.Headers)
	}
	// The input endpoint must not be mutated.
	if endpoint.URL != "wss://sandbox.example/bridge?existing=value" {
		t.Fatalf("input endpoint URL mutated: %q", endpoint.URL)
	}
}

// TS: "replaces an existing bridge token"
func TestWithBridgeTokenReplacesExisting(t *testing.T) {
	endpoint := harness.PortEndpoint{URL: "wss://sandbox.example/bridge?agent_bridge_token=old-token"}
	out, err := WithBridgeToken(endpoint, "new-token")
	if err != nil {
		t.Fatalf("WithBridgeToken: %v", err)
	}
	want := "wss://sandbox.example/bridge?agent_bridge_token=new-token"
	if out.URL != want {
		t.Fatalf("URL = %q, want %q", out.URL, want)
	}
	if len(out.Headers) != 0 {
		t.Fatalf("expected no headers, got %+v", out.Headers)
	}
}
