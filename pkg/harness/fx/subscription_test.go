package fx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// rewriteHostTransport redirects every request to target's host/scheme while
// preserving the path, so a hardcoded production URL (fx's xAI OAuth
// endpoints) can be driven through an httptest server in tests -- Go's
// http.Client has no URL-string interception hook like the TS tests' mocked
// `fetch`, so a RoundTripper stands in for it.
type rewriteHostTransport struct {
	target *url.URL
}

func (rt *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func rewriteHostClient(t *testing.T, serverURL string) *http.Client {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &rewriteHostTransport{target: target}}
}

func TestResolveSubscriptionEnvironment_Gateway(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gateway-key"}
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeAIGateway), Env: env, HomeDirectory: "/does-not-exist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["AI_GATEWAY_API_KEY"] != "gateway-key" {
		t.Errorf("got %v", got)
	}
}

func TestResolveSubscriptionEnvironment_AutoPrefersGateway(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gateway-key"}
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeAuto), Env: env, HomeDirectory: "/does-not-exist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["AI_GATEWAY_API_KEY"] != "gateway-key" {
		t.Errorf("got %v", got)
	}
}

func createChatGPTAccessToken(t *testing.T, accountID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte("{}"))
	payload, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func writePrivateJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func createCredential(accessToken, refreshToken, accountID string, expiresAt int64) map[string]any {
	if refreshToken == "" {
		refreshToken = "refresh-token"
	}
	if accountID == "" {
		accountID = "account-1"
	}
	if expiresAt == 0 {
		expiresAt = time.Now().Add(time.Hour).UnixMilli()
	}
	return map[string]any{
		"version":       1,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_at_ms": expiresAt,
		"account_id":    accountID,
	}
}

func createFxDirectory(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// Mirrors "uses native subscriptions in direct mode even when unrelated API keys exist".
func TestResolveSubscriptionEnvironment_UsesNativeSubscription(t *testing.T) {
	home := createFxDirectory(t)
	accessToken := createChatGPTAccessToken(t, "account-1")
	writePrivateJSON(t, filepath.Join(home, ".fx", "chatgpt-auth.json"), createCredential(accessToken, "", "account-1", 0))

	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeDirect), Env: map[string]string{"OPENAI_API_KEY": "unused-api-key"}, HomeDirectory: home,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"OPENAI_API_KEY":         "unused-api-key",
		ChatGPTAccessTokenEnvVar: accessToken,
		ChatGPTAccountIDEnvVar:   "account-1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// Mirrors "reads the exact ChatGPT subscription record used by fx".
func TestReadSubscriptions_ChatGPT(t *testing.T) {
	home := createFxDirectory(t)
	accessToken := createChatGPTAccessToken(t, "account-1")
	writePrivateJSON(t, filepath.Join(home, ".fx", "chatgpt-auth.json"), createCredential(accessToken, "", "account-1", 0))

	got, err := ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home})
	if err != nil {
		t.Fatal(err)
	}
	if got[ChatGPTAccessTokenEnvVar] != accessToken || got[ChatGPTAccountIDEnvVar] != "account-1" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "refreshes and persists an expiring Grok subscription".
func TestReadSubscriptions_RefreshesGrok(t *testing.T) {
	home := createFxDirectory(t)
	authPath := filepath.Join(home, ".fx", "grok-auth.json")
	writePrivateJSON(t, authPath, createCredential("old-access", "old-refresh", "account-1", time.Now().Add(60*time.Second).UnixMilli()))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			if !strings.Contains(readBody(r), "refresh_token=old-refresh") {
				t.Errorf("unexpected refresh body")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
		case "/oauth2/userinfo":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Errorf("unexpected userinfo auth header %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"account-1"}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home, HTTPClient: rewriteHostClient(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	if got[GrokAccessTokenEnvVar] != "new-access" || got[GrokAccountIDEnvVar] != "account-1" {
		t.Errorf("got %v", got)
	}

	persisted, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), "new-refresh") {
		t.Errorf("persisted file missing new-refresh: %s", persisted)
	}
}

// Mirrors "ignores insecure and unrelated fx authentication files".
func TestReadSubscriptions_IgnoresInsecureFiles(t *testing.T) {
	home := createFxDirectory(t)
	// Unrelated filename.
	data, _ := json.Marshal(createCredential("gateway-access", "", "", 0))
	if err := os.WriteFile(filepath.Join(home, ".fx", "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// World-readable grok-auth.json (insecure permissions -> ignored).
	data2, _ := json.Marshal(createCredential("grok-access", "", "", 0))
	if err := os.WriteFile(filepath.Join(home, ".fx", "grok-auth.json"), data2, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// Mirrors "materializes broker placeholders instead of host tokens".
func TestCreateSubscriptionAuthenticationFiles(t *testing.T) {
	hostAccessToken := createChatGPTAccessToken(t, "account-1")
	files := CreateSubscriptionAuthenticationFiles(
		map[string]string{
			ChatGPTAccessTokenEnvVar: hostAccessToken,
			ChatGPTAccountIDEnvVar:   "account-1",
			GrokAccessTokenEnvVar:    "grok-host-access",
			GrokAccountIDEnvVar:      "account-2",
		},
		map[string]string{
			ChatGPTAccessTokenEnvVar: "chatgpt-placeholder",
			GrokAccessTokenEnvVar:    "grok-placeholder",
		},
		true,
	)
	if len(files) != 2 || files[0].Path != ".fx/chatgpt-auth.json" || files[1].Path != ".fx/grok-auth.json" {
		t.Fatalf("files = %+v", files)
	}
	var chatGPT map[string]any
	if err := json.Unmarshal([]byte(files[0].Content), &chatGPT); err != nil {
		t.Fatal(err)
	}
	if chatGPT["refresh_token"] != "ai-sdk-harness-brokered" || chatGPT["account_id"] != "account-1" {
		t.Errorf("chatGPT record = %v", chatGPT)
	}
	accessToken, _ := chatGPT["access_token"].(string)
	if !strings.HasSuffix(accessToken, ".chatgpt-placeholder") {
		t.Errorf("chatGPT access_token = %q", accessToken)
	}
	var grok map[string]any
	if err := json.Unmarshal([]byte(files[1].Content), &grok); err != nil {
		t.Fatal(err)
	}
	if grok["access_token"] != "grok-placeholder" || grok["account_id"] != "account-2" {
		t.Errorf("grok record = %v", grok)
	}
	for _, f := range files {
		if strings.Contains(f.Content, hostAccessToken) || strings.Contains(f.Content, "grok-host-access") {
			t.Errorf("file leaks a host token: %s", f.Content)
		}
	}
}

func readBody(r *http.Request) string {
	buf := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(buf)
	return string(buf)
}
