package fx

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Mirrors TS fx-harness.test.ts "enforces the fx ACP implementation".
func TestBuildConfig_Recipe(t *testing.T) {
	cfg := BuildConfig(Settings{})

	if cfg.HarnessID != "fx" || cfg.ClientAppName != "ai-sdk/harness-fx" {
		t.Errorf("harnessID/clientApp = %q/%q", cfg.HarnessID, cfg.ClientAppName)
	}
	if cfg.Executable != "fx" || !reflect.DeepEqual(cfg.Args, []string{"acp"}) {
		t.Errorf("executable/args = %q/%v", cfg.Executable, cfg.Args)
	}
	if cfg.Source != (Source{Type: "install-command", Command: "curl -fsSL https://fx.sh/setup.sh | bash"}) {
		t.Errorf("source = %+v", cfg.Source)
	}
	wantCredentialEnv := []string{
		"VERCEL_OIDC_TOKEN", "AI_GATEWAY_API_KEY",
		ChatGPTAccessTokenEnvVar, ChatGPTAccountIDEnvVar,
		GrokAccessTokenEnvVar, GrokAccountIDEnvVar,
	}
	if !reflect.DeepEqual(cfg.CredentialEnv, wantCredentialEnv) {
		t.Errorf("credentialEnv = %v", cfg.CredentialEnv)
	}
	if cfg.InstructionMapping != (InstructionMapping{Type: "filesystem", Path: ".fx/AGENTS.md"}) {
		t.Errorf("instructionMapping = %+v", cfg.InstructionMapping)
	}
	wantPermissions := map[harness.PermissionMode]*PermissionModeTarget{
		harness.PermissionModeAllowReads: {Type: "session-mode", ModeID: "ask"},
		harness.PermissionModeAllowEdits: {Type: "session-mode", ModeID: "ask"},
		harness.PermissionModeAllowAll:   {Type: "session-mode", ModeID: "code"},
	}
	if !reflect.DeepEqual(cfg.PermissionModeMapping, wantPermissions) {
		t.Errorf("permissionModeMapping = %+v", cfg.PermissionModeMapping)
	}
	if len(cfg.BuiltinTools) != 26 {
		t.Errorf("BuiltinTools has %d entries, want 26", len(cfg.BuiltinTools))
	}
	glob, ok := cfg.BuiltinTools["glob"]
	if !ok || glob.NativeName != "glob_files" || glob.CommonName != harness.BuiltinToolGlob {
		t.Errorf("glob = %+v", glob)
	}
}

// Mirrors "classifies tool calls from configured external MCP servers".
func TestIsMCPToolCall(t *testing.T) {
	cfg := BuildConfig(Settings{MCPServers: map[string]any{
		"context7":          map[string]any{"type": "http", "url": "https://mcp.context7.com/mcp"},
		"github \U0001F680": map[string]any{"command": "github-mcp"},
	}})

	cases := []struct {
		title string
		want  bool
	}{
		{"mcp_context7_resolve-library-id", true},
		{"mcp_github______create_issue", true},
		{"mcp_ai-sdk-harness-tools_weather", false},
		{"mcp_search_tools", false},
		{"Reading file", false},
	}
	for _, tc := range cases {
		if got := cfg.IsMCPToolCall(ToolCall{Title: tc.title}); got != tc.want {
			t.Errorf("IsMCPToolCall(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

// Mirrors "brokers fx credentials only to AI Gateway".
func TestCredentialBrokering_AIGatewayOnly(t *testing.T) {
	transforms, err := CredentialBrokering(
		map[string]string{"VERCEL_OIDC_TOKEN": "oidc-secret", "AI_GATEWAY_API_KEY": "gateway-secret"},
		map[string]string{"VERCEL_OIDC_TOKEN": "sandbox-oidc-secret", "AI_GATEWAY_API_KEY": "sandbox-gateway-secret"},
		map[string]string{"x-tenant": "acme"},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(transforms) != 1 {
		t.Fatalf("got %d transforms", len(transforms))
	}
	tr := transforms[0]
	if tr.Match.Host != "ai-gateway.vercel.sh" {
		t.Errorf("host = %q", tr.Match.Host)
	}
	if tr.Transform.Headers["Authorization"] != "Bearer oidc-secret" {
		t.Errorf("authorization = %q", tr.Transform.Headers["Authorization"])
	}
	if tr.Transform.Headers["x-client-app"] != "ai-sdk/harness-fx/0.0.0-go" {
		t.Errorf("x-client-app = %q", tr.Transform.Headers["x-client-app"])
	}
	if tr.Transform.Headers["x-tenant"] != "acme" {
		t.Errorf("x-tenant not preserved: %v", tr.Transform.Headers)
	}

	empty, err := CredentialBrokering(map[string]string{}, map[string]string{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d transforms, want 0", len(empty))
	}
}

// Mirrors "brokers native subscription access tokens to their provider route".
func TestCredentialBrokering_Subscription(t *testing.T) {
	transforms, err := CredentialBrokering(
		map[string]string{
			ChatGPTAccessTokenEnvVar: "host-access",
			ChatGPTAccountIDEnvVar:   "account-1",
		},
		map[string]string{
			ChatGPTAccessTokenEnvVar: "sandbox-access",
			ChatGPTAccountIDEnvVar:   "sandbox-account",
		},
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(transforms) != 1 || transforms[0].Match.Host != "chatgpt.com" {
		t.Fatalf("transforms = %+v", transforms)
	}
	if transforms[0].Transform.Headers["Authorization"] != "Bearer host-access" {
		t.Errorf("authorization = %q", transforms[0].Transform.Headers["Authorization"])
	}
}
