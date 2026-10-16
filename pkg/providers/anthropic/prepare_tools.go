package anthropic

import (
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
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

// ProviderToolBeta returns the anthropic-beta flag a provider-defined tool
// (e.g. "anthropic.computer_20250124") requires, if any. Hosts that forward
// Anthropic tools through another transport (Amazon Bedrock) use it to build
// the equivalent anthropic_beta list.
func ProviderToolBeta(toolName string) (string, bool) {
	b, ok := providerToolBetas[toolName]
	return b, ok
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
	// err is a validation failure of a provider tool's arguments (TS
	// AI_TypeValidationError from validateTypes).
	err        error
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
		// Self-serializing provider tools carry their own args (advisor model,
		// max_uses, ... ) and win over the plain builtin table entry.
		if mapper, ok := t.ProviderOptions.(anthropicAPIMapper); ok && t.ProviderOptions != nil {
			if _, builtin := anthropicBuiltinToolTypes[t.Name]; builtin {
				if v, ok := t.ProviderOptions.(interface{ Validate() error }); ok {
					if err := v.Validate(); err != nil {
						out.err = &providererrors.TypeValidationError{Field: "args", Message: err.Error(), Cause: err}
						return out
					}
				}
				wire = append(wire, mapper.ToAnthropicAPIMap())
				if b, ok := providerToolBetas[t.Name]; ok {
					addBeta(b)
				}
				continue
			}
		}
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
			if v, ok := t.ProviderOptions.(interface{ Validate() error }); ok {
				if err := v.Validate(); err != nil {
					out.err = &providererrors.TypeValidationError{Field: "args", Message: err.Error(), Cause: err}
					return out
				}
			}
			wire = append(wire, mapper.ToAnthropicAPIMap())
			if b, ok := providerToolBetas[t.Name]; ok {
				addBeta(b)
			}
			continue
		}
		if t.Type == "provider" || (strings.HasPrefix(t.Name, "anthropic.")) {
			// Anthropic function names cannot contain ".", so an
			// "anthropic.*" name is always a provider tool id; unknown ids
			// get exactly one warning, as in TS.
			id := t.ProviderID
			if id == "" {
				id = t.Name
			}
			out.warnings = append(out.warnings, types.Warning{Type: "unsupported", Feature: "provider-defined tool " + id})
			continue
		}

		// Function tool.
		// TS forwards the tool's inputSchema untouched (only the
		// responseFormat schema is sanitized), so do the same.
		m := map[string]interface{}{"name": t.Name, "input_schema": providerutils.ResponseFormatJSONSchemaOrdered(t.Parameters)}
		if t.Description != "" {
			m["description"] = t.Description
		}
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
		// strictSet distinguishes "unset" from "explicitly false" (tri-state
		// *bool). The unsupported-strict warning only fires for strict ===
		// true (TS #22300); strict: false is silently omitted.
		strictSet := t.Strict != nil
		if !o.supportsStrictTools && strictSet && *t.Strict {
			out.warnings = append(out.warnings, types.Warning{
				Type:    "unsupported",
				Feature: "strict",
				Details: fmt.Sprintf("Tool '%s' has strict: %t, but strict mode is not supported by this provider. The strict property will be ignored.", t.Name, *t.Strict),
			})
		}
		if o.supportsStrictTools && strictSet {
			m["strict"] = *t.Strict
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
				Details: "toolChoice 'required' is not supported with this model or thinking configuration. " +
					"Using 'auto' instead. Instruct the model to use a tool in the prompt and verify that a tool call was made.",
			})
			out.toolChoice = withDisable(map[string]interface{}{"type": "auto"})
		} else {
			out.toolChoice = withDisable(map[string]interface{}{"type": "any"})
		}
	case types.ToolChoiceNone:
		// Keep the tool definitions so server-tool history stays valid
		// (TS #22440).
		out.toolChoice = map[string]interface{}{"type": "none"}
	case types.ToolChoiceTool:
		if o.rejectsForcedToolUse {
			out.warnings = append(out.warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolChoice",
				Details: fmt.Sprintf("toolChoice 'tool' is not supported with this model or thinking configuration. "+
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
