package anthropic

import (
	"regexp"
	"strings"
)

// ModelCapabilities describes the Claude model capabilities that drive request
// defaults and feature selection. Mirrors the return value of
// getModelCapabilities in the TypeScript SDK (anthropic-language-model.ts).
type ModelCapabilities struct {
	// MaxOutputTokens is the model's maximum output token count. It is the
	// default max_tokens when the caller does not set one.
	MaxOutputTokens int

	// SupportsStructuredOutput reports native structured output
	// (output_config.format) and strict tool support.
	SupportsStructuredOutput bool

	// SupportsAdaptiveThinking reports whether reasoning maps to adaptive
	// thinking with an effort level instead of a token budget.
	SupportsAdaptiveThinking bool

	// RejectsSamplingParameters reports whether temperature, topK and topP are
	// rejected by the model.
	RejectsSamplingParameters bool

	// SupportsXHighEffort reports whether the "xhigh" effort level exists.
	SupportsXHighEffort bool

	// RejectsThinkingDisabledAboveHighEffort reports whether thinking can only
	// be disabled at effort levels up to "high".
	RejectsThinkingDisabledAboveHighEffort bool

	// RejectsThinkingDisabled reports that thinking is always adaptive:
	// thinking.type "disabled" and "enabled" are rejected.
	RejectsThinkingDisabled bool

	// RejectsForcedToolUse reports that tool_choice "any" or a named tool is
	// rejected.
	RejectsForcedToolUse bool

	// IsKnownModel is false for unknown and legacy model IDs.
	IsKnownModel bool
}

var (
	claudeSonnet4Pattern = regexp.MustCompile(`claude-sonnet-4(?:-|@)`)
	claudeOpus4Pattern   = regexp.MustCompile(`claude-opus-4(?:-|@)`)
	// claude-(?:instant(?:-|$)|v?2(?=$|[-.:])|3(?=$|[-.])) — Go regexp has no
	// lookahead, so the lookaheads are expanded into explicit alternatives.
	claudeLegacyPattern = regexp.MustCompile(`claude-(?:instant(?:-|$)|v?2(?:$|[-.:])|3(?:$|[-.]))`)
)

// GetModelCapabilities returns the capabilities of a Claude model. Mirrors
// getModelCapabilities in the TypeScript SDK.
func GetModelCapabilities(modelID string) ModelCapabilities {
	switch {
	case strings.Contains(modelID, "claude-opus-5-5"):
		return ModelCapabilities{
			MaxOutputTokens:                        128000,
			SupportsStructuredOutput:               true,
			SupportsAdaptiveThinking:               true,
			RejectsSamplingParameters:              true,
			SupportsXHighEffort:                    true,
			RejectsThinkingDisabledAboveHighEffort: true,
			RejectsThinkingDisabled:                true,
			RejectsForcedToolUse:                   true,
			IsKnownModel:                           true,
		}
	case strings.Contains(modelID, "claude-opus-5"):
		return ModelCapabilities{
			MaxOutputTokens:                        128000,
			SupportsStructuredOutput:               true,
			SupportsAdaptiveThinking:               true,
			RejectsSamplingParameters:              true,
			SupportsXHighEffort:                    true,
			RejectsThinkingDisabledAboveHighEffort: true,
			IsKnownModel:                           true,
		}
	case strings.Contains(modelID, "claude-fable-5-1"):
		return ModelCapabilities{
			MaxOutputTokens:           128000,
			SupportsStructuredOutput:  true,
			SupportsAdaptiveThinking:  true,
			RejectsSamplingParameters: true,
			SupportsXHighEffort:       true,
			RejectsThinkingDisabled:   true,
			RejectsForcedToolUse:      true,
			IsKnownModel:              true,
		}
	case strings.Contains(modelID, "claude-fable-5"):
		return ModelCapabilities{
			MaxOutputTokens:           128000,
			SupportsStructuredOutput:  true,
			SupportsAdaptiveThinking:  true,
			RejectsSamplingParameters: true,
			SupportsXHighEffort:       true,
			RejectsThinkingDisabled:   true,
			IsKnownModel:              true,
		}
	case strings.Contains(modelID, "claude-opus-4-8"),
		strings.Contains(modelID, "claude-opus-4-7"),
		strings.Contains(modelID, "claude-sonnet-5"):
		return ModelCapabilities{
			MaxOutputTokens:           128000,
			SupportsStructuredOutput:  true,
			SupportsAdaptiveThinking:  true,
			RejectsSamplingParameters: true,
			SupportsXHighEffort:       true,
			IsKnownModel:              true,
		}
	case strings.Contains(modelID, "claude-sonnet-4-6"),
		strings.Contains(modelID, "claude-opus-4-6"):
		return ModelCapabilities{
			MaxOutputTokens:          128000,
			SupportsStructuredOutput: true,
			SupportsAdaptiveThinking: true,
			IsKnownModel:             true,
		}
	case strings.Contains(modelID, "claude-sonnet-4-5"),
		strings.Contains(modelID, "claude-opus-4-5"),
		strings.Contains(modelID, "claude-haiku-4-5"):
		return ModelCapabilities{
			MaxOutputTokens:          64000,
			SupportsStructuredOutput: true,
			IsKnownModel:             true,
		}
	case strings.Contains(modelID, "claude-opus-4-1"):
		return ModelCapabilities{
			MaxOutputTokens:          32000,
			SupportsStructuredOutput: true,
			IsKnownModel:             true,
		}
	case claudeSonnet4Pattern.MatchString(modelID):
		return ModelCapabilities{MaxOutputTokens: 64000, IsKnownModel: true}
	case claudeOpus4Pattern.MatchString(modelID):
		return ModelCapabilities{MaxOutputTokens: 32000, IsKnownModel: true}
	case strings.Contains(modelID, "claude-3-haiku"):
		return ModelCapabilities{MaxOutputTokens: 4096, IsKnownModel: true}
	case claudeLegacyPattern.MatchString(modelID):
		return ModelCapabilities{MaxOutputTokens: 4096}
	case strings.Contains(modelID, "claude-"):
		// Known and legacy Claude families are handled above, so any remaining
		// Claude ID is assumed to be newer than the known list. It stays
		// unknown so callers still receive the maxOutputTokens warning.
		return ModelCapabilities{
			MaxOutputTokens:                        128000,
			SupportsStructuredOutput:               true,
			SupportsAdaptiveThinking:               true,
			RejectsSamplingParameters:              true,
			SupportsXHighEffort:                    true,
			RejectsThinkingDisabledAboveHighEffort: true,
		}
	default:
		// Non-Claude models (e.g. served through Anthropic-compatible APIs)
		// keep conservative defaults.
		return ModelCapabilities{MaxOutputTokens: 4096}
	}
}

// HasDynamicFilteringWebToolWithoutCodeExecution reports whether the tools
// include a dynamic-filtering web tool (web_fetch/web_search 20260209 or
// 20260318) but no code execution tool. The API then runs code execution
// implicitly, so code_execution calls are marked dynamic. Mirrors
// hasDynamicFilteringWebToolWithoutCodeExecution in the TypeScript SDK. The
// argument is the prepared wire tools list.
func HasDynamicFilteringWebToolWithoutCodeExecution(tools []map[string]interface{}) bool {
	hasDynamicFilteringWebTool := false
	for _, t := range tools {
		switch t["type"] {
		case "web_fetch_20260209", "web_fetch_20260318", "web_search_20260209", "web_search_20260318":
			hasDynamicFilteringWebTool = true
		case "code_execution_20250522", "code_execution_20250825", "code_execution_20260120":
			return false
		}
	}
	return hasDynamicFilteringWebTool
}
