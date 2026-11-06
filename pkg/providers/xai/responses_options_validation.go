package xai

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// xaiResponsesOptionsSchema is xaiLanguageModelResponsesOptions
// (xai-responses-language-model-options.ts).
var xaiResponsesOptionsSchema = func() providerutils.OptionSchema {
	zero, eight := 0, 8
	return providerutils.OptionSchema{
		"reasoningEffort":    providerutils.OptEnum("none", "low", "medium", "high", "xhigh"),
		"reasoningSummary":   providerutils.OptEnum("auto", "concise", "detailed"),
		"logprobs":           providerutils.OptBool,
		"topLogprobs":        providerutils.OptInt(&zero, &eight),
		"minP":               providerutils.OptRange(0, 1),
		"maxTurns":           providerutils.OptInt(nil, nil),
		"parallelToolCalls":  providerutils.OptBool,
		"promptCacheKey":     providerutils.OptString,
		"safetyIdentifier":   providerutils.OptString,
		"serviceTier":        providerutils.OptEnum("default", "priority"),
		"store":              providerutils.OptBool,
		"previousResponseId": providerutils.OptString,
		"user":               providerutils.OptString,
		"include": providerutils.OptNullable(providerutils.OptArray(providerutils.OptEnum(
			"file_search_call.results", "web_search_call.action.sources", "code_interpreter_call.outputs",
			"reasoning.encrypted_content", "no_inline_citations"), 0, -1)),
	}
}()

// validateResponsesOptions is TS parseProviderOptions({provider: 'xai',
// schema: xaiLanguageModelResponsesOptions}); typed option structs are valid
// by construction.
func validateResponsesOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("xai", providerOptions, xaiResponsesOptionsSchema)
}
