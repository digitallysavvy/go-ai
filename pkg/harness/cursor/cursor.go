// Package cursor is the Go port of the TypeScript `@ai-sdk/harness-cursor`
// package (pinned to ai@7.0.113, hashes ac1384b).
//
// Cursor is one of the "ACP-derived" harness adapters (see
// state/parity/sep_23_2026/harness.md §2): TS `createCursor()` is a thin
// configuration layer over `createACP()` (`@ai-sdk/harness-acp`), which
// implements the actual bridge launch, ACP session lifecycle and stream
// translation. This package mirrors that shape: BuildConfig assembles the
// exact acp.Settings TS's `createCursor()` passes to `createACP()`, and
// CreateCursor wires it into a live harness.Harness.
//
// Cursor is server-side/sandboxable: the CLI (`agent`, installed via
// `curl https://cursor.com/install | bash`) is spawned inside the sandbox by
// the (already embedded, see pkg/harness/bridges) ACP bridge, so this
// adapter is fully portable to Go. It has no bridge assets of its own; it
// reuses the shared ACP bridge entry (pkg/harness/bridges.ACP).
package cursor

import (
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// ClientAppName / ClientAppVersion are the ai-sdk client-app identity Cursor
// requests are made with. VERSION mirrors the TS package's own version; the
// Go SDK does not (yet) stamp a package version into adapter packages, so it
// is fixed to "0.0.0-go" until a release process defines one.
const (
	ClientAppName    = "ai-sdk/harness-cursor"
	ClientAppVersion = "0.0.0-go"
	HarnessID        = "cursor"
)

// AuthenticationMode is TS `CursorAuthenticationMode` (= ACPAuthenticationMode
// = HarnessV1Authentication): "auto" | "ai-gateway" | "direct" | an isolated
// authentication environment.
type AuthenticationMode = harness.Authentication

// Settings configures the Cursor harness adapter. Mirrors TS
// `CursorHarnessSettings`.
type Settings struct {
	// Auth declares the provider authentication configured in Cursor, or
	// supplies an isolated environment for Cursor CLI authentication. The
	// adapter cannot change provider routing and warns for explicit routing
	// modes (direct / ai-gateway).
	Auth AuthenticationMode
	// CredentialForwarding customizes each credential value before it is
	// forwarded into a sandbox process.
	CredentialForwarding harness.CredentialForwarding
	// Port overrides the sandbox port used by the ACP bridge.
	Port *int
	// PortEndpoint overrides the host endpoint used to connect to the
	// sandbox bridge. Required together with Port when using a basic
	// sandbox session.
	PortEndpoint *harness.PortEndpoint
	// StartupTimeoutMS is the maximum time to wait for the ACP bridge to
	// start.
	StartupTimeoutMS *int
	// Reconnect configures reconnection attempts after an established bridge
	// connection drops.
	Reconnect *bridge.ReconnectOptions
	// MCPServers are MCP server definitions keyed by server name, in
	// Cursor's native ACP MCP server configuration format.
	MCPServers map[string]any
	// MintBridgeToken creates the bridge channel token for the session.
	// Defaults to a random 32-byte hexadecimal token.
	MintBridgeToken harness.MintBridgeTokenCallback
}

// CreateCursor returns the Cursor harness-v1 adapter, wiring BuildConfig's
// acp.Settings into a live harness.Harness via acp.CreateACP. Mirrors TS
// `createCursor()`.
func CreateCursor(settings ...Settings) (harness.Harness, error) {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	return acp.CreateACP(BuildConfig(s))
}

// BuildConfig assembles the acp.Settings `createCursor()` builds (TS
// `ACPHarnessSettings` as passed to `createACP()`). It also emits the TS
// "cannot configure Cursor provider authentication" warning for explicit
// auth modes.
func BuildConfig(settings Settings) acp.Settings {
	if settings.Auth.Mode == harness.AuthModeDirect || settings.Auth.Mode == harness.AuthModeAIGateway {
		WarnAuthenticationConfiguration(settings.Auth.Mode)
	}
	cfg := acp.Settings{
		HarnessID:                        HarnessID,
		ClientApp:                        acp.ClientApp{Name: ClientAppName, Version: ClientAppVersion},
		Auth:                             settings.Auth,
		ResolveAuthenticationEnvironment: ResolveSubscriptionEnvironment,
		CredentialForwarding:             settings.CredentialForwarding,
		PortEndpoint:                     settings.PortEndpoint,
		MCPServers:                       settings.MCPServers,
		IsMcpToolCall:                    IsMCPToolCall,
		MintBridgeToken:                  settings.MintBridgeToken,
		BuiltinTools:                     BuiltinTools,
		Source: acp.Source{
			Type:    acp.SourceInstallCommand,
			Command: "curl https://cursor.com/install -fsS | bash",
		},
		Executable: "agent",
		Args:       []string{"--disable-auto-update", "acp"},
		ModelMapping: acp.ModelMapping{
			Type: acp.ModelMappingSessionConfigOption,
			Path: "model",
		},
		ClientCapabilities: map[string]any{
			"_meta": map[string]any{"parameterizedModelPicker": true},
		},
		CredentialEnv: []string{"CURSOR_API_KEY"},
		CredentialBrokering: func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			return CredentialBrokering(settings.Auth, env, sandboxEnv, headers)
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

// IsMCPToolCall classifies a Cursor ACP tool call as a dynamic MCP tool call
// when its raw input has the `providerIdentifier` / `toolName` / `args`
// shape Cursor's CLI uses for `mcpToolCall`. Mirrors the `isMcpToolCall`
// closure in `createCursor()`.
func IsMCPToolCall(call acp.ToolCall) bool {
	raw, ok := call.RawInput.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := raw["providerIdentifier"].(string); !ok {
		return false
	}
	if _, ok := raw["toolName"].(string); !ok {
		return false
	}
	_, ok = raw["args"].(map[string]any)
	return ok
}

// WarnAuthenticationConfiguration logs the TS
// `warnCursorAuthenticationConfiguration` warning for an explicit
// (non-"auto") auth mode.
func WarnAuthenticationConfiguration(auth string) {
	detail := "Configure Cursor to use its direct model-provider routing."
	if auth == harness.AuthModeAIGateway {
		detail = "Configure an AI Gateway API key as Cursor's OpenAI API key and set Override OpenAI Base URL to https://ai-gateway.vercel.sh/cursor/v1."
	}
	Warn(fmt.Sprintf("[cursor] auth: %q cannot configure Cursor provider authentication. %s CURSOR_API_KEY is still required for Cursor CLI authentication.", auth, detail))
}

// Warn receives adapter warnings (TS console.warn). Replaceable for tests.
var Warn = func(message string) { println(message) }

// CredentialBrokering builds the request transformations that map Cursor's
// sandbox-side CURSOR_API_KEY (and any managed headers) back to the host
// credential. Mirrors the `credentialBrokering` closure in `createCursor()`.
func CredentialBrokering(auth AuthenticationMode, env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
	var transformations []harness.RequestTransformation
	if env["CURSOR_API_KEY"] != "" && sandboxEnv["CURSOR_API_KEY"] != "" {
		transformations = append(transformations, harness.RequestTransformation{
			Match: harness.RequestTransformationMatch{
				Host:   "api2.cursor.sh",
				Path:   &harness.StringMatcher{Exact: "/auth/exchange_user_api_key"},
				Method: []string{"POST"},
				Headers: []harness.KeyValueMatcher{{
					Key:   &harness.StringMatcher{Exact: "Authorization"},
					Value: &harness.StringMatcher{Exact: "Bearer " + sandboxEnv["CURSOR_API_KEY"]},
				}},
			},
			Transform: harness.RequestTransformationTransform{
				Headers: map[string]string{"Authorization": "Bearer " + env["CURSOR_API_KEY"]},
			},
		})
	}
	if headers != nil {
		host := "api2.cursor.sh"
		var pathMatch *harness.StringMatcher
		if auth.Mode == harness.AuthModeAIGateway {
			host = "ai-gateway.vercel.sh"
			pathMatch = &harness.StringMatcher{StartsWith: "/cursor/v1"}
		}
		transformations = append(transformations, harness.RequestTransformation{
			Match: harness.RequestTransformationMatch{
				Host: host,
				Path: pathMatch,
			},
			Transform: harness.RequestTransformationTransform{Headers: headers},
		})
	}
	return transformations
}
