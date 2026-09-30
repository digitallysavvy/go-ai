package grokbuild

import (
	"context"
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

const testScope = "https://auth.x.ai::b1a00492-073a-47ea-816f-4c329264a828"

type rewriteHostTransport struct{ target *url.URL }

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

// Mirrors "does not read a subscription when Gateway auth is selected".
func TestResolveSubscriptionEnvironment_Gateway(t *testing.T) {
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeAIGateway), Env: map[string]string{"AI_GATEWAY_API_KEY": "gateway"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["AI_GATEWAY_API_KEY"] != "gateway" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "prefers an environment API key".
func TestResolveSubscriptionEnvironment_PrefersDirectCredential(t *testing.T) {
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeDirect), Env: map[string]string{"XAI_API_KEY": "environment-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "environment-key" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "reads a fresh scope-keyed OAuth record".
func TestReadSubscription_FreshRecord(t *testing.T) {
	grokHome := t.TempDir()
	record := map[string]any{
		testScope: map[string]any{
			"key":           "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    float64(time.Now().Add(time.Hour).UnixMilli()),
			"auth_mode":     "personal",
		},
	}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(grokHome, "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{Env: map[string]string{"GROK_HOME": grokHome}})
	if err != nil {
		t.Fatal(err)
	}
	if got["XAI_API_KEY"] != "access-token" || got["GROK_CLI_CHAT_PROXY_BASE_URL"] != "https://cli-chat-proxy.grok.com/v1" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "discovers, refreshes, and persists an expiring record".
func TestReadSubscription_RefreshesExpiring(t *testing.T) {
	grokHome := t.TempDir()
	authPath := filepath.Join(grokHome, "auth.json")
	record := map[string]any{
		testScope: map[string]any{
			"key":           "old-access",
			"refresh_token": "old-refresh",
			"expires_at":    float64(time.Now().Add(60 * time.Second).UnixMilli()),
			"email":         "person@example.com",
		},
	}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(authPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(`{"token_endpoint":"https://auth.x.ai/oauth2/token"}`))
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env: map[string]string{"GROK_HOME": grokHome}, HTTPClient: rewriteHostClient(t, server.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]map[string]any
	if err := json.Unmarshal(persisted, &out); err != nil {
		t.Fatal(err)
	}
	rec := out[testScope]
	if rec["key"] != "new-access" || rec["refresh_token"] != "new-refresh" || rec["email"] != "person@example.com" {
		t.Errorf("persisted record = %v", rec)
	}
	if !strings.Contains(string(persisted), "new-refresh") {
		t.Errorf("persisted file missing new-refresh: %s", persisted)
	}
}
