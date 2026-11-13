package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// r87btoolsJSONEqual compares got (marshaled) with a TS JSON dump.
func r87btoolsJSONEqual(t *testing.T, name string, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(gotBytes, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s:\n got  %s\n want %s", name, gotBytes, wantJSON)
	}
}

// V061 item 42 and its class sweep: TS opencode-auth.ts tests credentials and
// URLs with `== null` / `??`, so a present empty value is used as given.
// Expected values are TS 3fab50aa9b output (vitest run of
// createOpenCodeRequestTransformations / resolveOpenCodeEnv).
func TestR87btoolsOpenCodeEmptyCredentialsFollowTSPresence(t *testing.T) {
	r87btoolsJSONEqual(t, "resolveEnv gitlab", resolveEnv(AuthenticationMode{Mode: harness.AuthModeAuto}, "", "gitlab", map[string]string{"GITLAB_TOKEN": ""}),
		`{"GITLAB_TOKEN":""}`)

	cases := []struct {
		name string
		in   createRequestTransformationsInput
		want string
	}{
		{"gitlab", createRequestTransformationsInput{Env: map[string]string{"GITLAB_TOKEN": ""}, SandboxEnv: map[string]string{"GITLAB_TOKEN": "ph"}, Auth: AuthGitLab},
			`[{"match":{"host":"gitlab.com","path":{"exact":"/api/v4/ai/third_party_agents/direct_access"},"method":["POST"],"headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer ph"}}]},"transform":{"headers":{"Authorization":"Bearer "}}}]`},
		{"xai", createRequestTransformationsInput{Env: map[string]string{"XAI_API_KEY": ""}, SandboxEnv: map[string]string{"XAI_API_KEY": "ph"}, Auth: AuthXAI},
			`[{"match":{"host":"api.x.ai","path":{"startsWith":"/v1"},"headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer ph"}}]},"transform":{"headers":{"Authorization":"Bearer "}}}]`},
		{"copilot", createRequestTransformationsInput{Env: map[string]string{"GITHUB_COPILOT_TOKEN": "", "GITHUB_TOKEN": "gh"}, SandboxEnv: map[string]string{"GITHUB_COPILOT_TOKEN": "ph", "GITHUB_TOKEN": "ph2"}, Auth: AuthGitHubCopilot},
			`[{"match":{"host":"api.githubcopilot.com","headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer ph"}}]},"transform":{"headers":{"Authorization":"Bearer "}}}]`},
		{"anthropic", createRequestTransformationsInput{Env: map[string]string{"ANTHROPIC_API_KEY": "", "ANTHROPIC_BASE_URL": "https://a.example/x"}, SandboxEnv: map[string]string{"ANTHROPIC_API_KEY": "ph"}, Auth: AuthAnthropic},
			`[{"match":{"host":"a.example","path":{"startsWith":"/x"},"headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"ph"}}]},"transform":{"headers":{"x-api-key":""}}}]`},
	}
	for _, tc := range cases {
		got, err := createOpenCodeRequestTransformations(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		r87btoolsJSONEqual(t, tc.name, got, tc.want)
	}

	// TS `OPENAI_BASE_URL ?? default`: a present empty base URL is parsed and
	// fails like `new URL('')`.
	_, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env: map[string]string{"OPENAI_API_KEY": "k", "OPENAI_BASE_URL": ""}, SandboxEnv: map[string]string{"OPENAI_API_KEY": "ph"}, Auth: AuthOpenAI,
	})
	if err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("empty OPENAI_BASE_URL error = %v, want Invalid URL", err)
	}

	// Absent credentials still return none (TS "omits GitLab transformations
	// when a credential is missing").
	for _, in := range []createRequestTransformationsInput{
		{Env: map[string]string{}, SandboxEnv: map[string]string{"GITLAB_TOKEN": "ph"}, Auth: AuthGitLab},
		{Env: map[string]string{"GITLAB_TOKEN": "s"}, SandboxEnv: map[string]string{}, Auth: AuthGitLab},
	} {
		got, err := createOpenCodeRequestTransformations(in)
		if err != nil || len(got) != 0 {
			t.Fatalf("missing credential: %+v, %v", got, err)
		}
	}
}

// The subscription half of the sweep: TS keeps `typeof x === 'string'`
// values from the auth store (accountId / enterpriseUrl "") and tests them
// with `== null` / `??`. Expected values are TS 3fab50aa9b output for the
// same auth.json.
func TestR87btoolsOpenCodeSubscriptionEmptyStoreFields(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := `{"github-copilot":{"type":"oauth","access":"a","refresh":"copilot-refresh","expires":99999999999999,"enterpriseUrl":""},
 "openai":{"type":"oauth","access":"oa","refresh":"r","expires":99999999999999,"accountId":""},
 "gitlab":{"type":"oauth","access":"gl","refresh":"r","expires":99999999999999,"enterpriseUrl":""}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ content, transformations string }{
		"github-copilot": {`{"github-copilot":{"type":"oauth","access":"sb","refresh":"sb","expires":9007199254740991,"enterpriseUrl":""}}`,
			`[{"match":{"host":"copilot-api.","headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer sb"}}]},"transform":{"headers":{"Authorization":"Bearer copilot-refresh"}}}]`},
		"openai": {`{"openai":{"type":"oauth","access":"sb","refresh":"host-managed","expires":9007199254740991,"accountId":""}}`,
			`[{"match":{"host":"chatgpt.com","path":{"startsWith":"/backend-api/codex"},"headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer sb"}}]},"transform":{"headers":{"Authorization":"Bearer oa"}}}]`},
		"gitlab": {`{"gitlab":{"type":"oauth","access":"sb","refresh":"host-managed","expires":9007199254740991,"enterpriseUrl":""}}`, ""},
	}
	for provider, w := range want {
		sub, err := readOpenCodeSubscriptionWithOptions(context.Background(), provider, map[string]string{}, subscriptionReadOptions{HomeDirectory: home, GOOS: "windows"})
		if err != nil || sub == nil {
			t.Fatalf("%s: %+v, %v", provider, sub, err)
		}
		content, err := createOpenCodeSubscriptionAuthContent(sub, "sb")
		if err != nil {
			t.Fatal(err)
		}
		if content != w.content {
			t.Errorf("%s content:\n got  %s\n want %s", provider, content, w.content)
		}
		got, err := createOpenCodeSubscriptionRequestTransformations(sub, "sb")
		if w.transformations == "" {
			// TS `enterpriseUrl ?? 'https://gitlab.com'` keeps "" and
			// `new URL('')` throws.
			if err == nil || err.Error() != "Invalid URL" {
				t.Errorf("%s transformations = %+v, %v; want Invalid URL", provider, got, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		r87btoolsJSONEqual(t, provider+" transformations", got, w.transformations)
	}
}

// TS `requestOpenCodeGitLabDirectAccess({ aiGatewayUrl = GITLAB_AI_GATEWAY_URL })`
// applies the default only when the value is absent; harness.go passes
// `process.env.GITLAB_AI_GATEWAY_URL` when it is `!= null`, so a present
// empty value yields an empty gateway URL (TS replaces trailing slashes on the empty string).
func TestR87btoolsGitLabDirectAccessEmptyGatewayURLIsKept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"t","headers":{}}`))
	}))
	defer server.Close()
	got, err := requestOpenCodeGitLabDirectAccessWith(context.Background(), server.Client(), "tok", server.URL, true, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.AIGatewayURL != "" {
		t.Fatalf("AIGatewayURL = %q, want empty (present empty value kept)", got.AIGatewayURL)
	}
	got, err = requestOpenCodeGitLabDirectAccessWith(context.Background(), server.Client(), "tok", server.URL, true, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.AIGatewayURL != gitlabAIGatewayURL {
		t.Fatalf("AIGatewayURL = %q, want the default when absent", got.AIGatewayURL)
	}
}
