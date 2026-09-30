package quiverai

// QuiverAILanguageModelOptions mirrors the TS SDK's
// QuiverAILanguageModelOptions (quiverai-language-model-options.ts):
// providerOptions.quiverai for the Arrow 2 / Arrow 2 Telos language models.
type QuiverAILanguageModelOptions struct {
	// ReasoningEffort controls the amount of reasoning used by the model.
	// One of: "low", "medium", "high", "xhigh".
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// ReasoningSummary requests the model's safe reasoning summary. The only
	// supported value is "auto".
	ReasoningSummary string `json:"reasoningSummary,omitempty"`
}

// quiverLanguageReasoningEfforts is the set of valid
// providerOptions.quiverai.reasoningEffort values for the language model
// (quiverai-language-model.ts's inline validation). Arrow 2 / Arrow 2 Telos
// model IDs are the same string values already declared as image model IDs
// in image_model.go (ModelArrow2, ModelArrow2Telos); arbitrary model ID
// strings are also accepted, like TS's `(string & {})`.
var quiverLanguageReasoningEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}
