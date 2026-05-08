package tool

import (
	"encoding/json"
	"fmt"
	"strings"

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
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": SanitizeAnthropicSchema(tool.Parameters),
		}
	}
	return result
}

// SanitizeAnthropicSchema mirrors the TypeScript SDK's Anthropic schema
// sanitizer. It keeps the structural subset Anthropic accepts, sets object
// schemas to additionalProperties=false, and moves unsupported validation
// constraints into descriptions so model guidance is not lost.
func SanitizeAnthropicSchema(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		return sanitizeAnthropicSchemaMap(v)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = SanitizeAnthropicSchema(item)
		}
		return out
	default:
		return schema
	}
}

func sanitizeAnthropicSchemaMap(schema map[string]interface{}) map[string]interface{} {
	if ref, ok := schema["$ref"]; ok {
		return map[string]interface{}{"$ref": ref}
	}

	out := map[string]interface{}{}
	copyIfPresent(out, schema, "$schema")
	copyIfPresent(out, schema, "$id")
	copyIfPresent(out, schema, "title")
	copyIfPresent(out, schema, "description")
	copyIfPresent(out, schema, "default")
	copyIfPresent(out, schema, "const")
	copyIfPresent(out, schema, "enum")
	copyIfPresent(out, schema, "type")

	if anyOf, ok := sanitizeAnthropicSchemaList(schema["anyOf"]); ok {
		out["anyOf"] = anyOf
	} else if oneOf, ok := sanitizeAnthropicSchemaList(schema["oneOf"]); ok {
		out["anyOf"] = oneOf
	}
	if allOf, ok := sanitizeAnthropicSchemaList(schema["allOf"]); ok {
		out["allOf"] = allOf
	}
	if definitions, ok := sanitizeAnthropicSchemaDefinitions(schema["definitions"]); ok {
		out["definitions"] = definitions
	}
	if defs, ok := sanitizeAnthropicSchemaDefinitions(schema["$defs"]); ok {
		out["$defs"] = defs
	}

	if schemaType, _ := schema["type"].(string); schemaType == "object" || schema["properties"] != nil {
		if properties, ok := sanitizeAnthropicSchemaDefinitions(schema["properties"]); ok {
			out["properties"] = properties
		}
		out["additionalProperties"] = false
		copyIfPresent(out, schema, "required")
	}

	if items, ok := schema["items"]; ok {
		out["items"] = SanitizeAnthropicSchema(items)
	}

	if format, ok := schema["format"].(string); ok && supportedAnthropicStringFormat(format) {
		out["format"] = format
	}

	if desc := anthropicConstraintDescription(schema); desc != "" {
		if existing, ok := out["description"].(string); ok && existing != "" {
			out["description"] = existing + "\n" + desc
		} else {
			out["description"] = desc
		}
	}

	return out
}

func copyIfPresent(dst, src map[string]interface{}, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}

func sanitizeAnthropicSchemaDefinitions(value interface{}) (map[string]interface{}, bool) {
	in, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	out := make(map[string]interface{}, len(in))
	for name, definition := range in {
		out[name] = SanitizeAnthropicSchema(definition)
	}
	return out, true
}

func sanitizeAnthropicSchemaList(value interface{}) ([]interface{}, bool) {
	in, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]interface{}, len(in))
	for i, definition := range in {
		out[i] = SanitizeAnthropicSchema(definition)
	}
	return out, true
}

func supportedAnthropicStringFormat(format string) bool {
	switch format {
	case "date-time", "time", "date", "duration", "email", "hostname", "uri", "ipv4", "ipv6", "uuid":
		return true
	default:
		return false
	}
}

func anthropicConstraintDescription(schema map[string]interface{}) string {
	keys := []string{
		"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum",
		"multipleOf", "minLength", "maxLength", "pattern", "minItems",
		"maxItems", "uniqueItems", "minProperties", "maxProperties", "not",
	}
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		value, ok := schema[key]
		if !ok || value == nil {
			continue
		}
		if b, ok := value.(bool); ok && !b {
			continue
		}
		parts = append(parts, formatAnthropicConstraintName(key)+": "+formatAnthropicConstraintValue(value))
	}
	if format, ok := schema["format"].(string); ok && !supportedAnthropicStringFormat(format) {
		parts = append(parts, "format: "+format)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + "."
}

func formatAnthropicConstraintName(key string) string {
	var b strings.Builder
	for _, r := range key {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatAnthropicConstraintValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

// ToGoogleFormat converts tools to Google's function calling format
func ToGoogleFormat(tools []types.Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		parameters := tool.Parameters
		if parameters == nil {
			parameters = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		result[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  parameters,
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
