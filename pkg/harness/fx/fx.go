// Package fx is the Go port of the TypeScript `@ai-sdk/harness-fx` package.
//
// CreateFx has two runtimes, chosen with Settings.Runtime:
//
//   - RuntimeSandboxACP (the default, go-ai v0.5.x behavior): fx is an
//     "ACP-derived" adapter. BuildConfig assembles the acp.Settings the
//     pre-libfx TS `createFx()` passed to `createACP()`, and `fx acp` runs
//     inside the sandbox through the shared ACP bridge.
//   - RuntimeLibfx (opt-in): the libfx core, fx-core.wasm, runs in the host
//     process on wazero, as TS @ai-sdk/harness-fx (pinned to ai e64ed82f27)
//     does. Its built-in tools (LibfxBuiltinTools) run as commands in the
//     session sandbox; the host makes the model requests. AI Gateway
//     authentication only for now.
package fx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// fxVersion is the pinned fx release the install command fetches, instead of
// whatever "latest" resolves to at bootstrap time (TS #23138 `FX_VERSION`).
// To update it, find the newest release with the fx installer's release list
// and bump this constant together with the TS-parity tests.
const fxVersion = "0.0.13"

const (
	ClientAppName    = "ai-sdk-harness-fx"
	ClientAppVersion = "0.0.0-go"
	HarnessID        = "fx"

	defaultAIGatewayBaseURL = "https://ai-gateway.vercel.sh"
)

// AuthenticationMode is TS `FxAuthenticationMode`.
type AuthenticationMode = harness.Authentication

// Settings configures the fx harness adapter. Mirrors TS `FxHarnessSettings`
// plus the go-ai v0.5.x sandbox-ACP settings.
type Settings struct {
	Auth AuthenticationMode
	// CredentialForwarding, Port, PortEndpoint, StartupTimeoutMS, Reconnect
	// and MintBridgeToken configure the sandbox-ACP runtime; the libfx
	// runtime ignores them.
	CredentialForwarding harness.CredentialForwarding
	Port                 *int
	PortEndpoint         *harness.PortEndpoint
	StartupTimeoutMS     *int
	Reconnect            *bridge.ReconnectOptions
	// MCPServers configures MCP servers. The libfx runtime does not support
	// them yet: CreateFx returns a *harness.CapabilityUnsupportedError.
	MCPServers      map[string]any
	MintBridgeToken harness.MintBridgeTokenCallback

	// Runtime selects the fx runtime; the zero value is RuntimeSandboxACP.
	Runtime Runtime

	// The settings below apply to RuntimeLibfx only.

	// Headers are added to AI Gateway requests (TS headers).
	Headers map[string]string
	// Effort is the reasoning effort passed to libfx (TS effort).
	Effort string
	// ServiceTier is "", "default", "fast" or "ultrafast" (TS serviceTier).
	ServiceTier string
	// Backend is "", "auto" or "wasm". Go always runs libfx's wasm backend;
	// "native" (the Node addon) returns a *harness.CapabilityUnsupportedError.
	Backend string
	// Wasm says where fx-core.wasm comes from; nil downloads and caches the
	// pinned module on first use (see WasmSource).
	Wasm *WasmSource
	// MemoryLimitBytes caps each core's memory; 0 means 1 GiB.
	MemoryLimitBytes int64
	// HTTPClient sends the core's model requests. nil uses an SSRF-safe
	// client. Either way only the AI Gateway base origin and libfx's default
	// provider origins are reachable.
	HTTPClient *http.Client
}

// CreateFx returns the fx harness-v1 adapter. With the default
// RuntimeSandboxACP it wires BuildConfig's acp.Settings into a live
// harness.Harness via acp.CreateACP, as go-ai v0.5.x did; with RuntimeLibfx
// it returns the libfx harness, the port of TS `createFx()`.
func CreateFx(settings ...Settings) (harness.Harness, error) {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	switch s.Runtime {
	case "", RuntimeSandboxACP:
		h, err := acp.CreateACP(BuildConfig(s))
		if err != nil {
			return nil, err
		}
		return legacyHarness{h}, nil
	case RuntimeLibfx:
		return newLibfxHarness(s)
	}
	return nil, fmt.Errorf("fx: unknown runtime %q", s.Runtime)
}

// BuildConfig assembles the acp.Settings the pre-libfx `createFx()` built
// (TS `ACPHarnessSettings` as passed to `createACP()`). It configures the
// RuntimeSandboxACP runtime only.
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
			Command: "curl -fsSL https://fx.sh/setup.sh | bash -s -- v" + fxVersion,
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
		CredentialBrokeringErr: func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
			return CredentialBrokeringErr(env, sandboxEnv, headers, suppliedAuthenticationEnvironment)
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
//
// A transformation that cannot be built (an invalid `AI_GATEWAY_BASE_URL`) is
// skipped here for source compatibility; CredentialBrokeringErr surfaces it
// the way TS (which throws) does, and is what createFx wires.
func CredentialBrokering(env, sandboxEnv, headers map[string]string, suppliedAuthenticationEnvironment bool) []harness.RequestTransformation {
	transformations, err := credentialBrokering(env, sandboxEnv, headers, suppliedAuthenticationEnvironment, true)
	if err != nil {
		return nil
	}
	return transformations
}

// CredentialBrokeringErr is CredentialBrokering returning the error from
// building a transformation instead of dropping it.
func CredentialBrokeringErr(env, sandboxEnv, headers map[string]string, suppliedAuthenticationEnvironment bool) ([]harness.RequestTransformation, error) {
	return credentialBrokering(env, sandboxEnv, headers, suppliedAuthenticationEnvironment, false)
}

func credentialBrokering(env, sandboxEnv, headers map[string]string, suppliedAuthenticationEnvironment, lenient bool) ([]harness.RequestTransformation, error) {
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
			if lenient {
				continue
			}
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
	// TS `env.AI_GATEWAY_BASE_URL ?? DEFAULT`: only an absent key falls back;
	// an empty string is kept (and then fails URL parsing like `new URL("")`).
	baseURL, ok := env["AI_GATEWAY_BASE_URL"]
	if !ok {
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
	// TS iterates the UTF-8 bytes of the string: every byte outside
	// [0-9A-Za-z_-] becomes one '_' (so a multi-byte rune becomes several).
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '-' || b == '_' {
			out = append(out, b)
			continue
		}
		out = append(out, '_')
	}
	return string(out)
}
