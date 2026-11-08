package gateway

// Model IDs that were removed from the TypeScript Gateway model ID unions but
// are kept so existing Go callers keep compiling. The Gateway still accepts any
// string, so these remain usable; they are no longer in the suggested ID lists.
const (
	// GatewayLanguageModelAlibabaQwen3235bA22bThinking is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3235bA22bThinking GatewayLanguageModelID = "alibaba/qwen3-235b-a22b-thinking"

	// GatewayLanguageModelAlibabaQwen36MaxPreview is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen36MaxPreview GatewayLanguageModelID = "alibaba/qwen-3.6-max-preview"

	// GatewayLanguageModelAlibabaQwen3CoderPlus is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3CoderPlus GatewayLanguageModelID = "alibaba/qwen3-coder-plus"

	// GatewayLanguageModelAlibabaQwen3Max is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3Max GatewayLanguageModelID = "alibaba/qwen3-max"

	// GatewayLanguageModelAlibabaQwen3MaxPreview is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3MaxPreview GatewayLanguageModelID = "alibaba/qwen3-max-preview"

	// GatewayLanguageModelAlibabaQwen3Vl235bA22bInstruct is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3Vl235bA22bInstruct GatewayLanguageModelID = "alibaba/qwen3-vl-235b-a22b-instruct"

	// GatewayLanguageModelAlibabaQwen3VlThinking is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAlibabaQwen3VlThinking GatewayLanguageModelID = "alibaba/qwen3-vl-thinking"

	// GatewayLanguageModelAnthropicClaude3Haiku is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAnthropicClaude3Haiku GatewayLanguageModelID = "anthropic/claude-3-haiku"

	// GatewayLanguageModelAnthropicClaudeOpus4 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAnthropicClaudeOpus4 GatewayLanguageModelID = "anthropic/claude-opus-4"

	// GatewayLanguageModelDeepseekDeepseekV31Terminus is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelDeepseekDeepseekV31Terminus GatewayLanguageModelID = "deepseek/deepseek-v3.1-terminus"

	// GatewayLanguageModelInclusionaiLing30FlashSanteFree is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelInclusionaiLing30FlashSanteFree GatewayLanguageModelID = "inclusionai/ling-3.0-flash-sante-free"

	// GatewayLanguageModelInclusionaiLing31FlashFree is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelInclusionaiLing31FlashFree GatewayLanguageModelID = "inclusionai/ling-3.1-flash-free"

	// GatewayLanguageModelMoonshotaiKimiK2Thinking is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelMoonshotaiKimiK2Thinking GatewayLanguageModelID = "moonshotai/kimi-k2-thinking"

	// GatewayLanguageModelStepfunStep35Flash is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelStepfunStep35Flash GatewayLanguageModelID = "stepfun/step-3.5-flash"

	// GatewayTranscriptionModelMicrosoftMaiTranscribe15 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayTranscriptionModelMicrosoftMaiTranscribe15 GatewayTranscriptionModelID = "microsoft/mai-transcribe-1.5"

	// GatewayLanguageModelAnthropicClaudeSonnet4 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelAnthropicClaudeSonnet4 GatewayLanguageModelID = "anthropic/claude-sonnet-4"

	// GatewayLanguageModelOpenaiGpt35Turbo is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelOpenaiGpt35Turbo GatewayLanguageModelID = "openai/gpt-3.5-turbo"

	// GatewayLanguageModelOpenaiGpt4Turbo is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelOpenaiGpt4Turbo GatewayLanguageModelID = "openai/gpt-4-turbo"

	// GatewayLanguageModelOpenaiGpt41NanoBatch is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelOpenaiGpt41NanoBatch GatewayLanguageModelID = "openai/gpt-4.1-nano-batch"

	// GatewayLanguageModelOpenaiGpt41NanoFast is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelOpenaiGpt41NanoFast GatewayLanguageModelID = "openai/gpt-4.1-nano-fast"

	// GatewayLanguageModelOpenaiO4MiniFast is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelOpenaiO4MiniFast GatewayLanguageModelID = "openai/o4-mini-fast"

	// GatewayLanguageModelSpacexaiGrok41FastNonReasoning is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelSpacexaiGrok41FastNonReasoning GatewayLanguageModelID = "spacexai/grok-4.1-fast-non-reasoning"

	// GatewayLanguageModelSpacexaiGrok41FastReasoning is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelSpacexaiGrok41FastReasoning GatewayLanguageModelID = "spacexai/grok-4.1-fast-reasoning"

	// GatewayImageModelOpenaiGptImage1 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayImageModelOpenaiGptImage1 GatewayImageModelID = "openai/gpt-image-1"

	// GatewayImageModelQuiveraiArrow11 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayImageModelQuiveraiArrow11 GatewayImageModelID = "quiverai/arrow-1.1"

	// GatewayRealtimeModelSpacexaiGrokVoiceThinkFast10 is no longer in the TS Gateway model ID union.
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayRealtimeModelSpacexaiGrokVoiceThinkFast10 GatewayRealtimeModelID = "spacexai/grok-voice-think-fast-1.0"

	// GatewayLanguageModelArceeAiTrinityLargeThinking is no longer in the TS Gateway model ID union (TS d05ff62360).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelArceeAiTrinityLargeThinking GatewayLanguageModelID = "arcee-ai/trinity-large-thinking"

	// GatewayLanguageModelMeituanLongcat25Preview is no longer in the TS Gateway model ID union (TS d05ff62360).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelMeituanLongcat25Preview GatewayLanguageModelID = "meituan/longcat-2.5-preview"

	// GatewayLanguageModelPoolsideLagunaS21Free is no longer in the TS Gateway model ID union (TS d05ff62360).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelPoolsideLagunaS21Free GatewayLanguageModelID = "poolside/laguna-s-2.1-free"

	// GatewayDecisionModelConvaiinnovationsLayaFree is no longer in the TS Gateway model ID union (TS d05ff62360).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayDecisionModelConvaiinnovationsLayaFree GatewayDecisionModelID = "convaiinnovations/laya-free"

	// GatewayLanguageModelXiaomiMimoV25 is no longer in the TS Gateway model ID union (TS acea2b5ec5).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelXiaomiMimoV25 GatewayLanguageModelID = "xiaomi/mimo-v2.5"

	// GatewayLanguageModelXiaomiMimoV25Pro is no longer in the TS Gateway model ID union (TS acea2b5ec5).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelXiaomiMimoV25Pro GatewayLanguageModelID = "xiaomi/mimo-v2.5-pro"

	// GatewayLanguageModelGoogleGemini31FlashImagePreview is no longer in the TS Gateway model ID union (TS bb4e2d0a3b).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelGoogleGemini31FlashImagePreview GatewayLanguageModelID = "google/gemini-3.1-flash-image-preview"

	// GatewayLanguageModelGoogleGemini35FlashBatch is no longer in the TS Gateway model ID union (TS bb4e2d0a3b).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelGoogleGemini35FlashBatch GatewayLanguageModelID = "google/gemini-3.5-flash-batch"

	// GatewayLanguageModelGoogleGemini37FlashBatch is no longer in the TS Gateway model ID union (TS bb4e2d0a3b).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayLanguageModelGoogleGemini37FlashBatch GatewayLanguageModelID = "google/gemini-3.7-flash-batch"

	// GatewayVideoModelGoogleVeo30FastGenerate001 is no longer in the TS Gateway model ID union (TS bb4e2d0a3b).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayVideoModelGoogleVeo30FastGenerate001 GatewayVideoModelID = "google/veo-3.0-fast-generate-001"

	// GatewayVideoModelGoogleVeo30Generate001 is no longer in the TS Gateway model ID union (TS bb4e2d0a3b).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayVideoModelGoogleVeo30Generate001 GatewayVideoModelID = "google/veo-3.0-generate-001"

	// GatewayRealtimeModelSpacexaiGrokVoiceThinkFast20 is no longer in the TS Gateway model ID union (TS bace5536e6).
	//
	// Deprecated: removed from the suggested Gateway model IDs.
	GatewayRealtimeModelSpacexaiGrokVoiceThinkFast20 GatewayRealtimeModelID = "spacexai/grok-voice-think-fast-2.0"
)
