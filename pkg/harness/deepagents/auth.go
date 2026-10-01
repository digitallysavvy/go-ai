package deepagents

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// CredentialEnvironmentVariables are the environment variables DeepAgents
// credential forwarding/brokering considers. Mirrors TS
// `DEEPAGENTS_CREDENTIAL_ENVIRONMENT_VARIABLES`.
var CredentialEnvironmentVariables = []string{"AI_GATEWAY_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// ResolvedAuthenticationMode is the concrete mode DeepAgents resolved to.
// Mirrors TS `DeepAgentsResolvedAuthenticationMode`.
type ResolvedAuthenticationMode string

const (
	AuthAnthropic ResolvedAuthenticationMode = "anthropic"
	AuthAIGateway ResolvedAuthenticationMode = "ai-gateway"
)

// AuthenticationMode is the adapter `auth` setting. Mirrors TS
// `DeepAgentsAuthenticationMode` = `HarnessV1Authentication<'anthropic'>`.
type AuthenticationMode = harness.Authentication

// createRequestTransformationsInput is the input of
// createDeepAgentsRequestTransformations.
type createRequestTransformationsInput struct {
	Env        map[string]string
	SandboxEnv map[string]string
	Auth       ResolvedAuthenticationMode
}

// createDeepAgentsRequestTransformations mirrors TS
// `createDeepAgentsRequestTransformations`.
func createDeepAgentsRequestTransformations(in createRequestTransformationsInput) ([]harness.RequestTransformation, error) {
	matchURL := in.Env["ANTHROPIC_BASE_URL"]
	if in.Auth != AuthAIGateway && matchURL == "" {
		matchURL = "https://api.anthropic.com"
	}

	var out []harness.RequestTransformation
	if apiKey := in.Env["ANTHROPIC_API_KEY"]; apiKey != "" && in.SandboxEnv["ANTHROPIC_API_KEY"] != "" {
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         matchURL,
			MatchHeaders:     map[string]string{"x-api-key": in.SandboxEnv["ANTHROPIC_API_KEY"]},
			TransformHeaders: map[string]string{"x-api-key": apiKey},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if authToken := in.Env["ANTHROPIC_AUTH_TOKEN"]; authToken != "" && in.SandboxEnv["ANTHROPIC_AUTH_TOKEN"] != "" {
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         matchURL,
			MatchHeaders:     map[string]string{"Authorization": "Bearer " + in.SandboxEnv["ANTHROPIC_AUTH_TOKEN"]},
			TransformHeaders: map[string]string{"Authorization": "Bearer " + authToken},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// resolveEnv mirrors TS `resolveDeepAgentsEnv`. DeepAgents always drives the
// Anthropic client; non-Anthropic models reach it through AI Gateway's
// Anthropic-compatible endpoint.
func resolveEnv(auth AuthenticationMode, processEnv map[string]string) map[string]string {
	authEnv := processEnv
	if auth.IsEnvironment() {
		authEnv = auth.Environment
	}
	if auth.Mode == harness.AuthModeDirect {
		return pickAnthropic(authEnv)
	}
	gateway := harnessutil.GetAIGatewayAuthFromEnv(authEnv)
	if auth.Mode == harness.AuthModeAIGateway || gateway.APIKey != "" {
		return pickGateway(gateway)
	}
	return pickAnthropic(authEnv)
}

// resolveAuthenticationMode mirrors TS `resolveDeepAgentsAuthenticationMode`.
func resolveAuthenticationMode(auth AuthenticationMode, processEnv map[string]string) ResolvedAuthenticationMode {
	if auth.IsEnvironment() {
		if harnessutil.GetAIGatewayAuthFromEnv(auth.Environment).APIKey != "" {
			return AuthAIGateway
		}
		return AuthAnthropic
	}
	switch auth.Mode {
	case harness.AuthModeDirect:
		return AuthAnthropic
	case harness.AuthModeAIGateway:
		return AuthAIGateway
	}
	if harnessutil.GetAIGatewayAuthFromEnv(processEnv).APIKey != "" {
		return AuthAIGateway
	}
	return AuthAnthropic
}

func pickAnthropic(processEnv map[string]string) map[string]string {
	env := map[string]string{}
	if v := processEnv["ANTHROPIC_API_KEY"]; v != "" {
		env["ANTHROPIC_API_KEY"] = v
	}
	if v := processEnv["ANTHROPIC_AUTH_TOKEN"]; v != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = v
	}
	if v := processEnv["ANTHROPIC_BASE_URL"]; v != "" {
		env["ANTHROPIC_BASE_URL"] = v
	}
	return env
}

func pickGateway(gateway harnessutil.AIGatewayAuth) map[string]string {
	env := map[string]string{}
	if gateway.APIKey != "" {
		env["AI_GATEWAY_API_KEY"] = gateway.APIKey
		env["ANTHROPIC_API_KEY"] = gateway.APIKey
	}
	// The Anthropic SDK appends /v1/messages, so the gateway base stays at
	// its root.
	env["ANTHROPIC_BASE_URL"] = strings.TrimRight(gateway.BaseURL, "/")
	return env
}
