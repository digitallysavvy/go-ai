package claudecode

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

func noHelper() string { return "" }

// Replays the data-driven cases of TS claude-code-auth.test.ts
// `resolveClaudeCodeEnv` / `resolveClaudeCodeAuthenticationMode`.
func TestTSReplay_ResolveClaudeCodeEnv(t *testing.T) {
	ctx := context.Background()
	gateway := func(env map[string]string) map[string]string {
		return pickGateway(harnessutil.GetAIGatewayAuthFromEnv(env))
	}
	autoDetect := func(env map[string]string, helper func() string) map[string]string {
		gw := harnessutil.GetAIGatewayAuthFromEnv(env)
		if gw.APIKey != "" {
			return pickGateway(gw)
		}
		out, err := pickAnthropic(ctx, env, helper, false, "")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	direct := func(env map[string]string, helper func() string) map[string]string {
		out, err := pickAnthropic(ctx, env, helper, false, harness.AuthModeDirect)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	cases := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{"pins direct anthropic auth when selected",
			direct(map[string]string{"ANTHROPIC_API_KEY": "sk-direct", "AI_GATEWAY_API_KEY": "gw-key"}, noHelper),
			map[string]string{"ANTHROPIC_API_KEY": "sk-direct"}},
		{"uses ANTHROPIC_* env in direct mode",
			direct(map[string]string{"ANTHROPIC_API_KEY": "sk-process", "ANTHROPIC_BASE_URL": "https://api.example.com"}, noHelper),
			map[string]string{"ANTHROPIC_API_KEY": "sk-process", "ANTHROPIC_BASE_URL": "https://api.example.com"}},
		{"routes through the gateway when gateway mode is selected",
			gateway(map[string]string{"AI_GATEWAY_API_KEY": "gw-explicit"}),
			map[string]string{"AI_GATEWAY_API_KEY": "gw-explicit", "ANTHROPIC_API_KEY": "gw-explicit", "AI_GATEWAY_BASE_URL": "https://ai-gateway.vercel.sh", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}},
		{"uses gateway auth from the environment",
			gateway(map[string]string{"AI_GATEWAY_BASE_URL": "https://gw.example", "VERCEL_OIDC_TOKEN": "oidc-env"}),
			map[string]string{"AI_GATEWAY_API_KEY": "oidc-env", "ANTHROPIC_API_KEY": "oidc-env", "AI_GATEWAY_BASE_URL": "https://gw.example", "ANTHROPIC_BASE_URL": "https://gw.example"}},
		{"auto-detects gateway when AI_GATEWAY_API_KEY is set",
			autoDetect(map[string]string{"AI_GATEWAY_API_KEY": "gw-auto"}, noHelper),
			map[string]string{"AI_GATEWAY_API_KEY": "gw-auto", "ANTHROPIC_API_KEY": "gw-auto", "AI_GATEWAY_BASE_URL": "https://ai-gateway.vercel.sh", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}},
		{"auto-detects gateway when VERCEL_OIDC_TOKEN is set",
			autoDetect(map[string]string{"VERCEL_OIDC_TOKEN": "oidc-auto"}, noHelper),
			map[string]string{"AI_GATEWAY_API_KEY": "oidc-auto", "ANTHROPIC_API_KEY": "oidc-auto", "AI_GATEWAY_BASE_URL": "https://ai-gateway.vercel.sh", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}},
		{"auto-detects direct anthropic when only ANTHROPIC_API_KEY is set",
			autoDetect(map[string]string{"ANTHROPIC_API_KEY": "sk-auto"}, noHelper),
			map[string]string{"ANTHROPIC_API_KEY": "sk-auto"}},
		{"forwards host ANTHROPIC_BASE_URL alongside the api key",
			autoDetect(map[string]string{"ANTHROPIC_API_KEY": "sk-auto", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}, noHelper),
			map[string]string{"ANTHROPIC_API_KEY": "sk-auto", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}},
		{"forwards a base URL alongside the apiKeyHelper-supplied credentials",
			autoDetect(map[string]string{"ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}, func() string { return "vck_from_helper" }),
			map[string]string{"ANTHROPIC_API_KEY": "vck_from_helper", "ANTHROPIC_AUTH_TOKEN": "vck_from_helper", "ANTHROPIC_BASE_URL": "https://ai-gateway.vercel.sh"}},
		{"populates both ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN from the apiKeyHelper",
			autoDetect(map[string]string{}, func() string { return "vck_from_helper" }),
			map[string]string{"ANTHROPIC_API_KEY": "vck_from_helper", "ANTHROPIC_AUTH_TOKEN": "vck_from_helper"}},
		{"prefers a static ANTHROPIC_API_KEY over the apiKeyHelper",
			autoDetect(map[string]string{"ANTHROPIC_API_KEY": "sk-static"}, func() string { return "sk-from-helper" }),
			map[string]string{"ANTHROPIC_API_KEY": "sk-static", "ANTHROPIC_AUTH_TOKEN": "sk-from-helper"}},
		{"returns an empty env when nothing is configured",
			autoDetect(map[string]string{}, noHelper), map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

// TS 'uses a supplied authentication environment without ambient or helper
// fallback' + resolveClaudeCodeAuthentication cases that need no native storage.
func TestTSReplay_ResolveAuthenticationEnvironment(t *testing.T) {
	ctx := context.Background()
	t.Run("supplied environment wins over ambient gateway key", func(t *testing.T) {
		auth := harness.AuthEnvironment(map[string]string{"ANTHROPIC_API_KEY": "programmatic-anthropic-key"})
		got, err := resolveAuthenticationEnvironment(ctx, auth, map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key"})
		if err != nil || !reflect.DeepEqual(got, map[string]string{"ANTHROPIC_API_KEY": "programmatic-anthropic-key"}) {
			t.Fatalf("got %v, %v", got, err)
		}
		if mode := resolveAuthenticationMode(auth, map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key"}); mode != AuthModeDirect {
			t.Fatalf("mode = %v, want direct", mode)
		}
	})
	t.Run("supplied OAuth environment skips ambient and native discovery", func(t *testing.T) {
		auth := harness.AuthEnvironment(map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "supplied-oauth-token"})
		got, err := resolveAuthenticationEnvironment(ctx, auth, map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key"})
		if err != nil || !reflect.DeepEqual(got, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "supplied-oauth-token"}) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("prefers an explicit process OAuth token over native storage", func(t *testing.T) {
		got, err := resolveAuthenticationEnvironment(ctx, harness.Authentication{Mode: harness.AuthModeDirect}, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "process-oauth-token"})
		if err != nil || !reflect.DeepEqual(got, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "process-oauth-token"}) {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("gateway mode does not inspect native storage", func(t *testing.T) {
		got, err := resolveAuthenticationEnvironment(ctx, harness.Authentication{Mode: harness.AuthModeAIGateway}, map[string]string{"AI_GATEWAY_API_KEY": "gateway"})
		if err != nil || got["AI_GATEWAY_API_KEY"] != "gateway" || got["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("authentication mode", func(t *testing.T) {
		if m := resolveAuthenticationMode(harness.Authentication{Mode: harness.AuthModeDirect}, map[string]string{"AI_GATEWAY_API_KEY": "gateway-key"}); m != AuthModeDirect {
			t.Fatalf("direct despite ambient gateway: %v", m)
		}
		if m := resolveAuthenticationMode(harness.Authentication{}, map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token"}); m != AuthModeAIGateway {
			t.Fatalf("ambient OIDC: %v", m)
		}
	})
}

// TS createClaudeCodeRequestTransformations cases, compared as JSON.
func TestTSReplay_RequestTransformations(t *testing.T) {
	toJSON := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	got, err := requestTransformations(
		map[string]string{"ANTHROPIC_API_KEY": "api-secret", "ANTHROPIC_AUTH_TOKEN": "token-secret", "ANTHROPIC_BASE_URL": "https://anthropic.example/v1"},
		map[string]string{"ANTHROPIC_API_KEY": "sandbox-api-secret", "ANTHROPIC_AUTH_TOKEN": "sandbox-token-secret"},
		AuthModeDirect)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"match":{"host":"anthropic.example","path":{"startsWith":"/v1"},"headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"sandbox-api-secret"}}]},"transform":{"headers":{"x-api-key":"api-secret"}}},` +
		`{"match":{"host":"anthropic.example","path":{"startsWith":"/v1"},"headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer sandbox-token-secret"}}]},"transform":{"headers":{"Authorization":"Bearer token-secret"}}}]`
	if toJSON(got) != want {
		t.Fatalf("direct:\n got %s\nwant %s", toJSON(got), want)
	}

	got, err = requestTransformations(
		map[string]string{"ANTHROPIC_API_KEY": "gateway-secret", "ANTHROPIC_BASE_URL": "https://gateway.example"},
		map[string]string{"ANTHROPIC_API_KEY": "sandbox-gateway-secret"}, AuthModeAIGateway)
	if err != nil {
		t.Fatal(err)
	}
	want = `[{"match":{"host":"gateway.example","headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"sandbox-gateway-secret"}}]},"transform":{"headers":{"x-api-key":"gateway-secret"}}}]`
	if toJSON(got) != want {
		t.Fatalf("gateway:\n got %s\nwant %s", toJSON(got), want)
	}
}
