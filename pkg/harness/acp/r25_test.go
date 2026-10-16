package acp

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

func TestR25EmptyPnpmWorkspaceYAMLRejected(t *testing.T) {
	i := tsLocked()
	i.Source.PnpmWorkspaceYAMLSet = true
	err := validateImplementation(i)
	if err == nil || !strings.Contains(err.Error(), "ACP source.pnpmWorkspaceYaml must not be empty.") {
		t.Fatalf("err = %v", err)
	}
	i.Source.PnpmWorkspaceYAMLSet = false
	if err := validateImplementation(i); err != nil {
		t.Fatal(err)
	}
	i.Source.PnpmWorkspaceYAMLSet = true
	i.Source.PnpmWorkspaceYAML = "x: 1\n"
	if err := validateImplementation(i); err != nil {
		t.Fatal(err)
	}
}

func TestR25CompatibilityBaseURLEmptyKept(t *testing.T) {
	pa := &ProviderAuthentication{GatewayEnv: tsGatewayEnv}
	c := resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa, map[string]string{"AI_GATEWAY_API_KEY": "k", "AI_GATEWAY_BASE_URL": ""})
	if c.BaseURL != "" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	c = resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa, map[string]string{"AI_GATEWAY_API_KEY": "k"})
	if c.BaseURL != defaultAIGatewayBaseURL {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

// A non-marker bridge error is rejected as the raw wire object in TS; its text
// is JSON.stringify of the object, not Go's map formatting.
func TestDeserializeBridgeErrorRendersObjectAsJSON(t *testing.T) {
	err := deserializeBridgeError(map[string]any{"name": "Error", "message": "boom"}, "h")
	if got := err.Error(); got != `{"message":"boom","name":"Error"}` {
		t.Fatalf("error text = %q", got)
	}
}

// Error text quotes values with JSON.stringify, not Go %q: a control character
// is \u0001 (not \x01) and U+2028 stays literal.
func TestImplementationErrorQuotesLikeJSONStringify(t *testing.T) {
	err := validateImplementation(implementation{
		Source:     Source{Type: SourceInstallCommand, Command: "x"},
		Executable: "a\x01 ",
	})
	want := "ACP executable must be a bare command name without a path; received \"a\\u0001 \"."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
