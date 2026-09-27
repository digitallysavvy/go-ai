// Package githubcopilot is the Go port of the TypeScript
// `@ai-sdk/harness-github-copilot` package (pinned to ai@7.0.113, hash
// 5ecd4f4).
//
// Like pkg/harness/cursor, GitHub Copilot is an "ACP-derived" adapter: TS
// `createGitHubCopilot()` is a thin configuration layer over `createACP()`
// (`@ai-sdk/harness-acp`). See the pkg/harness/cursor package doc for the
// rationale behind stopping at the configuration boundary (BuildConfig)
// instead of wiring a live harness.Harness: the ACP meta-adapter host
// (pkg/harness/acp, WG11) had not landed yet when this package was written.
//
//	h, err := acp.CreateACP(githubcopilot.BuildConfig(settings))
//
// Unlike cursor and fx, GitHub Copilot's ACP implementation (`@github/copilot`)
// is installed via a locked npm recipe: BuildConfig embeds the exact
// package.json/pnpm-lock.yaml/pnpm-workspace.yaml WG6 already synced to
// pkg/harness/bridges.GitHubCopilot. It also reads the CLI's own JSONC
// config file (jsonc.go) to discover a logged-in account for native
// subscription resolution (subscription.go).
package githubcopilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridges"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

const (
	ClientAppName    = "ai-sdk/harness-github-copilot"
	ClientAppVersion = "0.0.0-go"
	HarnessID        = "github-copilot"

	githubOAuthTokenBodyLength         = 36
	githubOAuthSandboxCredentialMarker = "aisdkhc"
)

// tokenEnvironmentVariables are the GitHub token variables Copilot accepts
// (order matches TS `GITHUB_COPILOT_TOKEN_ENVIRONMENT_VARIABLES`).
var tokenEnvironmentVariables = map[string]bool{
	"COPILOT_GITHUB_TOKEN": true,
	"GH_TOKEN":             true,
	"GITHUB_TOKEN":         true,
}

// AuthenticationMode is TS `GitHubCopilotAuthenticationMode`.
type AuthenticationMode = harness.Authentication

// ReasoningEffort is TS `GitHubCopilotHarnessSettings["reasoningEffort"]`.
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

// Settings configures the GitHub Copilot harness adapter. Mirrors TS
// `GitHubCopilotHarnessSettings`.
type Settings struct {
	Auth                 AuthenticationMode
	CredentialForwarding harness.CredentialForwarding
	ReasoningEffort      ReasoningEffort
	MCPServers           map[string]any
	Port                 *int
	PortEndpoint         *harness.PortEndpoint
	StartupTimeoutMS     *int
	Reconnect            *bridge.ReconnectOptions
	MintBridgeToken      harness.MintBridgeTokenCallback
}

// ToolCall is the subset of TS `ACPToolCall` the classifier closures need.
type ToolCall struct {
	Title string
}

// Source is TS `ACPNpmLockedSource`.
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

// ProviderAuthenticationValue is TS `ACPProfileValue`.
type ProviderAuthenticationValue struct {
	Literal      any
	Source       string
	Prefix       string
	EnsureSuffix string
}

// Config is the ACP meta-adapter configuration `createGitHubCopilot()`
// builds. See the package doc for how to wire it once pkg/harness/acp
// exists.
type Config struct {
	Version                string
	HarnessID              string
	ClientAppName          string
	ClientAppVersion       string
	Source                 Source
	Executable             string
	Args                   []string
	Auth                   AuthenticationMode
	ResolveAuthEnv         func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error)
	HostToolMCPTransport   string
	ForwardEnv             []string
	CredentialEnv          []string
	CredentialBrokering    func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error)
	CredentialForwarding   harness.CredentialForwarding
	ProviderAuthentication map[string]ProviderAuthenticationValue
	ModelMapping           ModelMapping
	SkillsDirectory        string
	InstructionMapping     InstructionMapping
	BuiltinTools           map[string]harness.BuiltinTool
	MCPServers             map[string]any
	IsMCPToolCall          func(ToolCall) bool
	Port                   *int
	PortEndpoint           *harness.PortEndpoint
	StartupTimeoutMS       *int
	Reconnect              *bridge.ReconnectOptions
	MintBridgeToken        harness.MintBridgeTokenCallback
}

// BuildConfig assembles Config from Settings, mirroring TS
// `createGitHubCopilot()` (github-copilot-harness.ts). It returns an error
// only if the embedded bridge asset lookup fails, which cannot happen for a
// correctly built binary.
func BuildConfig(settings Settings) (Config, error) {
	files, err := bridges.Files(bridges.GitHubCopilot)
	if err != nil {
		return Config{}, err
	}

	mcpToolTitlePrefixes := []string{"github-mcp-server-"}
	for name := range settings.MCPServers {
		mcpToolTitlePrefixes = append(mcpToolTitlePrefixes, name+"-")
	}

	args := []string{"--acp", "--stdio", "--no-auto-update"}
	if settings.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort="+string(settings.ReasoningEffort))
	}

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
		Executable: "copilot",
		Args:       args,
		Auth:       settings.Auth,
		ResolveAuthEnv: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		HostToolMCPTransport: "http",
		ForwardEnv:           []string{"COPILOT_GH_HOST", "GH_HOST"},
		CredentialEnv:        []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
		CredentialBrokering: func(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
			return CredentialBrokering(env, sandboxEnv, headers)
		},
		CredentialForwarding: CredentialForwarding(settings.CredentialForwarding),
		ProviderAuthentication: map[string]ProviderAuthenticationValue{
			"COPILOT_PROVIDER_BASE_URL": {Source: "gateway-base-url", EnsureSuffix: "/v1"},
			"COPILOT_PROVIDER_TYPE":     {Literal: "openai"},
			"COPILOT_PROVIDER_API_KEY":  {Source: "gateway-api-key"},
			"COPILOT_PROVIDER_WIRE_API": {Literal: "responses"},
			"COPILOT_MODEL":             {Literal: "openai/gpt-5.5"},
			"COPILOT_PROVIDER_HEADERS":  {Source: "client-app", Prefix: "x-client-app: "},
		},
		ModelMapping:    ModelMapping{Type: "session-config-option", Path: "model"},
		SkillsDirectory: ".copilot/skills",
		InstructionMapping: InstructionMapping{
			Type: "filesystem",
			Path: ".copilot/copilot-instructions.md",
		},
		BuiltinTools: BuiltinTools,
		MCPServers:   settings.MCPServers,
		IsMCPToolCall: func(call ToolCall) bool {
			for _, prefix := range mcpToolTitlePrefixes {
				if len(call.Title) >= len(prefix) && call.Title[:len(prefix)] == prefix {
					return true
				}
			}
			return false
		},
		Port:             settings.Port,
		PortEndpoint:     settings.PortEndpoint,
		StartupTimeoutMS: settings.StartupTimeoutMS,
		Reconnect:        settings.Reconnect,
		MintBridgeToken:  settings.MintBridgeToken,
	}, nil
}

// CredentialForwarding wraps a caller-supplied forwarding callback so that
// GitHub token placeholders are first reshaped into a value that satisfies
// Copilot CLI's GitHub-token-format validation. Mirrors TS
// `createGitHubCopilotCredentialForwarding`.
func CredentialForwarding(forwarding harness.CredentialForwarding) harness.CredentialForwarding {
	return func(ctx context.Context, opts harness.CredentialForwardingOptions) (string, error) {
		credential := opts.Credential
		if tokenEnvironmentVariables[opts.EnvironmentVariableName] && harnessutil.IsSandboxCredentialPlaceholder(credential) {
			credential = createGitHubOAuthSandboxCredentialPlaceholder(credential)
		}
		if forwarding == nil {
			return credential, nil
		}
		return forwarding(ctx, harness.CredentialForwardingOptions{
			Credential:              credential,
			EnvironmentVariableName: opts.EnvironmentVariableName,
		})
	}
}

// createGitHubOAuthSandboxCredentialPlaceholder mirrors TS
// `createGitHubOAuthSandboxCredentialPlaceholder`: Copilot CLI validates
// environment credentials as GitHub token formats before making the request
// that credential brokering can transform. GitHub OAuth tokens use `gho_`
// followed by a 36-character alphanumeric body.
func createGitHubOAuthSandboxCredentialPlaceholder(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	tokenBody := hex.EncodeToString(sum[:])[:githubOAuthTokenBodyLength-len(githubOAuthSandboxCredentialMarker)]
	return "gho_" + githubOAuthSandboxCredentialMarker + tokenBody
}
