package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// codexKeyring reads and writes the OS credential store entry Codex uses
// when `cli_auth_credentials_store` is "keyring" or "auto". Mirrors TS
// `CodexKeyring`.
type codexKeyring interface {
	Read(ctx context.Context, service, account string) string
	Write(ctx context.Context, service, account, value string) error
}

// newCodexKeyring returns the keyring for the host platform, nil when the
// platform has none. Replaceable in tests.
var newCodexKeyring = func() codexKeyring {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return osCodexKeyring{goos: runtime.GOOS}
	}
	return nil
}

type osCodexKeyring struct{ goos string }

func (k osCodexKeyring) Read(ctx context.Context, service, account string) string {
	switch k.goos {
	case "darwin":
		return subscription.ReadMacOSKeychainPassword(ctx, service, account)
	case "linux":
		return subscription.ReadLinuxSecretServicePassword(ctx, []subscription.SecretServiceAttribute{
			{Name: "service", Value: service}, {Name: "username", Value: account}, {Name: "target", Value: "default"},
		})
	default:
		return subscription.ReadWindowsCredentialManagerPassword(ctx, account+"."+service)
	}
}

func (k osCodexKeyring) Write(ctx context.Context, service, account, value string) error {
	switch k.goos {
	case "darwin":
		return subscription.WriteMacOSKeychainPassword(ctx, service, account, value)
	case "linux":
		return subscription.WriteLinuxSecretServicePassword(ctx, service, account, value)
	default:
		return subscription.WriteWindowsCredentialManagerPassword(ctx, account+"."+service, account, value)
	}
}

// codexSubscription is a resolved native credential: the environment for the
// Codex process plus the extra headers the credential-brokering request
// transformation must add (the ChatGPT account id).
type codexSubscription struct {
	Environment    map[string]string
	RequestHeaders map[string]string
}

// readCodexSubscription reads the native Codex (ChatGPT) subscription from
// the file store with no store-mode override.
func readCodexSubscription(ctx context.Context) (map[string]string, bool) {
	sub, ok, _ := resolveCodexSubscription(ctx, "")
	return sub.Environment, ok
}

// readCodexSubscriptionWithMode reads the native Codex (ChatGPT) subscription
// from `$CODEX_HOME` (default `~/.codex`). The credential store is chosen by
// storeMode ("file", "keyring", "auto", "ephemeral"), falling back to the
// `cli_auth_credentials_store` key of `$CODEX_HOME/config.toml`, then "file".
// The OAuth access token is refreshed when expiring soon and written back to
// the store it came from. ok is false when no usable credential is found; a
// refresh failure is treated as "not found". Mirrors TS `readCodexSubscription`
// (codex-subscription.ts).
func readCodexSubscriptionWithMode(ctx context.Context, storeMode string) (codexSubscription, bool) {
	sub, ok, _ := resolveCodexSubscription(ctx, storeMode)
	return sub, ok
}

// resolveCodexSubscription is readCodexSubscriptionWithMode with the refresh
// / write-back error reported: like TS, a failed OAuth refresh (or a failed
// write-back of the rotated credential) fails the caller instead of being
// treated as "no credential".
func resolveCodexSubscription(ctx context.Context, storeMode string) (codexSubscription, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return codexSubscription{}, false, nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if abs, err := filepath.Abs(codexHome); err == nil {
		codexHome = abs
	}
	authPath := filepath.Join(codexHome, "auth.json")
	mode := toStoreMode(storeMode)
	if mode == "" {
		mode = readStoreModeFromConfig(codexHome)
	}
	if mode == "" {
		mode = "file"
	}
	store := readCodexAuthStore(ctx, codexHome, authPath, mode)
	if store == nil {
		return codexSubscription{}, false, nil
	}
	value := store.value
	if authMode, _ := value["auth_mode"].(string); authMode != "chatgpt" {
		return codexSubscription{}, false, nil
	}
	tokens, ok := value["tokens"].(map[string]any)
	if !ok {
		return codexSubscription{}, false, nil
	}
	accessToken, _ := tokens["access_token"].(string)
	refreshToken, _ := tokens["refresh_token"].(string)
	if accessToken == "" || refreshToken == "" {
		return codexSubscription{}, false, nil
	}
	expiresAt, hasExpiry := subscription.GetJWTExpiresAt(accessToken)
	if !hasExpiry {
		return codexSubscription{}, false, nil
	}
	accountID, _ := tokens["account_id"].(string)

	if harnessutil.IsAccessTokenExpiringSoon(expiresAt, time.Now().UnixMilli(), 0) {
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL: openaiTokenURL, ClientID: openaiClientID, RefreshToken: refreshToken, RequestFormat: "json",
		})
		if err != nil {
			return codexSubscription{}, false, err
		}
		accessToken = refreshed.AccessToken
		tokens["access_token"] = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			tokens["refresh_token"] = refreshed.RefreshToken
		}
		value["tokens"] = tokens
		value["last_refresh"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		if err := store.write(value); err != nil {
			return codexSubscription{}, false, err
		}
	}

	sub := codexSubscription{Environment: map[string]string{
		"CODEX_API_KEY":   accessToken,
		"OPENAI_BASE_URL": chatGPTCodexBaseURL,
	}}
	if accountID != "" {
		sub.RequestHeaders = map[string]string{"ChatGPT-Account-ID": accountID}
	}
	return sub, true, nil
}

type codexAuthStore struct {
	value map[string]any
	write func(map[string]any) error
}

func readCodexAuthStore(ctx context.Context, codexHome, authPath, mode string) *codexAuthStore {
	if mode == "ephemeral" {
		return nil
	}
	readFileStore := func() *codexAuthStore {
		raw, err := os.ReadFile(authPath)
		if err != nil {
			return nil
		}
		value := parseCodexAuthFile(string(raw))
		if value == nil {
			return nil
		}
		return &codexAuthStore{value: value, write: func(updated map[string]any) error {
			return writeCodexAuthFile(authPath, updated)
		}}
	}
	readKeyringStore := func() *codexAuthStore {
		keyring := newCodexKeyring()
		if keyring == nil {
			return nil
		}
		canonical := codexHome
		if resolved, err := filepath.EvalSymlinks(codexHome); err == nil {
			canonical = resolved
		}
		digest := sha256.Sum256([]byte(canonical))
		account := "cli|" + hex.EncodeToString(digest[:])[:16]
		const service = "Codex Auth"
		text := keyring.Read(ctx, service, account)
		if text == "" {
			return nil
		}
		value := parseCodexAuthFile(text)
		if value == nil {
			return nil
		}
		return &codexAuthStore{value: value, write: func(updated map[string]any) error {
			data, err := jsstr.Marshal(updated)
			if err != nil {
				return err
			}
			return keyring.Write(ctx, service, account, string(data))
		}}
	}
	switch mode {
	case "file":
		return readFileStore()
	case "keyring":
		return readKeyringStore()
	}
	if s := readKeyringStore(); s != nil {
		return s
	}
	return readFileStore()
}

func parseCodexAuthFile(text string) map[string]any {
	var value map[string]any
	if err := securejson.Unmarshal([]byte(text), &value); err != nil || value == nil {
		return nil
	}
	return value
}

func toStoreMode(v string) string {
	switch v {
	case "file", "keyring", "auto", "ephemeral":
		return v
	}
	return ""
}

// jsWS is JavaScript's `\s` (Go's is ASCII-only) and jsDot its `.` (excludes
// all four line terminators), so the TS regex matches the same lines.
const (
	jsWS  = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	jsDot = `[^\n\r\x{2028}\x{2029}]`
)

var storeModeLine = regexp.MustCompile(`^` + jsWS + `*cli_auth_credentials_store` + jsWS + `*=` + jsWS + `*["'](file|keyring|auto|ephemeral)["']` + jsWS + `*(?:#` + jsDot + `*)?$`)

// readStoreModeFromConfig reads `cli_auth_credentials_store` from the
// top-level of `$CODEX_HOME/config.toml` (scanning stops at the first table
// header). Mirrors TS `readCodexAuthCredentialsStoreMode`.
func readStoreModeFromConfig(codexHome string) string {
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(providerutils.JSTrimStart(line), "[") {
			return ""
		}
		if m := storeModeLine.FindStringSubmatch(line); m != nil {
			return toStoreMode(m[1])
		}
	}
	return ""
}

// writeCodexAuthFile atomically rewrites the auth file (temp file in the
// same directory at mode 0600, then rename) after a token refresh.
func writeCodexAuthFile(path string, value map[string]any) error {
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
