package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
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

// claudeGOOS is the platform used to decide on the macOS Keychain fallback;
// replaceable in tests.
var claudeGOOS = runtime.GOOS

const claudeKeychainService = "Claude Code-credentials"

type claudeCredentialStore struct {
	value map[string]any
	write func(map[string]any) error
}

func claudeOAuth(value map[string]any) (map[string]any, bool) {
	oauth, ok := value["claudeAiOauth"].(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := oauth["accessToken"].(string); !ok {
		return nil, false
	}
	if _, ok := oauth["refreshToken"].(string); !ok {
		return nil, false
	}
	if _, ok := oauth["expiresAt"].(float64); !ok {
		return nil, false
	}
	return oauth, true
}

func parseClaudeCredentialFile(text string) map[string]any {
	var value map[string]any
	if err := securejson.Unmarshal([]byte(text), &value); err != nil || value == nil {
		return nil
	}
	return value
}

// readClaudeCredentialStore mirrors TS `readClaudeCredentialStore`: the
// credentials file when it holds a usable OAuth credential, otherwise (macOS,
// default config dir only) the "Claude Code-credentials" Keychain item.
func readClaudeCredentialStore(ctx context.Context, home, configDir, credentialPath string) *claudeCredentialStore {
	if raw, err := os.ReadFile(credentialPath); err == nil {
		if value := parseClaudeCredentialFile(string(raw)); value != nil {
			if _, ok := claudeOAuth(value); ok {
				return &claudeCredentialStore{value: value, write: func(updated map[string]any) error {
					return writeClaudeCredentialFile(credentialPath, updated)
				}}
			}
		}
	}
	if claudeGOOS != "darwin" {
		return nil
	}
	defaultDir, err := filepath.Abs(filepath.Join(home, ".claude"))
	if err != nil {
		return nil
	}
	if abs, err := filepath.Abs(configDir); err != nil || abs != defaultDir {
		return nil
	}
	account := os.Getenv("USER")
	text := subscription.ReadMacOSKeychainPassword(ctx, claudeKeychainService, account)
	if text == "" {
		return nil
	}
	value := parseClaudeCredentialFile(text)
	if value == nil {
		return nil
	}
	return &claudeCredentialStore{value: value, write: func(updated map[string]any) error {
		data, err := jsstr.Marshal(updated)
		if err != nil {
			return err
		}
		return subscription.WriteMacOSKeychainPassword(ctx, claudeKeychainService, account, string(data))
	}}
}

// readClaudeCodeSubscription reads the native Claude Code subscription
// credential from `$CLAUDE_CONFIG_DIR/.credentials.json` (default
// `~/.claude/.credentials.json`) or, on macOS with the default config dir,
// the Keychain; refreshes the OAuth access token when it is expiring soon and
// writes the refreshed credential back to the store it came from. Returns
// (nil, false) when no usable credential is found; use
// resolveClaudeCodeSubscription to observe refresh errors. Mirrors TS `readClaudeCodeSubscription`.
func readClaudeCodeSubscription(ctx context.Context) (map[string]string, bool) {
	env, ok, _ := resolveClaudeCodeSubscription(ctx)
	return env, ok
}

// resolveClaudeCodeSubscription is readClaudeCodeSubscription with the
// refresh / write-back error reported: like TS, a failed OAuth refresh (or a
// failed write-back of the rotated credential) fails the caller instead of
// being treated as "no credential".
func resolveClaudeCodeSubscription(ctx context.Context) (map[string]string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, false, nil
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	if abs, err := filepath.Abs(configDir); err == nil {
		configDir = abs
	}
	credentialPath := filepath.Join(configDir, ".credentials.json")

	store := readClaudeCredentialStore(ctx, home, configDir, credentialPath)
	if store == nil {
		return nil, false, nil
	}
	oauth, ok := claudeOAuth(store.value)
	if !ok {
		return nil, false, nil
	}
	accessToken, _ := oauth["accessToken"].(string)
	refreshToken, _ := oauth["refreshToken"].(string)
	expiresAt, _ := oauth["expiresAt"].(float64)
	if accessToken == "" || refreshToken == "" {
		return nil, false, nil
	}

	if harnessutil.IsAccessTokenExpiringSoon(int64(expiresAt), time.Now().UnixMilli(), 0) {
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL: claudeTokenURL, ClientID: claudeClientID, RefreshToken: refreshToken, RequestFormat: "json",
		})
		if err != nil {
			return nil, false, err
		}
		accessToken = refreshed.AccessToken
		oauth["accessToken"] = refreshed.AccessToken
		oauth["expiresAt"] = float64(refreshed.ExpiresAt)
		if refreshed.RefreshToken != "" {
			oauth["refreshToken"] = refreshed.RefreshToken
		}
		store.value["claudeAiOauth"] = oauth
		if err := store.write(store.value); err != nil {
			return nil, false, err
		}
	}

	return map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": accessToken,
		"ANTHROPIC_BASE_URL":      claudeSubscriptionBaseURL,
	}, true, nil
}

// writeClaudeCredentialFile atomically rewrites the credential file (temp
// file in the same directory at mode 0600, then rename) after a token
// refresh. Mirrors TS `writeClaudeCredentialFile`.
func writeClaudeCredentialFile(path string, value map[string]any) error {
	data, err := jsstr.MarshalIndent(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
