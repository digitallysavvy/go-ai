package githubcopilot

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
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
	if cfg.Source.Type != "npm-locked" || cfg.Source.PackageJSON == "" || cfg.Source.PnpmLockYAML == "" {
		t.Errorf("source = %+v", cfg.Source)
	}
	if cfg.HostToolMCPTransport != "http" {
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
	if cfg.InstructionMapping != (InstructionMapping{Type: "filesystem", Path: ".copilot/copilot-instructions.md"}) {
		t.Errorf("instructionMapping = %+v", cfg.InstructionMapping)
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
	transforms, err := CredentialBrokering(
		map[string]string{"COPILOT_GITHUB_TOKEN": "github-secret", "GH_TOKEN": "second-github-secret"},
		map[string]string{"COPILOT_GITHUB_TOKEN": "copilot-placeholder", "GH_TOKEN": "gh-placeholder"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
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
	transforms, err := CredentialBrokering(
		map[string]string{"COPILOT_GH_HOST": "example.ghe.com", "GITHUB_TOKEN": "github-secret"},
		map[string]string{"COPILOT_GH_HOST": "example.ghe.com", "GITHUB_TOKEN": "github-placeholder"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
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

// Mirrors "classifies built-in and configured MCP title prefixes".
func TestIsMCPToolCall(t *testing.T) {
	cfg, err := BuildConfig(Settings{MCPServers: map[string]any{"context7": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsMCPToolCall(ToolCall{Title: "github-mcp-server-search"}) {
		t.Error("expected github-mcp-server- prefix to classify as MCP")
	}
	if !cfg.IsMCPToolCall(ToolCall{Title: "context7-resolve"}) {
		t.Error("expected configured server prefix to classify as MCP")
	}
	if cfg.IsMCPToolCall(ToolCall{Title: "bash"}) {
		t.Error("did not expect a built-in tool title to classify as MCP")
	}
}
