package responses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func TestPrepareTools_Nil(t *testing.T) {
	result := PrepareTools(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestPrepareTools_Empty(t *testing.T) {
	result := PrepareTools([]types.Tool{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestPrepareTools_FunctionTool(t *testing.T) {
	tool := types.Tool{
		Name:        "get_weather",
		Description: "Get current weather",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}

	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Type != "function" {
		t.Errorf("Type: got %q, want %q", def.Type, "function")
	}
	if def.Name != "get_weather" {
		t.Errorf("Name: got %q", def.Name)
	}
	if def.Description != "Get current weather" {
		t.Errorf("Description: got %q", def.Description)
	}
}

func TestPrepareTools_FunctionTool_Strict(t *testing.T) {
	tool := types.Tool{
		Name:   "strict_tool",
		Strict: true,
	}

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(FunctionToolDef)
	if !ok {
		t.Fatalf("expected FunctionToolDef, got %T", result[0])
	}
	if def.Strict == nil || !*def.Strict {
		t.Error("expected Strict to be true")
	}
}

func TestPrepareTools_LocalShell(t *testing.T) {
	tool := NewLocalShellTool()
	result := PrepareTools([]types.Tool{tool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	def, ok := result[0].(LocalShellToolDef)
	if !ok {
		t.Fatalf("expected LocalShellToolDef, got %T", result[0])
	}
	if def.Type != "local_shell" {
		t.Errorf("Type: got %q, want %q", def.Type, "local_shell")
	}
}

func TestPrepareTools_Shell_NoEnvironment(t *testing.T) {
	tool := NewShellTool()
	result := PrepareTools([]types.Tool{tool})

	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Type != "shell" {
		t.Errorf("Type: got %q, want %q", def.Type, "shell")
	}
	if def.Environment != nil {
		t.Error("expected nil Environment")
	}
}

func TestPrepareTools_Shell_WithContainerAutoEnvironment(t *testing.T) {
	memLimit := "4g"
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type:        "container_auto",
		MemoryLimit: &memLimit,
		FileIDs:     []string{"file_1", "file_2"},
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil {
		t.Fatal("expected non-nil Environment")
	}
	if def.Environment.Type != "container_auto" {
		t.Errorf("Environment.Type: got %q", def.Environment.Type)
	}
	if def.Environment.MemoryLimit == nil || *def.Environment.MemoryLimit != "4g" {
		t.Error("MemoryLimit mismatch")
	}
	if len(def.Environment.FileIDs) != 2 {
		t.Errorf("FileIDs count: got %d, want 2", len(def.Environment.FileIDs))
	}
}

func TestPrepareTools_Shell_WithContainerReferenceEnvironment(t *testing.T) {
	containerID := "container_abc"
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type:        "container_reference",
		ContainerID: &containerID,
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil {
		t.Fatal("expected non-nil Environment")
	}
	if def.Environment.Type != "container_reference" {
		t.Errorf("Environment.Type: got %q", def.Environment.Type)
	}
	if def.Environment.ContainerID == nil || *def.Environment.ContainerID != "container_abc" {
		t.Error("ContainerID mismatch")
	}
}

func TestPrepareTools_ApplyPatch(t *testing.T) {
	tool := NewApplyPatchTool()
	result := PrepareTools([]types.Tool{tool})

	def, ok := result[0].(ApplyPatchToolDef)
	if !ok {
		t.Fatalf("expected ApplyPatchToolDef, got %T", result[0])
	}
	if def.Type != "apply_patch" {
		t.Errorf("Type: got %q, want %q", def.Type, "apply_patch")
	}
}

func TestPrepareTools_CustomTool_WithGrammarFormat(t *testing.T) {
	syntax := "lark"
	def := `start: WORD`
	ct := openaitool.NewCustomTool("json-extractor",
		openaitool.WithDescription("Extract JSON"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &def,
		}),
	)
	sdkTool := ct.ToTool()

	result := PrepareTools([]types.Tool{sdkTool})
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Type != "custom" {
		t.Errorf("Type: got %q, want %q", toolDef.Type, "custom")
	}
	if toolDef.Name != "json-extractor" {
		t.Errorf("Name: got %q, want %q", toolDef.Name, "json-extractor")
	}
	if toolDef.Description == nil || *toolDef.Description != "Extract JSON" {
		t.Error("Description mismatch")
	}
	if toolDef.Format == nil {
		t.Fatal("Format should not be nil")
	}
	if toolDef.Format.Type != "grammar" {
		t.Errorf("Format.Type: got %q", toolDef.Format.Type)
	}
	if toolDef.Format.Syntax == nil || *toolDef.Format.Syntax != "lark" {
		t.Error("Syntax mismatch")
	}
}

func TestPrepareTools_CustomTool_WithTextFormat(t *testing.T) {
	ct := openaitool.NewCustomTool("text-tool",
		openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
	)
	sdkTool := ct.ToTool()

	result := PrepareTools([]types.Tool{sdkTool})
	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Format == nil {
		t.Fatal("Format should not be nil")
	}
	if toolDef.Format.Type != "text" {
		t.Errorf("Format.Type: got %q, want text", toolDef.Format.Type)
	}
}

func TestPrepareTools_CustomTool_NoFormat(t *testing.T) {
	ct := openaitool.NewCustomTool("simple-tool")
	sdkTool := ct.ToTool()

	result := PrepareTools([]types.Tool{sdkTool})
	toolDef, ok := result[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", result[0])
	}
	if toolDef.Format != nil {
		t.Error("Format should be nil")
	}
	if toolDef.Description != nil {
		t.Error("Description should be nil")
	}
}

func TestPrepareTools_MixedTools_SerializesToJSON(t *testing.T) {
	// Verify the mixed result marshals correctly
	tools := []types.Tool{
		{Name: "get_weather", Description: "Get weather"},
		NewLocalShellTool(),
		NewShellTool(),
		NewApplyPatchTool(),
		openaitool.NewCustomTool("my-tool").ToTool(),
	}

	result := PrepareTools(tools)
	if len(result) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(result))
	}

	// Verify JSON marshaling works for all
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	expectedTypes := []string{"function", "local_shell", "shell", "apply_patch", "custom"}
	for i, expected := range expectedTypes {
		got, ok := raw[i]["type"].(string)
		if !ok || got != expected {
			t.Errorf("tool[%d].type: got %q, want %q", i, got, expected)
		}
	}
}

// TestPrepareTools_Shell_NetworkPolicy verifies network policy serialization
func TestPrepareTools_Shell_NetworkPolicy(t *testing.T) {
	tool := NewShellTool(WithShellEnvironment(ShellEnvironment{
		Type: "container_auto",
		NetworkPolicy: &ShellNetworkPolicy{
			Type:           "allowlist",
			AllowedDomains: []string{"example.com"},
		},
	}))

	result := PrepareTools([]types.Tool{tool})
	def, ok := result[0].(ShellToolDef)
	if !ok {
		t.Fatalf("expected ShellToolDef, got %T", result[0])
	}
	if def.Environment == nil || def.Environment.NetworkPolicy == nil {
		t.Fatal("expected non-nil NetworkPolicy")
	}
	if def.Environment.NetworkPolicy.Type != "allowlist" {
		t.Errorf("NetworkPolicy.Type: got %q", def.Environment.NetworkPolicy.Type)
	}
	if len(def.Environment.NetworkPolicy.AllowedDomains) != 1 {
		t.Error("AllowedDomains count mismatch")
	}
}
