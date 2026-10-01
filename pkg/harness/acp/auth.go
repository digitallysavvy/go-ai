package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// errGatewayCredentialMissing mirrors the TS `Error` thrown by
// `resolveACPProviderAuthentication` when AI Gateway mode was explicitly
// selected (or forced by an incompatibility check upstream) but no gateway
// credential is available.
var errGatewayCredentialMissing = errors.New("AI Gateway authentication was selected, but neither AI_GATEWAY_API_KEY nor VERCEL_OIDC_TOKEN is set.") //nolint:staticcheck // matches TS SDK's exact error text

const defaultAIGatewayBaseURL = "https://ai-gateway.vercel.sh"

// gatewayCredentialSource names which env var supplied the AI Gateway
// credential. Mirrors TS `ACPGatewayCredentialSource`.
type gatewayCredentialSource string

const (
	gatewaySourceAPIKey    gatewayCredentialSource = "AI_GATEWAY_API_KEY"
	gatewaySourceOIDCToken gatewayCredentialSource = "VERCEL_OIDC_TOKEN"
)

// providerAuthenticationCompatibility is the resolved provider-authentication
// mode, computed once per DoStart and reused for both the identity hash and
// the actual bridge environment. Mirrors TS
// `ACPProviderAuthenticationCompatibility`.
type providerAuthenticationCompatibility struct {
	Type             string // "direct" | "ai-gateway"
	Mode             string // "auto" | "direct" | "ai-gateway"
	GatewayEnv       map[string]any
	CredentialSource gatewayCredentialSource // "" when Type != ai-gateway or no credential
	BaseURL          string
}

// authenticationProfileIdentity is a stable digest of the authentication
// configuration, used to reject an incompatible resume. Mirrors TS
// `ACPAuthenticationProfileIdentity`.
type authenticationProfileIdentity struct {
	Digest                  string `json:"digest"`
	ACPMethodID             string `json:"acpMethodId,omitempty"`
	ProviderKind            string `json:"providerKind"`
	ProviderMode            string `json:"providerMode,omitempty"`
	GatewayCredentialSource string `json:"gatewayCredentialSource,omitempty"`
}

// createAuthenticationProfileIdentity mirrors TS
// `createACPAuthenticationProfileIdentity`.
func createAuthenticationProfileIdentity(authentication *Authentication, compat *providerAuthenticationCompatibility) authenticationProfileIdentity {
	providerKind := "direct"
	if compat != nil {
		providerKind = compat.Type
	}
	payload := map[string]any{}
	if authentication != nil {
		payload["authentication"] = map[string]any{
			"methodId": authentication.MethodID, "meta": authentication.Meta, "clientCapabilities": authentication.ClientCapabilities,
		}
	} else {
		payload["authentication"] = nil
	}
	if compat != nil {
		payload["providerAuthentication"] = compat
	} else {
		payload["providerAuthentication"] = nil
	}
	sum := sha256.Sum256([]byte(stableStringify(payload)))
	id := authenticationProfileIdentity{Digest: hex.EncodeToString(sum[:]), ProviderKind: providerKind}
	if authentication != nil {
		id.ACPMethodID = authentication.MethodID
	}
	if compat != nil {
		id.ProviderMode = compat.Mode
		if compat.Type == "ai-gateway" {
			id.GatewayCredentialSource = string(compat.CredentialSource)
		}
	}
	return id
}

// resolveAuthenticationEnvironment mirrors TS
// `resolveACPAuthenticationEnvironment`: an isolated authentication
// environment (Settings.Auth built with harness.AuthEnvironment) replaces
// the host process environment for authentication discovery.
func resolveAuthenticationEnvironment(auth harness.Authentication, env map[string]string) map[string]string {
	if auth.IsEnvironment() {
		return auth.Environment
	}
	return env
}

func resolveGatewayCredentialSource(env map[string]string) gatewayCredentialSource {
	if env["AI_GATEWAY_API_KEY"] != "" {
		return gatewaySourceAPIKey
	}
	if env["VERCEL_OIDC_TOKEN"] != "" {
		return gatewaySourceOIDCToken
	}
	return ""
}

func resolveGatewayCredential(env map[string]string, source gatewayCredentialSource) string {
	switch source {
	case gatewaySourceAPIKey:
		if env["AI_GATEWAY_API_KEY"] == "" {
			return ""
		}
		return harnessutil.GetAIGatewayAuthFromEnv(env).APIKey
	case gatewaySourceOIDCToken:
		return harnessutil.GetAIGatewayAuthFromEnv(map[string]string{"VERCEL_OIDC_TOKEN": env["VERCEL_OIDC_TOKEN"]}).APIKey
	}
	return ""
}

func resolveProviderAuthenticationMode(auth harness.Authentication) string {
	if auth.IsEnvironment() || auth.Mode == "" {
		return harness.AuthModeAuto
	}
	return auth.Mode
}

// resolveProviderAuthenticationCompatibility mirrors TS
// `resolveACPProviderAuthenticationCompatibility`.
func resolveProviderAuthenticationCompatibility(auth harness.Authentication, providerAuth *ProviderAuthentication, env map[string]string) *providerAuthenticationCompatibility {
	resolvedEnv := resolveAuthenticationEnvironment(auth, env)
	if providerAuth == nil {
		return nil
	}
	mode := resolveProviderAuthenticationMode(auth)
	if mode == harness.AuthModeDirect {
		return &providerAuthenticationCompatibility{Type: "direct", Mode: mode}
	}
	credentialSource := resolveGatewayCredentialSource(resolvedEnv)
	if mode == harness.AuthModeAuto && credentialSource == "" {
		return &providerAuthenticationCompatibility{Type: "direct", Mode: mode}
	}
	baseURL := resolvedEnv["AI_GATEWAY_BASE_URL"]
	if baseURL == "" {
		baseURL = defaultAIGatewayBaseURL
	}
	return &providerAuthenticationCompatibility{
		Type: "ai-gateway", Mode: mode, GatewayEnv: providerAuth.GatewayEnv,
		CredentialSource: credentialSource, BaseURL: baseURL,
	}
}

// resolvedProviderAuthentication is the result of resolveProviderAuthentication:
// what to send the bridge (as ACP_BRIDGE_CONFIGURATION's providerAuthentication)
// plus the client-app / gateway-credential env vars to forward into the
// sandbox process.
type resolvedProviderAuthentication struct {
	Type       string // "" (none configured) | "direct" | "ai-gateway"
	GatewayEnv map[string]any
	Env        map[string]string
}

// resolveProviderAuthentication mirrors TS `resolveACPProviderAuthentication`.
func resolveProviderAuthentication(auth harness.Authentication, providerAuth *ProviderAuthentication, clientApp ClientApp, env map[string]string, compat *providerAuthenticationCompatibility) (resolvedProviderAuthentication, error) {
	clientAppEnv := map[string]string{
		"AI_SDK_ACP_CLIENT_APP_NAME": clientApp.Name, "AI_SDK_ACP_CLIENT_APP_VERSION": clientApp.Version,
	}
	resolvedEnv := resolveAuthenticationEnvironment(auth, env)
	if providerAuth == nil {
		return resolvedProviderAuthentication{Env: clientAppEnv}, nil
	}
	if compat != nil && compat.Type == "direct" {
		return resolvedProviderAuthentication{Type: "direct", Env: clientAppEnv}, nil
	}
	mode := resolveProviderAuthenticationMode(auth)
	if mode == harness.AuthModeDirect {
		return resolvedProviderAuthentication{Type: "direct", Env: clientAppEnv}, nil
	}

	var apiKey, baseURL string
	var gatewayEnv map[string]any
	if compat != nil && compat.Type == "ai-gateway" {
		apiKey = resolveGatewayCredential(resolvedEnv, compat.CredentialSource)
		baseURL = compat.BaseURL
		gatewayEnv = compat.GatewayEnv
	} else {
		gw := harnessutil.GetAIGatewayAuthFromEnv(resolvedEnv)
		apiKey, baseURL = gw.APIKey, gw.BaseURL
		gatewayEnv = providerAuth.GatewayEnv
	}
	if compat == nil && mode == harness.AuthModeAuto && apiKey == "" {
		return resolvedProviderAuthentication{Type: "direct", Env: clientAppEnv}, nil
	}
	if apiKey == "" {
		return resolvedProviderAuthentication{}, errGatewayCredentialMissing
	}
	env2 := map[string]string{
		"AI_SDK_ACP_GATEWAY_API_KEY": apiKey, "AI_SDK_ACP_GATEWAY_BASE_URL": baseURL,
	}
	for k, v := range clientAppEnv {
		env2[k] = v
	}
	return resolvedProviderAuthentication{Type: "ai-gateway", GatewayEnv: gatewayEnv, Env: env2}, nil
}
