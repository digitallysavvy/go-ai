package bedrock

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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
	// Betas are the anthropic-beta flags required by Anthropic provider-defined
	// tools (TS prepareTools().betas); the caller merges them into
	// additionalModelRequestFields.anthropic_beta.
	Betas    []string
	Warnings []types.Warning
	// ToolNames maps the toolSpec name of each Anthropic provider tool sent
	// under its short wire name ("bash") back to the tool's Name when they
	// differ (the Go constructors name tools after their id,
	// "anthropic.bash_20250124"), so the model's calls and the prompt history
	// use the names the caller knows the tools by.
	ToolNames map[string]string
	// Err is an invalid provider tool argument (TS prepareAnthropicTools
	// validateTypes, a *TypeValidationError).
	Err error
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
// rejectsForcedToolUse mirrors the model-capability flag (e.g. Claude Opus
// 5.5 on Bedrock, per anthropic.GetModelCapabilities): when true and the
// model is Anthropic, forced tool_choice ('required' or a named tool) falls
// back to 'auto' with an "unsupported" warning instead of being sent as-is.
func prepareBedrockTools(tools []types.Tool, toolChoice types.ToolChoice, hasToolChoice bool, modelID, modelFamily string, reasoningBudgetTokens *int, disableParallelToolUse *bool, rejectsForcedToolUse bool) preparedTools {
	result := preparedTools{}

	if len(tools) == 0 {
		return result
	}

	// Filter out Anthropic web tools that Bedrock does not support.
	supportedTools := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		if id := anthropic.ProviderToolID(tool); bedrockUnsupportedWebToolIDs[id] {
			featureName := strings.TrimPrefix(id, "anthropic.")
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

	// Provider tools (TS tool.type === "provider"): Type ==
	// types.ToolTypeProviderDefined, which every constructor in
	// pkg/providers/anthropic/tools sets, or a hand-built tool named after an
	// Anthropic tool id ("anthropic.<tool>_<version>"). One Bedrock cannot
	// send (another provider's tool, or one used with a non-Anthropic model)
	// gets an "unsupported" warning, as in TS.
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

	// rejectsForcedToolChoice mirrors TS: only Anthropic models with the
	// capability flag reject forced tool_choice, and only when the caller
	// actually requested a forced choice ('required' or a named tool).
	rejectsForcedToolChoice := isAnthropic && rejectsForcedToolUse && hasToolChoice &&
		(toolChoice.Type == types.ToolChoiceRequired || toolChoice.Type == types.ToolChoiceTool)

	// providerToolsForRequest mirrors TS: when a forced named-tool choice is
	// rejected, only the requested provider tool is still sent to the model
	// (with 'auto' tool choice). TS matches tool.name; a tool keeping the Go
	// constructor's id as its Name is sent as its wire name ("bash"), so that
	// name matches too.
	providerToolsForRequest := providerTools
	if rejectsForcedToolChoice && toolChoice.Type == types.ToolChoiceTool {
		var filtered []types.Tool
		for _, t := range providerTools {
			if t.Name == toolChoice.ToolName || bedrockAnthropicProviderToolWireName(t) == toolChoice.ToolName {
				filtered = append(filtered, t)
			}
		}
		providerToolsForRequest = filtered
	}

	usingAnthropicTools := isAnthropic && len(providerToolsForRequest) > 0

	var bedrockTools []map[string]interface{}

	if usingAnthropicTools {
		// TS runs the Anthropic prepareTools over every provider tool (not
		// only providerToolsForRequest): its warnings (unknown provider tools,
		// a rejected forced tool choice), betas and tool_choice apply, and it
		// validates the tool args. Only its tool_choice is sent, as
		// additionalModelRequestFields.tool_choice; the tool specs go through
		// the standard toolConfig.
		var choice *types.ToolChoice
		if hasToolChoice {
			choice = &toolChoice
		}
		anthropicPrepared, err := anthropic.PrepareProviderTools(providerTools, choice, disableParallelToolUse, rejectsForcedToolUse)
		if err != nil {
			result.Err = err
			return result
		}
		result.Warnings = append(result.Warnings, anthropicPrepared.Warnings...)
		for _, b := range anthropicPrepared.Betas {
			result.Betas = appendUnique(result.Betas, b)
		}
		if anthropicPrepared.ToolChoice != nil {
			result.AdditionalTools = map[string]interface{}{"tool_choice": anthropicPrepared.ToolChoice}
		}
		for _, tool := range providerToolsForRequest {
			if spec := bedrockAnthropicProviderTool(tool); spec != nil {
				bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": spec})
				if name, _ := spec["name"].(string); name != tool.Name {
					if result.ToolNames == nil {
						result.ToolNames = map[string]string{}
					}
					result.ToolNames[name] = tool.Name
				}
			} else {
				result.Warnings = append(result.Warnings, types.Warning{
					Type:    "unsupported",
					Feature: "tool " + anthropic.ProviderToolID(tool),
				})
			}
		}
	} else {
		for _, tool := range providerToolsForRequest {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "tool " + anthropic.ProviderToolID(tool),
			})
		}
	}

	// preparedToolChoice mirrors TS: when a forced choice was rejected and no
	// Anthropic provider tools absorbed the fallback (i.e. we're about to
	// build the standard Bedrock toolConfig/tool_choice below), fall back to
	// 'auto' with a warning instead of sending the forced choice.
	preparedToolChoice := toolChoice
	hasPreparedToolChoice := hasToolChoice
	if !usingAnthropicTools && rejectsForcedToolChoice {
		if toolChoice.Type == types.ToolChoiceTool {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolChoice",
				Details: fmt.Sprintf("toolChoice 'tool' is not supported by this model because it rejects forced tool use. Only the '%s' tool is sent with 'auto' tool choice. Instruct the model to use the tool in the prompt and verify that a tool call was made.", toolChoice.ToolName),
			})
		} else {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolChoice",
				Details: "toolChoice 'required' is not supported by this model because it rejects forced tool use. Using 'auto' instead. Instruct the model to use a tool in the prompt and verify that a tool call was made.",
			})
		}
		preparedToolChoice = types.ToolChoice{Type: types.ToolChoiceAuto}
		hasPreparedToolChoice = true
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
		// types.Tool.Strict is now a tri-state *bool (hand-off: "Tool.Strict
		// bool -> *bool"), so this mirrors TS amazon-bedrock-prepare-tools.ts
		// exactly: strictSet distinguishes "unset" from "explicitly false"
		// (tool.strict != null), and strictTrue is the `=== true` check that
		// gates the unsupported-strict warning (TS #22300).
		strictSet := tool.Strict != nil
		strictTrue := strictSet && *tool.Strict
		supportsStrictForTool := supportsStrictOnTools && (!strictTrue || isStrictToolSchemaCompatible(providerutils.ResponseFormatJSONSchema(tool.Parameters)))

		if !supportsStrictOnTools && strictTrue {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: %t, but strict mode is not supported by this model on Amazon Bedrock. The strict property will be ignored.", tool.Name, *tool.Strict),
			})
		} else if strictTrue && !supportsStrictForTool {
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: true, but Amazon Bedrock requires every object in a strict tool schema to set additionalProperties: false. The strict property will be ignored.", tool.Name),
			})
		}

		toolSpec := map[string]interface{}{"name": tool.Name}
		// TS: description is omitted when it trims to the empty string.
		if providerutils.JSTrim(tool.Description) != "" {
			toolSpec["description"] = tool.Description
		}
		if strictSet && supportsStrictForTool {
			toolSpec["strict"] = *tool.Strict
		}
		inputSchema := providerutils.ResponseFormatJSONSchemaOrdered(tool.Parameters)
		if inputSchema == nil {
			inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		toolSpec["inputSchema"] = map[string]interface{}{"json": inputSchema}
		bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": toolSpec})
	}

	if isAnthropic && !usingAnthropicTools && disableParallelToolUse != nil && *disableParallelToolUse &&
		len(bedrockTools) > 0 && (!hasPreparedToolChoice || preparedToolChoice.Type != types.ToolChoiceNone) {
		var choice map[string]interface{}
		switch {
		case hasPreparedToolChoice && preparedToolChoice.Type == types.ToolChoiceRequired:
			choice = map[string]interface{}{"type": "any", "disable_parallel_tool_use": true}
		case hasPreparedToolChoice && preparedToolChoice.Type == types.ToolChoiceTool:
			choice = map[string]interface{}{"type": "tool", "name": preparedToolChoice.ToolName, "disable_parallel_tool_use": true}
		default:
			choice = map[string]interface{}{"type": "auto", "disable_parallel_tool_use": true}
		}
		result.AdditionalTools = map[string]interface{}{"tool_choice": choice}
	}

	var amazonToolChoice map[string]interface{}
	if !usingAnthropicTools && result.AdditionalTools == nil && len(bedrockTools) > 0 && hasPreparedToolChoice {
		switch preparedToolChoice.Type {
		case types.ToolChoiceAuto:
			amazonToolChoice = map[string]interface{}{"auto": map[string]interface{}{}}
		case types.ToolChoiceRequired:
			amazonToolChoice = map[string]interface{}{"any": map[string]interface{}{}}
		case types.ToolChoiceNone:
			bedrockTools = nil
			amazonToolChoice = nil
		case types.ToolChoiceTool:
			amazonToolChoice = map[string]interface{}{"tool": map[string]interface{}{"name": preparedToolChoice.ToolName}}
		}
	}

	if len(bedrockTools) > 0 {
		result.ToolConfig = bedrockToolConfig{Tools: bedrockTools, ToolChoice: amazonToolChoice}
	}

	return result
}

// bedrockAnthropicProviderTool maps an Anthropic provider tool to its
// Bedrock toolSpec, {name, inputSchema}, as TS amazon-bedrock-prepare-tools.ts
// does for every provider tool whose id matches an anthropicTools factory:
// the inputSchema is that factory's zod-derived schema (never the schema of
// the caller's tool instance) and the name is the tool's name. A tool that
// keeps the Go constructor's id as its Name ("anthropic.bash_20250124", not a
// valid Bedrock tool name) is sent under the Anthropic wire name ("bash");
// prepareBedrockTools maps that name back. Returns nil for a tool id without
// an Anthropic factory (the caller emits an "unsupported" warning).
func bedrockAnthropicProviderTool(t types.Tool) map[string]interface{} {
	id := anthropic.ProviderToolID(t)
	wireName := prompt.AnthropicProviderToolName(id)
	if wireName == id {
		return nil
	}
	var inputSchema interface{}
	if raw, ok := anthropicProviderToolSchema(id); ok {
		inputSchema = raw
	} else if fallback := providerutils.ResponseFormatJSONSchema(t.Parameters); fallback != nil {
		inputSchema = fallback
	} else {
		inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	name := t.Name
	if name == id {
		name = wireName
	}
	return map[string]interface{}{"name": name, "inputSchema": map[string]interface{}{"json": inputSchema}}
}

// bedrockAnthropicProviderToolWireName returns the toolSpec name
// bedrockAnthropicProviderTool would send for t, or "" if t is not a known
// Anthropic provider tool. A forced types.ToolChoiceTool.ToolName is matched
// against it (TS filters providerTools by tool.name).
func bedrockAnthropicProviderToolWireName(t types.Tool) string {
	spec := bedrockAnthropicProviderTool(t)
	if spec == nil {
		return ""
	}
	name, _ := spec["name"].(string)
	return name
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

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
