package cursor

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Mirrors TS cursor-harness.test.ts "enforces the Cursor ACP implementation".
func TestBuildConfig_Recipe(t *testing.T) {
	cfg := BuildConfig(Settings{})

	if cfg.Version != "v1" {
		t.Errorf("Version = %q, want v1", cfg.Version)
	}
	if cfg.HarnessID != "cursor" {
		t.Errorf("HarnessID = %q, want cursor", cfg.HarnessID)
	}
	if cfg.ClientAppName != "ai-sdk/harness-cursor" {
		t.Errorf("ClientAppName = %q", cfg.ClientAppName)
	}
	if cfg.Executable != "agent" {
		t.Errorf("Executable = %q, want agent", cfg.Executable)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"--disable-auto-update", "acp"}) {
		t.Errorf("Args = %v", cfg.Args)
	}
	if cfg.Source.Type != "install-command" || cfg.Source.Command != "curl https://cursor.com/install -fsS | bash" {
		t.Errorf("Source = %+v", cfg.Source)
	}
	if !reflect.DeepEqual(cfg.CredentialEnv, []string{"CURSOR_API_KEY"}) {
		t.Errorf("CredentialEnv = %v", cfg.CredentialEnv)
	}
	if cfg.ModelMapping != (ModelMapping{Type: "session-config-option", Path: "model"}) {
		t.Errorf("ModelMapping = %+v", cfg.ModelMapping)
	}
	wantCaps := map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}}
	if !reflect.DeepEqual(cfg.ClientCapabilities, wantCaps) {
		t.Errorf("ClientCapabilities = %+v", cfg.ClientCapabilities)
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

func TestCredentialBrokering(t *testing.T) {
	// mirrors "enforces the Cursor ACP implementation" credentialBrokering assertions
	transforms, err := CredentialBrokering(harness.Authentication{}, map[string]string{"CURSOR_API_KEY": "cursor-secret"}, map[string]string{"CURSOR_API_KEY": "sandbox-cursor-secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
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

	empty, err := CredentialBrokering(harness.Authentication{}, map[string]string{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d transforms, want 0", len(empty))
	}
}

// Mirrors "applies headers to configured model request routes".
func TestCredentialBrokering_HeaderRouting(t *testing.T) {
	gateway, err := CredentialBrokering(harness.AuthMode(harness.AuthModeAIGateway), map[string]string{}, nil, map[string]string{"x-tenant": "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway) != 1 || gateway[0].Match.Host != "ai-gateway.vercel.sh" || gateway[0].Match.Path == nil || gateway[0].Match.Path.StartsWith != "/cursor/v1" {
		t.Errorf("gateway transform = %+v", gateway)
	}

	direct, err := CredentialBrokering(harness.Authentication{}, map[string]string{}, nil, map[string]string{"x-tenant": "acme"})
	if err != nil {
		t.Fatal(err)
	}
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
		BuildConfig(Settings{Auth: harness.AuthMode(mode)})
		Warn = orig
		if got == "" {
			t.Errorf("mode %q: expected a warning", mode)
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

// Mirrors "classifies Cursor MCP calls from their raw input".
func TestIsMCPToolCall(t *testing.T) {
	if !IsMCPToolCall(ToolCall{RawInput: map[string]any{
		"providerIdentifier": "ai-sdk-harness-tools",
		"toolName":           "weather",
		"args":               map[string]any{"city": "Lima"},
	}}) {
		t.Error("expected true for MCP-shaped input")
	}
	if IsMCPToolCall(ToolCall{RawInput: map[string]any{"path": "README.md"}}) {
		t.Error("expected false for non-MCP input")
	}
}
