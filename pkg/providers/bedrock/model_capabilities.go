package bedrock

import (
	"regexp"
	"strings"
)

// anthropicCapabilities mirrors the subset of TS anthropic's
// getModelCapabilities() that the Bedrock Converse client needs to compute
// request defaults (max_tokens, reasoning budgets, sampling-parameter
// rejection, forced-tool-use rejection).
//
// This is a local, self-contained copy scoped to the amazon-bedrock package
// (WG-B1) rather than an import of pkg/providers/anthropic, because the
// Anthropic capability table (WG-A1) is owned by a different work group and
// pkg/providers/anthropic is out of scope for this change. If/when WG-A1
// exports a shared capability table, this can be replaced with a call into
// it.
type anthropicCapabilities struct {
	MaxOutputTokens          int
	SupportsStructuredOutput bool
	SupportsAdaptiveThinking bool
	RejectsSamplingParams    bool
	RejectsForcedToolUse     bool
	IsKnownModel             bool
}

var legacyClaudeIDPattern = regexp.MustCompile(`claude-(?:instant(?:-|$)|v?2(?:$|[-.:])|3(?:$|[-.]))`)
var sonnet4IDPattern = regexp.MustCompile(`claude-sonnet-4(?:-|@)`)
var opus4IDPattern = regexp.MustCompile(`claude-opus-4(?:-|@)`)

// bedrockAnthropicModelCapabilities returns the Anthropic capability profile
// for modelID, mirroring TS anthropic-language-model.ts#getModelCapabilities.
func bedrockAnthropicModelCapabilities(modelID string) anthropicCapabilities {
	switch {
	case strings.Contains(modelID, "claude-opus-5-5"):
		return anthropicCapabilities{128000, true, true, true, true, true}
	case strings.Contains(modelID, "claude-opus-5"):
		return anthropicCapabilities{128000, true, true, true, false, true}
	case strings.Contains(modelID, "claude-fable-5-1"):
		return anthropicCapabilities{128000, true, true, true, true, true}
	case strings.Contains(modelID, "claude-fable-5"):
		return anthropicCapabilities{128000, true, true, true, false, true}
	case strings.Contains(modelID, "claude-opus-4-8"),
		strings.Contains(modelID, "claude-opus-4-7"),
		strings.Contains(modelID, "claude-sonnet-5"):
		return anthropicCapabilities{128000, true, true, true, false, true}
	case strings.Contains(modelID, "claude-sonnet-4-6"), strings.Contains(modelID, "claude-opus-4-6"):
		return anthropicCapabilities{128000, true, true, false, false, true}
	case strings.Contains(modelID, "claude-sonnet-4-5"),
		strings.Contains(modelID, "claude-opus-4-5"),
		strings.Contains(modelID, "claude-haiku-4-5"):
		return anthropicCapabilities{64000, true, false, false, false, true}
	case strings.Contains(modelID, "claude-opus-4-1"):
		return anthropicCapabilities{32000, true, false, false, false, true}
	case sonnet4IDPattern.MatchString(modelID):
		return anthropicCapabilities{64000, false, false, false, false, true}
	case opus4IDPattern.MatchString(modelID):
		return anthropicCapabilities{32000, false, false, false, false, true}
	case strings.Contains(modelID, "claude-3-haiku"):
		return anthropicCapabilities{4096, false, false, false, false, true}
	case legacyClaudeIDPattern.MatchString(modelID):
		return anthropicCapabilities{4096, false, false, false, false, false}
	case strings.Contains(modelID, "claude-"):
		// Unknown, presumably newer Claude models: current-generation defaults.
		return anthropicCapabilities{128000, true, true, true, false, false}
	default:
		// Non-Claude models served through Anthropic-compatible APIs.
		return anthropicCapabilities{4096, false, false, false, false, false}
	}
}
