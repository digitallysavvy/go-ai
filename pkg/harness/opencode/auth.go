package opencode

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// SubscriptionAccessTokenEnvironmentVariable mirrors TS
// `OPENCODE_SUBSCRIPTION_ACCESS_TOKEN_ENVIRONMENT_VARIABLE`. Set on
// resolvedAuthEnvironment (in resolveOpenCodeAuthentication) when a native
// OpenCode subscription was read; brokered like any other credential (see
// subscription.go) and never forwarded to the sandbox verbatim.
const SubscriptionAccessTokenEnvironmentVariable = "AI_SDK_OPENCODE_NATIVE_ACCESS_TOKEN"

// CredentialEnvironmentVariables are the environment variables OpenCode
// credential forwarding/brokering considers. Mirrors TS
// `OPENCODE_CREDENTIAL_ENVIRONMENT_VARIABLES`.
var CredentialEnvironmentVariables = []string{
	"AI_GATEWAY_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"XAI_API_KEY", "GITHUB_TOKEN", "GITHUB_COPILOT_TOKEN", "POE_API_KEY", "OPENCODE_API_KEY",
	"GITLAB_TOKEN", SubscriptionAccessTokenEnvironmentVariable,
}

// ResolvedAuthenticationMode is the concrete provider OpenCode resolved to.
// Mirrors TS `OpenCodeResolvedAuthenticationMode`.
type ResolvedAuthenticationMode string

const (
	AuthAnthropic     ResolvedAuthenticationMode = "anthropic"
	AuthOpenAI        ResolvedAuthenticationMode = "openai"
	AuthXAI           ResolvedAuthenticationMode = "xai"
	AuthGitHubCopilot ResolvedAuthenticationMode = "github-copilot"
	AuthPoe           ResolvedAuthenticationMode = "poe"
	AuthOpenCodeGo    ResolvedAuthenticationMode = "opencode-go"
	AuthGitLab        ResolvedAuthenticationMode = "gitlab"
	AuthAIGateway     ResolvedAuthenticationMode = "ai-gateway"
)

// AuthenticationMode is the adapter `auth` setting. Mirrors TS
// `OpenCodeAuthenticationMode` = `HarnessV1Authentication<'anthropic'|'openai'>`.
type AuthenticationMode = harness.Authentication

func isDirectProvider(v string) bool {
	switch ResolvedAuthenticationMode(v) {
	case AuthAnthropic, AuthOpenAI, AuthXAI, AuthGitHubCopilot, AuthPoe, AuthOpenCodeGo, AuthGitLab:
		return true
	}
	return false
}

// resolveProvider mirrors TS `resolveOpenCodeProvider`.
func resolveProvider(model, provider string) ResolvedAuthenticationMode {
	if isDirectProvider(provider) {
		return ResolvedAuthenticationMode(provider)
	}
	if strings.Contains(model, "/") {
		modelProvider, _, _ := strings.Cut(model, "/")
		if isDirectProvider(modelProvider) {
			return ResolvedAuthenticationMode(modelProvider)
		}
	}
	return AuthAnthropic
}

// resolveEnv mirrors TS `resolveOpenCodeEnv`.
func resolveEnv(auth AuthenticationMode, model, provider string, processEnv map[string]string) map[string]string {
	authEnv := processEnv
	if auth.IsEnvironment() {
		authEnv = auth.Environment
	}
	selected := resolveProvider(model, provider)
	if (selected == AuthOpenAI && auth.Mode == "openai") || (selected == AuthAnthropic && auth.Mode == "anthropic") {
		return pickDirectProvider(selected, authEnv)
	}
	gateway := harnessutil.GetAIGatewayAuthFromEnv(authEnv)
	if auth.Mode == harness.AuthModeAIGateway || gateway.APIKey != "" {
		return pickGateway(gateway)
	}
	return pickDirectProvider(selected, authEnv)
}

// resolveAuthenticationMode mirrors TS `resolveOpenCodeAuthenticationMode`.
func resolveAuthenticationMode(auth AuthenticationMode, model, provider string, processEnv map[string]string) ResolvedAuthenticationMode {
	if auth.IsEnvironment() {
		if harnessutil.GetAIGatewayAuthFromEnv(auth.Environment).APIKey != "" {
			return AuthAIGateway
		}
		return resolveProvider(model, provider)
	}
	selected := resolveProvider(model, provider)
	if selected == AuthOpenAI && auth.Mode == "openai" {
		return AuthOpenAI
	}
	if selected == AuthAnthropic && auth.Mode == "anthropic" {
		return AuthAnthropic
	}
	if auth.Mode == harness.AuthModeAIGateway {
		return AuthAIGateway
	}
	if harnessutil.GetAIGatewayAuthFromEnv(processEnv).APIKey != "" {
		return AuthAIGateway
	}
	return selected
}

func pickOpenAI(env map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORGANIZATION", "OPENAI_PROJECT"} {
		if v := env[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

func pickAnthropic(env map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		if v := env[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

func pickDirectProvider(provider ResolvedAuthenticationMode, env map[string]string) map[string]string {
	switch provider {
	case AuthOpenAI:
		return pickOpenAI(env)
	case AuthAnthropic:
		return pickAnthropic(env)
	}
	var names []string
	switch provider {
	case AuthXAI:
		names = []string{"XAI_API_KEY", "XAI_BASE_URL"}
	case AuthGitHubCopilot:
		names = []string{"GITHUB_COPILOT_TOKEN", "GITHUB_TOKEN"}
	case AuthPoe:
		names = []string{"POE_API_KEY"}
	case AuthOpenCodeGo:
		names = []string{"OPENCODE_API_KEY"}
	default:
		names = []string{"GITLAB_TOKEN", "GITLAB_INSTANCE_URL"}
	}
	out := map[string]string{}
	for _, k := range names {
		if v := env[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

func pickGateway(gateway harnessutil.AIGatewayAuth) map[string]string {
	out := map[string]string{}
	if gateway.APIKey != "" {
		out["AI_GATEWAY_API_KEY"] = gateway.APIKey
	}
	out["AI_GATEWAY_BASE_URL"] = toGatewayBaseURL(gateway.BaseURL)
	return out
}

// toGatewayBaseURL mirrors TS `toOpenCodeGatewayBaseUrl`.
func toGatewayBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}

func createBearerTransformation(environment, sandboxEnvironment map[string]string, envVarName, matchURL string) ([]harness.RequestTransformation, error) {
	credential := environment[envVarName]
	sandboxCredential := sandboxEnvironment[envVarName]
	if credential == "" || sandboxCredential == "" {
		return nil, nil
	}
	t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
		MatchURL: matchURL, MatchHeaders: map[string]string{"Authorization": "Bearer " + sandboxCredential},
		TransformHeaders: map[string]string{"Authorization": "Bearer " + credential},
	})
	if err != nil {
		return nil, err
	}
	return []harness.RequestTransformation{t}, nil
}

// createRequestTransformationsInput is the input of
// createOpenCodeRequestTransformations.
type createRequestTransformationsInput struct {
	Env        map[string]string
	SandboxEnv map[string]string
	Auth       ResolvedAuthenticationMode
}

// createOpenCodeRequestTransformations mirrors TS
// `createOpenCodeRequestTransformations`.
func createOpenCodeRequestTransformations(in createRequestTransformationsInput) ([]harness.RequestTransformation, error) {
	switch in.Auth {
	case AuthAIGateway:
		if in.Env["AI_GATEWAY_API_KEY"] == "" || in.SandboxEnv["AI_GATEWAY_API_KEY"] == "" {
			return nil, nil
		}
		t1, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: in.Env["AI_GATEWAY_BASE_URL"], MatchHeaders: map[string]string{"x-api-key": in.SandboxEnv["AI_GATEWAY_API_KEY"]},
			TransformHeaders: map[string]string{"Authorization": "Bearer " + in.Env["AI_GATEWAY_API_KEY"]},
		})
		if err != nil {
			return nil, err
		}
		t2, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: in.Env["AI_GATEWAY_BASE_URL"], MatchHeaders: map[string]string{"Authorization": "Bearer " + in.SandboxEnv["AI_GATEWAY_API_KEY"]},
			TransformHeaders: map[string]string{"Authorization": "Bearer " + in.Env["AI_GATEWAY_API_KEY"]},
		})
		if err != nil {
			return nil, err
		}
		return []harness.RequestTransformation{t1, t2}, nil
	case AuthOpenAI:
		if in.Env["OPENAI_API_KEY"] == "" || in.SandboxEnv["OPENAI_API_KEY"] == "" {
			return nil, nil
		}
		base := in.Env["OPENAI_BASE_URL"]
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: base, MatchHeaders: map[string]string{"Authorization": "Bearer " + in.SandboxEnv["OPENAI_API_KEY"]},
			TransformHeaders: map[string]string{"Authorization": "Bearer " + in.Env["OPENAI_API_KEY"]},
		})
		if err != nil {
			return nil, err
		}
		return []harness.RequestTransformation{t}, nil
	case AuthAnthropic:
		matchURL := in.Env["ANTHROPIC_BASE_URL"]
		if matchURL == "" {
			matchURL = "https://api.anthropic.com"
		}
		var out []harness.RequestTransformation
		if in.Env["ANTHROPIC_API_KEY"] != "" && in.SandboxEnv["ANTHROPIC_API_KEY"] != "" {
			t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
				MatchURL: matchURL, MatchHeaders: map[string]string{"x-api-key": in.SandboxEnv["ANTHROPIC_API_KEY"]},
				TransformHeaders: map[string]string{"x-api-key": in.Env["ANTHROPIC_API_KEY"]},
			})
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		if in.Env["ANTHROPIC_AUTH_TOKEN"] != "" && in.SandboxEnv["ANTHROPIC_AUTH_TOKEN"] != "" {
			t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
				MatchURL: matchURL, MatchHeaders: map[string]string{"Authorization": "Bearer " + in.SandboxEnv["ANTHROPIC_AUTH_TOKEN"]},
				TransformHeaders: map[string]string{"Authorization": "Bearer " + in.Env["ANTHROPIC_AUTH_TOKEN"]},
			})
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, nil
	case AuthXAI:
		base := in.Env["XAI_BASE_URL"]
		if base == "" {
			base = "https://api.x.ai/v1"
		}
		return createBearerTransformation(in.Env, in.SandboxEnv, "XAI_API_KEY", base)
	case AuthGitHubCopilot:
		name := "GITHUB_TOKEN"
		if in.Env["GITHUB_COPILOT_TOKEN"] != "" {
			name = "GITHUB_COPILOT_TOKEN"
		}
		return createBearerTransformation(in.Env, in.SandboxEnv, name, "https://api.githubcopilot.com")
	case AuthPoe:
		return createBearerTransformation(in.Env, in.SandboxEnv, "POE_API_KEY", "https://api.poe.com")
	case AuthOpenCodeGo:
		return createBearerTransformation(in.Env, in.SandboxEnv, "OPENCODE_API_KEY", "https://opencode.ai/zen/go/v1")
	case AuthGitLab:
		base := in.Env["GITLAB_INSTANCE_URL"]
		if base == "" {
			base = "https://gitlab.com"
		}
		return createBearerTransformation(in.Env, in.SandboxEnv, "GITLAB_TOKEN", base)
	}
	return nil, nil
}
