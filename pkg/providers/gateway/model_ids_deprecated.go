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
)
