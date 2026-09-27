package fx

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// Mirrors TS fx-harness.test.ts "enforces the fx ACP implementation".
func TestBuildConfig_Recipe(t *testing.T) {
	cfg := BuildConfig(Settings{})

	if cfg.HarnessID != "fx" || cfg.ClientApp.Name != "ai-sdk/harness-fx" {
		t.Errorf("harnessID/clientApp = %q/%q", cfg.HarnessID, cfg.ClientApp.Name)
	}
	if cfg.Executable != "fx" || !reflect.DeepEqual(cfg.Args, []string{"acp"}) {
		t.Errorf("executable/args = %q/%v", cfg.Executable, cfg.Args)
	}
	if cfg.Source != (acp.Source{Type: acp.SourceInstallCommand, Command: "curl -fsSL https://fx.sh/setup.sh | bash"}) {
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
	if cfg.InstructionMapping == nil || !reflect.DeepEqual(*cfg.InstructionMapping, acp.InstructionMapping{Type: acp.InstructionMappingFilesystem, FilePath: ".fx/AGENTS.md"}) {
		t.Errorf("instructionMapping = %+v", cfg.InstructionMapping)
	}
	wantPermissions := &acp.PermissionModeMapping{
		AllowReads: &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "ask"},
		AllowEdits: &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "ask"},
		AllowAll:   &acp.PermissionModeTarget{Type: acp.PermissionTargetSessionMode, ModeID: "code"},
	}
	if !reflect.DeepEqual(cfg.PermissionModeMapping, wantPermissions) {
		t.Errorf("permissionModeMapping = %+v", cfg.PermissionModeMapping)
	}
	wantGatewayEnv := map[string]any{
		"AI_GATEWAY_API_KEY":  map[string]any{"$source": "gateway-api-key"},
		"AI_GATEWAY_BASE_URL": map[string]any{"$source": "gateway-base-url"},
	}
	if cfg.ProviderAuthentication == nil || !reflect.DeepEqual(cfg.ProviderAuthentication.GatewayEnv, wantGatewayEnv) {
		t.Errorf("providerAuthentication = %+v", cfg.ProviderAuthentication)
	}
	if len(cfg.BuiltinTools) != 26 {
		t.Errorf("BuiltinTools has %d entries, want 26", len(cfg.BuiltinTools))
	}
	glob, ok := cfg.BuiltinTools["glob"]
	if !ok || glob.NativeName != "glob_files" || glob.CommonName != harness.BuiltinToolGlob {
		t.Errorf("glob = %+v", glob)
	}
}

// Mirrors "forwards user-configurable settings".
func TestBuildConfig_ForwardsUserConfigurableSettings(t *testing.T) {
	mintBridgeToken := func(sandboxID string) string { return "token-for-" + sandboxID }
	portEndpoint := &harness.PortEndpoint{URL: "wss://sandbox.example/bridge"}
	reconnect := &bridge.ReconnectOptions{MaxElapsed: 120_000 * time.Millisecond, InitialDelay: 100 * time.Millisecond, MaxDelay: 5_000 * time.Millisecond}
	port := 4319
	startupTimeoutMS := 45_000

	cfg := BuildConfig(Settings{
		Auth:             harness.AuthMode(harness.AuthModeDirect),
		Port:             &port,
		PortEndpoint:     portEndpoint,
		StartupTimeoutMS: &startupTimeoutMS,
		Reconnect:        reconnect,
		MCPServers:       map[string]any{"external": map[string]any{"command": "external-mcp"}},
		MintBridgeToken:  mintBridgeToken,
	})

	if cfg.Auth.Mode != harness.AuthModeDirect {
		t.Errorf("Auth = %+v", cfg.Auth)
	}
	if cfg.Port != 4319 {
		t.Errorf("Port = %d, want 4319", cfg.Port)
	}
	if cfg.PortEndpoint != portEndpoint {
		t.Errorf("PortEndpoint = %+v", cfg.PortEndpoint)
	}
	if cfg.StartupTimeout != 45_000*time.Millisecond {
		t.Errorf("StartupTimeout = %v", cfg.StartupTimeout)
	}
	if cfg.Reconnect != *reconnect {
		t.Errorf("Reconnect = %+v", cfg.Reconnect)
	}
	if !reflect.DeepEqual(cfg.MCPServers, map[string]any{"external": map[string]any{"command": "external-mcp"}}) {
		t.Errorf("MCPServers = %+v", cfg.MCPServers)
	}
	if cfg.MintBridgeToken == nil || cfg.MintBridgeToken("sbx-1") != "token-for-sbx-1" {
		t.Errorf("MintBridgeToken not forwarded correctly")
	}
	if cfg.ModelMapping != (acp.ModelMapping{Type: acp.ModelMappingSessionConfigOption, Path: "model"}) {
		t.Errorf("ModelMapping = %+v", cfg.ModelMapping)
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
		if got := cfg.IsMcpToolCall(acp.ToolCall{Title: tc.title}); got != tc.want {
			t.Errorf("IsMcpToolCall(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

// Mirrors "brokers fx credentials only to AI Gateway".
func TestCredentialBrokering_AIGatewayOnly(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{"VERCEL_OIDC_TOKEN": "oidc-secret", "AI_GATEWAY_API_KEY": "gateway-secret"},
		map[string]string{"VERCEL_OIDC_TOKEN": "sandbox-oidc-secret", "AI_GATEWAY_API_KEY": "sandbox-gateway-secret"},
		map[string]string{"x-tenant": "acme"},
		false,
	)
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

	empty := CredentialBrokering(map[string]string{}, map[string]string{}, nil, false)
	if len(empty) != 0 {
		t.Errorf("got %d transforms, want 0", len(empty))
	}
}

// Mirrors "brokers native subscription access tokens to their provider route".
func TestCredentialBrokering_Subscription(t *testing.T) {
	transforms := CredentialBrokering(
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
	if len(transforms) != 1 || transforms[0].Match.Host != "chatgpt.com" {
		t.Fatalf("transforms = %+v", transforms)
	}
	if transforms[0].Transform.Headers["Authorization"] != "Bearer host-access" {
		t.Errorf("authorization = %q", transforms[0].Transform.Headers["Authorization"])
	}
}

// Mirrors "materializes native subscription records through the ACP home
// hook".
func TestBuildConfig_AuthenticationFiles(t *testing.T) {
	cfg := BuildConfig(Settings{})
	accessToken := createChatGPTAccessToken(t, "account-1")

	files := cfg.AuthenticationFiles(
		map[string]string{
			ChatGPTAccessTokenEnvVar: accessToken,
			ChatGPTAccountIDEnvVar:   "account-1",
		},
		map[string]string{
			ChatGPTAccessTokenEnvVar: "sandbox-access",
			ChatGPTAccountIDEnvVar:   "sandbox-account",
		},
		true,
	)
	if len(files) != 1 || files[0].Path != ".fx/chatgpt-auth.json" {
		t.Fatalf("files = %+v", files)
	}
	if strings.Contains(files[0].Content, accessToken) {
		t.Errorf("content leaked the host access token: %q", files[0].Content)
	}
	if !strings.Contains(files[0].Content, "sandbox-access") {
		t.Errorf("content missing the sandbox access token: %q", files[0].Content)
	}
}

// Mirrors "brokers the Gateway key selected from a supplied authentication
// environment".
func TestBuildConfig_SuppliedAuthenticationEnvironmentGateway(t *testing.T) {
	auth := harness.AuthEnvironment(map[string]string{
		"AI_GATEWAY_API_KEY":  "explicit-gateway-key",
		"AI_GATEWAY_BASE_URL": "https://gateway.example/v1",
	})
	cfg := BuildConfig(Settings{Auth: auth})

	transforms := cfg.CredentialBrokering(
		map[string]string{
			"AI_GATEWAY_API_KEY":  "explicit-gateway-key",
			"AI_GATEWAY_BASE_URL": "https://gateway.example/v1",
			"VERCEL_OIDC_TOKEN":   "ambient-oidc-token",
		},
		map[string]string{
			"AI_GATEWAY_API_KEY": "sandbox-explicit-gateway-key",
			"VERCEL_OIDC_TOKEN":  "sandbox-ambient-oidc-token",
		},
		nil,
	)
	if len(transforms) != 1 {
		t.Fatalf("transforms = %+v", transforms)
	}
	tr := transforms[0]
	if tr.Match.Host != "gateway.example" || tr.Match.Path == nil || tr.Match.Path.StartsWith != "/v1" {
		t.Errorf("match = %+v", tr.Match)
	}
	if len(tr.Match.Headers) != 1 || tr.Match.Headers[0].Value == nil || tr.Match.Headers[0].Value.Exact != "Bearer sandbox-explicit-gateway-key" {
		t.Errorf("match headers = %+v", tr.Match.Headers)
	}
	if tr.Transform.Headers["Authorization"] != "Bearer explicit-gateway-key" {
		t.Errorf("authorization = %q", tr.Transform.Headers["Authorization"])
	}
	if tr.Transform.Headers["x-client-app"] != "ai-sdk/harness-fx/0.0.0-go" {
		t.Errorf("x-client-app = %q", tr.Transform.Headers["x-client-app"])
	}
}

func TestCreateFx(t *testing.T) {
	h, err := CreateFx()
	if err != nil {
		t.Fatal(err)
	}
	if h.HarnessID() != "fx" {
		t.Errorf("HarnessID() = %q, want fx", h.HarnessID())
	}
}
