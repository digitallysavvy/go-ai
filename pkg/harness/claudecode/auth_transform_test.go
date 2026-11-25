package claudecode

import "testing"

func TestRequestTransformations_BrokersSubscriptionOAuthToken(t *testing.T) {
	env := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "real", "ANTHROPIC_BASE_URL": "https://api.anthropic.com"}
	sandboxEnv := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "placeholder", "ANTHROPIC_BASE_URL": "https://api.anthropic.com"}
	got, err := requestTransformations(env, sandboxEnv, AuthModeDirect)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("transformations = %d, want 1", len(got))
	}
	// No OAuth token on the sandbox side: nothing to broker.
	got, _ = requestTransformations(env, map[string]string{}, AuthModeDirect)
	if len(got) != 0 {
		t.Fatalf("transformations without sandbox token = %d, want 0", len(got))
	}
}
