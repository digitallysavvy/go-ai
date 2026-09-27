package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Ports TS opencode-subscription.test.ts `readOpenCodeSubscription` /
// `it.each` case: every provider round-trips through OPENCODE_AUTH_CONTENT.
func TestReadOpenCodeSubscriptionFromAuthContent(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	cases := []struct {
		providerID  string
		record      map[string]any
		accessToken string
	}{
		{"xai", map[string]any{"type": "oauth", "access": "xai-access", "refresh": "xai-refresh", "expires": float64(future)}, "xai-access"},
		{"github-copilot", map[string]any{"type": "oauth", "access": "github-token", "refresh": "github-token", "expires": float64(0)}, "github-token"},
		{"poe", map[string]any{"type": "oauth", "access": "poe-key", "refresh": "poe-key", "expires": float64(future)}, "poe-key"},
		{"opencode-go", map[string]any{"type": "api", "key": "go-key"}, "go-key"},
		{"gitlab", map[string]any{"type": "oauth", "access": "gitlab-access", "refresh": "gitlab-refresh", "expires": float64(future)}, "gitlab-access"},
	}
	for _, tc := range cases {
		t.Run(tc.providerID, func(t *testing.T) {
			content, err := json.Marshal(map[string]any{tc.providerID: tc.record})
			if err != nil {
				t.Fatal(err)
			}
			sub, err := readOpenCodeSubscription(context.Background(), tc.providerID, map[string]string{"OPENCODE_AUTH_CONTENT": string(content)})
			if err != nil {
				t.Fatal(err)
			}
			if sub == nil || string(sub.ProviderID) != tc.providerID || sub.AccessToken != tc.accessToken {
				t.Fatalf("readOpenCodeSubscription(%s) = %+v, want providerId=%s accessToken=%s", tc.providerID, sub, tc.providerID, tc.accessToken)
			}
		})
	}
}

// Ports TS `refreshes an expiring OpenAI record in the XDG auth store`.
func TestReadOpenCodeSubscriptionRefreshesExpiringOpenAIRecord(t *testing.T) {
	homeDirectory := t.TempDir()
	authDirectory := filepath.Join(homeDirectory, ".local", "share", "opencode")
	if err := os.MkdirAll(authDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(authDirectory, "auth.json")
	initial, _ := json.Marshal(map[string]any{
		"openai": map[string]any{
			"type": "oauth", "access": "old-access", "refresh": "old-refresh",
			"expires": float64(time.Now().Add(60 * time.Second).UnixMilli()), "accountId": "account-1",
		},
	})
	if err := os.WriteFile(authPath, initial, 0o600); err != nil {
		t.Fatal(err)
	}

	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	// resolveRefreshSettings hardcodes the OpenAI token URL; instead of
	// overriding it, verify the refresh actually happens end-to-end by
	// pointing HTTPClient's transport at the fake server via a redirecting
	// RoundTripper (there's no injectable tokenURL in this port, matching
	// TS's own resolveRefreshSettings, which also hardcodes it).
	client := &http.Client{Transport: redirectTransport{to: server.URL}}

	sub, err := readOpenCodeSubscriptionWithOptions(context.Background(), "openai", map[string]string{}, subscriptionReadOptions{
		HomeDirectory: homeDirectory, HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.AccessToken != "new-access" || sub.AccountID != "account-1" {
		t.Fatalf("readOpenCodeSubscription = %+v, want accessToken=new-access accountId=account-1", sub)
	}
	if !strings.Contains(capturedBody, "refresh_token=old-refresh") {
		t.Fatalf("refresh request body = %q, want it to contain refresh_token=old-refresh", capturedBody)
	}
	persisted, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), "new-refresh") {
		t.Fatalf("persisted auth.json = %s, want it to contain the rotated refresh token", persisted)
	}
}

// redirectTransport sends every request to `to` regardless of its original
// URL, letting a test intercept a hardcoded upstream URL.
type redirectTransport struct{ to string }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	target, err := clone.URL.Parse(rt.to)
	if err != nil {
		return nil, err
	}
	clone.URL = target
	clone.Host = target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

// Ports TS `reads the GitLab plugin auth store on Windows`.
func TestReadOpenCodeSubscriptionReadsWindowsAuthStore(t *testing.T) {
	homeDirectory := t.TempDir()
	authDirectory := filepath.Join(homeDirectory, ".opencode")
	if err := os.MkdirAll(authDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(map[string]any{
		"gitlab": map[string]any{
			"type": "oauth", "access": "gitlab-access", "refresh": "gitlab-refresh",
			"expires": float64(time.Now().Add(time.Hour).UnixMilli()), "enterpriseUrl": "https://gitlab.example.com",
		},
	})
	if err := os.WriteFile(filepath.Join(authDirectory, "auth.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	sub, err := readOpenCodeSubscriptionWithOptions(context.Background(), "gitlab", map[string]string{}, subscriptionReadOptions{
		HomeDirectory: homeDirectory, GOOS: "windows",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.AccessToken != "gitlab-access" || sub.EnterpriseURL != "https://gitlab.example.com" {
		t.Fatalf("readOpenCodeSubscription = %+v, want accessToken=gitlab-access enterpriseUrl=https://gitlab.example.com", sub)
	}
}

// Ports TS `requests an AI access token from GitLab on the host`.
func TestRequestOpenCodeGitLabDirectAccess(t *testing.T) {
	var gotPath, gotAuth, gotContentType, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"token":"direct-access-token","headers":{"x-gitlab-token":"routing-token"}}`))
	}))
	defer server.Close()

	got, err := requestOpenCodeGitLabDirectAccess(context.Background(), server.Client(), "oauth-access-token", server.URL+"/", "https://gateway.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "direct-access-token" || got.Headers["x-gitlab-token"] != "routing-token" || got.AIGatewayURL != "https://gateway.example.com" {
		t.Fatalf("requestOpenCodeGitLabDirectAccess = %+v", got)
	}
	if gotPath != "/api/v4/ai/third_party_agents/direct_access" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer oauth-access-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	if gotBody != `{"feature_flags":{"DuoAgentPlatformNext":true}}` {
		t.Fatalf("body = %q", gotBody)
	}
}

// Ports TS `rejects invalid direct access credentials without exposing them`.
func TestRequestOpenCodeGitLabDirectAccessRejectsInvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"token":"sensitive-token"}`))
	}))
	defer server.Close()

	_, err := requestOpenCodeGitLabDirectAccess(context.Background(), server.Client(), "sensitive-oauth-token", server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "OpenCode GitLab direct access returned invalid credentials.") {
		t.Fatalf("err = %v, want invalid-credentials error", err)
	}
	if strings.Contains(err.Error(), "sensitive-oauth-token") || strings.Contains(err.Error(), "sensitive-token") {
		t.Fatalf("err leaked a credential: %v", err)
	}
}

// Ports TS `brokers the AI access token and response headers for both proxy APIs`.
func TestCreateOpenCodeGitLabSubscriptionRequestTransformations(t *testing.T) {
	transforms, err := createOpenCodeGitLabSubscriptionRequestTransformations(&GitLabDirectAccess{
		AccessToken: "host-direct-access-token",
		Headers: map[string]string{
			"x-gitlab-token": "routing-token",
			"x-api-key":      "must-not-be-forwarded",
			"Authorization":  "must-not-be-forwarded",
		},
		AIGatewayURL: "https://cloud.gitlab.test",
	}, "sandbox-access-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(transforms) != 2 {
		t.Fatalf("len(transforms) = %d, want 2", len(transforms))
	}
	wantPaths := []string{"/ai/v1/proxy/anthropic", "/ai/v1/proxy/openai/v1"}
	for i, tr := range transforms {
		if tr.Match.Host != "cloud.gitlab.test" {
			t.Fatalf("transforms[%d].Match.Host = %q", i, tr.Match.Host)
		}
		if tr.Match.Path == nil || tr.Match.Path.StartsWith != wantPaths[i] {
			t.Fatalf("transforms[%d].Match.Path = %+v, want startsWith %q", i, tr.Match.Path, wantPaths[i])
		}
		if len(tr.Match.Headers) != 1 || tr.Match.Headers[0].Value.Exact != "Bearer sandbox-access-token" {
			t.Fatalf("transforms[%d].Match.Headers = %+v", i, tr.Match.Headers)
		}
		if tr.Transform.Headers["Authorization"] != "Bearer host-direct-access-token" || tr.Transform.Headers["x-gitlab-token"] != "routing-token" {
			t.Fatalf("transforms[%d].Transform.Headers = %v", i, tr.Transform.Headers)
		}
		if _, ok := tr.Transform.Headers["x-api-key"]; ok {
			t.Fatalf("transforms[%d] forwarded x-api-key, must not", i)
		}
	}
}

// Ports TS `configures native providers for each GitLab chat API`.
func TestCreateOpenCodeGitLabSubscriptionConfig(t *testing.T) {
	config := createOpenCodeGitLabSubscriptionConfig(
		map[string]any{"provider": map[string]any{"custom": map[string]any{"options": map[string]any{"apiKey": "custom"}}}},
		"sandbox-access-token", "https://cloud.gitlab.test", nil,
	)
	providers, ok := config["provider"].(map[string]any)
	if !ok {
		t.Fatalf("config[provider] = %v", config["provider"])
	}
	custom, ok := providers["custom"].(map[string]any)
	if !ok || custom["options"].(map[string]any)["apiKey"] != "custom" {
		t.Fatalf("providers[custom] = %v, want the original entry preserved", providers["custom"])
	}
	anthropic, ok := providers[GitLabAnthropicProviderID].(map[string]any)
	if !ok || anthropic["npm"] != "@ai-sdk/anthropic" || anthropic["api"] != "https://cloud.gitlab.test/ai/v1/proxy/anthropic/v1" {
		t.Fatalf("providers[%s] = %v", GitLabAnthropicProviderID, anthropic)
	}
	models := anthropic["models"].(map[string]any)
	if m, ok := models["duo-chat-sonnet-4-5"].(map[string]any); !ok || m["id"] != "claude-sonnet-4-5-20250929" {
		t.Fatalf("anthropic models[duo-chat-sonnet-4-5] = %v", models["duo-chat-sonnet-4-5"])
	}
	openAIChat := providers[GitLabOpenAIChatProviderID].(map[string]any)
	if openAIChat["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("openAIChat.npm = %v", openAIChat["npm"])
	}
	chatModels := openAIChat["models"].(map[string]any)
	if m, ok := chatModels["duo-chat-gpt-5-4"].(map[string]any); !ok || m["id"] != "gpt-5.4-2026-03-05" {
		t.Fatalf("chat models[duo-chat-gpt-5-4] = %v", chatModels["duo-chat-gpt-5-4"])
	}
	openAIResponses := providers[GitLabOpenAIResponsesProviderID].(map[string]any)
	if openAIResponses["npm"] != "@ai-sdk/openai" {
		t.Fatalf("openAIResponses.npm = %v", openAIResponses["npm"])
	}
	responsesModels := openAIResponses["models"].(map[string]any)
	if m, ok := responsesModels["duo-chat-gpt-5-6-sol"].(map[string]any); !ok || m["id"] != "gpt-5.6-sol" {
		t.Fatalf("responses models[duo-chat-gpt-5-6-sol] = %v", responsesModels["duo-chat-gpt-5-6-sol"])
	}
}

// Ports TS `maps GitLab chat models and rejects workflow models`.
func TestResolveOpenCodeGitLabSubscriptionModel(t *testing.T) {
	got, err := resolveOpenCodeGitLabSubscriptionModel("gitlab/duo-chat-sonnet-4-5", "gitlab")
	if err != nil || got != "ai-sdk-gitlab-anthropic/duo-chat-sonnet-4-5" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	got, err = resolveOpenCodeGitLabSubscriptionModel("duo-chat-gpt-5-4", "gitlab")
	if err != nil || got != "ai-sdk-gitlab-openai-chat/duo-chat-gpt-5-4" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	_, err = resolveOpenCodeGitLabSubscriptionModel("gitlab/duo-workflow", "gitlab")
	if err == nil || !strings.Contains(err.Error(), "because it authenticates over WebSocket") {
		t.Fatalf("err = %v, want a WebSocket-authentication rejection", err)
	}
}

// Ports TS `creates a sanitized non-expiring OAuth record`.
func TestCreateOpenCodeSubscriptionAuthContent(t *testing.T) {
	content, err := createOpenCodeSubscriptionAuthContent(&Subscription{
		ProviderID: SubscriptionOpenAI, AccessToken: "host-access", AccountID: "account-1",
	}, "sandbox-access")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatal(err)
	}
	openai, ok := parsed["openai"].(map[string]any)
	if !ok {
		t.Fatalf("parsed = %v", parsed)
	}
	if openai["type"] != "oauth" || openai["access"] != "sandbox-access" || openai["refresh"] != "host-managed" ||
		openai["expires"].(float64) != float64(numberMaxSafeInteger) || openai["accountId"] != "account-1" {
		t.Fatalf("openai = %v", openai)
	}
	if strings.Contains(content, "host-access") {
		t.Fatalf("content leaked the host access token: %s", content)
	}
}

// Ports TS `brokers the sandbox token at the subscription request route`.
func TestCreateOpenCodeSubscriptionRequestTransformations(t *testing.T) {
	transforms, err := createOpenCodeSubscriptionRequestTransformations(&Subscription{
		ProviderID: SubscriptionOpenAI, AccessToken: "host-access", AccountID: "account-1",
	}, "sandbox-access")
	if err != nil {
		t.Fatal(err)
	}
	if len(transforms) != 1 {
		t.Fatalf("len(transforms) = %d, want 1", len(transforms))
	}
	tr := transforms[0]
	if tr.Match.Host != "chatgpt.com" || tr.Match.Path == nil || tr.Match.Path.StartsWith != "/backend-api/codex" {
		t.Fatalf("Match = %+v", tr.Match)
	}
	if tr.Transform.Headers["Authorization"] != "Bearer host-access" || tr.Transform.Headers["ChatGPT-Account-Id"] != "account-1" {
		t.Fatalf("Transform.Headers = %v", tr.Transform.Headers)
	}
}

// hasOpenCodeCredential and resolveOpenCodeAuthentication's fallback-to-
// subscription orchestration (TS `resolveOpenCodeAuthentication`).
func TestResolveOpenCodeAuthenticationFallsBackToSubscription(t *testing.T) {
	calls := 0
	readSub := func(_ context.Context, providerID string, _ map[string]string) (*Subscription, error) {
		calls++
		if providerID != string(AuthAnthropic) {
			t.Fatalf("readSub called with providerId=%q, want %q", providerID, AuthAnthropic)
		}
		return &Subscription{ProviderID: SubscriptionProvider(providerID), AccessToken: "native-token"}, nil
	}

	resolved, err := resolveOpenCodeAuthentication(context.Background(), AuthenticationMode{}, "", "", map[string]string{}, readSub)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("readSub called %d times, want 1", calls)
	}
	if resolved.Subscription == nil || resolved.Subscription.AccessToken != "native-token" {
		t.Fatalf("resolved.Subscription = %+v", resolved.Subscription)
	}
	if resolved.Environment[SubscriptionAccessTokenEnvironmentVariable] != "native-token" {
		t.Fatalf("resolved.Environment[%s] = %q", SubscriptionAccessTokenEnvironmentVariable, resolved.Environment[SubscriptionAccessTokenEnvironmentVariable])
	}

	// A direct credential already present in the environment short-circuits
	// the subscription lookup entirely.
	calls = 0
	resolved, err = resolveOpenCodeAuthentication(context.Background(), AuthenticationMode{}, "", "", map[string]string{"ANTHROPIC_API_KEY": "real-key"}, readSub)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("readSub called %d times, want 0 (a direct credential was already available)", calls)
	}
	if resolved.Subscription != nil {
		t.Fatalf("resolved.Subscription = %+v, want nil", resolved.Subscription)
	}
}

func TestHasOpenCodeCredential(t *testing.T) {
	if hasOpenCodeCredential(map[string]string{}, AuthAIGateway) {
		t.Fatal("empty environment must not report an AI Gateway credential")
	}
	if !hasOpenCodeCredential(map[string]string{"AI_GATEWAY_API_KEY": "k"}, AuthAIGateway) {
		t.Fatal("AI_GATEWAY_API_KEY must report an AI Gateway credential")
	}
	if !hasOpenCodeCredential(map[string]string{"ANTHROPIC_API_KEY": "k"}, AuthAnthropic) {
		t.Fatal("*_API_KEY must report a credential")
	}
	if !hasOpenCodeCredential(map[string]string{"ANTHROPIC_AUTH_TOKEN": "k"}, AuthAnthropic) {
		t.Fatal("ANTHROPIC_AUTH_TOKEN must report a credential")
	}
	if hasOpenCodeCredential(map[string]string{"SOME_OTHER_VAR": "k"}, AuthAnthropic) {
		t.Fatal("an unrelated env var must not report a credential")
	}
}
