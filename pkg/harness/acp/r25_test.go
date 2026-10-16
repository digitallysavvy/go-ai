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
