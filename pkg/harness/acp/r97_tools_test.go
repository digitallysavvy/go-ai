package acp

import (
	"math"
	"testing"
)

// TS createACPBridgeEnvironment serializes the configuration with
// JSON.stringify: a non-finite number in sessionMeta (or clientCapabilities,
// or the provider env) is null. encoding/json failed the value and
// createBridgeEnvironment panicked (ruling 4, review round 97).
func TestR97toolsBridgeEnvironmentNonFinite(t *testing.T) {
	env := createBridgeEnvironment(bridgeConfiguration{
		Agent:       agentLaunch{Command: "agent"},
		SessionMeta: map[string]any{"n": math.Inf(1), "s": "a&b"},
	})
	want := `{"agent":{"command":"agent","args":[]},"sessionMeta":{"n":null,"s":"a&b"}}`
	if got := env[BridgeConfigurationEnv]; got != want {
		t.Fatalf("env = %s\nwant %s", got, want)
	}
}

// TS resolveACPLaunchEnvironment writes a non-string profile value with
// JSON.stringify: <, > and & stay literal and a non-finite number is null
// (Go escaped & as & and failed on NaN; review round 97).
func TestR97toolsLaunchEnvironmentJSONStringify(t *testing.T) {
	out, err := resolveLaunchEnvironment(map[string]any{
		"CFG": map[string]any{"url": "https://x/?a=1&b=<2>"},
		"N":   math.NaN(),
	}, gatewayValues{})
	if err != nil {
		t.Fatal(err)
	}
	if out["CFG"] != `{"url":"https://x/?a=1&b=<2>"}` || out["N"] != "null" {
		t.Fatalf("env = %#v", out)
	}
}

// TS resolveACPProfileValue treats any string $source as a source; an unknown
// one resolves to undefined, which `${prefix}${sourceValue}` writes as
// "undefined" (Go wrote "", review round 97).
func TestR97toolsProfileUnknownSource(t *testing.T) {
	got := resolveProfileValue(map[string]any{"$source": "gateway-token", "prefix": "Bearer "}, gatewayValues{APIKey: "k"})
	if got != "Bearer undefined" {
		t.Fatalf("resolved = %#v, want \"Bearer undefined\"", got)
	}
}
