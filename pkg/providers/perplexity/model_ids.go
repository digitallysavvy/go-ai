package perplexity

// PerplexityLanguageModelID identifies a Perplexity Agent API model, preset,
// or legacy Sonar model. Any string is accepted: presets are recognized by
// value (see PerplexityAgentPreset), everything else is forwarded verbatim as
// the Agent API "model" field (mirrors TS PerplexityLanguageModelId = Preset |
// 'perplexity/sonar' | (string & {})).
type PerplexityLanguageModelID string

const (
	ModelSonarDeepResearch PerplexityLanguageModelID = "sonar-deep-research"
	ModelSonarReasoningPro PerplexityLanguageModelID = "sonar-reasoning-pro"
	ModelSonarReasoning    PerplexityLanguageModelID = "sonar-reasoning"
	ModelSonarPro          PerplexityLanguageModelID = "sonar-pro"
	ModelSonar             PerplexityLanguageModelID = "sonar"
)

// PerplexityAgentPreset identifies an Agent API routing preset. Presets are
// sent as {"preset": id} rather than {"model": id} in the request body.
// Mirrors TS PerplexityAgentPreset.
type PerplexityAgentPreset string

const (
	PresetFast   PerplexityAgentPreset = "fast"
	PresetLow    PerplexityAgentPreset = "low"
	PresetMedium PerplexityAgentPreset = "medium"
	PresetHigh   PerplexityAgentPreset = "high"
	PresetXHigh  PerplexityAgentPreset = "xhigh"
)

// perplexityAgentPresets is the set of recognized preset IDs, mirroring TS's
// `presetIds` Set in perplexity-language-model.ts.
var perplexityAgentPresets = map[string]bool{
	string(PresetFast):   true,
	string(PresetLow):    true,
	string(PresetMedium): true,
	string(PresetHigh):   true,
	string(PresetXHigh):  true,
}

// isPerplexityAgentPreset reports whether modelID names one of the five
// Agent API routing presets (fast|low|medium|high|xhigh). Legacy Sonar model
// IDs (e.g. "sonar-pro") and direct model IDs (e.g. "openai/gpt-5.1") are not
// presets and are sent as the "model" field instead.
func isPerplexityAgentPreset(modelID string) bool {
	return perplexityAgentPresets[modelID]
}

// getModelSelection mirrors TS's getModelSelection(): a preset ID is sent as
// {"preset": id}; anything else (including legacy Sonar IDs and direct
// provider/model IDs) is sent as {"model": id}.
func getModelSelection(modelID string) (model string, preset string) {
	if isPerplexityAgentPreset(modelID) {
		return "", modelID
	}
	return modelID, ""
}
