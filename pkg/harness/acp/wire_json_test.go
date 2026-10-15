package acp

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// The embedded TS bridge validates these shapes with zod
// (acp-v1-bridge-protocol.ts), keyed by the TS field names.
func TestWireJSON_MappingsUseTSShapes(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"instruction session-meta", InstructionMapping{Type: InstructionMappingSessionMeta, Path: []string{"systemPrompt", "append"}}, `{"type":"session-meta","path":["systemPrompt","append"]}`},
		{"instruction launch-env-json", InstructionMapping{Type: InstructionMappingLaunchEnvJSON, Variable: "CODEX_CONFIG", Path: []string{"a"}}, `{"type":"launch-env-json","path":["a"],"variable":"CODEX_CONFIG"}`},
		{"instruction filesystem", InstructionMapping{Type: InstructionMappingFilesystem, FilePath: ".cursor/rules/AGENTS.md"}, `{"type":"filesystem","path":".cursor/rules/AGENTS.md"}`},
		{"output schema", OutputSchemaMapping{Path: []string{"outputSchema"}}, `{"type":"session-prompt-meta","path":["outputSchema"]}`},
		{"model", ModelMapping{Type: ModelMappingSessionConfigOption, Path: "model"}, `{"type":"session-config-option","path":"model"}`},
		{"permission mapping", PermissionModeMapping{AllowAll: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "agent-full-access"}},
			`{"allow-reads":null,"allow-edits":null,"allow-all":{"type":"session-mode","modeId":"agent-full-access"}}`},
		{"permission config option false", PermissionModeMapping{AllowReads: &PermissionModeTarget{Type: PermissionTargetSessionConfigOption, ConfigID: "readonly", Value: false}},
			`{"allow-reads":{"type":"session-config-option","configId":"readonly","value":false},"allow-edits":null,"allow-all":null}`},
		{"authentication", Authentication{MethodID: "api-key", Meta: map[string]any{"k": "v"}}, `{"methodId":"api-key","meta":{"k":"v"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Fatalf("got  %s\nwant %s", b, tc.want)
			}
			// Round trip through the decoder.
			ptr := reflect.New(reflect.TypeOf(tc.v))
			if err := json.Unmarshal(b, ptr.Interface()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ptr.Elem().Interface(), tc.v) {
				t.Fatalf("round trip = %+v, want %+v", ptr.Elem().Interface(), tc.v)
			}
		})
	}
}

// Lifecycle state persisted with the previous Go-field-name encoding still loads.
func TestWireJSON_LegacyEncodingStillDecodes(t *testing.T) {
	var m PermissionModeMapping
	if err := json.Unmarshal([]byte(`{"AllowReads":null,"AllowEdits":null,"AllowAll":{"Type":"session-mode","ModeID":"x","ConfigID":"","Value":null}}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.AllowAll == nil || m.AllowAll.ModeID != "x" || m.AllowAll.Type != PermissionTargetSessionMode {
		t.Fatalf("decoded = %+v", m)
	}
	var im InstructionMapping
	if err := json.Unmarshal([]byte(`{"Type":"filesystem","Path":null,"Variable":"","FilePath":"a/b.md"}`), &im); err != nil || im.FilePath != "a/b.md" {
		t.Fatalf("decoded = %+v, %v", im, err)
	}
}

// The bridge schema declares `builtinTools`/`tools`/`prompt` as arrays with a
// default only for undefined, so an empty list must encode as [] never null.
func TestWireJSON_EmptyListsEncodeAsArrays(t *testing.T) {
	cfg := createTurnStartConfig(createTurnStartConfigInput{
		Prompt: []TextContentBlock{}, Tools: []harness.ToolSpec{}, BuiltinTools: []BuiltinToolMapping{},
		PermissionMode: harness.PermissionModeAllowAll, ModelMapping: ModelMapping{Type: ModelMappingSessionModel, Path: "model"},
	})
	b, _ := json.Marshal(cfg)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"prompt", "tools", "builtinTools"} {
		if arr, ok := m[k].([]any); !ok || len(arr) != 0 {
			t.Errorf("%s = %#v, want []", k, m[k])
		}
	}
}

func TestWireJSON_BridgeConfigurationAuthentication(t *testing.T) {
	env := createBridgeEnvironment(bridgeConfiguration{Authentication: &Authentication{MethodID: "api-key", ClientCapabilities: map[string]any{"fs": true}}})
	var cfg struct {
		Authentication map[string]any `json:"authentication"`
	}
	if err := json.Unmarshal([]byte(env[BridgeConfigurationEnv]), &cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Authentication, map[string]any{"methodId": "api-key", "clientCapabilities": map[string]any{"fs": true}}) {
		t.Fatalf("authentication = %v", cfg.Authentication)
	}
}
