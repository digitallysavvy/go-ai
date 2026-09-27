package githubcopilot

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// Mirrors "configures the GitHub Copilot ACP implementation".
func TestBuildConfig_Recipe(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HarnessID != "github-copilot" || cfg.Executable != "copilot" {
		t.Errorf("harnessID/executable = %q/%q", cfg.HarnessID, cfg.Executable)
	}
	wantArgs := []string{"--acp", "--stdio", "--no-auto-update"}
	if !reflect.DeepEqual(cfg.Args, wantArgs) {
		t.Errorf("args = %v", cfg.Args)
	}
	if cfg.Source.Type != acp.SourceNPMLocked || cfg.Source.PackageJSON == "" || cfg.Source.PnpmLockYAML == "" {
		t.Errorf("source = %+v", cfg.Source)
	}
	if cfg.HostToolMCPTransport != acp.HostToolMCPHTTP {
		t.Errorf("hostToolMcpTransport = %q", cfg.HostToolMCPTransport)
	}
	if !reflect.DeepEqual(cfg.ForwardEnv, []string{"COPILOT_GH_HOST", "GH_HOST"}) {
		t.Errorf("forwardEnv = %v", cfg.ForwardEnv)
	}
	if !reflect.DeepEqual(cfg.CredentialEnv, []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"}) {
		t.Errorf("credentialEnv = %v", cfg.CredentialEnv)
	}
	if cfg.SkillsDirectory != ".copilot/skills" {
		t.Errorf("skillsDirectory = %q", cfg.SkillsDirectory)
	}
	if cfg.InstructionMapping == nil || !reflect.DeepEqual(*cfg.InstructionMapping, acp.InstructionMapping{Type: acp.InstructionMappingFilesystem, FilePath: ".copilot/copilot-instructions.md"}) {
		t.Errorf("instructionMapping = %+v", cfg.InstructionMapping)
	}
	wantGatewayEnv := map[string]any{
		"COPILOT_PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
		"COPILOT_PROVIDER_TYPE":     "openai",
		"COPILOT_PROVIDER_API_KEY":  map[string]any{"$source": "gateway-api-key"},
		"COPILOT_PROVIDER_WIRE_API": "responses",
		"COPILOT_MODEL":             "openai/gpt-5.5",
		"COPILOT_PROVIDER_HEADERS":  map[string]any{"$source": "client-app", "prefix": "x-client-app: "},
	}
	if cfg.ProviderAuthentication == nil || !reflect.DeepEqual(cfg.ProviderAuthentication.GatewayEnv, wantGatewayEnv) {
		t.Errorf("providerAuthentication = %+v", cfg.ProviderAuthentication)
	}
	if len(cfg.BuiltinTools) != 16 {
		t.Errorf("BuiltinTools has %d entries, want 16", len(cfg.BuiltinTools))
	}
}

// Mirrors "with a configured reasoning effort" style args assertion.
func TestBuildConfig_ReasoningEffort(t *testing.T) {
	cfg, err := BuildConfig(Settings{ReasoningEffort: ReasoningEffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Args[len(cfg.Args)-1] != "--reasoning-effort=high" {
		t.Errorf("args = %v", cfg.Args)
	}
}

// Mirrors "brokers GitHub credentials using their sandbox placeholders".
func TestCredentialBrokering_SandboxPlaceholders(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{"COPILOT_GITHUB_TOKEN": "github-secret", "GH_TOKEN": "second-github-secret"},
		map[string]string{"COPILOT_GITHUB_TOKEN": "copilot-placeholder", "GH_TOKEN": "gh-placeholder"},
		nil,
	)
	var sawBearer, sawToken bool
	for _, tr := range transforms {
		if tr.Match.Host == "github.com" && tr.Transform.Headers["Authorization"] == "Bearer github-secret" {
			sawBearer = true
		}
		if tr.Match.Host == "*.github.com" && tr.Transform.Headers["Authorization"] == "token second-github-secret" {
			sawToken = true
		}
	}
	if !sawBearer || !sawToken {
		t.Errorf("transforms = %+v", transforms)
	}
}

// Mirrors "brokers GitHub credentials for the configured enterprise host".
func TestCredentialBrokering_EnterpriseHost(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{"COPILOT_GH_HOST": "example.ghe.com", "GITHUB_TOKEN": "github-secret"},
		map[string]string{"COPILOT_GH_HOST": "example.ghe.com", "GITHUB_TOKEN": "github-placeholder"},
		nil,
	)
	var sawExample, sawWildcardExample, sawGithubCom bool
	for _, tr := range transforms {
		switch tr.Match.Host {
		case "example.ghe.com":
			sawExample = true
		case "*.example.ghe.com":
			sawWildcardExample = true
		case "*.github.com":
			sawGithubCom = true
		}
	}
	if !sawExample || !sawWildcardExample {
		t.Errorf("transforms = %+v", transforms)
	}
	if sawGithubCom {
		t.Error("should not broker default github.com hosts for an enterprise host")
	}
}

// Mirrors "formats the %s sandbox placeholder as a GitHub OAuth token".
func TestCredentialForwarding_FormatsPlaceholder(t *testing.T) {
	forwarding := CredentialForwarding(nil)
	placeholder := "aisdkhc_" + strings.Repeat("A", 43)
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		got, err := forwarding(context.Background(), harness.CredentialForwardingOptions{
			Credential: placeholder, EnvironmentVariableName: name,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^gho_aisdkhc[0-9a-f]{29}$`).MatchString(got) {
			t.Errorf("%s: got %q", name, got)
		}
		if len(got) != 40 {
			t.Errorf("%s: length = %d", name, len(got))
		}
	}
}

// Mirrors "does not format real credentials or unrelated placeholders".
func TestCredentialForwarding_PassesThroughUnrelated(t *testing.T) {
	forwarding := CredentialForwarding(nil)
	got, err := forwarding(context.Background(), harness.CredentialForwardingOptions{
		Credential: "gho_real-credential", EnvironmentVariableName: "COPILOT_GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "gho_real-credential" {
		t.Errorf("got %q", got)
	}

	placeholder := "aisdkhc_" + strings.Repeat("A", 43)
	got, err = forwarding(context.Background(), harness.CredentialForwardingOptions{
		Credential: placeholder, EnvironmentVariableName: "COPILOT_PROVIDER_API_KEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != placeholder {
		t.Errorf("got %q, want unchanged placeholder", got)
	}
}

// Mirrors "brokers the AI Gateway provider credential".
func TestCredentialBrokering_AIGatewayProvider(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{
			"COPILOT_PROVIDER_API_KEY":  "gateway-secret",
			"COPILOT_PROVIDER_BASE_URL": "https://gateway.example/v1",
		},
		map[string]string{
			"COPILOT_PROVIDER_API_KEY":  "gateway-placeholder",
			"COPILOT_PROVIDER_BASE_URL": "https://gateway.example/v1",
		},
		map[string]string{"x-tenant": "acme"},
	)
	found := false
	for _, tr := range transforms {
		if tr.Match.Host == "gateway.example" && tr.Match.Path != nil && tr.Match.Path.StartsWith == "/v1" &&
			len(tr.Match.Headers) == 1 && tr.Match.Headers[0].Value != nil && tr.Match.Headers[0].Value.Exact == "Bearer gateway-placeholder" &&
			tr.Transform.Headers["Authorization"] == "Bearer gateway-secret" && tr.Transform.Headers["x-tenant"] == "acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("transforms = %+v", transforms)
	}
}

// Mirrors "applies headers to direct Copilot model request routes".
func TestCredentialBrokering_DirectHeaders(t *testing.T) {
	transforms := CredentialBrokering(map[string]string{}, nil, map[string]string{"x-tenant": "acme"})
	if len(transforms) != 2 {
		t.Fatalf("got %d transforms, want 2", len(transforms))
	}
	if transforms[0].Match.Host != "githubcopilot.com" || transforms[1].Match.Host != "*.githubcopilot.com" {
		t.Errorf("hosts = %q, %q", transforms[0].Match.Host, transforms[1].Match.Host)
	}
	for _, tr := range transforms {
		if tr.Transform.Headers["x-tenant"] != "acme" {
			t.Errorf("headers = %v", tr.Transform.Headers)
		}
	}
}

// Mirrors "forwards launch and bridge settings".
func TestBuildConfig_ForwardsLaunchAndBridgeSettings(t *testing.T) {
	mintBridgeToken := func(sandboxID string) string { return sandboxID }
	mcpServers := map[string]any{"docs": map[string]any{"url": "https://mcp.example"}}
	portEndpoint := &harness.PortEndpoint{URL: "wss://sandbox.example/bridge"}
	port := 4319
	startupTimeoutMS := 45_000

	var forwardedCredential, forwardedVar string
	credentialForwarding := func(ctx context.Context, opts harness.CredentialForwardingOptions) (string, error) {
		forwardedCredential, forwardedVar = opts.Credential, opts.EnvironmentVariableName
		return opts.Credential, nil
	}

	cfg, err := BuildConfig(Settings{
		Auth:                 harness.AuthMode(harness.AuthModeDirect),
		CredentialForwarding: credentialForwarding,
		ReasoningEffort:      ReasoningEffortHigh,
		MCPServers:           mcpServers,
		Port:                 &port,
		PortEndpoint:         portEndpoint,
		StartupTimeoutMS:     &startupTimeoutMS,
		MintBridgeToken:      mintBridgeToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mode != harness.AuthModeDirect {
		t.Errorf("Auth = %+v", cfg.Auth)
	}
	wantArgs := []string{"--acp", "--stdio", "--no-auto-update", "--reasoning-effort=high"}
	if !reflect.DeepEqual(cfg.Args, wantArgs) {
		t.Errorf("Args = %v", cfg.Args)
	}
	if !reflect.DeepEqual(cfg.MCPServers, mcpServers) {
		t.Errorf("MCPServers = %+v", cfg.MCPServers)
	}
	if cfg.Port != 4319 || cfg.PortEndpoint != portEndpoint || cfg.StartupTimeout.Milliseconds() != 45_000 {
		t.Errorf("port/portEndpoint/startupTimeout = %d/%+v/%v", cfg.Port, cfg.PortEndpoint, cfg.StartupTimeout)
	}
	if cfg.MintBridgeToken == nil || cfg.MintBridgeToken("sbx-1") != "sbx-1" {
		t.Error("MintBridgeToken not forwarded correctly")
	}
	if _, err := cfg.CredentialForwarding(context.Background(), harness.CredentialForwardingOptions{
		Credential: "real-credential", EnvironmentVariableName: "COPILOT_GITHUB_TOKEN",
	}); err != nil {
		t.Fatal(err)
	}
	if forwardedCredential != "real-credential" || forwardedVar != "COPILOT_GITHUB_TOKEN" {
		t.Errorf("credentialForwarding called with %q/%q", forwardedCredential, forwardedVar)
	}
}

// Mirrors "classifies built-in and configured MCP title prefixes".
func TestIsMCPToolCall(t *testing.T) {
	cfg, err := BuildConfig(Settings{MCPServers: map[string]any{"context7": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsMcpToolCall(acp.ToolCall{Title: "github-mcp-server-search"}) {
		t.Error("expected github-mcp-server- prefix to classify as MCP")
	}
	if !cfg.IsMcpToolCall(acp.ToolCall{Title: "context7-resolve"}) {
		t.Error("expected configured server prefix to classify as MCP")
	}
	if cfg.IsMcpToolCall(acp.ToolCall{Title: "bash"}) {
		t.Error("did not expect a built-in tool title to classify as MCP")
	}
}

func TestCreateGitHubCopilot(t *testing.T) {
	h, err := CreateGitHubCopilot()
	if err != nil {
		t.Fatal(err)
	}
	if h.HarnessID() != "github-copilot" {
		t.Errorf("HarnessID() = %q, want github-copilot", h.HarnessID())
	}
}
