package anthropic

import "strings"

// This file mirrors amazon-bedrock-anthropic-model-support.ts (ai@7.0.113).

// modelsWithoutStrictToolSupport lists models for which Bedrock's copy of the
// Messages schema rejects tool.strict.
var modelsWithoutStrictToolSupport = []string{
	"claude-opus-4-7",
	"claude-opus-4-8",
	"claude-opus-5",
	"claude-fable-5",
	"claude-sonnet-5",
}

// modelsWithoutReliableNativeStructuredOutput additionally excludes models
// where native structured output (output_config.format) is unreliable even
// though strict tool support remains available.
var modelsWithoutReliableNativeStructuredOutput = append(append([]string{},
	modelsWithoutStrictToolSupport...),
	"claude-sonnet-4-6",
	"claude-haiku-4-5",
)

// supportsStrictTools reports whether Bedrock accepts tool.strict for modelID.
func supportsStrictTools(modelID string) bool {
	return !matchesModel(modelID, modelsWithoutStrictToolSupport)
}

// supportsNativeStructuredOutput reports whether Bedrock reliably supports
// output_config.format for modelID.
func supportsNativeStructuredOutput(modelID string) bool {
	return !matchesModel(modelID, modelsWithoutReliableNativeStructuredOutput)
}

func matchesModel(modelID string, models []string) bool {
	for _, m := range models {
		if strings.Contains(modelID, m) {
			return true
		}
	}
	return false
}
