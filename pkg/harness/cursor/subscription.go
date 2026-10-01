package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// ResolveSubscriptionEnvironment resolves CURSOR_API_KEY from Cursor's native
// subscription storage when settings.auth allows it. Mirrors TS
// `resolveCursorSubscriptionEnvironment`.
func ResolveSubscriptionEnvironment(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
	if harnessutil.IsAuthenticationEnvironment(auth) {
		return auth.Environment, nil
	}
	if !harnessutil.ShouldResolveNativeSubscription(auth.Mode, env, env["CURSOR_API_KEY"] != "") {
		return env, nil
	}
	token, err := ReadSubscription(ctx, ReadSubscriptionOptions{Env: env})
	if err != nil {
		return nil, err
	}
	if token == "" {
		return env, nil
	}
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out["CURSOR_API_KEY"] = token
	return out, nil
}

// ReadSubscriptionOptions is the input of ReadSubscription.
type ReadSubscriptionOptions struct {
	Env           map[string]string
	HomeDirectory string // defaults to the user's home directory
	GOOS          string // defaults to runtime.GOOS
}

// ReadSubscription reads a Cursor CLI browser access token, from the
// platform's `auth.json` file first, then (on macOS, unless
// AGENT_CLI_CREDENTIAL_STORE=file) the system Keychain. It returns an error
// if the token is a JWT that is expiring soon (Cursor does not expose a
// refresh flow). Mirrors TS `readCursorSubscription`.
func ReadSubscription(ctx context.Context, opts ReadSubscriptionOptions) (string, error) {
	env := opts.Env
	if env == nil {
		env = map[string]string{}
	}
	if env["AGENT_CLI_CREDENTIAL_STORE"] == "memory" {
		return "", nil
	}
	homeDirectory := opts.HomeDirectory
	if homeDirectory == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		homeDirectory = h
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	authPath := ResolveAuthPath(env, homeDirectory, goos)
	accessToken := ""
	if text, err := os.ReadFile(authPath); err == nil {
		accessToken = parseAccessToken(text)
	}
	if accessToken == "" && harnessutil.IsMacOS(goos) && env["AGENT_CLI_CREDENTIAL_STORE"] != "file" {
		accessToken = subscription.ReadMacOSKeychainPassword(ctx, "cursor-access-token", "cursor-user")
	}
	if accessToken == "" {
		return "", nil
	}
	if expiresAt, ok := subscription.GetJWTExpiresAt(accessToken); ok {
		if harnessutil.IsAccessTokenExpiringSoon(expiresAt, time.Now().UnixMilli(), 0) {
			return "", errors.New("Cursor subscription access token is expiring soon. Run Cursor login again.") //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	return accessToken, nil
}

// ResolveAuthPath returns the platform-native path to Cursor's `auth.json`.
// Mirrors TS `resolveCursorAuthPath`.
func ResolveAuthPath(env map[string]string, homeDirectory, goos string) string {
	if harnessutil.IsWindows(goos) {
		appData := env["APPDATA"]
		if appData == "" {
			appData = filepath.Join(homeDirectory, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Cursor", "auth.json")
	}
	if harnessutil.IsLinux(goos) {
		xdg := env["XDG_CONFIG_HOME"]
		if xdg == "" {
			xdg = filepath.Join(homeDirectory, ".config")
		}
		return filepath.Join(xdg, "cursor", "auth.json")
	}
	return filepath.Join(homeDirectory, ".cursor", "auth.json")
}

func parseAccessToken(text []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(text, &parsed); err != nil {
		return ""
	}
	token, _ := parsed["accessToken"].(string)
	return token
}
