// Package fx is the Go port of the TypeScript `@ai-sdk/harness-fx` package
// (pinned to ai@7.0.113, hash d0b241d).
//
// Like pkg/harness/cursor, fx is an "ACP-derived" adapter: TS `createFx()`
// is a thin configuration layer over `createACP()`
// (`@ai-sdk/harness-acp`). See the pkg/harness/cursor package doc for the
// rationale behind stopping at the configuration boundary (BuildConfig)
// instead of wiring a live harness.Harness: the ACP meta-adapter host
// (pkg/harness/acp, WG11) had not landed yet when this package was written.
//
//	h, err := acp.CreateACP(fx.BuildConfig(settings))
//
// fx is server-side/sandboxable: `fx acp` is spawned inside the sandbox by
// the shared ACP bridge (pkg/harness/bridges.ACP); this package has no
// bridge assets of its own.
package fx

import (
	"context"
	"unicode"

	"github.com/digitallysavvy/go-ai/pkg/harness"
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

// ToolCall is the subset of TS `ACPToolCall` the classifier closures need.
type ToolCall struct {
	Title string
}

// Source, ModelMapping and InstructionMapping mirror the corresponding
// `ACP*` union members (see pkg/harness/cursor for Source/ModelMapping).
type Source struct {
	Type    string
	Command string
}

type ModelMapping struct {
	Type string
	Path string
}

type InstructionMapping struct {
	Type string
	Path string
}

type PermissionModeTarget struct {
	Type   string
	ModeID string
}

// ProviderAuthenticationValue is TS `ACPProfileValue`: either a literal or a
// `{ $source }` placeholder resolved by the ACP host from gateway
// credentials / client-app identity.
type ProviderAuthenticationValue struct {
	Literal      any
	Source       string
	EnsureSuffix string
}

// Config is the ACP meta-adapter configuration `createFx()` builds. See the
// package doc for how to wire it once pkg/harness/acp exists.
type Config struct {
	Version                string
	HarnessID              string
	ClientAppName          string
	ClientAppVersion       string
	Auth                   AuthenticationMode
	ResolveAuthEnv         func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error)
	AuthenticationFiles    func(env, sandboxEnv map[string]string, credentialBrokeringAvailable bool) []AuthenticationFile
	CredentialForwarding   harness.CredentialForwarding
	Port                   *int
	PortEndpoint           *harness.PortEndpoint
	StartupTimeoutMS       *int
	Reconnect              *bridge.ReconnectOptions
	MCPServers             map[string]any
	IsMCPToolCall          func(ToolCall) bool
	MintBridgeToken        harness.MintBridgeTokenCallback
	BuiltinTools           map[string]harness.BuiltinTool
	Source                 Source
	Executable             string
	Args                   []string
	ModelMapping           ModelMapping
	InstructionMapping     InstructionMapping
	CredentialEnv          []string
	CredentialBrokering    func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error)
	ProviderAuthentication map[string]ProviderAuthenticationValue
	PermissionModeMapping  map[harness.PermissionMode]*PermissionModeTarget
}

// BuildConfig assembles Config from Settings, mirroring TS `createFx()`
// (fx-harness.ts).
func BuildConfig(settings Settings) Config {
	mcpToolTitlePrefixes := make([]string, 0, len(settings.MCPServers))
	for name := range settings.MCPServers {
		mcpToolTitlePrefixes = append(mcpToolTitlePrefixes, "mcp_"+sanitizeMCPToolNameSegment(name)+"_")
	}
	suppliedAuthenticationEnvironment := harnessutil.IsAuthenticationEnvironment(settings.Auth)

	return Config{
		Version:          "v1",
		HarnessID:        HarnessID,
		ClientAppName:    ClientAppName,
		ClientAppVersion: ClientAppVersion,
		Auth:             settings.Auth,
		ResolveAuthEnv: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		AuthenticationFiles:  CreateSubscriptionAuthenticationFiles,
		CredentialForwarding: settings.CredentialForwarding,
		Port:                 settings.Port,
		PortEndpoint:         settings.PortEndpoint,
		StartupTimeoutMS:     settings.StartupTimeoutMS,
		Reconnect:            settings.Reconnect,
		MCPServers:           settings.MCPServers,
		IsMCPToolCall: func(call ToolCall) bool {
			for _, prefix := range mcpToolTitlePrefixes {
				if len(call.Title) >= len(prefix) && call.Title[:len(prefix)] == prefix {
					return true
				}
			}
			return false
		},
		MintBridgeToken: settings.MintBridgeToken,
		BuiltinTools:    BuiltinTools,
		Source: Source{
			Type:    "install-command",
			Command: "curl -fsSL https://fx.sh/setup.sh | bash",
		},
		Executable: "fx",
		Args:       []string{"acp"},
		ModelMapping: ModelMapping{
			Type: "session-config-option",
			Path: "model",
		},
		InstructionMapping: InstructionMapping{
			Type: "filesystem",
			Path: ".fx/AGENTS.md",
		},
		CredentialEnv: append([]string{"VERCEL_OIDC_TOKEN", "AI_GATEWAY_API_KEY"}, SubscriptionEnvironmentVariables...),
		CredentialBrokering: func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
			return CredentialBrokering(env, sandboxEnv, headers, suppliedAuthenticationEnvironment)
		},
		ProviderAuthentication: map[string]ProviderAuthenticationValue{
			"AI_GATEWAY_API_KEY":  {Source: "gateway-api-key"},
			"AI_GATEWAY_BASE_URL": {Source: "gateway-base-url"},
		},
		PermissionModeMapping: map[harness.PermissionMode]*PermissionModeTarget{
			harness.PermissionModeAllowReads: {Type: "session-mode", ModeID: "ask"},
			harness.PermissionModeAllowEdits: {Type: "session-mode", ModeID: "ask"},
			harness.PermissionModeAllowAll:   {Type: "session-mode", ModeID: "code"},
		},
	}
}

// CredentialBrokering builds the request transformations for fx's
// subscription and AI Gateway credentials. Mirrors the `credentialBrokering`
// closure in `createFx()`.
func CredentialBrokering(env, sandboxEnv, headers map[string]string, suppliedAuthenticationEnvironment bool) ([]harness.RequestTransformation, error) {
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
			return nil, err
		}
		transformations = append(transformations, tr)
	}
	if len(transformations) > 0 {
		return transformations, nil
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
		return nil, nil
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
		return nil, err
	}
	return []harness.RequestTransformation{tr}, nil
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
