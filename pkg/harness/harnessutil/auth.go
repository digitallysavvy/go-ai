package harnessutil

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultAIGatewayBaseURL is the AI Gateway base URL used when
// AI_GATEWAY_BASE_URL is unset.
const DefaultAIGatewayBaseURL = "https://ai-gateway.vercel.sh"

// AIGatewayAuth is the result of GetAIGatewayAuthFromEnv.
type AIGatewayAuth struct {
	// APIKey is empty when no gateway credential is configured.
	APIKey  string
	BaseURL string
}

// GetAIGatewayAuthFromEnv reads AI Gateway credentials: AI_GATEWAY_API_KEY,
// falling back to VERCEL_OIDC_TOKEN (534dac6), and AI_GATEWAY_BASE_URL.
// Mirrors TS `getAiGatewayAuthFromEnv`.
func GetAIGatewayAuthFromEnv(env map[string]string) AIGatewayAuth {
	apiKey := env["AI_GATEWAY_API_KEY"]
	if apiKey == "" {
		apiKey = env["VERCEL_OIDC_TOKEN"]
	}
	baseURL, ok := env["AI_GATEWAY_BASE_URL"]
	if !ok {
		baseURL = DefaultAIGatewayBaseURL
	}
	return AIGatewayAuth{APIKey: apiKey, BaseURL: baseURL}
}

// IsAuthenticationEnvironment reports whether auth is an isolated
// authentication environment (e0d7cfb). Mirrors TS
// `isHarnessAuthenticationEnvironment`; non-flat records are rejected at JSON
// decode time by harness.Authentication.
func IsAuthenticationEnvironment(auth harness.Authentication) bool {
	return auth.IsEnvironment()
}

// AuthenticationEnvironment returns the environment used for authentication
// discovery: the isolated environment when auth supplies one (it replaces the
// host environment), otherwise processEnv.
func AuthenticationEnvironment(auth harness.Authentication, processEnv map[string]string) map[string]string {
	if auth.IsEnvironment() {
		return auth.Environment
	}
	return processEnv
}

// ProcessEnv returns the current process environment as a map.
func ProcessEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// ShouldResolveNativeSubscription reports whether an adapter should look up
// adapter-native subscription credentials (cdc12a1): not when auth is
// "ai-gateway", not when a direct credential exists, and in auto mode only
// when no AI Gateway credential is configured. auth is the string mode ("" for
// unset). Mirrors TS `shouldResolveNativeSubscription`.
func ShouldResolveNativeSubscription(auth string, env map[string]string, hasDirectCredential bool) bool {
	return auth != harness.AuthModeAIGateway &&
		!hasDirectCredential &&
		(auth == harness.AuthModeDirect || GetAIGatewayAuthFromEnv(env).APIKey == "")
}

// ClientApp returns the value adapters send as `User-Agent` / `x-client-app`
// (b2d0306), e.g. ClientApp("claude-code", "1.0.0") =
// "ai-sdk/harness-claude-code/1.0.0".
func ClientApp(harnessID, version string) string {
	return fmt.Sprintf("ai-sdk/harness-%s/%s", harnessID, version)
}

// ManagedHeaderNames are headers the harness manages itself; callers may not
// set them via HarnessAgent `headers` (lowercase).
var ManagedHeaderNames = []string{"authorization", "x-api-key", "user-agent", "x-client-app"}

// NormalizeHeaders lowercases header names and rejects managed headers,
// mirroring the HarnessAgent constructor validation (eeed977). It returns nil
// for an empty result.
func NormalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	normalized := make(map[string]string, len(headers))
	for name, value := range headers {
		lower := strings.ToLower(name)
		for _, managed := range ManagedHeaderNames {
			if lower == managed {
				return nil, fmt.Errorf("HarnessAgent: `headers` must not include the managed header `%s`.", lower)
			}
		}
		normalized[lower] = value
	}
	return normalized, nil
}

// OS predicates (TS `isMacOS` / `isLinux` / `isWindows`, taking a Node
// platform). They accept Go GOOS values; "win32" is accepted for Windows.
func IsMacOS(goos string) bool   { return goos == "darwin" }
func IsLinux(goos string) bool   { return goos == "linux" }
func IsWindows(goos string) bool { return goos == "windows" || goos == "win32" }

// CurrentOS is runtime.GOOS, for use with the predicates above.
func CurrentOS() string { return runtime.GOOS }

// ShellQuote single-quotes value for POSIX shells (43a8c68). Mirrors TS
// `shellQuote`.
func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// ResolveSandboxHomeDir re-exports harness.ResolveSandboxHomeDir (TS utils
// `resolveSandboxHomeDir`).
func ResolveSandboxHomeDir(ctx context.Context, sandbox providerutils.SandboxSession) (string, error) {
	return harness.ResolveSandboxHomeDir(ctx, sandbox)
}

// ResolveSandboxDefaultWorkingDirectory re-exports the harness helper.
func ResolveSandboxDefaultWorkingDirectory(ctx context.Context, sandbox providerutils.SandboxSession) (string, error) {
	return harness.ResolveSandboxDefaultWorkingDirectory(ctx, sandbox)
}

// GetRestrictedSandboxSession re-exports the harness helper.
func GetRestrictedSandboxSession(sandbox providerutils.SandboxSession) providerutils.SandboxSession {
	return harness.GetRestrictedSandboxSession(sandbox)
}
