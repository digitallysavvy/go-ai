package grokbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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
	if !harnessutil.ShouldResolveNativeSubscription(opts.Auth.Mode, opts.Env, envHas(opts.Env, "XAI_API_KEY")) {
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
	grokHome, hasGrokHome := env["GROK_HOME"] // TS `??`: only an absent variable falls back
	if !hasGrokHome {
		grokHome = filepath.Join(homeDirectory, ".grok")
	}
	if abs, err := filepath.Abs(grokHome); err == nil {
		grokHome = abs // TS resolve()
	}
	authPath := filepath.Join(grokHome, "auth.json")
	text, err := os.ReadFile(authPath)
	if err != nil {
		return nil, nil
	}
	if securejson.Check(text) != nil {
		return nil, nil // safeParseJSON rejects a __proto__ key
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
		rendered, err := renderRefreshedAuthRecords(text, selected.scope, refreshed.AccessToken, refreshToken, refreshed.ExpiresAt)
		if err != nil {
			return nil, err
		}
		if err := persistAuthRecords(authPath, rendered); err != nil {
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
	return jsonorder.JSKeyOrder(order), m, nil
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
		// TS only checks `typeof === 'string'`; empty strings are accepted.
		key, keyOK := record["key"].(string)
		refreshToken, refreshOK := record["refresh_token"].(string)
		expiresAt, ok := normalizeExpiresAt(record["expires_at"])
		separator := strings.LastIndex(scope, "::")
		if !keyOK || !refreshOK || !ok || separator <= 0 {
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
		// TS Date.parse: ISO forms, date-only (UTC) and offset-less
		// date-times (local time).
		for _, layout := range []string{time.RFC3339, "2006-01-02", time.RFC1123Z, time.RFC1123} {
			if t, err := time.Parse(layout, v); err == nil {
				return t.UnixMilli(), true
			}
		}
		for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
			if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
				return t.UnixMilli(), true
			}
		}
		return 0, false
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
	body, err := harnessutil.ReadBoundedResponse(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("Grok OAuth discovery failed with status %d.", resp.StatusCode) //nolint:staticcheck // matches TS SDK's exact error text
	}
	var parsed map[string]any
	if err := securejson.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("Grok OAuth discovery returned no token endpoint.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	endpoint, ok := parsed["token_endpoint"].(string)
	if !ok || endpoint == "" {
		return "", fmt.Errorf("Grok OAuth discovery returned no token endpoint.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return endpoint, nil
}

// renderRefreshedAuthRecords rewrites auth.json with the selected scope's
// record updated in place, like TS (`{...authRecords}` plus a spread of the
// original record keeps every key's position, and JSON.stringify keeps
// insertion order). Key order matters: selectOAuthRecord picks the first
// matching scope, so a re-sorted file could change which account is used.
func renderRefreshedAuthRecords(text []byte, scope, accessToken, refreshToken string, expiresAt int64) ([]byte, error) {
	keys, values, err := decodeRawObject(text)
	if err != nil {
		return nil, err
	}
	updates := []struct {
		key   string
		value any
	}{{"key", accessToken}, {"refresh_token", refreshToken}, {"expires_at", expiresAt}}
	var buf bytes.Buffer
	enc := func(v any) ([]byte, error) {
		return jsstr.Marshal(v)
	}
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := enc(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		if k != scope {
			buf.WriteString(canonicalJSON(values[k]))
			continue
		}
		innerKeys, innerValues, err := decodeRawObject(values[k])
		if err != nil {
			innerKeys, innerValues = nil, map[string]json.RawMessage{}
		}
		for ik, raw := range innerValues {
			innerValues[ik] = json.RawMessage(canonicalJSON(raw))
		}
		for _, u := range updates {
			ub, err := enc(u.value)
			if err != nil {
				return nil, err
			}
			if _, exists := innerValues[u.key]; !exists {
				innerKeys = append(innerKeys, u.key)
			}
			innerValues[u.key] = ub
		}
		buf.WriteByte('{')
		for j, ik := range innerKeys {
			if j > 0 {
				buf.WriteByte(',')
			}
			ikb, err := enc(ik)
			if err != nil {
				return nil, err
			}
			buf.Write(ikb)
			buf.WriteByte(':')
			buf.Write(innerValues[ik])
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// canonicalJSON is JSON.stringify(JSON.parse(raw)): the persisted file is
// re-serialized from the parsed value, so escapes (\u003c), number spellings
// (1.0) and key order normalize the way they do in TS.
func canonicalJSON(raw json.RawMessage) string {
	if v, ok := jsonorder.Parse(raw); ok {
		return jsonorder.Stringify(v)
	}
	return string(raw)
}

// decodeRawObject returns an object's keys in file order (first occurrence)
// with each key's last raw value, like a JS object parsed from JSON.
func decodeRawObject(data []byte) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("grokbuild: not a JSON object")
	}
	var order []string
	m := map[string]json.RawMessage{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, seen := m[key]; !seen {
			order = append(order, key)
		}
		m[key] = raw
	}
	return jsonorder.JSKeyOrder(order), m, nil
}

func persistAuthRecords(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// envHas mirrors TS `env.X != null`: a variable that is set to the empty
// string still counts as a direct credential.
func envHas(env map[string]string, key string) bool {
	_, ok := env[key]
	return ok
}
