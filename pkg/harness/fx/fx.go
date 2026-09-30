// Package fx is the Go port of the TypeScript `@ai-sdk/harness-fx` package
// (pinned to ai@7.0.113, hash d0b241d).
//
// Like pkg/harness/cursor, fx is an "ACP-derived" adapter: TS `createFx()`
// is a thin configuration layer over `createACP()` (`@ai-sdk/harness-acp`).
// BuildConfig assembles the exact acp.Settings TS's `createFx()` passes to
// `createACP()`, and CreateFx wires it into a live harness.Harness.
//
// fx is server-side/sandboxable: `fx acp` is spawned inside the sandbox by
// the shared ACP bridge (pkg/harness/bridges.ACP); this package has no
// bridge assets of its own.
package fx

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

const (
	ClientAppName    = "ai-sdk/harness-fx"
	ClientAppVersion = "0.0.0-go"
	HarnessID        = "fx"

	defaultAIGatewayBaseURL = "https://ai-gateway.vercel.sh"
)

// AuthenticationMode is TS `FxAuthenticationMode`.
type AuthenticationMode = harness.Authentication

// Settings configures the fx harness adapter. Mirrors TS `FxHarnessSettings`.
type Settings struct {
	Auth                 AuthenticationMode
	CredentialForwarding harness.CredentialForwarding
	Port                 *int
	PortEndpoint         *harness.PortEndpoint
	StartupTimeoutMS     *int
	Reconnect            *bridge.ReconnectOptions
	MCPServers           map[string]any
	MintBridgeToken      harness.MintBridgeTokenCallback
}

// CreateFx returns the fx harness-v1 adapter, wiring BuildConfig's
// acp.Settings into a live harness.Harness via acp.CreateACP. Mirrors TS
// `createFx()`.
func CreateFx(settings ...Settings) (harness.Harness, error) {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	return acp.CreateACP(BuildConfig(s))
}

// BuildConfig assembles the acp.Settings `createFx()` builds (TS
// `ACPHarnessSettings` as passed to `createACP()`).
func BuildConfig(settings Settings) acp.Settings {
	mcpToolTitlePrefixes := make([]string, 0, len(settings.MCPServers))
	for name := range settings.MCPServers {
		mcpToolTitlePrefixes = append(mcpToolTitlePrefixes, "mcp_"+sanitizeMCPToolNameSegment(name)+"_")
	}
	suppliedAuthenticationEnvironment := harnessutil.IsAuthenticationEnvironment(settings.Auth)

	cfg := acp.Settings{
		HarnessID: HarnessID,
		ClientApp: acp.ClientApp{Name: ClientAppName, Version: ClientAppVersion},
		Auth:      settings.Auth,
		ResolveAuthenticationEnvironment: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		AuthenticationFiles:  CreateSubscriptionAuthenticationFiles,
		CredentialForwarding: settings.CredentialForwarding,
		PortEndpoint:         settings.PortEndpoint,
		MCPServers:           settings.MCPServers,
		IsMcpToolCall: func(call acp.ToolCall) bool {
			for _, prefix := range mcpToolTitlePrefixes {
				if strings.HasPrefix(call.Title, prefix) {
					return true
				}
			}
			return false
		},
		MintBridgeToken: settings.MintBridgeToken,
		BuiltinTools:    BuiltinTools,
		Source: acp.Source{
			Type:    acp.SourceInstallCommand,
			Command: "curl -fsSL https://fx.sh/setup.sh | bash",
		},
		Executable: "fx",
		Args:       []string{"acp"},
		ModelMapping: acp.ModelMapping{
			Type: acp.ModelMappingSessionConfigOption,
			Path: "model",
		},
		InstructionMapping: &acp.InstructionMapping{
			Type:     acp.InstructionMappingFilesystem,
			FilePath: ".fx/AGENTS.md",
		},
		CredentialEnv: append([]string{"VERCEL_OIDC_TOKEN", "AI_GATEWAY_API_KEY"}, SubscriptionEnvironmentVariables...),
		CredentialBrokering: func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			return CredentialBrokering(env, sandboxEnv, headers, suppliedAuthenticationEnvironment)
		},
		ProviderAuthentication: &acp.ProviderAuthentication{
			GatewayEnv: map[string]any{
				"AI_GATEWAY_API_KEY":  map[string]any{"$source": "gateway-api-key"},
				"AI_GATEWAY_BASE_URL": map[string]any{"$source": "gateway-base-url"},
			},
		},
		PermissionModeMapping: &acp.PermissionModeMapping{
			AllowReads: &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "ask"},
			AllowEdits: &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "ask"},
			AllowAll:   &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "code"},
		},
	}
	if settings.Port != nil {
		cfg.Port = *settings.Port
	}
	if settings.StartupTimeoutMS != nil {
		cfg.StartupTimeout = time.Duration(*settings.StartupTimeoutMS) * time.Millisecond
	}
	if settings.Reconnect != nil {
		cfg.Reconnect = *settings.Reconnect
	}
	return cfg
}

// CredentialBrokering builds the request transformations for fx's
// subscription and AI Gateway credentials. Mirrors the `credentialBrokering`
// closure in `createFx()`.
func CredentialBrokering(env, sandboxEnv, headers map[string]string, suppliedAuthenticationEnvironment bool) []harness.RequestTransformation {
	var transformations []harness.RequestTransformation
	for _, rc := range GetSubscriptionRequestCredentials(env, sandboxEnv) {
		matchURL := "https://api.x.ai/v1"
		if rc.Provider == "chatgpt" {
			matchURL = "https://chatgpt.com/backend-api/codex"
		}
		transformHeaders := map[string]string{}
		for k, v := range headers {
			transformHeaders[k] = v
		}
		transformHeaders["Authorization"] = "Bearer " + rc.AccessToken
		transformHeaders["x-client-app"] = ClientAppName + "/" + ClientAppVersion
		tr, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         matchURL,
			MatchHeaders:     map[string]string{"Authorization": "Bearer " + rc.SandboxAccessToken},
			TransformHeaders: transformHeaders,
		})
		if err != nil {
			continue
		}
		transformations = append(transformations, tr)
	}
	if len(transformations) > 0 {
		return transformations
	}

	environmentVariableName := "AI_GATEWAY_API_KEY"
	if suppliedAuthenticationEnvironment {
		if env["AI_GATEWAY_API_KEY"] == "" {
			environmentVariableName = "VERCEL_OIDC_TOKEN"
		}
	} else if env["VERCEL_OIDC_TOKEN"] != "" {
		environmentVariableName = "VERCEL_OIDC_TOKEN"
	}
	credential := env[environmentVariableName]
	sandboxCredential := sandboxEnv[environmentVariableName]
	if credential == "" || sandboxCredential == "" {
		return nil
	}
	baseURL := env["AI_GATEWAY_BASE_URL"]
	if baseURL == "" {
		baseURL = defaultAIGatewayBaseURL
	}
	transformHeaders := map[string]string{}
	for k, v := range headers {
		transformHeaders[k] = v
	}
	transformHeaders["Authorization"] = "Bearer " + credential
	transformHeaders["x-client-app"] = ClientAppName + "/" + ClientAppVersion
	tr, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
		MatchURL:         baseURL,
		MatchHeaders:     map[string]string{"Authorization": "Bearer " + sandboxCredential},
		TransformHeaders: transformHeaders,
	})
	if err != nil {
		return nil
	}
	return []harness.RequestTransformation{tr}
}

// sanitizeMCPToolNameSegment mirrors TS `sanitizeFxMcpToolNameSegment`:
// non-alphanumeric, non `-`/`_` bytes become `_`; an empty segment becomes
// "server".
func sanitizeMCPToolNameSegment(value string) string {
	if value == "" {
		return "server"
	}
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' || r == '_' {
			out = append(out, r)
			continue
		}
		if r > unicode.MaxLatin1 {
			// TS encodes to UTF-8 bytes first; each non-ASCII byte becomes
			// its own '_', so a single multi-byte rune becomes several '_'.
			for range []byte(string(r)) {
				out = append(out, '_')
			}
			continue
		}
		out = append(out, '_')
	}
	return string(out)
}
