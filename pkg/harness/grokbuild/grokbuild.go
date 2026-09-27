// Package grokbuild is the Go port of the TypeScript
// `@ai-sdk/harness-grok-build` package (pinned to ai@7.0.113, hashes 39e8870
// bb54c38).
//
// Like pkg/harness/cursor, Grok Build is an "ACP-derived" adapter: TS
// `createGrokBuild()` is a thin configuration layer over `createACP()`
// (`@ai-sdk/harness-acp`). See the pkg/harness/cursor package doc for the
// rationale behind stopping at the configuration boundary (BuildConfig)
// instead of wiring a live harness.Harness: the ACP meta-adapter host
// (pkg/harness/acp, WG11) had not landed yet when this package was written.
//
//	h, err := acp.CreateACP(grokbuild.BuildConfig(settings))
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

	"github.com/digitallysavvy/go-ai/pkg/harness"
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

// ToolCall is the subset of TS `ACPToolCall` the classifier closures need.
type ToolCall struct {
	Meta map[string]any // `_meta`
}

type Source struct {
	Type              string
	PackageJSON       string
	PnpmLockYAML      string
	PnpmWorkspaceYAML string
}

type ModelMapping struct {
	Type string
	Path string
}

type InstructionMapping struct {
	Type string
	Path string
}

type OutputSchemaMapping struct {
	Type string
	Path []string
}

// ProviderAuthenticationValue is TS `ACPProfileValue`.
type ProviderAuthenticationValue struct {
	Literal      any
	Source       string
	EnsureSuffix string
}

// Authentication is TS `ACPAuthentication`.
type Authentication struct {
	MethodID string
}

// Config is the ACP meta-adapter configuration `createGrokBuild()` builds.
// See the package doc for how to wire it once pkg/harness/acp exists.
type Config struct {
	Version                string
	HarnessID              string
	ClientAppName          string
	ClientAppVersion       string
	Source                 Source
	Executable             string
	Args                   []string
	Auth                   AuthenticationMode
	Authentication         Authentication
	ResolveAuthEnv         func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error)
	ForwardEnv             []string
	CredentialEnv          []string
	CredentialBrokering    func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error)
	CredentialForwarding   harness.CredentialForwarding
	ProviderAuthentication map[string]ProviderAuthenticationValue
	ModelMapping           ModelMapping
	InstructionMapping     InstructionMapping
	OutputSchemaMapping    OutputSchemaMapping
	BuiltinTools           map[string]harness.BuiltinTool
	AskUserQuestions       QuestionsSettings
	MCPServers             map[string]any
	IsMCPToolCall          func(ToolCall) bool
	Port                   *int
	PortEndpoint           *harness.PortEndpoint
	StartupTimeoutMS       *int
	Reconnect              *bridge.ReconnectOptions
	MintBridgeToken        harness.MintBridgeTokenCallback
}

// BuildConfig assembles Config from Settings, mirroring TS
// `createGrokBuild()` (grok-build-harness.ts).
func BuildConfig(settings Settings) (Config, error) {
	files, err := bridges.Files(bridges.GrokBuild)
	if err != nil {
		return Config{}, err
	}

	args := []string{"agent"}
	if settings.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort", string(settings.ReasoningEffort))
	}
	args = append(args, "stdio")

	return Config{
		Version:          "v1",
		HarnessID:        HarnessID,
		ClientAppName:    ClientAppName,
		ClientAppVersion: ClientAppVersion,
		Source: Source{
			Type:              "npm-locked",
			PackageJSON:       string(files["package.json"]),
			PnpmLockYAML:      string(files["pnpm-lock.yaml"]),
			PnpmWorkspaceYAML: string(files["pnpm-workspace.yaml"]),
		},
		Executable:     "grok",
		Args:           args,
		Auth:           settings.Auth,
		Authentication: Authentication{MethodID: "xai.api_key"},
		ResolveAuthEnv: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		ForwardEnv:    []string{"GROK_XAI_API_BASE_URL", "GROK_MODELS_BASE_URL", "GROK_CLI_CHAT_PROXY_BASE_URL"},
		CredentialEnv: []string{"XAI_API_KEY"},
		CredentialBrokering: func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
			return CredentialBrokering(env, sandboxEnv, headers)
		},
		CredentialForwarding: settings.CredentialForwarding,
		ProviderAuthentication: map[string]ProviderAuthenticationValue{
			"GROK_CLIENT_NAME":      {Source: "client-app-name"},
			"GROK_CLIENT_VERSION":   {Source: "client-app-version"},
			"XAI_API_KEY":           {Source: "gateway-api-key"},
			"GROK_XAI_API_BASE_URL": {Source: "gateway-base-url", EnsureSuffix: "/v1"},
			"GROK_MODELS_BASE_URL":  {Source: "gateway-base-url", EnsureSuffix: "/v1"},
		},
		ModelMapping: ModelMapping{Type: "session-model", Path: "modelId"},
		InstructionMapping: InstructionMapping{
			Type: "filesystem",
			Path: ".grok/AGENTS.md",
		},
		OutputSchemaMapping: OutputSchemaMapping{Type: "session-prompt-meta", Path: []string{"outputSchema"}},
		BuiltinTools:        BuiltinTools,
		AskUserQuestions:    AskUserQuestions,
		MCPServers:          settings.MCPServers,
		IsMCPToolCall: func(call ToolCall) bool {
			meta, ok := call.Meta["x.ai/tool"].(map[string]any)
			if !ok {
				return false
			}
			namespace, _ := meta["namespace"].(string)
			return namespace == "mcp"
		},
		Port:             settings.Port,
		PortEndpoint:     settings.PortEndpoint,
		StartupTimeoutMS: settings.StartupTimeoutMS,
		Reconnect:        settings.Reconnect,
		MintBridgeToken:  settings.MintBridgeToken,
	}, nil
}

// CredentialBrokering builds the request transformation mapping Grok
// Build's sandbox XAI_API_KEY back to the host credential. Mirrors the
// `credentialBrokering` closure in `createGrokBuild()`.
func CredentialBrokering(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
	if env["XAI_API_KEY"] == "" || sandboxEnv["XAI_API_KEY"] == "" {
		return nil, nil
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
		return nil, err
	}
	return []harness.RequestTransformation{tr}, nil
}
