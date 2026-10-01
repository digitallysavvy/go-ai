package githubcopilot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

func writeConfig(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func noSubscription(context.Context, ReadSubscriptionOptions) (*Subscription, error) { return nil, nil }

// Mirrors "uses an explicit authentication environment without native discovery".
func TestResolveSubscriptionEnvironment_ExplicitEnvironment(t *testing.T) {
	called := false
	auth := harness.AuthEnvironment(map[string]string{"COPILOT_GITHUB_TOKEN": "explicit-token"})
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: auth,
		Env:  map[string]string{"COPILOT_GITHUB_TOKEN": "process-token"},
		ReadSubscription: func(context.Context, ReadSubscriptionOptions) (*Subscription, error) {
			called = true
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["COPILOT_GITHUB_TOKEN"] != "explicit-token" {
		t.Errorf("got %v", got)
	}
	if called {
		t.Error("readSubscription should not be called for an explicit environment")
	}
}

// Mirrors "does not inspect native credentials for explicit Gateway auth" and
// "...when auto selects Gateway auth".
func TestResolveSubscriptionEnvironment_GatewaySkipsDiscovery(t *testing.T) {
	for _, mode := range []string{harness.AuthModeAIGateway, harness.AuthModeAuto} {
		called := false
		env := map[string]string{"AI_GATEWAY_API_KEY": "gateway-token"}
		got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
			Auth: harness.AuthMode(mode),
			Env:  env,
			ReadSubscription: func(context.Context, ReadSubscriptionOptions) (*Subscription, error) {
				called = true
				return nil, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got["AI_GATEWAY_API_KEY"] != "gateway-token" {
			t.Errorf("mode %q: got %v", mode, got)
		}
		if called {
			t.Errorf("mode %q: readSubscription should not be called", mode)
		}
	}
}

// Mirrors "prefers %s over native credentials".
func TestResolveSubscriptionEnvironment_PrefersDirectCredential(t *testing.T) {
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		called := false
		env := map[string]string{name: "environment-token"}
		got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
			Auth: harness.AuthMode(harness.AuthModeDirect),
			Env:  env,
			ReadSubscription: func(context.Context, ReadSubscriptionOptions) (*Subscription, error) {
				called = true
				return nil, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got[name] != "environment-token" {
			t.Errorf("%s: got %v", name, got)
		}
		if called {
			t.Errorf("%s: readSubscription should not be called", name)
		}
	}
}

// Mirrors "discovers native credentials for direct auth even with Gateway credentials".
func TestResolveSubscriptionEnvironment_DiscoversForDirectAuth(t *testing.T) {
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth: harness.AuthMode(harness.AuthModeDirect),
		Env:  map[string]string{"AI_GATEWAY_API_KEY": "gateway-token"},
		ReadSubscription: func(context.Context, ReadSubscriptionOptions) (*Subscription, error) {
			return &Subscription{Token: "subscription-token", Host: "https://enterprise.example"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["COPILOT_GITHUB_TOKEN"] != "subscription-token" || got["COPILOT_GH_HOST"] != "enterprise.example" || got["AI_GATEWAY_API_KEY"] != "gateway-token" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "keeps the original environment when discovery finds no token".
func TestResolveSubscriptionEnvironment_NoTokenFound(t *testing.T) {
	env := map[string]string{"PATH": "/usr/bin"}
	got, err := ResolveSubscriptionEnvironment(context.Background(), ResolveSubscriptionEnvironmentOptions{
		Auth:             harness.AuthMode(harness.AuthModeDirect),
		Env:              env,
		ReadSubscription: noSubscription,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["PATH"] != "/usr/bin" || len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

// Mirrors "reads the last logged-in account from the macOS Keychain".
func TestReadSubscription_MacOSKeychain(t *testing.T) {
	dir := writeConfig(t, `
		// This file is managed by Copilot CLI.
		{
		  "last_logged_in_user": { "host": "https://enterprise.example/", "login": "last-user" },
		  "logged_in_users": [ { "host": "https://github.com", "login": "first-user" } ],
		  "copilotTokens": { "https://enterprise.example:last-user": "plaintext-token" },
		}
	`)
	var gotService, gotAccount string
	sub, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": dir},
		GOOS:                    "darwin",
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", nil },
		ReadSecureCredential: func(_ context.Context, service, account string) (string, error) {
			gotService, gotAccount = service, account
			return "secure-token", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.Token != "secure-token" || sub.Host != "https://enterprise.example" {
		t.Fatalf("sub = %+v", sub)
	}
	if gotService != "copilot-cli" || gotAccount != "https://enterprise.example:last-user" {
		t.Errorf("service/account = %q/%q", gotService, gotAccount)
	}
}

// Mirrors "uses the first logged-in account when no last account is stored".
func TestReadSubscription_FirstLoggedInAccount(t *testing.T) {
	dir := writeConfig(t, `{
		"logged_in_users": [
		  { "host": "invalid", "login": "" },
		  { "host": "https://github.com", "login": "first-valid-user" },
		  { "host": "https://enterprise.example", "login": "second-user" }
		]
	}`)
	var gotAccount string
	_, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": dir},
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", nil },
		ReadSecureCredential: func(_ context.Context, _, account string) (string, error) {
			gotAccount = account
			return "secure-token", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAccount != "https://github.com:first-valid-user" {
		t.Errorf("account = %q", gotAccount)
	}
}

// Mirrors "falls back to the plaintext token without writing native state".
func TestReadSubscription_PlaintextFallback(t *testing.T) {
	dir := writeConfig(t, `{
		"lastLoggedInUser": { "host": "https://github.com", "login": "octocat" },
		"copilotTokens": { "https://github.com:octocat": "plaintext-token" }
	}`)
	sub, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": dir},
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", nil },
		ReadSecureCredential: func(context.Context, string, string) (string, error) {
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.Token != "plaintext-token" || sub.Host != "https://github.com" {
		t.Fatalf("sub = %+v", sub)
	}
}

// Mirrors "uses the selected account host for the gh fallback".
func TestReadSubscription_GhFallbackForSelectedAccount(t *testing.T) {
	dir := writeConfig(t, `{"lastLoggedInUser": {"host": "https://enterprise.example", "login": "octocat"}}`)
	var gotHostname string
	sub, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": dir, "PATH": "/usr/bin"},
		ReadSecureCredential:    func(context.Context, string, string) (string, error) { return "", errors.New("no credential") },
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "/usr/bin/gh", nil },
		ReadGitHubCliToken: func(_ context.Context, executable, hostname string, env map[string]string) (string, error) {
			gotHostname = hostname
			return " gh-token\n", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.Token != "gh-token" || sub.Host != "https://enterprise.example" {
		t.Fatalf("sub = %+v", sub)
	}
	if gotHostname != "enterprise.example" {
		t.Errorf("hostname = %q", gotHostname)
	}
}

// Mirrors "does not execute gh when it is unavailable on the host".
func TestReadSubscription_NoGhExecutable(t *testing.T) {
	called := false
	sub, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"PATH": "/missing-bin"},
		HomeDirectory:           t.TempDir(),
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", nil },
		ReadGitHubCliToken: func(context.Context, string, string, map[string]string) (string, error) {
			called = true
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Errorf("sub = %+v, want nil", sub)
	}
	if called {
		t.Error("readGitHubCliToken should not be called")
	}
}

// Mirrors "ignores malformed native configuration".
func TestReadSubscription_MalformedConfig(t *testing.T) {
	dir := writeConfig(t, "{ invalid")
	called := false
	sub, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{
		Env:                     map[string]string{"COPILOT_HOME": dir},
		FindGitHubCliExecutable: func(context.Context, map[string]string, string) (string, error) { return "", nil },
		ReadSecureCredential: func(context.Context, string, string) (string, error) {
			called = true
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Errorf("sub = %+v, want nil", sub)
	}
	if called {
		t.Error("readSecureCredential should not be called")
	}
}

// Mirrors findHostGitHubCliExecutable "finds an executable in a Unix PATH".
func TestFindGitHubCliExecutable_Unix(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "gh")
	if err := os.WriteFile(executable, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := FindGitHubCliExecutable(context.Background(), map[string]string{"PATH": "/missing:" + dir}, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got != executable {
		t.Errorf("got %q, want %q", got, executable)
	}
}

// Mirrors "returns undefined without a host PATH".
func TestFindGitHubCliExecutable_NoPath(t *testing.T) {
	got, err := FindGitHubCliExecutable(context.Background(), map[string]string{}, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestNormalizeGitHubHost(t *testing.T) {
	if got := normalizeGitHubHost("github.com"); got != "github.com" {
		t.Errorf("got %q", got)
	}
	if got := normalizeGitHubHost("https://enterprise.example"); got != "enterprise.example" {
		t.Errorf("got %q", got)
	}
}
