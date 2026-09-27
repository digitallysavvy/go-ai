package anthropic

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// providerToolBetas maps Anthropic provider tool IDs to the beta flag the TS
// prepareTools adds for them (anthropic-prepare-tools.ts).
var providerToolBetas = map[string]string{
	"anthropic.code_execution_20250522": BetaHeaderCodeExecution20250522,
	"anthropic.code_execution_20250825": BetaHeaderCodeExecution20250825,
	"anthropic.computer_20241022":       BetaHeaderComputerUse20241022,
	"anthropic.computer_20250124":       BetaHeaderComputerUse20250124,
	"anthropic.computer_20251124":       BetaHeaderComputerUse20251124,
	"anthropic.text_editor_20241022":    BetaHeaderComputerUse20241022,
	"anthropic.text_editor_20250124":    BetaHeaderComputerUse20250124,
	"anthropic.text_editor_20250429":    BetaHeaderComputerUse20250124,
	"anthropic.bash_20241022":           BetaHeaderComputerUse20241022,
	"anthropic.bash_20250124":           BetaHeaderComputerUse20250124,
	"anthropic.memory_20250818":         BetaHeaderContextManagement,
	"anthropic.web_fetch_20250910":      BetaHeaderWebFetch20250910,
	"anthropic.web_fetch_20260209":      BetaHeaderWebTools20260209,
	"anthropic.web_search_20260209":     BetaHeaderWebTools20260209,
	"anthropic.advisor_20260301":        BetaHeaderAdvisorTool,
}

// BetaHeaderStructuredOutputs is added for function tools when the model
// supports native structured output (strict tools).
const BetaHeaderStructuredOutputs = "structured-outputs-2025-11-13"

type prepareToolsOptions struct {
	tools                      []types.Tool
	toolChoice                 *types.ToolChoice
	disableParallelToolUse     *bool
	validator                  *prompt.AnthropicCacheControlValidator
	supportsStructuredOutput   bool
	supportsStrictTools        bool
	defaultEagerInputStreaming bool
	rejectsForcedToolUse       bool
}

type preparedTools struct {
	tools      []map[string]interface{}
	toolChoice map[string]interface{}
	warnings   []types.Warning
	betas      []string
}

// toolCacheControlOptions returns the tool's Anthropic provider options in
// the prompt provider-options map shape so the shared cache-control validator
// can read cacheControl from it.
func toolCacheControlOptions(t types.Tool) map[string]interface{} {
	switch o := t.ProviderOptions.(type) {
	case *ToolOptions:
		if o != nil && o.CacheControl != nil {
			return map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": o.CacheControl}}
		}
	case map[string]interface{}:
		return o
	}
	return nil
}

func toolOptionsOf(t types.Tool) *ToolOptions {
	switch o := t.ProviderOptions.(type) {
	case *ToolOptions:
		return o
	case map[string]interface{}:
		anth, _ := o["anthropic"].(map[string]interface{})
		if anth == nil {
			return nil
		}
		out := &ToolOptions{}
		if v, ok := anth["eagerInputStreaming"].(bool); ok {
			out.EagerInputStreaming = &v
		}
		if v, ok := anth["deferLoading"].(bool); ok {
			out.DeferLoading = &v
		}
		if v, ok := anth["allowedCallers"].([]interface{}); ok {
			for _, c := range v {
				if s, ok := c.(string); ok {
					out.AllowedCallers = append(out.AllowedCallers, s)
				}
			}
		} else if v, ok := anth["allowedCallers"].([]string); ok {
			out.AllowedCallers = v
		}
		return out
	}
	return nil
}

// prepareTools mirrors prepareTools in anthropic-prepare-tools.ts.
func prepareTools(o prepareToolsOptions) preparedTools {
	var out preparedTools
	betaSet := map[string]bool{}
	addBeta := func(b string) {
		if !betaSet[b] {
			betaSet[b] = true
			out.betas = append(out.betas, b)
		}
	}
	if len(o.tools) == 0 {
		return out
	}
	validator := o.validator
	if validator == nil {
		validator = prompt.NewAnthropicCacheControlValidator()
	}

	wire := make([]map[string]interface{}, 0, len(o.tools))
	for _, t := range o.tools {
		// Simple builtin provider tools.
		if def, ok := anthropicBuiltinToolTypes[t.Name]; ok {
			m := map[string]interface{}{"type": def.apiType}
			if def.name != "" {
				m["name"] = def.name
			}
			if cc := validator.GetCacheControl(toolCacheControlOptions(t), "tool definition", true); cc != nil {
				m["cache_control"] = cc
			}
			if b, ok := providerToolBetas[t.Name]; ok {
				addBeta(b)
			}
			wire = append(wire, m)
			continue
		}
		// Self-serializing provider tools.
		if mapper, ok := t.ProviderOptions.(anthropicAPIMapper); ok && t.ProviderOptions != nil {
			wire = append(wire, mapper.ToAnthropicAPIMap())
			if b, ok := providerToolBetas[t.Name]; ok {
				addBeta(b)
			}
			continue
		}
		if t.Type == "provider" || (strings.HasPrefix(t.Name, "anthropic.") && t.ProviderExecuted) {
			id := t.ProviderID
			if id == "" {
				id = t.Name
			}
			out.warnings = append(out.warnings, types.Warning{Type: "unsupported", Feature: "provider-defined tool " + id})
			continue
		}

		// Function tool.
		m := tool.ToAnthropicFormat([]types.Tool{t})[0]
		if cc := validator.GetCacheControl(toolCacheControlOptions(t), "tool definition", true); cc != nil {
			m["cache_control"] = cc
		}
		toolOpts := toolOptionsOf(t)
		eager := o.defaultEagerInputStreaming
		if toolOpts != nil && toolOpts.EagerInputStreaming != nil {
			eager = *toolOpts.EagerInputStreaming
		}
		if eager {
			m["eager_input_streaming"] = true
		}
		strictSet := t.Strict != nil && *t.Strict
		if !o.supportsStrictTools && strictSet {
			out.warnings = append(out.warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: %t, but strict mode is not supported by this provider. The strict property will be ignored.", t.Name, strictSet),
			})
		}
		if o.supportsStrictTools && strictSet {
			m["strict"] = true
		}
		var allowedCallers []string
		if toolOpts != nil {
			if toolOpts.DeferLoading != nil {
				m["defer_loading"] = *toolOpts.DeferLoading
			}
			allowedCallers = toolOpts.AllowedCallers
			if len(allowedCallers) > 0 {
				m["allowed_callers"] = allowedCallers
			}
		}
		if len(t.InputExamples) > 0 {
			examples := make([]interface{}, len(t.InputExamples))
			for j, ex := range t.InputExamples {
				examples[j] = ex.Input
			}
			m["input_examples"] = examples
		}
		wire = append(wire, m)
		if o.supportsStructuredOutput {
			addBeta(BetaHeaderStructuredOutputs)
		}
		if len(t.InputExamples) > 0 || len(allowedCallers) > 0 {
			addBeta(BetaHeaderAdvancedToolUse)
		}
	}
	out.tools = wire

	disable := o.disableParallelToolUse
	withDisable := func(m map[string]interface{}) map[string]interface{} {
		if disable != nil {
			m["disable_parallel_tool_use"] = *disable
		}
		return m
	}

	if o.toolChoice == nil || o.toolChoice.Type == "" {
		if disable != nil && *disable {
			out.toolChoice = map[string]interface{}{"type": "auto", "disable_parallel_tool_use": true}
		}
		return out
	}

	switch o.toolChoice.Type {
	case types.ToolChoiceAuto:
		out.toolChoice = withDisable(map[string]interface{}{"type": "auto"})
	case types.ToolChoiceRequired:
		if o.rejectsForcedToolUse {
			out.warnings = append(out.warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolChoice",
				Details: "toolChoice 'required' is not supported by this model because it rejects forced tool use. " +
					"Using 'auto' instead. Instruct the model to use a tool in the prompt and verify that a tool call was made.",
			})
			out.toolChoice = withDisable(map[string]interface{}{"type": "auto"})
		} else {
			out.toolChoice = withDisable(map[string]interface{}{"type": "any"})
		}
	case types.ToolChoiceNone:
		// Anthropic does not support 'none' tool choice, so the tools are removed.
		out.tools = nil
		out.toolChoice = nil
	case types.ToolChoiceTool:
		if o.rejectsForcedToolUse {
			out.warnings = append(out.warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolChoice",
				Details: fmt.Sprintf("toolChoice 'tool' is not supported by this model because it rejects forced tool use. "+
					"Only the '%s' tool is sent with 'auto' tool choice. "+
					"Instruct the model to use the tool in the prompt and verify that a tool call was made.", o.toolChoice.ToolName),
			})
			var filtered []map[string]interface{}
			for _, w := range out.tools {
				if w["name"] == o.toolChoice.ToolName {
					filtered = append(filtered, w)
				}
			}
			out.tools = filtered
			out.toolChoice = withDisable(map[string]interface{}{"type": "auto"})
		} else {
			out.toolChoice = withDisable(map[string]interface{}{"type": "tool", "name": o.toolChoice.ToolName})
		}
	default:
		out.toolChoice = withDisable(map[string]interface{}{"type": "auto"})
	}
	return out
}
