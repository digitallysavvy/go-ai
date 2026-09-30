package bedrock

import "strings"

// isAnthropicModelID reports whether modelID (optionally combined with an
// explicit ModelFamily setting and a reasoning budget) identifies an
// Anthropic Claude model on Bedrock. Ports TS
// amazon-bedrock-anthropic-model-support.ts#isAnthropicModel.
func isAnthropicModelID(modelID, modelFamily string, reasoningBudgetTokens *int) bool {
	if modelFamily == "anthropic" {
		return true
	}
	if strings.Contains(modelID, "anthropic") {
		return true
	}
	if strings.Contains(modelID, ":application-inference-profile/") && reasoningBudgetTokens != nil {
		return true
	}
	return false
}

// modelsWithoutStrictToolSupport lists Claude model-ID substrings for which
// Bedrock's copy of the Messages schema rejects `output_config.format` and
// tool `strict`. Ports TS MODELS_WITHOUT_STRICT_TOOL_SUPPORT.
var modelsWithoutStrictToolSupport = []string{
	"claude-opus-4-7",
	"claude-opus-4-8",
	"claude-opus-5",
	"claude-fable-5",
	"claude-sonnet-5",
}

// modelsWithoutReliableNativeStructuredOutput extends
// modelsWithoutStrictToolSupport with models whose native structured output
// is unreliable even though strict tool support remains available. Ports TS
// MODELS_WITHOUT_RELIABLE_NATIVE_STRUCTURED_OUTPUT.
var modelsWithoutReliableNativeStructuredOutput = append(append([]string{}, modelsWithoutStrictToolSupport...),
	"claude-sonnet-4-6",
	"claude-haiku-4-5",
)

func matchesModelList(modelID string, models []string) bool {
	for _, m := range models {
		if strings.Contains(modelID, m) {
			return true
		}
	}
	return false
}

// bedrockSupportsStrictTools reports whether modelID supports the `strict`
// tool flag and native `output_config.format` on Bedrock's Converse API.
func bedrockSupportsStrictTools(modelID string) bool {
	return !matchesModelList(modelID, modelsWithoutStrictToolSupport)
}

// bedrockSupportsNativeStructuredOutput reports whether modelID reliably
// supports native structured output on Bedrock.
func bedrockSupportsNativeStructuredOutput(modelID string) bool {
	return !matchesModelList(modelID, modelsWithoutReliableNativeStructuredOutput)
}
