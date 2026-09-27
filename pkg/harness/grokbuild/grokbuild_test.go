package grokbuild

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// Mirrors TS grok-build-harness.test.ts recipe assertions.
func TestBuildConfig_Recipe(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HarnessID != "grok-build" || cfg.Executable != "grok" {
		t.Errorf("harnessID/executable = %q/%q", cfg.HarnessID, cfg.Executable)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"agent", "stdio"}) {
		t.Errorf("args = %v", cfg.Args)
	}
	if cfg.Source.Type != acp.SourceNPMLocked || cfg.Source.PackageJSON == "" {
		t.Errorf("source = %+v", cfg.Source)
	}
	if cfg.Authentication == nil || cfg.Authentication.MethodID != "xai.api_key" {
		t.Errorf("authentication = %+v", cfg.Authentication)
	}
	if !reflect.DeepEqual(cfg.CredentialEnv, []string{"XAI_API_KEY"}) {
		t.Errorf("credentialEnv = %v", cfg.CredentialEnv)
	}
	if cfg.ModelMapping != (acp.ModelMapping{Type: acp.ModelMappingSessionModel, Path: "modelId"}) {
		t.Errorf("modelMapping = %+v", cfg.ModelMapping)
	}
	if cfg.InstructionMapping == nil || !reflect.DeepEqual(*cfg.InstructionMapping, acp.InstructionMapping{Type: acp.InstructionMappingFilesystem, FilePath: ".grok/AGENTS.md"}) {
		t.Errorf("instructionMapping = %+v", cfg.InstructionMapping)
	}
	if cfg.OutputSchemaMapping == nil || !reflect.DeepEqual(cfg.OutputSchemaMapping.Path, []string{"outputSchema"}) {
		t.Errorf("outputSchemaMapping = %+v", cfg.OutputSchemaMapping)
	}
	wantGatewayEnv := map[string]any{
		"GROK_CLIENT_NAME":      map[string]any{"$source": "client-app-name"},
		"GROK_CLIENT_VERSION":   map[string]any{"$source": "client-app-version"},
		"XAI_API_KEY":           map[string]any{"$source": "gateway-api-key"},
		"GROK_XAI_API_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
		"GROK_MODELS_BASE_URL":  map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
	}
	if cfg.ProviderAuthentication == nil || !reflect.DeepEqual(cfg.ProviderAuthentication.GatewayEnv, wantGatewayEnv) {
		t.Errorf("providerAuthentication = %+v", cfg.ProviderAuthentication)
	}
	if len(cfg.BuiltinTools) != 25 {
		t.Errorf("BuiltinTools has %d entries, want 25", len(cfg.BuiltinTools))
	}
	ask, ok := cfg.BuiltinTools["askUserQuestions"]
	if !ok || ask.CommonName != harness.BuiltinToolAskUserQuestions {
		t.Errorf("askUserQuestions = %+v", ask)
	}
	if cfg.AskUserQuestions == nil || cfg.AskUserQuestions.RequestMethod != "_x.ai/ask_user_question" {
		t.Errorf("askUserQuestions settings = %+v", cfg.AskUserQuestions)
	}
}

func TestBuildConfig_ReasoningEffort(t *testing.T) {
	cfg, err := BuildConfig(Settings{ReasoningEffort: ReasoningEffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"agent", "--reasoning-effort", "high", "stdio"}) {
		t.Errorf("args = %v", cfg.Args)
	}
}

func TestCredentialBrokering(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{"XAI_API_KEY": "xai-secret"},
		map[string]string{"XAI_API_KEY": "sandbox-secret"},
		nil,
	)
	if len(transforms) != 1 || transforms[0].Match.Host != "api.x.ai" {
		t.Fatalf("transforms = %+v", transforms)
	}
	if transforms[0].Transform.Headers["Authorization"] != "Bearer xai-secret" {
		t.Errorf("authorization = %q", transforms[0].Transform.Headers["Authorization"])
	}

	empty := CredentialBrokering(map[string]string{}, map[string]string{}, nil)
	if len(empty) != 0 {
		t.Errorf("got %d transforms, want 0", len(empty))
	}
}

func TestCredentialBrokering_ChatProxyHeader(t *testing.T) {
	transforms := CredentialBrokering(
		map[string]string{"XAI_API_KEY": "xai-secret", "GROK_CLI_CHAT_PROXY_BASE_URL": "https://cli-chat-proxy.grok.com/v1"},
		map[string]string{"XAI_API_KEY": "sandbox-secret"},
		nil,
	)
	if len(transforms) != 1 || transforms[0].Transform.Headers["X-XAI-Token-Auth"] != "xai-grok-cli" {
		t.Fatalf("transforms = %+v", transforms)
	}
}

func TestIsMCPToolCall(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsMcpToolCall(acp.ToolCall{Meta: map[string]any{"x.ai/tool": map[string]any{"namespace": "mcp"}}}) {
		t.Error("expected mcp namespace to classify as MCP")
	}
	if cfg.IsMcpToolCall(acp.ToolCall{Meta: map[string]any{"x.ai/tool": map[string]any{"namespace": "builtin"}}}) {
		t.Error("did not expect a non-mcp namespace to classify as MCP")
	}
	if cfg.IsMcpToolCall(acp.ToolCall{}) {
		t.Error("did not expect a nil meta to classify as MCP")
	}
}

func TestCreateGrokBuild(t *testing.T) {
	h, err := CreateGrokBuild()
	if err != nil {
		t.Fatal(err)
	}
	if h.HarnessID() != "grok-build" {
		t.Errorf("HarnessID() = %q, want grok-build", h.HarnessID())
	}
}
