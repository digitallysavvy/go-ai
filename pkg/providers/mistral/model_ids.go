package mistral

// Model IDs mirror ai/packages/mistral/src/mistral-chat-language-model-options.ts
// (MistralChatModelId), captured 2026-09-26. Mistral's chat model catalog is
// open-ended (the TS type also allows arbitrary strings via `string & {}`),
// so these constants are convenience values, not an exhaustive allow-list.
const (
	ModelCodestral2508           = "codestral-2508"
	ModelCodestralLatest         = "codestral-latest"
	ModelGLM52                   = "glm-5-2"
	ModelLabsLeanstral15         = "labs-leanstral-1-5"
	ModelLabsLeanstral151        = "labs-leanstral-1-5-1"
	ModelMagistralMediumLatest   = "magistral-medium-latest"
	ModelMagistralSmallLatest    = "magistral-small-latest"
	ModelMinistral14B2512        = "ministral-14b-2512"
	ModelMinistral14BLatest      = "ministral-14b-latest"
	ModelMinistral3B2512         = "ministral-3b-2512"
	ModelMinistral3BLatest       = "ministral-3b-latest"
	ModelMinistral8B2512         = "ministral-8b-2512"
	ModelMinistral8BLatest       = "ministral-8b-latest"
	ModelMistralCodeFimLatest    = "mistral-code-fim-latest"
	ModelMistralCodeLatest       = "mistral-code-latest"
	ModelMistralLargeLatest      = "mistral-large-latest"
	ModelMistralLarge2512        = "mistral-large-2512"
	ModelMistralMediumBare       = "mistral-medium"
	ModelMistralMediumLatest     = "mistral-medium-latest"
	ModelMistralMedium2604       = "mistral-medium-2604"
	ModelMistralMedium3          = "mistral-medium-3"
	ModelMistralMedium35Dashed   = "mistral-medium-3-5"
	ModelMistralMedium35         = "mistral-medium-3.5"
	ModelMistralSmall2603        = "mistral-small-2603"
	ModelMistralSmallLatest      = "mistral-small-latest"
	ModelMistralVibeCliFast      = "mistral-vibe-cli-fast"
	ModelMistralVibeCliLatest    = "mistral-vibe-cli-latest"
	ModelMistralVibeCliWithTools = "mistral-vibe-cli-with-tools"
	ModelVoxtralSmall2507        = "voxtral-small-2507"
	ModelVoxtralSmallLatest      = "voxtral-small-latest"
	ModelZaiGLM52                = "zai-glm-5-2"
)

// mistralReasoningEffortModelIDs mirrors reasoningEffortModelIds in
// ai/packages/mistral/src/mistral-chat-language-model.ts: models that accept
// the reasoning_effort chat completion parameter.
var mistralReasoningEffortModelIDs = map[string]bool{
	ModelGLM52:                   true,
	ModelLabsLeanstral15:         true,
	ModelLabsLeanstral151:        true,
	ModelMagistralMediumLatest:   true,
	ModelMagistralSmallLatest:    true,
	ModelMistralMediumBare:       true,
	ModelMistralMedium2604:       true,
	ModelMistralMedium3:          true,
	ModelMistralMedium35Dashed:   true,
	ModelMistralMedium35:         true,
	ModelMistralMediumLatest:     true,
	ModelMistralSmall2603:        true,
	ModelMistralSmallLatest:      true,
	ModelMistralVibeCliFast:      true,
	ModelMistralVibeCliLatest:    true,
	ModelMistralVibeCliWithTools: true,
	ModelZaiGLM52:                true,
}
