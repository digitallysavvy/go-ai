package opencode

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func r88toolsAwait(t *testing.T, c harness.PromptControl) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish")
	}
}

// TS 3af9d06557: every prompt start frame carries readableSkillRoots (`[]`
// without skills, opencode-harness.ts 1035-1037); compact never does.
func TestR88toolsOpenCodeReadableSkillRoots(t *testing.T) {
	const token = "r88-roots-token"
	starts := make(chan map[string]any, 4)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		starts <- start
		turn.Emit(finishPayload())
	})
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/vercel/sandbox/s", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Skills: []harness.Skill{{Name: "demo", Description: "Demo skill.", Content: "Read reference.md."}}},
		Prompt:       harness.TextPrompt("Use demo."), Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	r88toolsAwait(t, c)
	if got := (<-starts)["readableSkillRoots"]; !reflect.DeepEqual(got, []any{"/home/vercel-sandbox/.agents/skills/demo"}) {
		t.Fatalf("readableSkillRoots = %#v", got)
	}

	c, err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("hi"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	r88toolsAwait(t, c)
	if got, ok := (<-starts)["readableSkillRoots"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("readableSkillRoots without skills = %#v (present %v), want []", got, ok)
	}

	if err := sess.DoCompact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got, ok := (<-starts)["readableSkillRoots"]; ok {
		t.Fatalf("compact start carries readableSkillRoots %#v", got)
	}
}

// TS a54f0fd8c6 opencode-harness.test.ts: the flag follows --skills-dir.
func TestR88toolsOpenCodeHostDisconnectTimeout(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		want    string
	}{{60 * time.Second, " --host-disconnect-timeout-ms '60000'"}, {0, ""}} {
		srv := bridgetest.NewServer(bridgetest.Options{AnyToken: true})
		t.Cleanup(srv.Close)
		sandbox := newFakeSandbox(srv)
		h, err := CreateOpenCode(Settings{HostDisconnectTimeout: tc.timeout})
		if err != nil {
			t.Fatal(err)
		}
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s", SessionWorkDir: "/vercel/sandbox/s", SandboxSession: sandbox})
		if err == nil {
			_ = sess.DoDestroy(context.Background())
		}
		sandbox.mu.Lock()
		if len(sandbox.spawnCommands) == 0 {
			sandbox.mu.Unlock()
			t.Fatalf("no spawn (DoStart: %v)", err)
		}
		cmd := sandbox.spawnCommands[0]
		sandbox.mu.Unlock()
		if tc.want != "" && !strings.HasSuffix(cmd, "/.agents/skills'"+tc.want) {
			t.Fatalf("command = %q", cmd)
		}
		if tc.want == "" && strings.Contains(cmd, "host-disconnect") {
			t.Fatalf("command = %q", cmd)
		}
	}
}

// TS a97f49029f opencode-auth.test.ts: auth "direct".
func TestR88toolsOpenCodeDirectAuth(t *testing.T) {
	direct := AuthenticationMode{Mode: harness.AuthModeDirect}
	env := map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "VERCEL_OIDC_TOKEN": "oidc-token", "GITHUB_TOKEN": "github-token", "OPENAI_API_KEY": "openai-key"}
	if got := resolveEnv(direct, "", "github-copilot", env); !reflect.DeepEqual(got, map[string]string{"GITHUB_TOKEN": "github-token"}) {
		t.Fatalf("env = %v", got)
	}
	if got := resolveAuthenticationMode(direct, "", "github-copilot", env); got != AuthGitHubCopilot {
		t.Fatalf("mode = %q", got)
	}
	env = map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token", "ANTHROPIC_API_KEY": "anthropic-key"}
	if got := resolveEnv(direct, "", "", env); !reflect.DeepEqual(got, map[string]string{"ANTHROPIC_API_KEY": "anthropic-key"}) {
		t.Fatalf("default env = %v", got)
	}
	if got := resolveAuthenticationMode(direct, "", "", env); got != AuthAnthropic {
		t.Fatalf("default mode = %q", got)
	}
	for _, p := range []string{"xai", "github-copilot", "poe", "opencode-go", "gitlab"} {
		if got := resolveAuthenticationMode(direct, "", p, map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "VERCEL_OIDC_TOKEN": "oidc-token"}); string(got) != p {
			t.Fatalf("%s: mode = %q", p, got)
		}
	}
}

// TS a97f49029f resolveOpenCodeAuthentication cases.
func TestR88toolsOpenCodeDirectAuthSubscription(t *testing.T) {
	direct := AuthenticationMode{Mode: harness.AuthModeDirect}
	for _, p := range []string{"openai", "xai", "github-copilot", "poe", "opencode-go", "gitlab"} {
		env := map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "VERCEL_OIDC_TOKEN": "oidc-token"}
		var calls []string
		got, err := resolveOpenCodeAuthentication(context.Background(), direct, "", p, env, func(_ context.Context, id string, e map[string]string) (*Subscription, error) {
			calls = append(calls, id)
			if !reflect.DeepEqual(e, env) {
				t.Fatalf("subscription env = %v", e)
			}
			return &Subscription{ProviderID: SubscriptionProvider(id), AccessToken: "subscription-token"}, nil
		})
		if err != nil || string(got.AuthenticationMode) != p || got.Subscription == nil ||
			!reflect.DeepEqual(got.Environment, map[string]string{SubscriptionAccessTokenEnvironmentVariable: "subscription-token"}) {
			t.Fatalf("%s: %+v, %v", p, got, err)
		}
		if !reflect.DeepEqual(calls, []string{p}) {
			t.Fatalf("%s: reads = %v", p, calls)
		}
	}

	read := func(context.Context, string, map[string]string) (*Subscription, error) {
		t.Fatal("subscription read")
		return nil, nil
	}
	got, err := resolveOpenCodeAuthentication(context.Background(), direct, "", "xai", map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token", "XAI_API_KEY": "environment-key"}, read)
	if err != nil || got.AuthenticationMode != AuthXAI || !reflect.DeepEqual(got.Environment, map[string]string{"XAI_API_KEY": "environment-key"}) {
		t.Fatalf("api key: %+v, %v", got, err)
	}
	for _, auth := range []AuthenticationMode{{}, {Mode: "auto"}} {
		got, err := resolveOpenCodeAuthentication(context.Background(), auth, "", "github-copilot", map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token"}, read)
		want := map[string]string{"AI_GATEWAY_API_KEY": "oidc-token", "AI_GATEWAY_BASE_URL": "https://ai-gateway.vercel.sh/v1"}
		if err != nil || got.AuthenticationMode != AuthAIGateway || !reflect.DeepEqual(got.Environment, want) {
			t.Fatalf("%q: %+v, %v", auth.Mode, got, err)
		}
	}
}
