package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// OAuth endpoint constants for the Codex native (ChatGPT) subscription.
// Mirror TS `OPENAI_TOKEN_URL` / `OPENAI_CLIENT_ID` / `CHATGPT_CODEX_BASE_URL`
// (codex-subscription.ts). openaiTokenURL is a var (not const) so tests can
// point the refresh call at a fake token endpoint.
var openaiTokenURL = "https://auth.openai.com/oauth/token"

const (
	openaiClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	chatGPTCodexBaseURL = "https://chatgpt.com/backend-api/codex"
)

// readCodexSubscription reads the native Codex (ChatGPT) subscription
// credential from `$CODEX_HOME/auth.json` (default `~/.codex/auth.json`),
// refreshing the OAuth access token when it is expiring soon and writing the
// refreshed credential back to disk. Returns (nil, false) when no usable
// credential is found; any I/O, parse or refresh failure is treated as "not
// found", matching TS's blanket `.catch(() => undefined)`. Mirrors TS
// `readCodexSubscription` (codex-subscription.ts), file storage mode only —
// see the package doc for the OS-keyring and `cli_auth_credentials_store`
// config.toml override, which are not ported.
func readCodexSubscription(ctx context.Context) (map[string]string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, false
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	authPath := filepath.Join(codexHome, "auth.json")

	raw, err := os.ReadFile(authPath)
	if err != nil {
		return nil, false
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, false
	}
	if authMode, _ := store["auth_mode"].(string); authMode != "chatgpt" {
		return nil, false
	}
	tokens, ok := store["tokens"].(map[string]any)
	if !ok {
		return nil, false
	}
	accessToken, _ := tokens["access_token"].(string)
	refreshToken, _ := tokens["refresh_token"].(string)
	if accessToken == "" || refreshToken == "" {
		return nil, false
	}
	expiresAt, hasExpiry := subscription.GetJWTExpiresAt(accessToken)
	if !hasExpiry {
		return nil, false
	}

	if harnessutil.IsAccessTokenExpiringSoon(expiresAt, time.Now().UnixMilli(), 0) {
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL: openaiTokenURL, ClientID: openaiClientID, RefreshToken: refreshToken, RequestFormat: "json",
		})
		if err != nil {
			return nil, false
		}
		accessToken = refreshed.AccessToken
		tokens["access_token"] = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			tokens["refresh_token"] = refreshed.RefreshToken
		}
		store["tokens"] = tokens
		store["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
		// A failed write-back never invalidates the freshly refreshed token
		// for this call — it just means the next call refreshes again.
		_ = writeCodexAuthFile(authPath, store)
	}

	return map[string]string{
		"CODEX_API_KEY":   accessToken,
		"OPENAI_BASE_URL": chatGPTCodexBaseURL,
	}, true
}

// writeCodexAuthFile atomically rewrites the auth file (temp file in the
// same directory at mode 0600, then rename) after a token refresh. Mirrors
// TS's credential-store write-back.
func writeCodexAuthFile(path string, value map[string]any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
