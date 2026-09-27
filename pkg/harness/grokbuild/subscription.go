package grokbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

const (
	xaiClientID             = "b1a00492-073a-47ea-816f-4c329264a828"
	grokSubscriptionBaseURL = "https://cli-chat-proxy.grok.com/v1"
)

// ResolveSubscriptionEnvironmentOptions is the input of
// ResolveSubscriptionEnvironment.
type ResolveSubscriptionEnvironmentOptions struct {
	Auth          AuthenticationMode
	Env           map[string]string
	HomeDirectory string
	HTTPClient    *http.Client
}

// ResolveSubscriptionEnvironment resolves XAI_API_KEY (and the Grok CLI
// proxy base URLs) from Grok's native OAuth store when settings.auth allows
// it. Mirrors TS `resolveGrokBuildSubscriptionEnvironment`.
func ResolveSubscriptionEnvironment(ctx context.Context, opts ResolveSubscriptionEnvironmentOptions) (map[string]string, error) {
	if harnessutil.IsAuthenticationEnvironment(opts.Auth) {
		return opts.Auth.Environment, nil
	}
	if !harnessutil.ShouldResolveNativeSubscription(opts.Auth.Mode, opts.Env, opts.Env["XAI_API_KEY"] != "") {
		return opts.Env, nil
	}
	subs, err := ReadSubscription(ctx, ReadSubscriptionOptions{
		Env: opts.Env, HomeDirectory: opts.HomeDirectory, HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	if subs == nil {
		return opts.Env, nil
	}
	merged := make(map[string]string, len(opts.Env)+len(subs))
	for k, v := range opts.Env {
		merged[k] = v
	}
	for k, v := range subs {
		merged[k] = v
	}
	return merged, nil
}

// ReadSubscriptionOptions is the input of ReadSubscription.
type ReadSubscriptionOptions struct {
	Env           map[string]string
	HomeDirectory string
	HTTPClient    *http.Client
}

// ReadSubscription reads Grok's scope-keyed OAuth record from
// `~/.grok/auth.json` (or $GROK_HOME/auth.json), refreshing (and
// persisting) it if it is expiring soon via OIDC discovery. Mirrors TS
// `readGrokBuildSubscription`.
func ReadSubscription(ctx context.Context, opts ReadSubscriptionOptions) (map[string]string, error) {
	env := opts.Env
	if env == nil {
		env = map[string]string{}
	}
	homeDirectory := opts.HomeDirectory
	if homeDirectory == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		homeDirectory = h
	}
	grokHome := env["GROK_HOME"]
	if grokHome == "" {
		grokHome = filepath.Join(homeDirectory, ".grok")
	}
	authPath := filepath.Join(grokHome, "auth.json")
	text, err := os.ReadFile(authPath)
	if err != nil {
		return nil, nil
	}
	scopeOrder, authRecords, err := decodeOrderedObject(text)
	if err != nil {
		return nil, nil
	}
	selected := selectOAuthRecord(scopeOrder, authRecords)
	if selected == nil {
		return nil, nil
	}
	accessToken := selected.record.key

	if harnessutil.IsAccessTokenExpiringSoon(selected.record.expiresAt, time.Now().UnixMilli(), 0) {
		client := opts.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		tokenEndpoint, err := discoverTokenEndpoint(ctx, selected.issuer, client)
		if err != nil {
			return nil, err
		}
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL:      tokenEndpoint,
			ClientID:      xaiClientID,
			RefreshToken:  selected.record.refreshToken,
			RequestFormat: "form",
			HTTPClient:    client,
		})
		if err != nil {
			return nil, err
		}
		accessToken = refreshed.AccessToken
		refreshToken := refreshed.RefreshToken
		if refreshToken == "" {
			refreshToken = selected.record.refreshToken
		}
		original, _ := authRecords[selected.scope].(map[string]any)
		updated := map[string]any{}
		for k, v := range original {
			updated[k] = v
		}
		updated["key"] = refreshed.AccessToken
		updated["refresh_token"] = refreshToken
		updated["expires_at"] = refreshed.ExpiresAt
		authRecords[selected.scope] = updated

		if err := persistAuthRecords(authPath, authRecords); err != nil {
			return nil, err
		}
	}

	return map[string]string{
		"XAI_API_KEY":                  accessToken,
		"GROK_XAI_API_BASE_URL":        grokSubscriptionBaseURL,
		"GROK_MODELS_BASE_URL":         grokSubscriptionBaseURL,
		"GROK_CLI_CHAT_PROXY_BASE_URL": grokSubscriptionBaseURL,
	}, nil
}

type oauthRecord struct {
	key          string
	refreshToken string
	expiresAt    int64
}

type selectedOAuthRecord struct {
	scope  string
	issuer string
	record oauthRecord
}

// decodeOrderedObject parses a flat JSON object, returning its top-level
// keys in their original (file) order alongside the decoded value map. Go's
// map iteration order is randomized, unlike TS's insertion-ordered
// `Object.entries`, so selectOAuthRecord needs this to deterministically
// pick the same "first matching scope" TS does when auth.json holds more
// than one xAI OAuth record.
func decodeOrderedObject(data []byte) ([]string, map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("grokbuild: auth.json is not a JSON object")
	}
	var order []string
	m := map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("grokbuild: auth.json has a non-string key")
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		order = append(order, key)
		m[key] = v
	}
	return order, m, nil
}

// selectOAuthRecord mirrors TS `selectOAuthRecord`: the first scope (in
// auth.json's own key order) whose `issuer::clientId` suffix matches the
// pinned xAI OAuth client and carries a complete record.
func selectOAuthRecord(scopeOrder []string, value map[string]any) *selectedOAuthRecord {
	for _, scope := range scopeOrder {
		candidate := value[scope]
		record, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		key, _ := record["key"].(string)
		refreshToken, _ := record["refresh_token"].(string)
		expiresAt, ok := normalizeExpiresAt(record["expires_at"])
		separator := strings.LastIndex(scope, "::")
		if key == "" || refreshToken == "" || !ok || separator <= 0 {
			continue
		}
		if scope[separator+2:] != xaiClientID {
			continue
		}
		return &selectedOAuthRecord{
			scope:  scope,
			issuer: scope[:separator],
			record: oauthRecord{key: key, refreshToken: refreshToken, expiresAt: expiresAt},
		}
	}
	return nil
}

func normalizeExpiresAt(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v < 10_000_000_000 {
			return int64(v * 1000), true
		}
		return int64(v), true
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return 0, false
		}
		return t.UnixMilli(), true
	}
	return 0, false
}

func discoverTokenEndpoint(ctx context.Context, issuer string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("Grok OAuth discovery failed with status %d.", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("Grok OAuth discovery returned no token endpoint.")
	}
	endpoint, ok := parsed["token_endpoint"].(string)
	if !ok || endpoint == "" {
		return "", fmt.Errorf("Grok OAuth discovery returned no token endpoint.")
	}
	return endpoint, nil
}

func persistAuthRecords(path string, records map[string]any) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
