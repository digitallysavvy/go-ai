package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ToJSONSchema converts a Tool to JSON Schema format
// This is used when sending tool definitions to AI providers
func ToJSONSchema(tool types.Tool) map[string]interface{} {
	functionDef := map[string]interface{}{
		"name": tool.Name,
	}
	// TS sends `description: tool.description`, which JSON.stringify drops
	// when the tool has no description.
	if tool.Description != "" {
		functionDef["description"] = tool.Description
	}

	functionDef["parameters"] = defaultObjectSchema(tool.Parameters)

	// Pass strict mode whenever it was explicitly set, including `false`
	// (#12893). Matches the `tool.strict != null ? {strict: tool.strict} :
	// {}` pattern shared by TS's OpenAI-compatible-family prepareTools
	// (openai-chat, openai-compatible, groq, mistral, alibaba, ...): the
	// nil-vs-false distinction (tool.Strict *bool) must survive to the wire,
	// not just the true case.
	if tool.Strict != nil {
		functionDef["strict"] = *tool.Strict
	}

	schema := map[string]interface{}{
		"type":     "function",
		"function": functionDef,
	}

	return schema
}

func defaultObjectSchema(parameters interface{}) interface{} {
	// A schema.Schema marshals as {}; send its JSON Schema. An
	// *schema.OrderedSchema (and a struct schema, in field order) is kept so
	// its key order reaches the wire.
	if o, isOrdered := parameters.(*schema.OrderedSchema); isOrdered && o == nil {
		parameters = nil
	} else if ordered, ok := orderedStructSchema(parameters); ok {
		parameters = ordered
	} else if resolved, ok, none := resolveParameters(parameters); ok {
		parameters = resolved
	} else if none {
		parameters = nil
	}
	if parameters == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	if ordered, ok := parameters.(*schema.OrderedSchema); ok {
		// Keeps the source key order on the wire; a schema without a type
		// gets the default object type like a map does.
		if _, hasType := ordered.JSONSchema()["type"]; hasType {
			return ordered
		}
		parameters = ordered.JSONSchema()
	}
	if schema, ok := parameters.(map[string]interface{}); ok {
		if _, hasType := schema["type"]; !hasType {
			copied := make(map[string]interface{}, len(schema)+1)
			for key, value := range schema {
				copied[key] = value
			}
			copied["type"] = "object"
			return copied
		}
	}
	return parameters
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
//
// A schema.Schema (what ai.GenerateObject passes) is resolved to its JSON
// Schema first. Raw JSON (json.RawMessage or []byte) keeps its key order on the
// wire, like the JS object TS sanitizes, so `properties` stay in the order the
// caller wrote them.
func SanitizeAnthropicSchema(in interface{}) interface{} {
	switch v := in.(type) {
	case map[string]interface{}:
		return sanitizeAnthropicSchemaMap(v)
	case json.RawMessage:
		return sanitizeAnthropicRawSchema(v)
	case []byte:
		return sanitizeAnthropicRawSchema(v)
	case *schema.OrderedSchema:
		if v == nil {
			return nil
		}
		// Sanitize in source key order, like a raw JSON schema.
		raw, err := v.MarshalJSON()
		if err != nil {
			return sanitizeAnthropicSchemaMap(v.JSONSchema())
		}
		return sanitizeAnthropicRawSchema(raw)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = SanitizeAnthropicSchema(item)
		}
		return out
	default:
		if ordered, ok := orderedStructSchema(in); ok {
			// A struct schema: sanitized in its field order, as TS
			// sanitizes the zod JSON schema object in its key order.
			return SanitizeAnthropicSchema(ordered)
		}
		resolved, ok, none := resolveParameters(in)
		if ok {
			return sanitizeAnthropicSchemaMap(resolved)
		}
		if none {
			return nil
		}
		return in
	}
}

// orderedStructSchema is the JSON Schema of a struct schema
// (*schema.SimpleStructSchema, *schema.StructValidator) in its field order.
func orderedStructSchema(p interface{}) (*schema.OrderedSchema, bool) {
	switch p.(type) {
	case *schema.SimpleStructSchema, *schema.StructValidator:
		return schema.OrderedParameters(p)
	}
	return nil, false
}

func sanitizeAnthropicRawSchema(raw []byte) interface{} {
	hint, ok := jsonorder.Parse(raw)
	if !ok {
		return json.RawMessage(raw)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return json.RawMessage(raw)
	}
	return jsonorder.Hinted{V: SanitizeAnthropicSchema(withOrderedConstraintValues(decoded, hint)), Hint: anthropicSanitizedHint(decoded, hint)}
}

// anthropicOrderedConstraintValue is an object or array constraint value of a
// raw schema, already serialized in the input's key order (TS
// formatConstraintValue: JSON.stringify(value) of the JS object).
type anthropicOrderedConstraintValue struct{ text string }

// anthropicConstraintKeys are TS DESCRIPTION_CONSTRAINT_KEYS, in order.
var anthropicConstraintKeys = []string{
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum",
	"multipleOf", "minLength", "maxLength", "pattern", "minItems",
	"maxItems", "uniqueItems", "minProperties", "maxProperties", "not",
}

// withOrderedConstraintValues returns a copy of a decoded raw schema in which
// every object or array constraint value of a schema node the sanitizer
// visits is replaced by its text in the raw input's key order (hint), so the
// constraint description matches TS. Other values are left as they are.
func withOrderedConstraintValues(node, hint interface{}) interface{} {
	switch v := node.(type) {
	case []interface{}:
		hs, _ := hint.([]interface{})
		out := make([]interface{}, len(v))
		for i, item := range v {
			var ch interface{}
			if i < len(hs) {
				ch = hs[i]
			}
			out[i] = withOrderedConstraintValues(item, ch)
		}
		return out
	case map[string]interface{}:
		h, _ := hint.(*jsonorder.Object)
		if h == nil {
			return v
		}
		child := func(key string) interface{} {
			c, _ := h.Get(key)
			return c
		}
		out := make(map[string]interface{}, len(v))
		for key, value := range v {
			out[key] = value
		}
		for _, key := range anthropicConstraintKeys {
			switch v[key].(type) {
			case map[string]interface{}, []interface{}:
				if c, ok := h.Get(key); ok {
					out[key] = anthropicOrderedConstraintValue{text: jsonorder.Stringify(c)}
				}
			}
		}
		for _, key := range []string{"properties", "definitions", "$defs"} {
			entries, ok := v[key].(map[string]interface{})
			if !ok {
				continue
			}
			eh, _ := child(key).(*jsonorder.Object)
			copied := make(map[string]interface{}, len(entries))
			for name, entry := range entries {
				var ch interface{}
				if eh != nil {
					ch, _ = eh.Get(name)
				}
				copied[name] = withOrderedConstraintValues(entry, ch)
			}
			out[key] = copied
		}
		for _, key := range []string{"anyOf", "oneOf", "allOf", "items"} {
			if value, ok := v[key]; ok {
				out[key] = withOrderedConstraintValues(value, child(key))
			}
		}
		return out
	}
	return node
}

// anthropicSanitizedHint is the key order of SanitizeAnthropicSchema(in): the
// TS sanitizer's insertion order for each schema node (sanitize-json-schema.ts
// sanitizeSchema), with properties, definitions and $defs entries and
// verbatim values (default, const, enum, required) in the input's order
// (inHint, the ordered tree of the raw input).
func anthropicSanitizedHint(in, inHint interface{}) interface{} {
	switch v := in.(type) {
	case map[string]interface{}:
		h, _ := inHint.(*jsonorder.Object)
		return anthropicSanitizedMapHint(v, h)
	case []interface{}:
		hs, _ := inHint.([]interface{})
		out := make([]interface{}, len(v))
		for i, item := range v {
			var ch interface{}
			if i < len(hs) {
				ch = hs[i]
			}
			out[i] = anthropicSanitizedHint(item, ch)
		}
		return out
	}
	return inHint
}

func anthropicSanitizedMapHint(schema map[string]interface{}, h *jsonorder.Object) *jsonorder.Object {
	child := func(key string) interface{} {
		if h == nil {
			return nil
		}
		v, _ := h.Get(key)
		return v
	}
	out := jsonorder.New()
	setDefinitions := func() {
		for _, key := range []string{"definitions", "$defs"} {
			if defs, ok := schema[key].(map[string]interface{}); ok {
				out.Set(key, anthropicSanitizedEntriesHint(defs, child(key)))
			}
		}
	}
	if ref, ok := schema["$ref"]; ok && ref != nil {
		out.Set("$ref", nil)
		setDefinitions()
		return out
	}
	for _, key := range []string{"$schema", "$id", "title", "description", "default", "const", "enum", "type"} {
		value, ok := schema[key]
		if !ok || (value == nil && key != "default" && key != "const") {
			continue
		}
		out.Set(key, child(key))
	}
	if list, ok := schema["anyOf"].([]interface{}); ok {
		out.Set("anyOf", anthropicSanitizedHint(list, child("anyOf")))
	} else if list, ok := schema["oneOf"].([]interface{}); ok {
		out.Set("anyOf", anthropicSanitizedHint(list, child("oneOf")))
	}
	if list, ok := schema["allOf"].([]interface{}); ok {
		out.Set("allOf", anthropicSanitizedHint(list, child("allOf")))
	}
	setDefinitions()
	if schemaType, _ := schema["type"].(string); schemaType == "object" || schema["properties"] != nil {
		if properties, ok := schema["properties"].(map[string]interface{}); ok {
			out.Set("properties", anthropicSanitizedEntriesHint(properties, child("properties")))
		}
		out.Set("additionalProperties", nil)
		if schema["required"] != nil {
			out.Set("required", child("required"))
		}
	}
	if items, ok := schema["items"]; ok && items != nil {
		out.Set("items", anthropicSanitizedHint(items, child("items")))
	}
	if format, ok := schema["format"].(string); ok && supportedAnthropicStringFormat(format) {
		out.Set("format", nil)
	}
	if _, has := out.Get("description"); !has && anthropicConstraintDescription(schema) != "" {
		out.Set("description", nil)
	}
	return out
}

// anthropicSanitizedEntriesHint is the hint of a sanitized name -> schema map
// (properties, definitions, $defs): the input's entry order (TS
// Object.fromEntries(Object.entries(...))).
func anthropicSanitizedEntriesHint(entries map[string]interface{}, inHint interface{}) *jsonorder.Object {
	h, _ := inHint.(*jsonorder.Object)
	out := jsonorder.New()
	if h != nil {
		for _, name := range h.Keys() {
			if value, ok := entries[name]; ok {
				ch, _ := h.Get(name)
				out.Set(name, anthropicSanitizedHint(value, ch))
			}
		}
	}
	return out
}

func sanitizeAnthropicSchemaMap(schema map[string]interface{}) map[string]interface{} {
	if ref, ok := schema["$ref"]; ok && ref != nil {
		// TS f4c5f1e959: keep the definitions next to a $ref so a top-level
		// reference still resolves (sanitize-json-schema.ts 57-60).
		out := map[string]interface{}{"$ref": ref}
		sanitizeAnthropicSchemaDefinitionsInto(out, schema)
		return out
	}

	out := map[string]interface{}{}
	copyIfNotNil(out, schema, "$schema")
	copyIfNotNil(out, schema, "$id")
	copyIfNotNil(out, schema, "title")
	copyIfNotNil(out, schema, "description")
	copyIfPresent(out, schema, "default")
	copyIfPresent(out, schema, "const")
	copyIfNotNil(out, schema, "enum")
	copyIfNotNil(out, schema, "type")

	if anyOf, ok := sanitizeAnthropicSchemaList(schema["anyOf"]); ok {
		out["anyOf"] = anyOf
	} else if oneOf, ok := sanitizeAnthropicSchemaList(schema["oneOf"]); ok {
		out["anyOf"] = oneOf
	}
	if allOf, ok := sanitizeAnthropicSchemaList(schema["allOf"]); ok {
		out["allOf"] = allOf
	}
	sanitizeAnthropicSchemaDefinitionsInto(out, schema)

	if schemaType, _ := schema["type"].(string); schemaType == "object" || schema["properties"] != nil {
		if properties, ok := sanitizeAnthropicSchemaDefinitions(schema["properties"]); ok {
			out["properties"] = properties
		}
		out["additionalProperties"] = false
		copyIfNotNil(out, schema, "required")
	}

	if items, ok := schema["items"]; ok && items != nil {
		// TS schema.items != null: a JSON null items is dropped.
		out["items"] = SanitizeAnthropicSchema(items)
	}

	if format, ok := schema["format"].(string); ok && supportedAnthropicStringFormat(format) {
		out["format"] = format
	}

	if desc := anthropicConstraintDescription(schema); desc != "" {
		// TS: result.description == null ? desc : `${description}\n${desc}`,
		// so an empty description still gets the newline.
		if existing, ok := out["description"].(string); ok {
			out["description"] = existing + "\n" + desc
		} else {
			out["description"] = desc
		}
	}

	return out
}

// sanitizeAnthropicSchemaDefinitionsInto sets the sanitized definitions and
// $defs of schema on out (TS sanitizeSchemaDefinitions).
func sanitizeAnthropicSchemaDefinitionsInto(out, schema map[string]interface{}) {
	if definitions, ok := sanitizeAnthropicSchemaDefinitions(schema["definitions"]); ok {
		out["definitions"] = definitions
	}
	if defs, ok := sanitizeAnthropicSchemaDefinitions(schema["$defs"]); ok {
		out["$defs"] = defs
	}
}

// copyIfNotNil mirrors TS `schema.key != null`: a JSON null is skipped.
func copyIfNotNil(dst, src map[string]interface{}, key string) {
	if value, ok := src[key]; ok && value != nil {
		dst[key] = value
	}
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
	parts := make([]string, 0, len(anthropicConstraintKeys)+1)
	for _, key := range anthropicConstraintKeys {
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
	switch v := value.(type) {
	case string:
		return v
	case anthropicOrderedConstraintValue:
		return v.text
	}
	data, err := jsonstr.Marshal(value)
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

// DropProviderDefinedTools removes provider-defined tools (Type "provider")
// from tools for chat APIs that only take function tools and returns an
// `unsupported` warning per dropped tool. TS prepareTools does this with
// feature `provider-defined tool <tool.id>` (the OpenAI chat model uses
// `tool type: provider`); label overrides the default feature text.
func DropProviderDefinedTools(tools []types.Tool, label func(types.Tool) string) ([]types.Tool, []types.Warning) {
	var kept []types.Tool
	var warnings []types.Warning
	for _, t := range tools {
		if t.Type != "provider" {
			kept = append(kept, t)
			continue
		}
		feature := ""
		if label != nil {
			feature = label(t)
		} else {
			id := t.ProviderID
			if id == "" {
				id = t.Name
			}
			feature = "provider-defined tool " + id
		}
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature})
	}
	return kept, warnings
}
