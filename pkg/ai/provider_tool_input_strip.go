package ai

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// providerToolsStrippingInput lists the provider-defined tools whose TS input
// schema is a flat zod z.object (TS 3fab50aa9b, packages/anthropic/src/tool):
// zod's default `strip` drops undeclared keys when the call is parsed. Each
// Go Parameters schema was checked to declare exactly the TS keys, with no
// nested object schema, so stripping to the declared properties is TS's
// result (round 87e audit, coordinator ruling "Undeclared tool input keys").
// anthropic.advisor_20260301 is z.object({}).strict() in TS: an extra key is
// an invalid input, which its Go schema (additionalProperties false) already
// gives (r91 core1-01). Tools left out keep undeclared keys (S7): discriminated unions
// (anthropic.memory_20250818, anthropic.code_execution_20250825 /
// _20260120), nested object schemas, and schemas not audited key by key.
var providerToolsStrippingInput = map[string]bool{
	"anthropic.bash_20241022":              true,
	"anthropic.bash_20250124":              true,
	"anthropic.code_execution_20250522":    true,
	"anthropic.computer_20241022":          true,
	"anthropic.computer_20250124":          true,
	"anthropic.computer_20251124":          true,
	"anthropic.computer_toolset_20260801":  true,
	"anthropic.text_editor_20241022":       true,
	"anthropic.text_editor_20250124":       true,
	"anthropic.text_editor_20250429":       true,
	"anthropic.text_editor_20250728":       true,
	"anthropic.tool_search_bm25_20251119":  true,
	"anthropic.tool_search_regex_20251119": true,
	"anthropic.web_fetch_20250910":         true,
	"anthropic.web_fetch_20260209":         true,
	"anthropic.web_fetch_20260318":         true,
	"anthropic.web_search_20250305":        true,
	"anthropic.web_search_20260209":        true,
	"anthropic.web_search_20260318":        true,
}

// stripUndeclaredProviderToolInput returns value without the top-level keys
// the tool's Parameters do not declare, for the provider-defined tools in
// providerToolsStrippingInput (TS zod z.object strip). Other tools, values
// that are not objects, schemas without properties and tools with a custom
// ValidateInput are returned unchanged. value itself is never modified.
func stripUndeclaredProviderToolInput(tool *types.Tool, value interface{}) interface{} {
	if tool == nil || tool.Type != "provider" || tool.ValidateInput != nil || !providerToolsStrippingInput[tool.ProviderID] {
		return value
	}
	input, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	params, ok := schema.ParametersMap(tool.Parameters)
	if !ok {
		return value
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		return value
	}
	declared := make(map[string]bool, len(props))
	for k := range props {
		declared[k] = true
	}
	out := make(map[string]interface{}, len(input))
	for k, v := range input {
		if declared[k] {
			out[k] = v
		}
	}
	return out
}
