// Package githubcopilot is the Go port of the TypeScript
// `@ai-sdk/harness-github-copilot` package (pinned to ai@7.0.113, hash
// 5ecd4f4).
//
// Like pkg/harness/cursor, GitHub Copilot is an "ACP-derived" adapter: TS
// `createGitHubCopilot()` is a thin configuration layer over `createACP()`
// (`@ai-sdk/harness-acp`). BuildConfig assembles the exact acp.Settings TS's
// `createGitHubCopilot()` passes to `createACP()`, and CreateGitHubCopilot
// wires it into a live harness.Harness.
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
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
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

// CreateGitHubCopilot returns the GitHub Copilot harness-v1 adapter, wiring
// BuildConfig's acp.Settings into a live harness.Harness via acp.CreateACP.
// Mirrors TS `createGitHubCopilot()`.
func CreateGitHubCopilot(settings ...Settings) (harness.Harness, error) {
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

// BuildConfig assembles the acp.Settings `createGitHubCopilot()` builds (TS
// `ACPHarnessSettings` as passed to `createACP()`). It returns an error only
// if the embedded bridge asset lookup fails, which cannot happen for a
// correctly built binary.
func BuildConfig(settings Settings) (acp.Settings, error) {
	files, err := bridges.Files(bridges.GitHubCopilot)
	if err != nil {
		return acp.Settings{}, err
	}

	mcpToolTitlePrefixes := []string{"github-mcp-server-"}
	for name := range settings.MCPServers {
		mcpToolTitlePrefixes = append(mcpToolTitlePrefixes, name+"-")
	}

	args := []string{"--acp", "--stdio", "--no-auto-update"}
	if settings.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort="+string(settings.ReasoningEffort))
	}

	cfg := acp.Settings{
		HarnessID: HarnessID,
		ClientApp: acp.ClientApp{Name: ClientAppName, Version: ClientAppVersion},
		Source: acp.Source{
			Type:              acp.SourceNPMLocked,
			PackageJSON:       string(files["package.json"]),
			PnpmLockYAML:      string(files["pnpm-lock.yaml"]),
			PnpmWorkspaceYAML: string(files["pnpm-workspace.yaml"]),
		},
		Executable: "copilot",
		Args:       args,
		Auth:       settings.Auth,
		ResolveAuthenticationEnvironment: func(ctx context.Context, auth AuthenticationMode, env map[string]string) (map[string]string, error) {
			return ResolveSubscriptionEnvironment(ctx, ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: env})
		},
		HostToolMCPTransport: acp.HostToolMCPHTTP,
		ForwardEnv:           []string{"COPILOT_GH_HOST", "GH_HOST"},
		CredentialEnv:        []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
		CredentialBrokering:  CredentialBrokering,
		CredentialForwarding: CredentialForwarding(settings.CredentialForwarding),
		ProviderAuthentication: &acp.ProviderAuthentication{
			GatewayEnv: map[string]any{
				"COPILOT_PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
				"COPILOT_PROVIDER_TYPE":     "openai",
				"COPILOT_PROVIDER_API_KEY":  map[string]any{"$source": "gateway-api-key"},
				"COPILOT_PROVIDER_WIRE_API": "responses",
				"COPILOT_MODEL":             "openai/gpt-5.5",
				"COPILOT_PROVIDER_HEADERS":  map[string]any{"$source": "client-app", "prefix": "x-client-app: "},
			},
		},
		ModelMapping:    acp.ModelMapping{Type: acp.ModelMappingSessionConfigOption, Path: "model"},
		SkillsDirectory: ".copilot/skills",
		InstructionMapping: &acp.InstructionMapping{
			Type:     acp.InstructionMappingFilesystem,
			FilePath: ".copilot/copilot-instructions.md",
		},
		BuiltinTools: BuiltinTools,
		MCPServers:   settings.MCPServers,
		IsMcpToolCall: func(call acp.ToolCall) bool {
			for _, prefix := range mcpToolTitlePrefixes {
				if len(call.Title) >= len(prefix) && call.Title[:len(prefix)] == prefix {
					return true
				}
			}
			return false
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
