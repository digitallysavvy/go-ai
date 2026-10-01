package codex

import (
	"context"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// DefaultOpenAIBaseURL mirrors TS `DEFAULT_OPENAI_BASE_URL`.
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

const defaultAIGatewayBaseURL = "https://ai-gateway.vercel.sh/v1"

// CredentialEnvironmentVariables mirrors TS
// `CODEX_CREDENTIAL_ENVIRONMENT_VARIABLES`.
var CredentialEnvironmentVariables = []string{"AI_GATEWAY_API_KEY", "CODEX_API_KEY"}

// ResolvedAuthenticationMode is the concrete route once `auth` has been
// resolved. Mirrors TS `CodexResolvedAuthenticationMode`.
type ResolvedAuthenticationMode string

const (
	AuthModeDirect    ResolvedAuthenticationMode = "direct"
	AuthModeAIGateway ResolvedAuthenticationMode = "ai-gateway"
)

// resolveAuthenticationMode mirrors TS `resolveCodexAuthenticationMode`.
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

// resolveAuthenticationEnvironment mirrors TS `resolveCodexEnv`, including
// the native-subscription fallback (`resolveCodexAuthentication`).
func resolveAuthenticationEnvironment(ctx context.Context, auth harness.Authentication, processEnv map[string]string) map[string]string {
	authEnv := processEnv
	if auth.IsEnvironment() {
		authEnv = auth.Environment
	}
	// An explicit isolated authentication environment opts out of the
	// host-filesystem native-subscription fallback: the caller asked for
	// full control over the auth environment.
	trySubscription := !auth.IsEnvironment()
	authModeString := ""
	if auth.Mode == harness.AuthModeDirect {
		authModeString = harness.AuthModeDirect
	}
	if auth.Mode == harness.AuthModeDirect {
		return pickOpenAI(ctx, authEnv, trySubscription, authModeString)
	}
	gw := harnessutil.GetAIGatewayAuthFromEnv(authEnv)
	if auth.Mode == harness.AuthModeAIGateway || gw.APIKey != "" {
		return pickGateway(gw)
	}
	return pickOpenAI(ctx, authEnv, trySubscription, authModeString)
}

func pickOpenAI(ctx context.Context, env map[string]string, trySubscription bool, authModeString string) map[string]string {
	if trySubscription {
		hasDirect := env["OPENAI_API_KEY"] != "" || env["CODEX_API_KEY"] != ""
		if harnessutil.ShouldResolveNativeSubscription(authModeString, env, hasDirect) {
			if subEnv, ok := readCodexSubscription(ctx); ok {
				return subEnv
			}
		}
	}
	out := map[string]string{}
	apiKey := env["OPENAI_API_KEY"]
	if apiKey == "" {
		apiKey = env["CODEX_API_KEY"]
	}
	if apiKey != "" {
		out["CODEX_API_KEY"] = apiKey
	}
	if v := env["OPENAI_BASE_URL"]; v != "" {
		out["OPENAI_BASE_URL"] = v
	}
	if v := env["OPENAI_ORGANIZATION"]; v != "" {
		out["OPENAI_ORGANIZATION"] = v
	}
	if v := env["OPENAI_PROJECT"]; v != "" {
		out["OPENAI_PROJECT"] = v
	}
	return out
}

func pickGateway(gw harnessutil.AIGatewayAuth) map[string]string {
	out := map[string]string{}
	baseURL := toCodexGatewayBaseURL(gw.BaseURL)
	if gw.APIKey != "" {
		out["AI_GATEWAY_API_KEY"] = gw.APIKey
		out["CODEX_API_KEY"] = gw.APIKey
	}
	out["AI_GATEWAY_BASE_URL"] = baseURL
	out["OPENAI_BASE_URL"] = baseURL
	return out
}

func toCodexGatewayBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}

// requestTransformations mirrors TS `createCodexRequestTransformations`. TS
// throws synchronously on an invalid matchUrl (`new URL(matchUrl)`), failing
// the whole `doStart` call; the Go port propagates the same error instead of
// silently dropping the transformation.
func requestTransformations(env, sandboxEnv map[string]string, authMode ResolvedAuthenticationMode) ([]harness.RequestTransformation, error) {
	if env["CODEX_API_KEY"] == "" || sandboxEnv["CODEX_API_KEY"] == "" {
		return nil, nil
	}
	matchURL := env["OPENAI_BASE_URL"]
	if matchURL == "" {
		if authMode == AuthModeAIGateway {
			matchURL = defaultAIGatewayBaseURL
		} else {
			matchURL = DefaultOpenAIBaseURL
		}
	}
	t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
		MatchURL:         matchURL,
		MatchHeaders:     map[string]string{"Authorization": "Bearer " + sandboxEnv["CODEX_API_KEY"]},
		TransformHeaders: map[string]string{"Authorization": "Bearer " + env["CODEX_API_KEY"]},
	})
	if err != nil {
		return nil, err
	}
	return []harness.RequestTransformation{t}, nil
}
