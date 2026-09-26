package anthropic

import "testing"

// Ports amazon-bedrock-anthropic-provider.test.ts's model-support it.each
// tables (ai@7.0.113).

func TestSupportsNativeStructuredOutput_DisabledModels(t *testing.T) {
	models := []string{
		"anthropic.claude-haiku-4-5-20251001-v1:0",
		"us.anthropic.claude-haiku-4-5-20251001-v1:0",
		"eu.anthropic.claude-haiku-4-5-20251001-v1:0",
		"global.anthropic.claude-haiku-4-5-20251001-v1:0",
		"anthropic.claude-sonnet-4-6-v1",
		"us.anthropic.claude-sonnet-4-6-v1",
		"eu.anthropic.claude-sonnet-4-6-v1",
		"global.anthropic.claude-sonnet-4-6-v1",
		"anthropic.claude-opus-4-7",
		"us.anthropic.claude-opus-4-7",
		"eu.anthropic.claude-opus-4-7",
		"anthropic.claude-opus-4-8",
		"us.anthropic.claude-opus-4-8",
		"eu.anthropic.claude-opus-4-8",
		"anthropic.claude-opus-5",
		"us.anthropic.claude-opus-5",
		"eu.anthropic.claude-opus-5",
		"anthropic.claude-fable-5",
		"us.anthropic.claude-fable-5",
		"eu.anthropic.claude-fable-5",
		"anthropic.claude-fable-5-1",
		"us.anthropic.claude-fable-5-1",
		"global.anthropic.claude-fable-5-1",
		"anthropic.claude-sonnet-5",
		"us.anthropic.claude-sonnet-5",
		"eu.anthropic.claude-sonnet-5",
	}
	for _, modelID := range models {
		t.Run(modelID, func(t *testing.T) {
			if supportsNativeStructuredOutput(modelID) {
				t.Errorf("supportsNativeStructuredOutput(%q) = true, want false", modelID)
			}
		})
	}
}

func TestSupportsStrictTools_DisabledModels(t *testing.T) {
	models := []string{
		"anthropic.claude-opus-4-7",
		"us.anthropic.claude-opus-4-7",
		"eu.anthropic.claude-opus-4-7",
		"anthropic.claude-opus-4-8",
		"us.anthropic.claude-opus-4-8",
		"eu.anthropic.claude-opus-4-8",
		"anthropic.claude-opus-5",
		"us.anthropic.claude-opus-5",
		"eu.anthropic.claude-opus-5",
		"anthropic.claude-fable-5",
		"us.anthropic.claude-fable-5",
		"eu.anthropic.claude-fable-5",
		"anthropic.claude-fable-5-1",
		"us.anthropic.claude-fable-5-1",
		"global.anthropic.claude-fable-5-1",
		"anthropic.claude-sonnet-5",
		"us.anthropic.claude-sonnet-5",
		"eu.anthropic.claude-sonnet-5",
	}
	for _, modelID := range models {
		t.Run(modelID, func(t *testing.T) {
			if supportsStrictTools(modelID) {
				t.Errorf("supportsStrictTools(%q) = true, want false", modelID)
			}
		})
	}
}

func TestSupportsStrictTools_EnabledModels(t *testing.T) {
	models := []string{
		"anthropic.claude-sonnet-4-6-v1",
		"us.anthropic.claude-haiku-4-5-20251001-v1:0",
	}
	for _, modelID := range models {
		t.Run(modelID, func(t *testing.T) {
			if !supportsStrictTools(modelID) {
				t.Errorf("supportsStrictTools(%q) = false, want true", modelID)
			}
		})
	}
}

func TestSupportsNativeStructuredOutput_DefaultEnabled(t *testing.T) {
	if !supportsNativeStructuredOutput("anthropic.claude-3-5-sonnet-20241022-v2:0") {
		t.Error("supportsNativeStructuredOutput should default to true for models not on either list")
	}
}
