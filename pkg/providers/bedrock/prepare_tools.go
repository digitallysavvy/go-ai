package bedrock

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// bedrockToolConfig mirrors AmazonBedrockToolConfiguration.
type bedrockToolConfig struct {
	Tools      []map[string]interface{}
	ToolChoice map[string]interface{}
}

// preparedTools is the result of prepareBedrockTools, mirroring the TS
// prepareTools() return shape.
type preparedTools struct {
	ToolConfig      bedrockToolConfig
	AdditionalTools map[string]interface{}
	Warnings        []types.Warning
}

// bedrockUnsupportedWebToolIDs lists Anthropic web-tool provider IDs that
// Amazon Bedrock does not support and must be filtered out with a warning.
// Ports TS amazon-bedrock-prepare-tools.ts.
var bedrockUnsupportedWebToolIDs = map[string]bool{
	"anthropic.web_search_20250305": true,
	"anthropic.web_search_20260318": true,
	"anthropic.web_fetch_20260318":  true,
}

// prepareBedrockTools ports TS amazon-bedrock-prepare-tools.ts#prepareTools.
func prepareBedrockTools(tools []types.Tool, toolChoice types.ToolChoice, hasToolChoice bool, modelID, modelFamily string, reasoningBudgetTokens *int, disableParallelToolUse *bool) preparedTools {
	result := preparedTools{}

	if len(tools) == 0 {
		return result
	}

	// Filter out Anthropic web tools that Bedrock does not support.
	supportedTools := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		if bedrockUnsupportedWebToolIDs[tool.Name] {
			featureName := strings.TrimPrefix(tool.Name, "anthropic.")
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: featureName + " tool",
				Details: fmt.Sprintf("The %s tool is not supported on Amazon Bedrock.", featureName),
			})
			continue
		}
		supportedTools = append(supportedTools, tool)
	}

	if len(supportedTools) == 0 {
		return result
	}

	isAnthropic := isAnthropicModelID(modelID, modelFamily, reasoningBudgetTokens)

	// Anthropic provider-defined tools (bash, computer, text_editor,
	// code_execution, memory, advisor, tool_search, web_search, web_fetch,
	// computer_toolset) are represented in the Go SDK by a namespaced Name
	// ("anthropic.<tool>_<version>") rather than Type ==
	// types.ToolTypeProviderDefined (that convention is used by other
	// providers, e.g. OpenAI/Google/Gateway — see
	// pkg/providers/anthropic/tool_converter.go's anthropicBuiltinToolTypes,
	// which also keys off Name). A tool with Type ==
	// types.ToolTypeProviderDefined is included here too for forward
	// compatibility with that convention, in case a caller constructs an
	// Anthropic tool that way.
	isAnthropicProviderTool := func(t types.Tool) bool {
		return strings.HasPrefix(t.Name, "anthropic.") || t.Type == types.ToolTypeProviderDefined
	}

	var providerTools []types.Tool
	var functionTools []types.Tool
	for _, t := range supportedTools {
		if isAnthropicProviderTool(t) {
			providerTools = append(providerTools, t)
		} else {
			functionTools = append(functionTools, t)
		}
	}

	usingAnthropicTools := isAnthropic && len(providerTools) > 0

	var bedrockTools []map[string]interface{}

	if usingAnthropicTools {
		// Bedrock only forwards a `tool_choice` for Anthropic provider-defined
		// tools; the tool spec itself is sent through the standard toolConfig
		// via bedrockAnthropicProviderTool.
		for _, tool := range providerTools {
			if spec := bedrockAnthropicProviderTool(tool); spec != nil {
				bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": spec})
			} else {
				result.Warnings = append(result.Warnings, types.Warning{
					Type:    "unsupported",
					Feature: "tool " + tool.Name,
				})
			}
		}
		if hasToolChoice {
			if choice := bedrockAnthropicToolChoice(toolChoice, disableParallelToolUse); choice != nil {
				result.AdditionalTools = map[string]interface{}{"tool_choice": choice}
			}
		}
	} else {
		for _, tool := range providerTools {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "tool " + tool.Name,
			})
		}
	}

	filteredFunctionTools := functionTools
	if hasToolChoice && toolChoice.Type == types.ToolChoiceTool {
		filtered := make([]types.Tool, 0, 1)
		for _, t := range functionTools {
			if t.Name == toolChoice.ToolName {
				filtered = append(filtered, t)
			}
		}
		filteredFunctionTools = filtered
	}

	supportsStrictOnTools := bedrockSupportsStrictTools(modelID)

	for _, tool := range filteredFunctionTools {
		supportsStrictForTool := supportsStrictOnTools && (!tool.Strict || isStrictToolSchemaCompatible(tool.Parameters))

		// NOTE: types.Tool.Strict is a plain bool (not a tri-state pointer), so
		// unlike the TS SDK we cannot distinguish "explicitly false" from
		// "unset". We warn for the "strict: true is ignored" cases, which
		// cover the practically meaningful scenarios.
		if tool.Strict && !supportsStrictOnTools {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: true, but strict mode is not supported by this model on Amazon Bedrock. The strict property will be ignored.", tool.Name),
			})
		} else if tool.Strict && !supportsStrictForTool {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: true, but Amazon Bedrock requires every object in a strict tool schema to set additionalProperties: false. The strict property will be ignored.", tool.Name),
			})
		}

		toolSpec := map[string]interface{}{"name": tool.Name}
		if tool.Description != "" {
			toolSpec["description"] = tool.Description
		}
		if tool.Strict && supportsStrictForTool {
			toolSpec["strict"] = tool.Strict
		}
		inputSchema := tool.Parameters
		if inputSchema == nil {
			inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		toolSpec["inputSchema"] = map[string]interface{}{"json": inputSchema}
		bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": toolSpec})
	}

	if isAnthropic && !usingAnthropicTools && disableParallelToolUse != nil && *disableParallelToolUse &&
		len(bedrockTools) > 0 && !(hasToolChoice && toolChoice.Type == types.ToolChoiceNone) {
		var choice map[string]interface{}
		switch {
		case hasToolChoice && toolChoice.Type == types.ToolChoiceRequired:
			choice = map[string]interface{}{"type": "any", "disable_parallel_tool_use": true}
		case hasToolChoice && toolChoice.Type == types.ToolChoiceTool:
			choice = map[string]interface{}{"type": "tool", "name": toolChoice.ToolName, "disable_parallel_tool_use": true}
		default:
			choice = map[string]interface{}{"type": "auto", "disable_parallel_tool_use": true}
		}
		result.AdditionalTools = map[string]interface{}{"tool_choice": choice}
	}

	var amazonToolChoice map[string]interface{}
	if !usingAnthropicTools && result.AdditionalTools == nil && len(bedrockTools) > 0 && hasToolChoice {
		switch toolChoice.Type {
		case types.ToolChoiceAuto:
			amazonToolChoice = map[string]interface{}{"auto": map[string]interface{}{}}
		case types.ToolChoiceRequired:
			amazonToolChoice = map[string]interface{}{"any": map[string]interface{}{}}
		case types.ToolChoiceNone:
			bedrockTools = nil
			amazonToolChoice = nil
		case types.ToolChoiceTool:
			amazonToolChoice = map[string]interface{}{"tool": map[string]interface{}{"name": toolChoice.ToolName}}
		}
	}

	if len(bedrockTools) > 0 {
		result.ToolConfig = bedrockToolConfig{Tools: bedrockTools, ToolChoice: amazonToolChoice}
	}

	return result
}

// anthropicAPIMapper is satisfied by ProviderOptions types that produce their
// own Anthropic API tool map (computer_*, computer_toolset_20260801,
// text_editor_20250728, web_search/web_fetch). Declared locally rather than
// imported because pkg/providers/anthropic's identically-shaped interface is
// unexported — Go interface satisfaction is structural, so a type assertion
// against this local declaration works the same way against ProviderOptions
// values built by pkg/providers/anthropic/tools.
type anthropicAPIMapper interface {
	ToAnthropicAPIMap() map[string]interface{}
}

// bedrockToolsetNames maps toolset tool Names to the short display name
// Bedrock's toolSpec needs. A toolset has no "name" of its own in its
// Anthropic API map (its member tool_use blocks come back keyed by
// toolset_name instead of name) but is still exposed to callers as one named
// tool call — mirrors prompt.AnthropicToolsetNames in
// pkg/providerutils/prompt/anthropic.go.
var bedrockToolsetNames = map[string]string{
	"anthropic.computer_toolset_20260801": "computer",
}

// bedrockAnthropicProviderTool maps a supported Anthropic provider-defined
// tool to its Bedrock toolSpec representation: {name, inputSchema}, matching
// the standard AmazonBedrockTool shape (Bedrock's toolSpec has no separate
// "type" field — unlike Anthropic's native Messages API tool wire format).
// Ports TS amazon-bedrock-prepare-tools.ts's generic anthropicTools
// factory-id lookup: for every ProviderTool (other than web_search/web_fetch,
// already filtered out as unsupported earlier), TS finds the matching
// anthropicTools factory by id and forwards {name: tool.name, inputSchema:
// factory.inputSchema} — i.e. it forwards ANY builtin Anthropic tool it can
// find a schema for, not just tool_search.
//
// Two families of Anthropic provider tools reach this function:
//
//   - Simple builtins (bash, text editors 20241022/20250124/20250429,
//     code_execution, memory, advisor, tool_search): the short API name comes
//     from anthropic.BuiltinToolAPIName, the same table pkg/providers/anthropic
//     itself uses for these. They need no per-instance config and their Go
//     constructors (pkg/providers/anthropic/tools) already populate
//     types.Tool.Parameters with a concrete JSON Schema, exactly like their TS
//     factory counterparts, so no separate schema table is needed here.
//
//   - Self-serializing tools (computer_20241022/20250124/20251124,
//     computer_toolset_20260801, text_editor_20250728): these implement
//     ToAnthropicAPIMap() on their ProviderOptions (like the direct Anthropic
//     provider's own prepare_tools.go does) to produce their Anthropic API
//     map, whose "name" field is the short wire name TS's factory lookup
//     would have found via tool.name. computer_toolset_20260801's map has no
//     "name" (it is a toolset, not a single named tool), so its name comes
//     from bedrockToolsetNames instead. TS uses a *default-args* factory
//     instance purely to read a static schema; in Go, t.Parameters was
//     already built by the caller's own constructor call with their actual
//     args, which is the concrete schema for the instance actually being
//     sent, so it is used directly instead of reconstructing a separate
//     "static" one.
//
// Returns nil for tools Bedrock does not recognize this way (the caller
// emits an "unsupported" warning).
func bedrockAnthropicProviderTool(t types.Tool) map[string]interface{} {
	inputSchema := t.Parameters
	if inputSchema == nil {
		inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	wireSchema := map[string]interface{}{"json": inputSchema}

	if shortName, ok := anthropic.BuiltinToolAPIName(t.Name); ok {
		return map[string]interface{}{"name": shortName, "inputSchema": wireSchema}
	}

	if mapper, ok := t.ProviderOptions.(anthropicAPIMapper); ok && t.ProviderOptions != nil {
		name, _ := mapper.ToAnthropicAPIMap()["name"].(string)
		if name == "" {
			name = bedrockToolsetNames[t.Name]
		}
		if name == "" {
			return nil
		}
		return map[string]interface{}{"name": name, "inputSchema": wireSchema}
	}

	return nil
}

// bedrockAnthropicToolChoice maps a ToolChoice to Bedrock's Anthropic
// `tool_choice` shape used in `additionalModelRequestFields.tool_choice` for
// Anthropic provider-defined tools. The base {type, name?} mapping is shared
// with the direct Anthropic provider via
// providerutils/tool.ConvertToolChoiceToAnthropic (also used by
// pkg/providers/anthropic/language_model.go); this only adds Bedrock's
// disable_parallel_tool_use augmentation and the "auto with no override
// needs no explicit tool_choice" special case.
func bedrockAnthropicToolChoice(toolChoice types.ToolChoice, disableParallelToolUse *bool) map[string]interface{} {
	disable := disableParallelToolUse != nil && *disableParallelToolUse

	if toolChoice.Type == types.ToolChoiceAuto || toolChoice.Type == "" {
		if !disable {
			return nil
		}
		return map[string]interface{}{"type": "auto", "disable_parallel_tool_use": true}
	}
	if toolChoice.Type == types.ToolChoiceNone {
		return nil
	}

	choice, _ := tool.ConvertToolChoiceToAnthropic(toolChoice).(map[string]interface{})
	if choice == nil {
		return nil
	}
	if disable {
		choice["disable_parallel_tool_use"] = true
	}
	return choice
}

// isStrictToolSchemaCompatible ports TS
// amazon-bedrock-prepare-tools.ts#isStrictToolSchemaCompatible: every object
// in the schema must set additionalProperties: false.
func isStrictToolSchemaCompatible(schema interface{}) bool {
	m, ok := schema.(map[string]interface{})
	if !ok {
		// Booleans (JSON Schema `true`/`false`) are compatible.
		return true
	}

	if typeIncludesObject(m["type"]) {
		if additional, ok := m["additionalProperties"].(bool); !ok || additional {
			return false
		}
	}

	for _, key := range []string{"properties", "patternProperties", "definitions", "$defs"} {
		if schemaMap, ok := m[key].(map[string]interface{}); ok {
			for _, def := range schemaMap {
				if !isStrictToolSchemaCompatible(def) {
					return false
				}
			}
		}
	}

	if dependencies, ok := m["dependencies"].(map[string]interface{}); ok {
		for _, dep := range dependencies {
			if _, isArray := dep.([]interface{}); isArray {
				continue
			}
			if !isStrictToolSchemaCompatible(dep) {
				return false
			}
		}
	}

	for _, key := range []string{"propertyNames", "contains", "not", "if", "then", "else"} {
		if nested, ok := m[key]; ok && nested != nil {
			if !isStrictToolSchemaCompatible(nested) {
				return false
			}
		}
	}

	if items, ok := m["items"]; ok && items != nil {
		if arr, isArray := items.([]interface{}); isArray {
			for _, item := range arr {
				if !isStrictToolSchemaCompatible(item) {
					return false
				}
			}
		} else if !isStrictToolSchemaCompatible(items) {
			return false
		}
	}

	for _, key := range []string{"anyOf", "allOf", "oneOf"} {
		if alternatives, ok := m[key].([]interface{}); ok {
			for _, alt := range alternatives {
				if !isStrictToolSchemaCompatible(alt) {
					return false
				}
			}
		}
	}

	return true
}

func typeIncludesObject(t interface{}) bool {
	switch v := t.(type) {
	case string:
		return v == "object"
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "object" {
				return true
			}
		}
	}
	return false
}
