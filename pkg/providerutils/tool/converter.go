package tool

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToJSONSchema converts a Tool to JSON Schema format
// This is used when sending tool definitions to AI providers
func ToJSONSchema(tool types.Tool) map[string]interface{} {
	functionDef := map[string]interface{}{
		"name":        tool.Name,
		"description": tool.Description,
	}

	// Add parameters if present
	if tool.Parameters != nil {
		functionDef["parameters"] = tool.Parameters
	}

	// Pass strict mode when requested (#12893)
	if tool.Strict {
		functionDef["strict"] = true
	}

	schema := map[string]interface{}{
		"type":     "function",
		"function": functionDef,
	}

	return schema
}

// ToOpenAIFormat converts tools to OpenAI's tool format
func ToOpenAIFormat(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		result[i] = ToJSONSchema(tool)
	}
	return result
}

// ToAnthropicFormat converts tools to Anthropic's tool format
func ToAnthropicFormat(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		result[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"input_schema": tool.Parameters,
		}
	}
	return result
}

// ToGoogleFormat converts tools to Google's function calling format
func ToGoogleFormat(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		result[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
	}
	return result
}

// ParseToolCallArguments parses tool call arguments from various formats
func ParseToolCallArguments(args interface{}) (map[string]interface{}, error) {
	switch v := args.(type) {
	case map[string]interface{}:
		return v, nil
	case string:
		// Try to parse as JSON
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(v), &result); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments JSON: %w", err)
		}
		return result, nil
	case []byte:
		// Try to parse as JSON
		var result map[string]interface{}
		if err := json.Unmarshal(v, &result); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments JSON: %w", err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported tool arguments type: %T", args)
	}
}

// ValidateToolCall validates that a tool call references a known tool
func ValidateToolCall(toolCall types.ToolCall, availableTools []types.Tool) error {
	// Find the tool
	for _, tool := range availableTools {
		if tool.Name == toolCall.ToolName {
			return nil // Tool found
		}
	}
	return fmt.Errorf("unknown tool: %s", toolCall.ToolName)
}

// FindTool finds a tool by name in a list of tools
func FindTool(toolName string, tools []types.Tool) (*types.Tool, error) {
	for i := range tools {
		if tools[i].Name == toolName {
			return &tools[i], nil
		}
	}
	return nil, fmt.Errorf("tool not found: %s", toolName)
}

// ConvertToolChoice converts a unified ToolChoice to provider-specific format
func ConvertToolChoiceToOpenAI(choice types.ToolChoice) interface{} {
	switch choice.Type {
	case types.ToolChoiceAuto:
		return "auto"
	case types.ToolChoiceNone:
		return "none"
	case types.ToolChoiceRequired:
		return "required"
	case types.ToolChoiceTool:
		return map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": choice.ToolName,
			},
		}
	default:
		return "auto"
	}
}

// ConvertToolChoiceToAnthropic converts a unified ToolChoice to Anthropic's format
func ConvertToolChoiceToAnthropic(choice types.ToolChoice) interface{} {
	switch choice.Type {
	case types.ToolChoiceAuto:
		return map[string]interface{}{"type": "auto"}
	case types.ToolChoiceNone:
		return nil // Anthropic doesn't have explicit "none"
	case types.ToolChoiceRequired:
		return map[string]interface{}{"type": "any"}
	case types.ToolChoiceTool:
		return map[string]interface{}{
			"type": "tool",
			"name": choice.ToolName,
		}
	default:
		return map[string]interface{}{"type": "auto"}
	}
}

// ConvertToolChoiceToGoogle converts a unified ToolChoice to Google's format
func ConvertToolChoiceToGoogle(choice types.ToolChoice) string {
	switch choice.Type {
	case types.ToolChoiceAuto:
		return "AUTO"
	case types.ToolChoiceNone:
		return "NONE"
	case types.ToolChoiceRequired:
		return "ANY"
	default:
		return "AUTO"
	}
}
