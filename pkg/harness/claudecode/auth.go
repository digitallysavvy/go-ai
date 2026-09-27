package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// CredentialEnvironmentVariables mirrors TS
// `CLAUDE_CODE_CREDENTIAL_ENVIRONMENT_VARIABLES`.
var CredentialEnvironmentVariables = []string{
	"AI_GATEWAY_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
}

// ResolvedAuthenticationMode is the concrete route once `auth` has been
// resolved. Mirrors TS `ClaudeCodeResolvedAuthenticationMode`.
type ResolvedAuthenticationMode string

const (
	AuthModeDirect    ResolvedAuthenticationMode = "direct"
	AuthModeAIGateway ResolvedAuthenticationMode = "ai-gateway"
)

// resolveAuthenticationMode mirrors TS `resolveClaudeCodeAuthenticationMode`.
func resolveAuthenticationMode(auth harness.Authentication, processEnv map[string]string) ResolvedAuthenticationMode {
	if auth.IsEnvironment() {
		if harnessutil.GetAIGatewayAuthFromEnv(auth.Environment).APIKey != "" {
			return AuthModeAIGateway
		}
		return AuthModeDirect
	}
	switch auth.Mode {
	case harness.AuthModeDirect:
		return AuthModeDirect
	case harness.AuthModeAIGateway:
		return AuthModeAIGateway
	}
	if harnessutil.GetAIGatewayAuthFromEnv(processEnv).APIKey != "" {
		return AuthModeAIGateway
	}
	return AuthModeDirect
}

// resolveAuthenticationEnvironment mirrors TS `resolveClaudeCodeEnv`,
// including the native-subscription fallback (`resolveClaudeCodeAuthentication`).
func resolveAuthenticationEnvironment(ctx context.Context, auth harness.Authentication, processEnv map[string]string) map[string]string {
	authEnv := processEnv
	if auth.IsEnvironment() {
		authEnv = auth.Environment
	}
	readHelper := readAPIKeyHelper
	if auth.IsEnvironment() || auth.Mode == harness.AuthModeDirect {
		readHelper = func() string { return "" }
	}
	// An explicit isolated authentication environment opts out of every
	// host-filesystem fallback (apiKeyHelper above, native subscription
	// here): the caller asked for full control over the auth environment.
	trySubscription := !auth.IsEnvironment()
	authModeString := ""
	if auth.Mode == harness.AuthModeDirect {
		authModeString = harness.AuthModeDirect
	}
	if auth.Mode == harness.AuthModeDirect {
		return pickAnthropic(ctx, authEnv, readHelper, trySubscription, authModeString)
	}
	gw := harnessutil.GetAIGatewayAuthFromEnv(authEnv)
	if auth.Mode == harness.AuthModeAIGateway || gw.APIKey != "" {
		return pickGateway(gw)
	}
	return pickAnthropic(ctx, authEnv, readHelper, trySubscription, authModeString)
}

func pickAnthropic(ctx context.Context, env map[string]string, readHelper func() string, trySubscription bool, authModeString string) map[string]string {
	if trySubscription {
		hasDirect := env["ANTHROPIC_API_KEY"] != "" || env["ANTHROPIC_AUTH_TOKEN"] != "" || env["CLAUDE_CODE_OAUTH_TOKEN"] != ""
		if harnessutil.ShouldResolveNativeSubscription(authModeString, env, hasDirect) {
			if subEnv, ok := readClaudeCodeSubscription(ctx); ok {
				return subEnv
			}
		}
	}
	out := map[string]string{}
	helperKey := readHelper()
	apiKey := env["ANTHROPIC_API_KEY"]
	if apiKey == "" {
		apiKey = helperKey
	}
	authToken := env["ANTHROPIC_AUTH_TOKEN"]
	if authToken == "" {
		authToken = helperKey
	}
	if apiKey != "" {
		out["ANTHROPIC_API_KEY"] = apiKey
	}
	if authToken != "" {
		out["ANTHROPIC_AUTH_TOKEN"] = authToken
	}
	if v := env["CLAUDE_CODE_OAUTH_TOKEN"]; v != "" {
		out["CLAUDE_CODE_OAUTH_TOKEN"] = v
	}
	if v := env["ANTHROPIC_BASE_URL"]; v != "" {
		out["ANTHROPIC_BASE_URL"] = v
	}
	return out
}

func pickGateway(gw harnessutil.AIGatewayAuth) map[string]string {
	out := map[string]string{}
	if gw.APIKey != "" {
		out["AI_GATEWAY_API_KEY"] = gw.APIKey
		out["ANTHROPIC_API_KEY"] = gw.APIKey
	}
	out["AI_GATEWAY_BASE_URL"] = gw.BaseURL
	out["ANTHROPIC_BASE_URL"] = gw.BaseURL
	return out
}

// readAPIKeyHelper mirrors TS `readApiKeyHelper`: runs the `apiKeyHelper`
// shell command from `~/.claude/settings.json`, matching the `claude` CLI's
// own fallback. Returns "" on any failure.
func readAPIKeyHelper() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	command := extractAPIKeyHelperCommand(raw)
	if command == "" {
		return ""
	}
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func extractAPIKeyHelperCommand(raw []byte) string {
	var settings struct {
		APIKeyHelper string `json:"apiKeyHelper"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return ""
	}
	return settings.APIKeyHelper
}

// requestTransformations mirrors TS `createClaudeCodeRequestTransformations`.
// TS throws synchronously on an invalid matchUrl (`new URL(matchUrl)`),
// failing the whole `doStart` call; the Go port propagates the same error
// instead of silently dropping the transformation.
func requestTransformations(env, sandboxEnv map[string]string, authMode ResolvedAuthenticationMode) ([]harness.RequestTransformation, error) {
	matchURL := env["ANTHROPIC_BASE_URL"]
	if authMode != AuthModeAIGateway && matchURL == "" {
		matchURL = "https://api.anthropic.com"
	}
	var out []harness.RequestTransformation
	if env["ANTHROPIC_API_KEY"] != "" && sandboxEnv["ANTHROPIC_API_KEY"] != "" {
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         matchURL,
			MatchHeaders:     map[string]string{"x-api-key": sandboxEnv["ANTHROPIC_API_KEY"]},
			TransformHeaders: map[string]string{"x-api-key": env["ANTHROPIC_API_KEY"]},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "" && sandboxEnv["ANTHROPIC_AUTH_TOKEN"] != "" {
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         matchURL,
			MatchHeaders:     map[string]string{"Authorization": "Bearer " + sandboxEnv["ANTHROPIC_AUTH_TOKEN"]},
			TransformHeaders: map[string]string{"Authorization": "Bearer " + env["ANTHROPIC_AUTH_TOKEN"]},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
