package opencode

import (
	"context"
	"testing"
)

func TestR23AuthContentKeyOrder(t *testing.T) {
	got, _ := createOpenCodeSubscriptionAuthContent(&Subscription{ProviderID: SubscriptionOpenAI, AccountID: "a<1", EnterpriseURL: "e"}, "tok")
	want := `{"openai":{"type":"oauth","access":"tok","refresh":"host-managed","expires":9007199254740991,"accountId":"a<1","enterpriseUrl":"e"}}`
	if got != want {
		t.Fatalf("got %s", got)
	}
	got, _ = createOpenCodeSubscriptionAuthContent(&Subscription{ProviderID: SubscriptionOpenCodeGo}, "k")
	if got != `{"opencode-go":{"type":"api","key":"k"}}` {
		t.Fatalf("got %s", got)
	}
}

func TestR23NormalizeDomain(t *testing.T) {
	for in, want := range map[string]string{
		"Example.COM":          "example.com",
		"https://Ghe.Corp:443": "ghe.corp",
		"ghe.corp:8443/":       "ghe.corp:8443",
	} {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TS only checks typeof string for access/refresh: empty strings are kept.
func TestR23EmptyAccessAndRefreshAreAccepted(t *testing.T) {
	env := map[string]string{"OPENCODE_AUTH_CONTENT": `{"github-copilot":{"type":"oauth","access":"","refresh":"gho_x","expires":0}}`}
	sub, err := readOpenCodeSubscriptionWithOptions(context.Background(), "github-copilot", env, subscriptionReadOptions{})
	if err != nil || sub == nil || sub.AccessToken != "gho_x" {
		t.Fatalf("sub = %+v err = %v", sub, err)
	}
}

func TestR23RefreshSettingsKeepEmptyStrings(t *testing.T) {
	url, id := resolveRefreshSettings("gitlab", map[string]any{"enterpriseUrl": ""}, map[string]string{"GITLAB_OAUTH_CLIENT_ID": ""})
	if url != "/oauth/token" || id != "" {
		t.Fatalf("url=%q id=%q", url, id)
	}
}
