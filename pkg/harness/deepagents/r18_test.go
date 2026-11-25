package deepagents

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS `DeepAgentsAuthenticationMode = HarnessV1Authentication<'anthropic'>`:
// the adapter choice is "anthropic" (not "direct"), pinning the Anthropic
// route even when an AI Gateway key is present.
func TestAnthropicAuthModePinsDirectRoute(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gw", "ANTHROPIC_API_KEY": "ak"}
	for _, mode := range []string{AuthModeAnthropic, harness.AuthModeDirect} {
		auth := harness.AuthMode(mode)
		if got := resolveAuthenticationMode(auth, env); got != AuthAnthropic {
			t.Errorf("mode %q resolved to %q", mode, got)
		}
		got := resolveEnv(auth, env)
		if got["ANTHROPIC_API_KEY"] != "ak" || got["AI_GATEWAY_API_KEY"] != "" {
			t.Errorf("mode %q env = %v", mode, got)
		}
	}
	if got := resolveAuthenticationMode(harness.AuthMode(""), env); got != AuthAIGateway {
		t.Errorf("auto with gateway key resolved to %q", got)
	}
}
