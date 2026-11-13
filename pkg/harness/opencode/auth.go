package opencode

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
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
	"GOOGLE_GENERATIVE_AI_API_KEY",
	"XAI_API_KEY", "GITHUB_TOKEN", "GITHUB_COPILOT_TOKEN", "POE_API_KEY", "OPENCODE_API_KEY",
	"GITLAB_TOKEN", SubscriptionAccessTokenEnvironmentVariable,
}

// nonGoogleDirectCredentialEnvironmentVariables are the direct-provider
// credential variables that must all be absent/empty for automatic startup
// inference to select Google. Mirrors TS
// `NON_GOOGLE_DIRECT_CREDENTIAL_ENVIRONMENT_VARIABLES`.
var nonGoogleDirectCredentialEnvironmentVariables = []string{
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"XAI_API_KEY", "GITHUB_TOKEN", "GITHUB_COPILOT_TOKEN", "POE_API_KEY", "OPENCODE_API_KEY",
	"GITLAB_TOKEN",
}

// ResolvedAuthenticationMode is the concrete provider OpenCode resolved to.
// Mirrors TS `OpenCodeResolvedAuthenticationMode`.
type ResolvedAuthenticationMode string

const (
	AuthAnthropic     ResolvedAuthenticationMode = "anthropic"
	AuthOpenAI        ResolvedAuthenticationMode = "openai"
	AuthGoogle        ResolvedAuthenticationMode = "google"
	AuthXAI           ResolvedAuthenticationMode = "xai"
	AuthGitHubCopilot ResolvedAuthenticationMode = "github-copilot"
	AuthPoe           ResolvedAuthenticationMode = "poe"
	AuthOpenCodeGo    ResolvedAuthenticationMode = "opencode-go"
	AuthGitLab        ResolvedAuthenticationMode = "gitlab"
	AuthAIGateway     ResolvedAuthenticationMode = "ai-gateway"
)

// AuthenticationMode is the adapter `auth` setting. Mirrors TS
// `OpenCodeAuthenticationMode` = `HarnessV1Authentication<'direct'|'anthropic'|'openai'>`.
// "direct" uses the selected provider's own credentials (environment
// variables, then a native OpenCode subscription) and ignores AI Gateway
// credentials such as VERCEL_OIDC_TOKEN.
type AuthenticationMode = harness.Authentication

func isDirectProvider(v string) bool {
	switch ResolvedAuthenticationMode(v) {
	case AuthAnthropic, AuthOpenAI, AuthGoogle, AuthXAI, AuthGitHubCopilot, AuthPoe, AuthOpenCodeGo, AuthGitLab:
		return true
	}
	return false
}

// SplitOpenCodeModel splits a harness model id into its OpenCode provider
// and model components. A "provider/model" id splits on the first slash
// (with any remaining slashes kept in ModelID); a bare model id pairs with
// the given provider (and ModelID matches Model when no provider is given
// either). Mirrors TS `splitOpenCodeModel`.
func SplitOpenCodeModel(model, provider string) (providerID, modelID, fullModel string) {
	if model == "" {
		return "", "", ""
	}
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[:idx], model[idx+1:], model
	}
	full := model
	if provider != "" {
		full = provider + "/" + model
	}
	return provider, model, full
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
	selected := resolveAuthenticationProvider(auth, model, provider, authEnv)
	if (!auth.IsEnvironment() && auth.Mode == harness.AuthModeDirect) ||
		(selected == AuthOpenAI && auth.Mode == "openai") || (selected == AuthAnthropic && auth.Mode == "anthropic") {
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
		return resolveAuthenticationProvider(auth, model, provider, auth.Environment)
	}
	selected := resolveAuthenticationProvider(auth, model, provider, processEnv)
	// TS a97f49029f: direct uses the selected provider's own credentials
	// (environment, then a native subscription) and ignores AI Gateway
	// credentials such as VERCEL_OIDC_TOKEN.
	if auth.Mode == harness.AuthModeDirect {
		return selected
	}
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

// resolveAuthenticationProvider mirrors TS
// `resolveOpenCodeAuthenticationProvider`: explicit anthropic/openai auth
// modes stay authoritative over any ambient Google credential, and
// automatic startup inference (no explicit model/provider) selects Google
// only when a non-empty GOOGLE_GENERATIVE_AI_API_KEY is present with no
// non-empty competing direct-provider credential (TS #21568).
func resolveAuthenticationProvider(auth AuthenticationMode, model, provider string, environment map[string]string) ResolvedAuthenticationMode {
	selected := resolveProvider(model, provider)
	if model == "" && provider == "" && !auth.IsEnvironment() && (auth.Mode == "anthropic" || auth.Mode == "openai") {
		return ResolvedAuthenticationMode(auth.Mode)
	}
	if model == "" && provider == "" && environment["GOOGLE_GENERATIVE_AI_API_KEY"] != "" && !hasAnyNonGoogleDirectCredential(environment) {
		return AuthGoogle
	}
	return selected
}

func hasAnyNonGoogleDirectCredential(environment map[string]string) bool {
	for _, name := range nonGoogleDirectCredentialEnvironmentVariables {
		if environment[name] != "" {
			return true
		}
	}
	return false
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
	case AuthGoogle:
		names = []string{"GOOGLE_GENERATIVE_AI_API_KEY"}
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
	// TS `processEnv[name] == null ? [] : [[name, value]]`: a present empty
	// value is kept (only pickOpenAI / pickAnthropic use truthiness).
	out := map[string]string{}
	for _, k := range names {
		if v, ok := env[k]; ok {
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

// createBearerTransformation mirrors TS `createBearerTransformation`: TS
// checks `credential == null || sandboxCredential == null`, so a present
// empty value still builds the transformation (it sends only the given
// value, `Bearer ` + the empty credential).
func createBearerTransformation(environment, sandboxEnvironment map[string]string, envVarName, matchURL string) ([]harness.RequestTransformation, error) {
	credential, hasCredential := environment[envVarName]
	sandboxCredential, hasSandboxCredential := sandboxEnvironment[envVarName]
	if !hasCredential || !hasSandboxCredential {
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
		base, ok := in.Env["OPENAI_BASE_URL"] // TS `??`
		if !ok {
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
		matchURL, ok := in.Env["ANTHROPIC_BASE_URL"] // TS `??`
		if !ok {
			matchURL = "https://api.anthropic.com"
		}
		var out []harness.RequestTransformation
		// TS `!= null` (presence), unlike the truthy openai / google checks.
		if hasKey(in.Env, "ANTHROPIC_API_KEY") && hasKey(in.SandboxEnv, "ANTHROPIC_API_KEY") {
			t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
				MatchURL: matchURL, MatchHeaders: map[string]string{"x-api-key": in.SandboxEnv["ANTHROPIC_API_KEY"]},
				TransformHeaders: map[string]string{"x-api-key": in.Env["ANTHROPIC_API_KEY"]},
			})
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		if hasKey(in.Env, "ANTHROPIC_AUTH_TOKEN") && hasKey(in.SandboxEnv, "ANTHROPIC_AUTH_TOKEN") {
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
	case AuthGoogle:
		if in.Env["GOOGLE_GENERATIVE_AI_API_KEY"] == "" || in.SandboxEnv["GOOGLE_GENERATIVE_AI_API_KEY"] == "" {
			return nil, nil
		}
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: "https://generativelanguage.googleapis.com", MatchHeaders: map[string]string{"x-goog-api-key": in.SandboxEnv["GOOGLE_GENERATIVE_AI_API_KEY"]},
			TransformHeaders: map[string]string{"x-goog-api-key": in.Env["GOOGLE_GENERATIVE_AI_API_KEY"]},
		})
		if err != nil {
			return nil, err
		}
		return []harness.RequestTransformation{t}, nil
	case AuthXAI:
		base, ok := in.Env["XAI_BASE_URL"] // TS `??`
		if !ok {
			base = "https://api.x.ai/v1"
		}
		return createBearerTransformation(in.Env, in.SandboxEnv, "XAI_API_KEY", base)
	case AuthGitHubCopilot:
		name := "GITHUB_TOKEN"
		if hasKey(in.Env, "GITHUB_COPILOT_TOKEN") { // TS `!= null`
			name = "GITHUB_COPILOT_TOKEN"
		}
		return createBearerTransformation(in.Env, in.SandboxEnv, name, "https://api.githubcopilot.com")
	case AuthPoe:
		return createBearerTransformation(in.Env, in.SandboxEnv, "POE_API_KEY", "https://api.poe.com")
	case AuthOpenCodeGo:
		return createBearerTransformation(in.Env, in.SandboxEnv, "OPENCODE_API_KEY", "https://opencode.ai/zen/go/v1")
	case AuthGitLab:
		return createGitLabRequestTransformations(in.Env, in.SandboxEnv)
	}
	return nil, nil
}

// gitLabDirectAccessPath is the chat token-exchange route OpenCode's GitLab
// provider POSTs to with GITLAB_TOKEN (TS 138d66eb78).
const gitLabDirectAccessPath = "/api/v4/ai/third_party_agents/direct_access"

// createGitLabRequestTransformations restricts the real GITLAB_TOKEN to the
// POST chat token exchange; every other GitLab request keeps the sandbox
// placeholder. The URL is built by string concatenation and parsed with the
// WHATWG parser exactly like TS `new URL(`${GITLAB_INSTANCE_URL ??
// 'https://gitlab.com'}/api/v4/...`)`, so a trailing slash on the instance
// URL yields a `//api/v4/...` path that never matches (fails closed, as in
// TS). Mirrors TS `opencode-auth.ts` 175-199.
func createGitLabRequestTransformations(environment, sandboxEnvironment map[string]string) ([]harness.RequestTransformation, error) {
	// TS `credential == null || sandboxCredential == null`: a present empty
	// GITLAB_TOKEN still builds the rule; it can only send the given value.
	credential, hasCredential := environment["GITLAB_TOKEN"]
	sandboxCredential, hasSandboxCredential := sandboxEnvironment["GITLAB_TOKEN"]
	if !hasCredential || !hasSandboxCredential {
		return nil, nil
	}
	base, ok := environment["GITLAB_INSTANCE_URL"]
	if !ok {
		base = "https://gitlab.com"
	}
	requestURL, err := urlnorm.Parse(base + gitLabDirectAccessPath)
	if err != nil {
		// TS `new URL` throws a TypeError whose message is just "Invalid URL";
		// the input is not echoed (an instance URL can carry userinfo).
		return nil, urlnorm.ErrInvalidURL
	}
	return []harness.RequestTransformation{{
		Match: harness.RequestTransformationMatch{
			Host:   requestURL.Host,
			Path:   &harness.StringMatcher{Exact: requestURL.Pathname()},
			Method: []string{"POST"},
			Headers: []harness.KeyValueMatcher{{
				Key:   &harness.StringMatcher{Exact: "Authorization"},
				Value: &harness.StringMatcher{Exact: "Bearer " + sandboxCredential},
			}},
		},
		Transform: harness.RequestTransformationTransform{Headers: map[string]string{"Authorization": "Bearer " + credential}},
	}}, nil
}
