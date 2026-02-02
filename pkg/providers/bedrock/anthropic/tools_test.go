package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestUpgradeToolVersion(t *testing.T) {
	provider := New(Config{Region: "us-east-1"})

	tests := []struct {
		name       string
		inputTool  types.Tool
		expectName string
	}{
		{
			name: "bash tool upgrade",
			inputTool: types.Tool{
				Name:        "bash_20241022",
				Description: "Execute bash commands",
			},
			expectName: "bash_20250124",
		},
		{
			name: "text_editor tool upgrade",
			inputTool: types.Tool{
				Name:        "text_editor_20241022",
				Description: "Edit text files",
			},
			expectName: "text_editor_20250728",
		},
		{
			name: "computer tool upgrade",
			inputTool: types.Tool{
				Name:        "computer_20241022",
				Description: "Control computer",
			},
			expectName: "computer_20250124",
		},
		{
			name: "no upgrade needed",
			inputTool: types.Tool{
				Name:        "custom_tool",
				Description: "Custom tool",
			},
			expectName: "custom_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.UpgradeToolVersion(tt.inputTool)
			if result.Name != tt.expectName {
				t.Errorf("expected tool name %s, got %s", tt.expectName, result.Name)
			}
		})
	}
}

func TestMapToolName(t *testing.T) {
	provider := New(Config{Region: "us-east-1"})

	tests := []struct {
		name       string
		inputTool  types.Tool
		expectName string
	}{
		{
			name: "text_editor_20250728 name mapping",
			inputTool: types.Tool{
				Name:        "text_editor_20250728",
				Description: "Edit text files",
			},
			expectName: "str_replace_based_edit_tool",
		},
		{
			name: "no mapping needed",
			inputTool: types.Tool{
				Name:        "bash_20250124",
				Description: "Execute bash commands",
			},
			expectName: "bash_20250124",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.MapToolName(tt.inputTool)
			if result.Name != tt.expectName {
				t.Errorf("expected tool name %s, got %s", tt.expectName, result.Name)
			}
		})
	}
}

func TestPrepareTools(t *testing.T) {
	provider := New(Config{Region: "us-east-1"})

	tests := []struct {
		name        string
		inputTools  []types.Tool
		expectNames []string
	}{
		{
			name: "full preparation pipeline",
			inputTools: []types.Tool{
				{Name: "bash_20241022", Description: "Bash"},
				{Name: "text_editor_20241022", Description: "Editor"},
				{Name: "custom_tool", Description: "Custom"},
			},
			expectNames: []string{
				"bash_20250124",
				"str_replace_based_edit_tool",
				"custom_tool",
			},
		},
		{
			name:        "empty tools",
			inputTools:  []types.Tool{},
			expectNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.PrepareTools(tt.inputTools)
			if len(result) != len(tt.expectNames) {
				t.Fatalf("expected %d tools, got %d", len(tt.expectNames), len(result))
			}
			for i, expectedName := range tt.expectNames {
				if result[i].Name != expectedName {
					t.Errorf("tool %d: expected name %s, got %s", i, expectedName, result[i].Name)
				}
			}
		})
	}
}

func TestGetBetaHeaders(t *testing.T) {
	provider := New(Config{Region: "us-east-1"})

	tests := []struct {
		name        string
		tools       []types.Tool
		expectBetas []string
	}{
		{
			name: "computer use tools",
			tools: []types.Tool{
				{Name: "bash_20250124"},
				{Name: "computer_20250124"},
			},
			expectBetas: []string{"computer-use-2025-01-24"},
		},
		{
			name: "mixed tools",
			tools: []types.Tool{
				{Name: "bash_20250124"},
				{Name: "custom_tool"},
			},
			expectBetas: []string{"computer-use-2025-01-24"},
		},
		{
			name: "no computer use tools",
			tools: []types.Tool{
				{Name: "custom_tool"},
			},
			expectBetas: nil,
		},
		{
			name:        "empty tools",
			tools:       []types.Tool{},
			expectBetas: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.GetBetaHeaders(tt.tools)
			if len(result) != len(tt.expectBetas) {
				t.Errorf("expected %d beta headers, got %d", len(tt.expectBetas), len(result))
			}
			// Check that expected betas are present (order doesn't matter for map-based deduplication)
			if len(tt.expectBetas) > 0 && len(result) > 0 {
				found := false
				for _, beta := range result {
					for _, expected := range tt.expectBetas {
						if beta == expected {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("expected beta headers %v, got %v", tt.expectBetas, result)
				}
			}
		})
	}
}

func TestIsComputerUseTool(t *testing.T) {
	provider := New(Config{Region: "us-east-1"})

	tests := []struct {
		toolName string
		expected bool
	}{
		{"bash_20250124", true},
		{"bash_20241022", true},
		{"text_editor_20250728", true},
		{"computer_20250124", true},
		{"custom_tool", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := provider.IsComputerUseTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("expected %v for %s, got %v", tt.expected, tt.toolName, result)
			}
		})
	}
}
