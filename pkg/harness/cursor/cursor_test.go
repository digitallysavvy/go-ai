package cursor

import (
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// Mirrors TS cursor-harness.test.ts "enforces the Cursor ACP implementation".
func TestBuildConfig_Recipe(t *testing.T) {
	cfg := BuildConfig(Settings{})

	if cfg.HarnessID != "cursor" {
		t.Errorf("HarnessID = %q, want cursor", cfg.HarnessID)
	}
	if cfg.ClientApp.Name != "ai-sdk/harness-cursor" || cfg.ClientApp.Version != "0.0.0-go" {
		t.Errorf("ClientApp = %+v", cfg.ClientApp)
	}
	if cfg.Executable != "agent" {
		t.Errorf("Executable = %q, want agent", cfg.Executable)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"--disable-auto-update", "acp"}) {
		t.Errorf("Args = %v", cfg.Args)
	}
	if cfg.Source.Type != acp.SourceInstallCommand || cfg.Source.Command != "curl https://cursor.com/install -fsS | bash" {
		t.Errorf("Source = %+v", cfg.Source)
	}
	if !reflect.DeepEqual(cfg.CredentialEnv, []string{"CURSOR_API_KEY"}) {
		t.Errorf("CredentialEnv = %v", cfg.CredentialEnv)
	}
	if cfg.ModelMapping != (acp.ModelMapping{Type: acp.ModelMappingSessionConfigOption, Path: "model"}) {
		t.Errorf("ModelMapping = %+v", cfg.ModelMapping)
	}
	wantCaps := map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}}
	if !reflect.DeepEqual(cfg.ClientCapabilities, wantCaps) {
		t.Errorf("ClientCapabilities = %+v", cfg.ClientCapabilities)
	}
	if cfg.Auth.Mode != "" || len(cfg.Auth.Environment) != 0 {
		t.Errorf("Auth = %+v, want zero value (undefined)", cfg.Auth)
	}
	if cfg.ProviderAuthentication != nil {
		t.Errorf("ProviderAuthentication = %+v, want nil", cfg.ProviderAuthentication)
	}

	wantNames := []string{
		"bash", "delete", "glob", "grep", "read", "updateTodos", "readTodos",
		"edit", "ls", "readLints", "semanticSearch", "createPlan", "webSearch",
		"task", "listMcpResources", "readMcpResource", "applyAgentDiff",
		"fetch", "switchMode", "generateImage", "recordScreen", "computerUse",
		"writeShellStdin", "reflect", "setupVmEnvironment", "replaceEnv",
		"startGrindExecution", "startGrindPlanning", "webFetch",
		"reportBugfixResults",
	}
	if len(cfg.BuiltinTools) != len(wantNames) {
		t.Fatalf("BuiltinTools has %d entries, want %d", len(cfg.BuiltinTools), len(wantNames))
	}
	for _, name := range wantNames {
		if _, ok := cfg.BuiltinTools[name]; !ok {
			t.Errorf("BuiltinTools missing %q", name)
		}
	}
}

// Mirrors "forwards user-configurable settings".
func TestBuildConfig_ForwardsUserConfigurableSettings(t *testing.T) {
	mintBridgeToken := func(sandboxID string) string { return "token-for-" + sandboxID }
	credentialForwarding := harness.CredentialForwarding(nil)
	portEndpoint := &harness.PortEndpoint{URL: "wss://sandbox.example/bridge"}
	reconnect := &bridge.ReconnectOptions{MaxElapsed: 120_000 * time.Millisecond, InitialDelay: 100 * time.Millisecond, MaxDelay: 5_000 * time.Millisecond}
	port := 4319
	startupTimeoutMS := 45_000

	cfg := BuildConfig(Settings{
		CredentialForwarding: credentialForwarding,
		Port:                 &port,
		PortEndpoint:         portEndpoint,
		StartupTimeoutMS:     &startupTimeoutMS,
		Reconnect:            reconnect,
		MCPServers:           map[string]any{"external": map[string]any{"command": "external-mcp"}},
		MintBridgeToken:      mintBridgeToken,
	})

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
}

func TestCredentialBrokering(t *testing.T) {
	// mirrors "enforces the Cursor ACP implementation" credentialBrokering assertions
	transforms := CredentialBrokering(harness.Authentication{}, map[string]string{"CURSOR_API_KEY": "cursor-secret"}, map[string]string{"CURSOR_API_KEY": "sandbox-cursor-secret"}, nil)
	if len(transforms) != 1 {
		t.Fatalf("got %d transforms, want 1", len(transforms))
	}
	tr := transforms[0]
	if tr.Match.Host != "api2.cursor.sh" || tr.Match.Path == nil || tr.Match.Path.Exact != "/auth/exchange_user_api_key" {
		t.Errorf("match = %+v", tr.Match)
	}
	if tr.Transform.Headers["Authorization"] != "Bearer cursor-secret" {
		t.Errorf("transform headers = %v", tr.Transform.Headers)
	}

	empty := CredentialBrokering(harness.Authentication{}, map[string]string{}, nil, nil)
	if len(empty) != 0 {
		t.Errorf("got %d transforms, want 0", len(empty))
	}
}

// Mirrors "applies headers to configured model request routes".
func TestCredentialBrokering_HeaderRouting(t *testing.T) {
	gateway := CredentialBrokering(harness.AuthMode(harness.AuthModeAIGateway), map[string]string{}, nil, map[string]string{"x-tenant": "acme"})
	if len(gateway) != 1 || gateway[0].Match.Host != "ai-gateway.vercel.sh" || gateway[0].Match.Path == nil || gateway[0].Match.Path.StartsWith != "/cursor/v1" {
		t.Errorf("gateway transform = %+v", gateway)
	}

	direct := CredentialBrokering(harness.Authentication{}, map[string]string{}, nil, map[string]string{"x-tenant": "acme"})
	if len(direct) != 1 || direct[0].Match.Host != "api2.cursor.sh" || direct[0].Match.Path != nil {
		t.Errorf("direct transform = %+v", direct)
	}
}

// Mirrors "accepts %s auth and warns" / "accepts auto auth without warning".
func TestWarnAuthenticationConfiguration(t *testing.T) {
	for _, mode := range []string{harness.AuthModeDirect, harness.AuthModeAIGateway} {
		var got string
		orig := Warn
		Warn = func(message string) { got = message }
		cfg := BuildConfig(Settings{Auth: harness.AuthMode(mode)})
		Warn = orig
		if got == "" {
			t.Errorf("mode %q: expected a warning", mode)
		}
		if cfg.Auth.Mode != mode {
			t.Errorf("mode %q: Auth = %+v", mode, cfg.Auth)
		}
		if cfg.ProviderAuthentication != nil {
			t.Errorf("mode %q: ProviderAuthentication = %+v, want nil", mode, cfg.ProviderAuthentication)
		}
	}

	for _, mode := range []string{harness.AuthModeAuto, ""} {
		called := false
		orig := Warn
		Warn = func(message string) { called = true }
		BuildConfig(Settings{Auth: harness.AuthMode(mode)})
		Warn = orig
		if called {
			t.Errorf("mode %q: unexpected warning", mode)
		}
	}
}

// Mirrors "forwards a supplied authentication environment for Cursor
// credentials".
func TestBuildConfig_IsolatedAuthenticationEnvironment(t *testing.T) {
	auth := harness.Authentication{Environment: map[string]string{"CURSOR_API_KEY": "programmatic-cursor-key"}}
	cfg := BuildConfig(Settings{Auth: auth})
	if !reflect.DeepEqual(cfg.Auth, auth) {
		t.Errorf("Auth = %+v", cfg.Auth)
	}
	if cfg.ProviderAuthentication != nil {
		t.Errorf("ProviderAuthentication = %+v, want nil", cfg.ProviderAuthentication)
	}
}

// Mirrors "classifies Cursor MCP calls from their raw input".
func TestIsMCPToolCall(t *testing.T) {
	if !IsMCPToolCall(acp.ToolCall{RawInput: map[string]any{
		"providerIdentifier": "ai-sdk-harness-tools",
		"toolName":           "weather",
		"args":               map[string]any{"city": "Lima"},
	}}) {
		t.Error("expected true for MCP-shaped input")
	}
	if IsMCPToolCall(acp.ToolCall{RawInput: map[string]any{"path": "README.md"}}) {
		t.Error("expected false for non-MCP input")
	}
}

func TestCreateCursor(t *testing.T) {
	h, err := CreateCursor()
	if err != nil {
		t.Fatal(err)
	}
	if h.HarnessID() != "cursor" {
		t.Errorf("HarnessID() = %q, want cursor", h.HarnessID())
	}
	if !harness.SupportsBuiltinToolApprovals(h) {
		t.Error("expected SupportsBuiltinToolApprovals() to be true")
	}
}
