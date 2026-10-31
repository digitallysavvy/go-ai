package anthropic

import (
	"regexp"
	"strings"
)

// This file mirrors amazon-bedrock-anthropic-model-support.ts (ai@7.0.113).

// Bedrock validates its own Messages schema. New Claude generations currently
// reject native structured output and strict tools, so only known supported
// Claude models receive those fields. Non-Claude models and opaque inference
// profile IDs retain their existing behavior. Ports TS
// amazon-bedrock-anthropic-model-support.ts (TS #22487).

// legacyClaudePattern matches Claude Instant, Claude 2 and Claude 3 IDs.
var legacyClaudePattern = regexp.MustCompile(`claude-(?:instant|v?2|3)(?:[-.:]|$)`)

// modelsWithStrictToolSupport lists the known Claude families whose Bedrock
// schema accepts tool.strict.
var modelsWithStrictToolSupport = []*regexp.Regexp{
	// Original Claude 4 IDs include a release date instead of a minor version.
	regexp.MustCompile(`claude-(?:opus|sonnet)-4(?:-\d{8})?(?:-v\d+)?(?:[:.]|$)`),
	regexp.MustCompile(`claude-opus-4-(?:1|5|6)(?:[-.:]|$)`),
	regexp.MustCompile(`claude-sonnet-4-(?:5|6)(?:[-.:]|$)`),
	regexp.MustCompile(`claude-haiku-4-5(?:[-.:]|$)`),
}

// nativeStructuredOutputExcludedPattern matches models that support strict
// tools but whose native structured output is unreliable.
var nativeStructuredOutputExcludedPattern = regexp.MustCompile(`claude-(?:sonnet-4-6|haiku-4-5)(?:[-.:]|$)`)

// supportsStrictTools reports whether Bedrock accepts tool.strict for modelID.

func supportsStrictTools(modelID string) bool {
	if !strings.Contains(modelID, "claude-") || legacyClaudePattern.MatchString(modelID) {
		return true
	}
	for _, p := range modelsWithStrictToolSupport {
		if p.MatchString(modelID) {
			return true
		}
	}
	return false
}

// supportsNativeStructuredOutput reports whether Bedrock reliably supports
// output_config.format for modelID.
func supportsNativeStructuredOutput(modelID string) bool {
	return supportsStrictTools(modelID) && !nativeStructuredOutputExcludedPattern.MatchString(modelID)
}
