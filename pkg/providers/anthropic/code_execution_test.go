package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// ============================================================================
// Beta header injection tests (ACODE-T10)
// ============================================================================

func TestCombineBetaHeaders_CodeExecutionTool(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Run some code"},
		Tools:  []types.Tool{tools.CodeExecution20260120()},
	}

	header := model.combineBetaHeaders(opts, false)
	if header != BetaHeaderCodeExecution {
		t.Errorf("combineBetaHeaders() = %q, want %q", header, BetaHeaderCodeExecution)
	}
}

func TestCombineBetaHeaders_NoCodeExecutionTool(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	// Regular tool (not code execution)
	regularTool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather",
	}

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "What's the weather?"},
		Tools:  []types.Tool{regularTool},
	}

	header := model.combineBetaHeaders(opts, false)
	if header != "" {
		t.Errorf("combineBetaHeaders() = %q, want empty string for non-code-execution tools", header)
	}
}

func TestCombineBetaHeaders_NoTools(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	}

	header := model.combineBetaHeaders(opts, false)
	if header != "" {
		t.Errorf("combineBetaHeaders() = %q, want empty string when no tools", header)
	}
}

func TestCombineBetaHeaders_CodeExecutionPlusFastMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", &ModelOptions{
		Speed: SpeedFast,
	})

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Run some code"},
		Tools:  []types.Tool{tools.CodeExecution20260120()},
	}

	header := model.combineBetaHeaders(opts, false)
	// Should contain both fast mode and code execution headers
	if header == "" {
		t.Error("combineBetaHeaders() should not be empty")
	}

	// Check both headers are present
	containsFastMode := false
	containsCodeExecution := false
	for _, h := range splitBetaHeaders(header) {
		if h == BetaHeaderFastMode {
			containsFastMode = true
		}
		if h == BetaHeaderCodeExecution {
			containsCodeExecution = true
		}
	}

	if !containsFastMode {
		t.Errorf("combineBetaHeaders() = %q, missing %q", header, BetaHeaderFastMode)
	}
	if !containsCodeExecution {
		t.Errorf("combineBetaHeaders() = %q, missing %q", header, BetaHeaderCodeExecution)
	}
}

func TestCombineBetaHeaders_NilOpts(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	// nil opts should not panic
	header := model.combineBetaHeaders(nil, false)
	if header != "" {
		t.Errorf("combineBetaHeaders(nil) = %q, want empty", header)
	}
}

// ============================================================================
// Tool format in request body tests (ACODE-T09)
// ============================================================================

func TestToAnthropicFormatWithCache_CodeExecutionTool(t *testing.T) {
	tool := tools.CodeExecution20260120()
	result := ToAnthropicFormatWithCache([]types.Tool{tool})

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool in result, got %d", len(result))
	}

	toolMap := result[0]

	// Code execution tool must be sent with type field only
	apiType, ok := toolMap["type"].(string)
	if !ok {
		t.Fatalf("Tool map must have string 'type' field, got %T", toolMap["type"])
	}
	if apiType != tools.AnthropicCodeExecutionToolType {
		t.Errorf("Tool type = %q, want %q", apiType, tools.AnthropicCodeExecutionToolType)
	}

	// Code execution tool should NOT have name or input_schema
	if _, hasName := toolMap["name"]; hasName {
		t.Error("Code execution tool should not have 'name' field in API format")
	}
	if _, hasSchema := toolMap["input_schema"]; hasSchema {
		t.Error("Code execution tool should not have 'input_schema' field in API format")
	}
}

func TestToAnthropicFormatWithCache_RegularTool(t *testing.T) {
	regularTool := types.Tool{
		Name:        "get_weather",
		Description: "Get weather",
		Parameters:  map[string]interface{}{"type": "object"},
	}
	result := ToAnthropicFormatWithCache([]types.Tool{regularTool})

	if len(result) != 1 {
		t.Fatalf("Expected 1 tool in result, got %d", len(result))
	}

	toolMap := result[0]

	// Regular tools must have name and input_schema
	if toolMap["name"] != "get_weather" {
		t.Errorf("Regular tool name = %v, want get_weather", toolMap["name"])
	}
	if toolMap["description"] != "Get weather" {
		t.Errorf("Regular tool description = %v, want 'Get weather'", toolMap["description"])
	}
	if _, hasSchema := toolMap["input_schema"]; !hasSchema {
		t.Error("Regular tool must have input_schema")
	}
}

func TestToAnthropicFormatWithCache_MixedTools(t *testing.T) {
	codeExecTool := tools.CodeExecution20260120()
	regularTool := types.Tool{
		Name:        "my_tool",
		Description: "A regular tool",
	}

	result := ToAnthropicFormatWithCache([]types.Tool{codeExecTool, regularTool})

	if len(result) != 2 {
		t.Fatalf("Expected 2 tools in result, got %d", len(result))
	}

	// First tool is code execution - must have type only
	if result[0]["type"] != tools.AnthropicCodeExecutionToolType {
		t.Errorf("Code execution tool type = %v, want %v", result[0]["type"], tools.AnthropicCodeExecutionToolType)
	}
	if _, hasName := result[0]["name"]; hasName {
		t.Error("Code execution tool must not have name field")
	}

	// Second tool is regular - must have name
	if result[1]["name"] != "my_tool" {
		t.Errorf("Regular tool name = %v, want my_tool", result[1]["name"])
	}
}

// ============================================================================
// Helper
// ============================================================================

// splitBetaHeaders splits comma-separated beta header values.
func splitBetaHeaders(header string) []string {
	if header == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(header); i++ {
		if i == len(header) || header[i] == ',' {
			if i > start {
				result = append(result, header[start:i])
			}
			start = i + 1
		}
	}
	return result
}
