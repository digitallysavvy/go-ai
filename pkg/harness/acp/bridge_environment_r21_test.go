package acp

import "testing"

// TS spreads sessionMeta / clientCapabilities / nonTextContentTypes under a
// `== null` check, so empty-but-defined values still reach the bridge.
func TestCreateBridgeEnvironment_EmptyDefinedValuesKept(t *testing.T) {
	env := createBridgeEnvironment(bridgeConfiguration{
		Authentication:      &Authentication{MethodID: "m", Meta: map[string]any{}, ClientCapabilities: map[string]any{}},
		SessionMeta:         map[string]any{},
		ClientCapabilities:  map[string]any{},
		NonTextContentTypes: []NonTextContentType{},
	})
	want := `{"agent":{"command":"","args":[]},"authentication":{"methodId":"m","meta":{},"clientCapabilities":{}},"sessionMeta":{},"clientCapabilities":{},"nonTextContentTypes":[]}`
	if got := env[BridgeConfigurationEnv]; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	env = createBridgeEnvironment(bridgeConfiguration{Authentication: &Authentication{MethodID: "m"}})
	if got, want := env[BridgeConfigurationEnv], `{"agent":{"command":"","args":[]},"authentication":{"methodId":"m"}}`; got != want {
		t.Fatalf("nil values must stay omitted: got %s want %s", got, want)
	}
}
