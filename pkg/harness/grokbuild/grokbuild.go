// Package grokbuild is the Go port of the TypeScript
// `@ai-sdk/harness-grok-build` package (pinned to ai@7.0.113, hashes 39e8870
// bb54c38).
//
// Like pkg/harness/cursor, Grok Build is an "ACP-derived" adapter: TS
// `createGrokBuild()` is a thin configuration layer over `createACP()`
// (`@ai-sdk/harness-acp`). BuildConfig assembles the exact acp.Settings TS's
// `createGrokBuild()` passes to `createACP()`, and CreateGrokBuild wires it
// into a live harness.Harness.
//
// Grok Build's ACP implementation (`@xai-official/grok`) is installed via a
// locked npm recipe: BuildConfig embeds the exact
// package.json/pnpm-lock.yaml/pnpm-workspace.yaml WG6 already synced to
// pkg/harness/bridges.GrokBuild. Its askUserQuestions mapping
// (question_tool.go) and native xAI subscription reader (subscription.go,
// scope-keyed OAuth with discovery-driven refresh) are ported in full.
package grokbuild

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

const (
	ClientAppName    = "ai-sdk/harness-grok-build"
	ClientAppVersion = "0.0.0-go"
	HarnessID        = "grok-build"
)

// AuthenticationMode is TS `GrokBuildAuthenticationMode`.
type AuthenticationMode = harness.Authentication

// ReasoningEffort is TS `GrokBuildHarnessSettings["reasoningEffort"]`.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
)

// Settings configures the Grok Build harness adapter. Mirrors TS
// `GrokBuildHarnessSettings`.
type Settings struct {
	Auth                 AuthenticationMode
	CredentialForwarding harness.CredentialForwarding
	ReasoningEffort      ReasoningEffort
	Port                 *int
	PortEndpoint         *harness.PortEndpoint
	StartupTimeoutMS     *int
	Reconnect            *bridge.ReconnectOptions
	MCPServers           map[string]any
	MintBridgeToken      harness.MintBridgeTokenCallback
}

// CreateGrokBuild returns the Grok Build harness-v1 adapter, wiring
// BuildConfig's acp.Settings into a live harness.Harness via acp.CreateACP.
// Mirrors TS `createGrokBuild()`.
func CreateGrokBuild(settings ...Settings) (harness.Harness, error) {
	s := Settings{}
	if len(settings) > 0 {
		s = settings[0]
	}
	cfg, err := BuildConfig(s)
	if err != nil {
		return nil, err
	}
	return acp.CreateACP(cfg)
}

// BuildConfig assembles the acp.Settings `createGrokBuild()` builds (TS
// `ACPHarnessSettings` as passed to `createACP()`).
func BuildConfig(settings Settings) (acp.Settings, error) {
	files, err := bridges.Files(bridges.GrokBuild)
	if err != nil {
		return acp.Settings{}, err
	}

	args := []string{"agent"}
	if settings.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort", string(settings.ReasoningEffort))
	}
	args = append(args, "stdio")

	cfg := acp.Settings{
		HarnessID: HarnessID,
		ClientApp: acp.ClientApp{Name: ClientAppName, Version: ClientAppVersion},
		Source: acp.Source{
			Type:              acp.SourceNPMLocked,
			PackageJSON:       string(files["package.json"]),
			PnpmLockYAML:      string(files["pnpm-lock.yaml"]),
			PnpmWorkspaceYAML: string(files["pnpm-workspace.yaml"]),
		},
		Executable:     "grok",
		Args:           args,
		Auth:           settings.Auth,
		Authentication: &acp.Authentication{MethodID: "xai.api_key"},
		ResolveAuthenticationEnvironment: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		ForwardEnv:           []string{"GROK_XAI_API_BASE_URL", "GROK_MODELS_BASE_URL", "GROK_CLI_CHAT_PROXY_BASE_URL"},
		CredentialEnv:        []string{"XAI_API_KEY"},
		CredentialBrokering:  CredentialBrokering,
		CredentialForwarding: settings.CredentialForwarding,
		ProviderAuthentication: &acp.ProviderAuthentication{
			GatewayEnv: map[string]any{
				"GROK_CLIENT_NAME":      map[string]any{"$source": "client-app-name"},
				"GROK_CLIENT_VERSION":   map[string]any{"$source": "client-app-version"},
				"XAI_API_KEY":           map[string]any{"$source": "gateway-api-key"},
				"GROK_XAI_API_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
				"GROK_MODELS_BASE_URL":  map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
			},
		},
		ModelMapping: acp.ModelMapping{Type: acp.ModelMappingSessionModel, Path: "modelId"},
		InstructionMapping: &acp.InstructionMapping{
			Type:     acp.InstructionMappingFilesystem,
			FilePath: ".grok/AGENTS.md",
		},
		OutputSchemaMapping: &acp.OutputSchemaMapping{Path: []string{"outputSchema"}},
		BuiltinTools:        BuiltinTools,
		AskUserQuestions:    &AskUserQuestions,
		MCPServers:          settings.MCPServers,
		IsMcpToolCall: func(call acp.ToolCall) bool {
			meta, ok := call.Meta["x.ai/tool"].(map[string]any)
			if !ok {
				return false
			}
			namespace, _ := meta["namespace"].(string)
			return namespace == "mcp"
		},
		PortEndpoint:    settings.PortEndpoint,
		MintBridgeToken: settings.MintBridgeToken,
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
	return cfg, nil
}

// CredentialBrokering builds the request transformation mapping Grok
// Build's sandbox XAI_API_KEY back to the host credential. Mirrors the
// `credentialBrokering` closure in `createGrokBuild()`.
func CredentialBrokering(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
	if env["XAI_API_KEY"] == "" || sandboxEnv["XAI_API_KEY"] == "" {
		return nil
	}
	matchURL := env["GROK_XAI_API_BASE_URL"]
	if matchURL == "" {
		matchURL = "https://api.x.ai/v1"
	}
	transformHeaders := map[string]string{}
	for k, v := range headers {
		transformHeaders[k] = v
	}
	transformHeaders["Authorization"] = "Bearer " + env["XAI_API_KEY"]
	if env["GROK_CLI_CHAT_PROXY_BASE_URL"] != "" {
		transformHeaders["X-XAI-Token-Auth"] = "xai-grok-cli"
	}
	tr, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
		MatchURL:         matchURL,
		MatchHeaders:     map[string]string{"Authorization": "Bearer " + sandboxEnv["XAI_API_KEY"]},
		TransformHeaders: transformHeaders,
	})
	if err != nil {
		return nil
	}
	return []harness.RequestTransformation{tr}
}
