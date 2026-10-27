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
	ModelMistralLarge4           = "mistral-large-4"
	ModelMistralLarge40          = "mistral-large-4-0"
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
	ModelZaiGLM5                 = "zai-glm-5"
	ModelZaiGLM52                = "zai-glm-5-2"
	ModelZaiGLM53                = "zai-glm-5-3"
	ModelZaiGLM53Flash           = "zai-glm-5-3-flash"
	ModelZaiGLMLatest            = "zai-glm-latest"
)

// mistralModelsWithLowHighMaxReasoningEffort mirrors
// modelsWithLowHighMaxReasoningEffort in the TS chat model (TS #24062):
// GLM-5.3 models accept only low, high and max reasoning effort and cannot
// disable reasoning. Omitting reasoning_effort uses max.
var mistralModelsWithLowHighMaxReasoningEffort = map[string]bool{
	ModelZaiGLM5:       true,
	ModelZaiGLM53:      true,
	ModelZaiGLM53Flash: true,
	ModelZaiGLMLatest:  true,
}

// Mistral speech (text-to-speech) model IDs, mirroring MistralSpeechModelId in
// ai/packages/mistral/src/mistral-speech-model-options.ts. Any other model ID
// string is accepted as well.
const (
	ModelVoxtralMiniTTS2603   = "voxtral-mini-tts-2603"
	ModelVoxtralMiniTTS30     = "voxtral-mini-tts-3-0"
	ModelVoxtralMiniTTS3      = "voxtral-mini-tts-3"
	ModelVoxtralMiniTTSLatest = "voxtral-mini-tts-latest"
)

// Mistral transcription model IDs, mirroring MistralTranscriptionModelId in
// ai/packages/mistral/src/mistral-transcription-model-options.ts. Any other
// model ID string is accepted as well.
const (
	ModelVoxtralMini2602              = "voxtral-mini-2602"
	ModelVoxtralMiniLatest            = "voxtral-mini-latest"
	ModelVoxtralMiniTranscribe30      = "voxtral-mini-transcribe-3-0"
	ModelVoxtralMiniTranscribe3       = "voxtral-mini-transcribe-3"
	ModelVoxtralMiniTranscribeLatest  = "voxtral-mini-transcribe-latest"
	ModelVoxtralSmallTranscribe30     = "voxtral-small-transcribe-3-0"
	ModelVoxtralSmallTranscribe3      = "voxtral-small-transcribe-3"
	ModelVoxtralSmallTranscribeLatest = "voxtral-small-transcribe-latest"
)

// mistralModelsWithoutReasoningEffort mirrors modelsWithoutReasoningEffort in
// ai/packages/mistral/src/mistral-chat-language-model.ts: known non-reasoning
// models from https://api.mistral.ai/v1/models (2026-10-06). Unknown IDs
// inherit current reasoning configuration support (TS #22494).
var mistralModelsWithoutReasoningEffort = map[string]bool{
	ModelCodestral2508:        true,
	ModelCodestralLatest:      true,
	ModelMinistral14B2512:     true,
	ModelMinistral14BLatest:   true,
	ModelMinistral3B2512:      true,
	ModelMinistral3BLatest:    true,
	ModelMinistral8B2512:      true,
	ModelMinistral8BLatest:    true,
	ModelMistralCodeFimLatest: true,
	ModelMistralCodeLatest:    true,
	ModelMistralLargeLatest:   true,
	ModelMistralLarge2512:     true,
	ModelVoxtralSmall2507:     true,
	ModelVoxtralSmallLatest:   true,
}
