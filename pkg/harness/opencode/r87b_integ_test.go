package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// Isolated test for tools 4ce90c2e (TS 3fab50aa9b opencode-harness.ts
// 537-557, `subscriptionAccessToken == null`): a forwarded empty subscription
// access token still writes OPENCODE_AUTH_CONTENT, and the token variable
// itself is never forwarded. Expected content from bun running the TS
// createOpenCodeSubscriptionAuthContent with accessToken "".
func TestR87bintegBridgeAuthEnvironmentEmptyTokenWritesAuthContent(t *testing.T) {
	sub := &Subscription{ProviderID: SubscriptionOpenAI, AccessToken: "host", AccountID: "acc"}
	for _, tc := range []struct {
		name      string
		forwarded map[string]string
		sub       *Subscription
		brokered  bool
		want      string // "" = no OPENCODE_AUTH_CONTENT
	}{
		{"empty token", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "", "K": "v"}, sub, false,
			`{"openai":{"type":"oauth","access":"","refresh":"host-managed","expires":9007199254740991,"accountId":"acc"}}`},
		{"opencode-go empty token", map[string]string{SubscriptionAccessTokenEnvironmentVariable: ""}, &Subscription{ProviderID: SubscriptionOpenCodeGo, AccessToken: "host"}, false,
			`{"opencode-go":{"type":"api","key":""}}`},
		{"absent token", map[string]string{"K": "v"}, sub, false, ""},
		{"no subscription", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "t"}, nil, false, ""},
		{"gitlab brokered", map[string]string{SubscriptionAccessTokenEnvironmentVariable: "t"}, sub, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := openCodeBridgeAuthEnvironment(tc.forwarded, tc.sub, tc.brokered)
			if err != nil {
				t.Fatal(err)
			}
			if _, leaked := env[SubscriptionAccessTokenEnvironmentVariable]; leaked {
				t.Fatalf("access token variable forwarded: %v", env)
			}
			got, ok := env["OPENCODE_AUTH_CONTENT"]
			if tc.want == "" {
				if ok {
					t.Fatalf("OPENCODE_AUTH_CONTENT = %s, want none", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("OPENCODE_AUTH_CONTENT = %s, want %s", got, tc.want)
			}
		})
	}
}

// r87bintegUnsetenv unsets name for the test and restores it afterwards.
func r87bintegUnsetenv(t *testing.T, name string) {
	t.Helper()
	if old, ok := os.LookupEnv(name); ok {
		t.Cleanup(func() { _ = os.Setenv(name, old) })
		_ = os.Unsetenv(name)
	}
}

// tools 6d828e8e, DoStart site (TS 3fab50aa9b opencode-harness.ts
// `sandboxSubscriptionAccessToken != null`): a GitLab subscription whose
// sandbox access token is present but empty (here a CredentialForwarding hook
// that returns "") still takes the GitLab direct-access branch, as in TS.
func TestR87bintegGitLabEmptySandboxSubscriptionTokenUsesDirectAccess(t *testing.T) {
	var directAccessCalls atomic.Int32
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/ai/third_party_agents/direct_access" {
			directAccessCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"direct","headers":{"x-gitlab-instance-id":"i"}}`))
	}))
	defer gitlab.Close()
	for _, name := range []string{"GITLAB_TOKEN", "GITLAB_INSTANCE_URL", "GITLAB_AI_GATEWAY_URL", "XDG_DATA_HOME", "AI_GATEWAY_API_KEY"} {
		r87bintegUnsetenv(t, name)
	}
	t.Setenv("OPENCODE_AUTH_CONTENT", `{"gitlab":{"type":"oauth","access":"gl","refresh":"r","expires":99999999999999,"enterpriseUrl":"`+gitlab.URL+`"}}`)

	const token = "r87b-gitlab-token"
	srv := newServer(t, token, func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{
		MintBridgeToken: func(string) string { return token },
		Provider:        "gitlab",
		CredentialForwarding: func(_ context.Context, opts harness.CredentialForwardingOptions) (string, error) {
			if opts.EnvironmentVariableName == SubscriptionAccessTokenEnvironmentVariable {
				return "", nil
			}
			return opts.Credential, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/workspace/project", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if n := directAccessCalls.Load(); n != 1 {
		t.Fatalf("direct access requests = %d, want 1", n)
	}
}

// Survivors of the 6d828e8e line mutants. Expected values from bun running
// the TS 3fab50aa9b createOpenCodeRequestTransformations /
// hasOpenCodeCredential: ANTHROPIC_AUTH_TOKEN "" (`!= null`) builds the
// bearer rule; ANTHROPIC_BASE_URL / XAI_BASE_URL "" (`??`) are parsed and
// fail like new URL(""); AI_GATEWAY_API_KEY "" is a credential (`!= null`).
func TestR87bintegOpenCodePresenceSurvivors(t *testing.T) {
	got, err := createOpenCodeRequestTransformations(createRequestTransformationsInput{
		Env: map[string]string{"ANTHROPIC_AUTH_TOKEN": ""}, SandboxEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "ph"}, Auth: AuthAnthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	r87btoolsJSONEqual(t, "anthropic auth token", got,
		`[{"match":{"host":"api.anthropic.com","headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer ph"}}]},"transform":{"headers":{"Authorization":"Bearer "}}}]`)
	for _, in := range []createRequestTransformationsInput{
		{Env: map[string]string{"ANTHROPIC_API_KEY": "k", "ANTHROPIC_BASE_URL": ""}, SandboxEnv: map[string]string{"ANTHROPIC_API_KEY": "ph"}, Auth: AuthAnthropic},
		{Env: map[string]string{"XAI_API_KEY": "k", "XAI_BASE_URL": ""}, SandboxEnv: map[string]string{"XAI_API_KEY": "ph"}, Auth: AuthXAI},
	} {
		if _, err := createOpenCodeRequestTransformations(in); err == nil || err.Error() != "Invalid URL" {
			t.Fatalf("%v: err = %v, want Invalid URL", in.Env, err)
		}
	}
	if !hasOpenCodeCredential(map[string]string{"AI_GATEWAY_API_KEY": ""}, AuthAIGateway) || hasOpenCodeCredential(map[string]string{}, AuthAIGateway) {
		t.Fatal("hasOpenCodeCredential(ai-gateway) does not follow AI_GATEWAY_API_KEY != null")
	}
}

// tools 6d828e8e DoStart site: TS passes `instanceUrl` / `aiGatewayUrl`
// when `enterpriseUrl != null` / `GITLAB_AI_GATEWAY_URL != null`, so a
// present empty value is used as given; an absent one takes the default.
func TestR87bintegGitLabDirectAccessInputsPresence(t *testing.T) {
	got := openCodeGitLabDirectAccessInputs(&Subscription{ProviderID: SubscriptionGitLab, enterpriseURLSet: true}, map[string]string{"GITLAB_AI_GATEWAY_URL": ""})
	if got != (gitLabDirectAccessInputs{instanceURLSet: true, aiGatewayURLSet: true}) {
		t.Fatalf("present empty: %+v", got)
	}
	got = openCodeGitLabDirectAccessInputs(&Subscription{ProviderID: SubscriptionGitLab, EnterpriseURL: "https://g"}, map[string]string{})
	if got != (gitLabDirectAccessInputs{instanceURL: "https://g", instanceURLSet: true}) {
		t.Fatalf("absent gateway: %+v", got)
	}
	if got := openCodeGitLabDirectAccessInputs(&Subscription{ProviderID: SubscriptionGitLab}, nil); got != (gitLabDirectAccessInputs{}) {
		t.Fatalf("absent: %+v", got)
	}
}
