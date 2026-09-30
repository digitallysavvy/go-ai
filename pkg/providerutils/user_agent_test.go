package providerutils

import (
	"runtime"
	"strings"
	"testing"
)

// Ported from packages/provider-utils/src/with-user-agent-suffix.test.ts.

// "should create a new user-agent header when no existing user-agent exists"
func TestWithUserAgentSuffixCreatesHeaderWhenMissing(t *testing.T) {
	headers := map[string]string{
		"content-type":  "application/json",
		"authorization": "Bearer token123",
	}

	result := WithUserAgentSuffix(headers, "ai-sdk/0.0.0-test", "provider/test-openai")

	if got, want := result["user-agent"], "ai-sdk/0.0.0-test provider/test-openai"; got != want {
		t.Errorf("user-agent = %q, want %q", got, want)
	}
	if result["content-type"] != "application/json" {
		t.Errorf("content-type = %q, want application/json", result["content-type"])
	}
	if result["authorization"] != "Bearer token123" {
		t.Errorf("authorization = %q, want Bearer token123", result["authorization"])
	}
}

// "should append suffix parts to existing user-agent header"
func TestWithUserAgentSuffixAppendsToExisting(t *testing.T) {
	headers := map[string]string{
		"user-agent": "TestApp/0.0.0-test",
		"accept":     "application/json",
	}

	result := WithUserAgentSuffix(headers, "ai-sdk/0.0.0-test", "provider/test-anthropic")

	if got, want := result["user-agent"], "TestApp/0.0.0-test ai-sdk/0.0.0-test provider/test-anthropic"; got != want {
		t.Errorf("user-agent = %q, want %q", got, want)
	}
	if result["accept"] != "application/json" {
		t.Errorf("accept = %q, want application/json", result["accept"])
	}
}

// "should automatically remove undefined entries from headers"
//
// TS exercises Headers/undefined/null filtering that a Go map[string]string
// cannot represent (a Go map either has a key with a string value, or lacks
// the key entirely — there is no "present but undefined" state). The
// closest equivalent for Go is: a header simply absent from the input map
// does not appear in the output, and appending an empty suffix part is a
// no-op. Case-insensitive-duplicate collapsing (the part of this scenario
// that *does* carry over) is covered by
// TestWithUserAgentSuffixCaseInsensitiveHeaderKeys below.
func TestWithUserAgentSuffixOmitsAbsentHeaders(t *testing.T) {
	headers := map[string]string{
		"content-type": "application/json",
		"user-agent":   "TestApp/0.0.0-test",
		"accept":       "application/json",
	}

	result := WithUserAgentSuffix(headers, "ai-sdk/0.0.0-test")

	if got, want := result["user-agent"], "TestApp/0.0.0-test ai-sdk/0.0.0-test"; got != want {
		t.Errorf("user-agent = %q, want %q", got, want)
	}
	if result["content-type"] != "application/json" {
		t.Errorf("content-type = %q, want application/json", result["content-type"])
	}
	if result["accept"] != "application/json" {
		t.Errorf("accept = %q, want application/json", result["accept"])
	}
	if _, ok := result["authorization"]; ok {
		t.Errorf("authorization should be absent, got %q", result["authorization"])
	}
	if _, ok := result["cache-control"]; ok {
		t.Errorf("cache-control should be absent, got %q", result["cache-control"])
	}
}

// "should preserve headers when given a Headers instance" /
// "should handle array header entries"
//
// TS covers alternate HeadersInit shapes (a Headers instance, an array of
// tuples); Go's signature only accepts map[string]string, so both reduce to:
// arbitrarily-cased input keys come back lowercased.
func TestWithUserAgentSuffixLowercasesKeys(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer token123",
		"X-Custom":      "value",
	}

	result := WithUserAgentSuffix(headers, "ai-sdk/0.0.0-test")

	if result["authorization"] != "Bearer token123" {
		t.Errorf("authorization = %q, want Bearer token123", result["authorization"])
	}
	if result["x-custom"] != "value" {
		t.Errorf("x-custom = %q, want value", result["x-custom"])
	}
	if result["user-agent"] != "ai-sdk/0.0.0-test" {
		t.Errorf("user-agent = %q, want ai-sdk/0.0.0-test", result["user-agent"])
	}
}

// Go-specific: a caller-supplied map can hold two differently-cased spellings
// of the same header at once (impossible for TS's Headers-backed input).
// Resolution should be deterministic rather than depend on Go's randomized
// map iteration order.
func TestWithUserAgentSuffixCaseInsensitiveHeaderKeys(t *testing.T) {
	for i := 0; i < 20; i++ {
		headers := map[string]string{
			"User-Agent": "First/1.0",
			"user-agent": "Second/1.0",
		}
		result := WithUserAgentSuffix(headers, "ai-sdk/0.0.0-test")
		if got, want := result["user-agent"], "Second/1.0 ai-sdk/0.0.0-test"; got != want {
			t.Fatalf("run %d: user-agent = %q, want %q (deterministic last-sorted-key wins)", i, got, want)
		}
	}
}

func TestWithUserAgentSuffixNoPartsKeepsExistingHeader(t *testing.T) {
	headers := map[string]string{"user-agent": "TestApp/1.0"}
	result := WithUserAgentSuffix(headers)
	if result["user-agent"] != "TestApp/1.0" {
		t.Errorf("user-agent = %q, want TestApp/1.0", result["user-agent"])
	}
}

func TestWithUserAgentSuffixEmptyPartsFiltered(t *testing.T) {
	result := WithUserAgentSuffix(nil, "", "ai-sdk/openai/1.0", "")
	if result["user-agent"] != "ai-sdk/openai/1.0" {
		t.Errorf("user-agent = %q, want ai-sdk/openai/1.0 (empty parts filtered, no stray spaces)", result["user-agent"])
	}
}

func TestWithUserAgentSuffixNilHeaders(t *testing.T) {
	result := WithUserAgentSuffix(nil, "ai-sdk/openai/1.0")
	if result["user-agent"] != "ai-sdk/openai/1.0" {
		t.Errorf("user-agent = %q, want ai-sdk/openai/1.0", result["user-agent"])
	}
}

// Ported from get-runtime-environment-user-agent.ts's Go analogue: Go has a
// single runtime, so RuntimeEnvironmentUserAgent should always report it via
// runtime.Version(), matching the documented "runtime/go/go1.25.1" shape.
func TestRuntimeEnvironmentUserAgent(t *testing.T) {
	got := RuntimeEnvironmentUserAgent()
	want := "runtime/go/" + runtime.Version()
	if got != want {
		t.Fatalf("RuntimeEnvironmentUserAgent() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "runtime/go/go") {
		t.Fatalf("RuntimeEnvironmentUserAgent() = %q, want prefix runtime/go/go", got)
	}
}
