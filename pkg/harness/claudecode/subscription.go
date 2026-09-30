package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// OAuth endpoint constants for the Claude Code native subscription (a `claude
// login` credential). Mirror TS `CLAUDE_TOKEN_URL` / `CLAUDE_CLIENT_ID`
// (claude-code-subscription.ts). claudeTokenURL is a var (not const) so
// tests can point the refresh call at a fake token endpoint.
var claudeTokenURL = "https://platform.claude.com/v1/oauth/token"

const (
	claudeClientID            = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeSubscriptionBaseURL = "https://api.anthropic.com"
)

// readClaudeCodeSubscription reads the native Claude Code subscription
// credential from `$CLAUDE_CONFIG_DIR/.credentials.json` (default
// `~/.claude/.credentials.json`), refreshing the OAuth access token when it
// is expiring soon and writing the refreshed credential back to disk.
// Returns (nil, false) when no usable credential is found; any I/O, parse or
// refresh failure is treated as "not found", matching TS's blanket
// `.catch(() => undefined)`. Mirrors TS `readClaudeCodeSubscription`
// (claude-code-subscription.ts). The macOS Keychain fallback is not ported
// — see the package doc.
func readClaudeCodeSubscription(ctx context.Context) (map[string]string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, false
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	credentialPath := filepath.Join(configDir, ".credentials.json")

	raw, err := os.ReadFile(credentialPath)
	if err != nil {
		return nil, false
	}
	var store map[string]any
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, false
	}
	oauth, ok := store["claudeAiOauth"].(map[string]any)
	if !ok {
		return nil, false
	}
	accessToken, _ := oauth["accessToken"].(string)
	refreshToken, _ := oauth["refreshToken"].(string)
	expiresAt, hasExpiry := oauth["expiresAt"].(float64)
	if accessToken == "" || refreshToken == "" || !hasExpiry {
		return nil, false
	}

	if harnessutil.IsAccessTokenExpiringSoon(int64(expiresAt), time.Now().UnixMilli(), 0) {
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL: claudeTokenURL, ClientID: claudeClientID, RefreshToken: refreshToken, RequestFormat: "json",
		})
		if err != nil {
			return nil, false
		}
		accessToken = refreshed.AccessToken
		oauth["accessToken"] = refreshed.AccessToken
		oauth["expiresAt"] = float64(refreshed.ExpiresAt)
		if refreshed.RefreshToken != "" {
			oauth["refreshToken"] = refreshed.RefreshToken
		}
		store["claudeAiOauth"] = oauth
		// A failed write-back never invalidates the freshly refreshed token
		// for this call — it just means the next call refreshes again.
		_ = writeClaudeCredentialFile(credentialPath, store)
	}

	return map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": accessToken,
		"ANTHROPIC_BASE_URL":      claudeSubscriptionBaseURL,
	}, true
}

// writeClaudeCredentialFile atomically rewrites the credential file (temp
// file in the same directory at mode 0600, then rename) after a token
// refresh. Mirrors TS `writeClaudeCredentialFile`.
func writeClaudeCredentialFile(path string, value map[string]any) error {
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
