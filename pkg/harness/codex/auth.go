package codex

import (
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

// resolveAuthenticationEnvironment mirrors TS `resolveCodexEnv`.
func resolveAuthenticationEnvironment(auth harness.Authentication, processEnv map[string]string) map[string]string {
	authEnv := processEnv
	if auth.IsEnvironment() {
		authEnv = auth.Environment
	}
	if auth.Mode == harness.AuthModeDirect {
		return pickOpenAI(authEnv)
	}
	gw := harnessutil.GetAIGatewayAuthFromEnv(authEnv)
	if auth.Mode == harness.AuthModeAIGateway || gw.APIKey != "" {
		return pickGateway(gw)
	}
	return pickOpenAI(authEnv)
}

func pickOpenAI(env map[string]string) map[string]string {
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
