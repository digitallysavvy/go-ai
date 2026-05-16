// Code generated from ai/packages/gateway/src/gateway-*-model-settings.ts; DO NOT EDIT.
package gateway

// Gateway model ID types mirror the TypeScript AI SDK Gateway*ModelId unions.
type GatewayLanguageModelID string
type GatewayEmbeddingModelID string
type GatewayImageModelID string
type GatewayVideoModelID string
type GatewayRerankingModelID string

// GatewayLanguageModelID constants mirror gateway-language-model-settings.ts.
const (
	GatewayLanguageModelAlibabaQwen314b                 GatewayLanguageModelID = "alibaba/qwen-3-14b"
	GatewayLanguageModelAlibabaQwen3235b                GatewayLanguageModelID = "alibaba/qwen-3-235b"
	GatewayLanguageModelAlibabaQwen330b                 GatewayLanguageModelID = "alibaba/qwen-3-30b"
	GatewayLanguageModelAlibabaQwen332b                 GatewayLanguageModelID = "alibaba/qwen-3-32b"
	GatewayLanguageModelAlibabaQwen36MaxPreview         GatewayLanguageModelID = "alibaba/qwen-3.6-max-preview"
	GatewayLanguageModelAlibabaQwen3235bA22bThinking    GatewayLanguageModelID = "alibaba/qwen3-235b-a22b-thinking"
	GatewayLanguageModelAlibabaQwen3Coder               GatewayLanguageModelID = "alibaba/qwen3-coder"
	GatewayLanguageModelAlibabaQwen3Coder30bA3b         GatewayLanguageModelID = "alibaba/qwen3-coder-30b-a3b"
	GatewayLanguageModelAlibabaQwen3CoderNext           GatewayLanguageModelID = "alibaba/qwen3-coder-next"
	GatewayLanguageModelAlibabaQwen3CoderPlus           GatewayLanguageModelID = "alibaba/qwen3-coder-plus"
	GatewayLanguageModelAlibabaQwen3Max                 GatewayLanguageModelID = "alibaba/qwen3-max"
	GatewayLanguageModelAlibabaQwen3MaxPreview          GatewayLanguageModelID = "alibaba/qwen3-max-preview"
	GatewayLanguageModelAlibabaQwen3MaxThinking         GatewayLanguageModelID = "alibaba/qwen3-max-thinking"
	GatewayLanguageModelAlibabaQwen3Next80bA3bInstruct  GatewayLanguageModelID = "alibaba/qwen3-next-80b-a3b-instruct"
	GatewayLanguageModelAlibabaQwen3Next80bA3bThinking  GatewayLanguageModelID = "alibaba/qwen3-next-80b-a3b-thinking"
	GatewayLanguageModelAlibabaQwen3Vl235bA22bInstruct  GatewayLanguageModelID = "alibaba/qwen3-vl-235b-a22b-instruct"
	GatewayLanguageModelAlibabaQwen3VlInstruct          GatewayLanguageModelID = "alibaba/qwen3-vl-instruct"
	GatewayLanguageModelAlibabaQwen3VlThinking          GatewayLanguageModelID = "alibaba/qwen3-vl-thinking"
	GatewayLanguageModelAlibabaQwen35Flash              GatewayLanguageModelID = "alibaba/qwen3.5-flash"
	GatewayLanguageModelAlibabaQwen35Plus               GatewayLanguageModelID = "alibaba/qwen3.5-plus"
	GatewayLanguageModelAlibabaQwen3627b                GatewayLanguageModelID = "alibaba/qwen3.6-27b"
	GatewayLanguageModelAlibabaQwen36Plus               GatewayLanguageModelID = "alibaba/qwen3.6-plus"
	GatewayLanguageModelAmazonNova2Lite                 GatewayLanguageModelID = "amazon/nova-2-lite"
	GatewayLanguageModelAmazonNovaLite                  GatewayLanguageModelID = "amazon/nova-lite"
	GatewayLanguageModelAmazonNovaMicro                 GatewayLanguageModelID = "amazon/nova-micro"
	GatewayLanguageModelAmazonNovaPro                   GatewayLanguageModelID = "amazon/nova-pro"
	GatewayLanguageModelAnthropicClaude3Haiku           GatewayLanguageModelID = "anthropic/claude-3-haiku"
	GatewayLanguageModelAnthropicClaude35Haiku          GatewayLanguageModelID = "anthropic/claude-3.5-haiku"
	GatewayLanguageModelAnthropicClaudeHaiku45          GatewayLanguageModelID = "anthropic/claude-haiku-4.5"
	GatewayLanguageModelAnthropicClaudeOpus4            GatewayLanguageModelID = "anthropic/claude-opus-4"
	GatewayLanguageModelAnthropicClaudeOpus41           GatewayLanguageModelID = "anthropic/claude-opus-4.1"
	GatewayLanguageModelAnthropicClaudeOpus45           GatewayLanguageModelID = "anthropic/claude-opus-4.5"
	GatewayLanguageModelAnthropicClaudeOpus46           GatewayLanguageModelID = "anthropic/claude-opus-4.6"
	GatewayLanguageModelAnthropicClaudeOpus47           GatewayLanguageModelID = "anthropic/claude-opus-4.7"
	GatewayLanguageModelAnthropicClaudeSonnet4          GatewayLanguageModelID = "anthropic/claude-sonnet-4"
	GatewayLanguageModelAnthropicClaudeSonnet45         GatewayLanguageModelID = "anthropic/claude-sonnet-4.5"
	GatewayLanguageModelAnthropicClaudeSonnet46         GatewayLanguageModelID = "anthropic/claude-sonnet-4.6"
	GatewayLanguageModelArceeAiTrinityLargePreview      GatewayLanguageModelID = "arcee-ai/trinity-large-preview"
	GatewayLanguageModelArceeAiTrinityLargeThinking     GatewayLanguageModelID = "arcee-ai/trinity-large-thinking"
	GatewayLanguageModelArceeAiTrinityMini              GatewayLanguageModelID = "arcee-ai/trinity-mini"
	GatewayLanguageModelBytedanceSeed16                 GatewayLanguageModelID = "bytedance/seed-1.6"
	GatewayLanguageModelBytedanceSeed18                 GatewayLanguageModelID = "bytedance/seed-1.8"
	GatewayLanguageModelCohereCommandA                  GatewayLanguageModelID = "cohere/command-a"
	GatewayLanguageModelDeepseekDeepseekR1              GatewayLanguageModelID = "deepseek/deepseek-r1"
	GatewayLanguageModelDeepseekDeepseekV3              GatewayLanguageModelID = "deepseek/deepseek-v3"
	GatewayLanguageModelDeepseekDeepseekV31             GatewayLanguageModelID = "deepseek/deepseek-v3.1"
	GatewayLanguageModelDeepseekDeepseekV31Terminus     GatewayLanguageModelID = "deepseek/deepseek-v3.1-terminus"
	GatewayLanguageModelDeepseekDeepseekV32             GatewayLanguageModelID = "deepseek/deepseek-v3.2"
	GatewayLanguageModelDeepseekDeepseekV32Thinking     GatewayLanguageModelID = "deepseek/deepseek-v3.2-thinking"
	GatewayLanguageModelDeepseekDeepseekV4Flash         GatewayLanguageModelID = "deepseek/deepseek-v4-flash"
	GatewayLanguageModelDeepseekDeepseekV4Pro           GatewayLanguageModelID = "deepseek/deepseek-v4-pro"
	GatewayLanguageModelGoogleGemini20Flash             GatewayLanguageModelID = "google/gemini-2.0-flash"
	GatewayLanguageModelGoogleGemini20FlashLite         GatewayLanguageModelID = "google/gemini-2.0-flash-lite"
	GatewayLanguageModelGoogleGemini25Flash             GatewayLanguageModelID = "google/gemini-2.5-flash"
	GatewayLanguageModelGoogleGemini25FlashImage        GatewayLanguageModelID = "google/gemini-2.5-flash-image"
	GatewayLanguageModelGoogleGemini25FlashLite         GatewayLanguageModelID = "google/gemini-2.5-flash-lite"
	GatewayLanguageModelGoogleGemini25Pro               GatewayLanguageModelID = "google/gemini-2.5-pro"
	GatewayLanguageModelGoogleGemini3Flash              GatewayLanguageModelID = "google/gemini-3-flash"
	GatewayLanguageModelGoogleGemini3ProImage           GatewayLanguageModelID = "google/gemini-3-pro-image"
	GatewayLanguageModelGoogleGemini3ProPreview         GatewayLanguageModelID = "google/gemini-3-pro-preview"
	GatewayLanguageModelGoogleGemini31FlashImagePreview GatewayLanguageModelID = "google/gemini-3.1-flash-image-preview"
	GatewayLanguageModelGoogleGemini31FlashLite         GatewayLanguageModelID = "google/gemini-3.1-flash-lite"
	GatewayLanguageModelGoogleGemini31FlashLitePreview  GatewayLanguageModelID = "google/gemini-3.1-flash-lite-preview"
	GatewayLanguageModelGoogleGemini31ProPreview        GatewayLanguageModelID = "google/gemini-3.1-pro-preview"
	GatewayLanguageModelGoogleGemma426bA4bIt            GatewayLanguageModelID = "google/gemma-4-26b-a4b-it"
	GatewayLanguageModelGoogleGemma431bIt               GatewayLanguageModelID = "google/gemma-4-31b-it"
	GatewayLanguageModelInceptionMercury2               GatewayLanguageModelID = "inception/mercury-2"
	GatewayLanguageModelInceptionMercuryCoderSmall      GatewayLanguageModelID = "inception/mercury-coder-small"
	GatewayLanguageModelInterfazeInterfazeBeta          GatewayLanguageModelID = "interfaze/interfaze-beta"
	GatewayLanguageModelKwaipilotKatCoderProV1          GatewayLanguageModelID = "kwaipilot/kat-coder-pro-v1"
	GatewayLanguageModelKwaipilotKatCoderProV2          GatewayLanguageModelID = "kwaipilot/kat-coder-pro-v2"
	GatewayLanguageModelMeituanLongcatFlashChat         GatewayLanguageModelID = "meituan/longcat-flash-chat"
	GatewayLanguageModelMeituanLongcatFlashThinking2601 GatewayLanguageModelID = "meituan/longcat-flash-thinking-2601"
	GatewayLanguageModelMetaLlama3170b                  GatewayLanguageModelID = "meta/llama-3.1-70b"
	GatewayLanguageModelMetaLlama318b                   GatewayLanguageModelID = "meta/llama-3.1-8b"
	GatewayLanguageModelMetaLlama3211b                  GatewayLanguageModelID = "meta/llama-3.2-11b"
	GatewayLanguageModelMetaLlama321b                   GatewayLanguageModelID = "meta/llama-3.2-1b"
	GatewayLanguageModelMetaLlama323b                   GatewayLanguageModelID = "meta/llama-3.2-3b"
	GatewayLanguageModelMetaLlama3290b                  GatewayLanguageModelID = "meta/llama-3.2-90b"
	GatewayLanguageModelMetaLlama3370b                  GatewayLanguageModelID = "meta/llama-3.3-70b"
	GatewayLanguageModelMetaLlama4Maverick              GatewayLanguageModelID = "meta/llama-4-maverick"
	GatewayLanguageModelMetaLlama4Scout                 GatewayLanguageModelID = "meta/llama-4-scout"
	GatewayLanguageModelMinimaxMinimaxM2                GatewayLanguageModelID = "minimax/minimax-m2"
	GatewayLanguageModelMinimaxMinimaxM21               GatewayLanguageModelID = "minimax/minimax-m2.1"
	GatewayLanguageModelMinimaxMinimaxM21Lightning      GatewayLanguageModelID = "minimax/minimax-m2.1-lightning"
	GatewayLanguageModelMinimaxMinimaxM25               GatewayLanguageModelID = "minimax/minimax-m2.5"
	GatewayLanguageModelMinimaxMinimaxM25Highspeed      GatewayLanguageModelID = "minimax/minimax-m2.5-highspeed"
	GatewayLanguageModelMinimaxMinimaxM27               GatewayLanguageModelID = "minimax/minimax-m2.7"
	GatewayLanguageModelMinimaxMinimaxM27Highspeed      GatewayLanguageModelID = "minimax/minimax-m2.7-highspeed"
	GatewayLanguageModelMistralCodestral                GatewayLanguageModelID = "mistral/codestral"
	GatewayLanguageModelMistralDevstral2                GatewayLanguageModelID = "mistral/devstral-2"
	GatewayLanguageModelMistralDevstralSmall            GatewayLanguageModelID = "mistral/devstral-small"
	GatewayLanguageModelMistralDevstralSmall2           GatewayLanguageModelID = "mistral/devstral-small-2"
	GatewayLanguageModelMistralMagistralMedium          GatewayLanguageModelID = "mistral/magistral-medium"
	GatewayLanguageModelMistralMagistralSmall           GatewayLanguageModelID = "mistral/magistral-small"
	GatewayLanguageModelMistralMinistral14b             GatewayLanguageModelID = "mistral/ministral-14b"
	GatewayLanguageModelMistralMinistral3b              GatewayLanguageModelID = "mistral/ministral-3b"
	GatewayLanguageModelMistralMinistral8b              GatewayLanguageModelID = "mistral/ministral-8b"
	GatewayLanguageModelMistralMistralLarge3            GatewayLanguageModelID = "mistral/mistral-large-3"
	GatewayLanguageModelMistralMistralMedium            GatewayLanguageModelID = "mistral/mistral-medium"
	GatewayLanguageModelMistralMistralNemo              GatewayLanguageModelID = "mistral/mistral-nemo"
	GatewayLanguageModelMistralMistralSmall             GatewayLanguageModelID = "mistral/mistral-small"
	GatewayLanguageModelMistralPixtral12b               GatewayLanguageModelID = "mistral/pixtral-12b"
	GatewayLanguageModelMistralPixtralLarge             GatewayLanguageModelID = "mistral/pixtral-large"
	GatewayLanguageModelMoonshotaiKimiK2                GatewayLanguageModelID = "moonshotai/kimi-k2"
	GatewayLanguageModelMoonshotaiKimiK2Thinking        GatewayLanguageModelID = "moonshotai/kimi-k2-thinking"
	GatewayLanguageModelMoonshotaiKimiK2ThinkingTurbo   GatewayLanguageModelID = "moonshotai/kimi-k2-thinking-turbo"
	GatewayLanguageModelMoonshotaiKimiK2Turbo           GatewayLanguageModelID = "moonshotai/kimi-k2-turbo"
	GatewayLanguageModelMoonshotaiKimiK25               GatewayLanguageModelID = "moonshotai/kimi-k2.5"
	GatewayLanguageModelMoonshotaiKimiK26               GatewayLanguageModelID = "moonshotai/kimi-k2.6"
	GatewayLanguageModelMorphMorphV3Fast                GatewayLanguageModelID = "morph/morph-v3-fast"
	GatewayLanguageModelMorphMorphV3Large               GatewayLanguageModelID = "morph/morph-v3-large"
	GatewayLanguageModelNvidiaNemotron3Nano30bA3b       GatewayLanguageModelID = "nvidia/nemotron-3-nano-30b-a3b"
	GatewayLanguageModelNvidiaNemotron3Super120bA12b    GatewayLanguageModelID = "nvidia/nemotron-3-super-120b-a12b"
	GatewayLanguageModelNvidiaNemotronNano12bV2Vl       GatewayLanguageModelID = "nvidia/nemotron-nano-12b-v2-vl"
	GatewayLanguageModelNvidiaNemotronNano9bV2          GatewayLanguageModelID = "nvidia/nemotron-nano-9b-v2"
	GatewayLanguageModelOpenaiGpt35Turbo                GatewayLanguageModelID = "openai/gpt-3.5-turbo"
	GatewayLanguageModelOpenaiGpt35TurboInstruct        GatewayLanguageModelID = "openai/gpt-3.5-turbo-instruct"
	GatewayLanguageModelOpenaiGpt4Turbo                 GatewayLanguageModelID = "openai/gpt-4-turbo"
	GatewayLanguageModelOpenaiGpt41                     GatewayLanguageModelID = "openai/gpt-4.1"
	GatewayLanguageModelOpenaiGpt41Mini                 GatewayLanguageModelID = "openai/gpt-4.1-mini"
	GatewayLanguageModelOpenaiGpt41Nano                 GatewayLanguageModelID = "openai/gpt-4.1-nano"
	GatewayLanguageModelOpenaiGpt4o                     GatewayLanguageModelID = "openai/gpt-4o"
	GatewayLanguageModelOpenaiGpt4oMini                 GatewayLanguageModelID = "openai/gpt-4o-mini"
	GatewayLanguageModelOpenaiGpt4oMiniSearchPreview    GatewayLanguageModelID = "openai/gpt-4o-mini-search-preview"
	GatewayLanguageModelOpenaiGpt5                      GatewayLanguageModelID = "openai/gpt-5"
	GatewayLanguageModelOpenaiGpt5Chat                  GatewayLanguageModelID = "openai/gpt-5-chat"
	GatewayLanguageModelOpenaiGpt5Codex                 GatewayLanguageModelID = "openai/gpt-5-codex"
	GatewayLanguageModelOpenaiGpt5Mini                  GatewayLanguageModelID = "openai/gpt-5-mini"
	GatewayLanguageModelOpenaiGpt5Nano                  GatewayLanguageModelID = "openai/gpt-5-nano"
	GatewayLanguageModelOpenaiGpt5Pro                   GatewayLanguageModelID = "openai/gpt-5-pro"
	GatewayLanguageModelOpenaiGpt51Codex                GatewayLanguageModelID = "openai/gpt-5.1-codex"
	GatewayLanguageModelOpenaiGpt51CodexMax             GatewayLanguageModelID = "openai/gpt-5.1-codex-max"
	GatewayLanguageModelOpenaiGpt51CodexMini            GatewayLanguageModelID = "openai/gpt-5.1-codex-mini"
	GatewayLanguageModelOpenaiGpt51Instant              GatewayLanguageModelID = "openai/gpt-5.1-instant"
	GatewayLanguageModelOpenaiGpt51Thinking             GatewayLanguageModelID = "openai/gpt-5.1-thinking"
	GatewayLanguageModelOpenaiGpt52                     GatewayLanguageModelID = "openai/gpt-5.2"
	GatewayLanguageModelOpenaiGpt52Chat                 GatewayLanguageModelID = "openai/gpt-5.2-chat"
	GatewayLanguageModelOpenaiGpt52Codex                GatewayLanguageModelID = "openai/gpt-5.2-codex"
	GatewayLanguageModelOpenaiGpt52Pro                  GatewayLanguageModelID = "openai/gpt-5.2-pro"
	GatewayLanguageModelOpenaiGpt53Chat                 GatewayLanguageModelID = "openai/gpt-5.3-chat"
	GatewayLanguageModelOpenaiGpt53Codex                GatewayLanguageModelID = "openai/gpt-5.3-codex"
	GatewayLanguageModelOpenaiGpt54                     GatewayLanguageModelID = "openai/gpt-5.4"
	GatewayLanguageModelOpenaiGpt54Mini                 GatewayLanguageModelID = "openai/gpt-5.4-mini"
	GatewayLanguageModelOpenaiGpt54Nano                 GatewayLanguageModelID = "openai/gpt-5.4-nano"
	GatewayLanguageModelOpenaiGpt54Pro                  GatewayLanguageModelID = "openai/gpt-5.4-pro"
	GatewayLanguageModelOpenaiGpt55                     GatewayLanguageModelID = "openai/gpt-5.5"
	GatewayLanguageModelOpenaiGpt55Pro                  GatewayLanguageModelID = "openai/gpt-5.5-pro"
	GatewayLanguageModelOpenaiGptOss120b                GatewayLanguageModelID = "openai/gpt-oss-120b"
	GatewayLanguageModelOpenaiGptOss20b                 GatewayLanguageModelID = "openai/gpt-oss-20b"
	GatewayLanguageModelOpenaiGptOssSafeguard20b        GatewayLanguageModelID = "openai/gpt-oss-safeguard-20b"
	GatewayLanguageModelOpenaiO1                        GatewayLanguageModelID = "openai/o1"
	GatewayLanguageModelOpenaiO3                        GatewayLanguageModelID = "openai/o3"
	GatewayLanguageModelOpenaiO3DeepResearch            GatewayLanguageModelID = "openai/o3-deep-research"
	GatewayLanguageModelOpenaiO3Mini                    GatewayLanguageModelID = "openai/o3-mini"
	GatewayLanguageModelOpenaiO3Pro                     GatewayLanguageModelID = "openai/o3-pro"
	GatewayLanguageModelOpenaiO4Mini                    GatewayLanguageModelID = "openai/o4-mini"
	GatewayLanguageModelPerplexitySonar                 GatewayLanguageModelID = "perplexity/sonar"
	GatewayLanguageModelPerplexitySonarPro              GatewayLanguageModelID = "perplexity/sonar-pro"
	GatewayLanguageModelPerplexitySonarReasoningPro     GatewayLanguageModelID = "perplexity/sonar-reasoning-pro"
	GatewayLanguageModelXaiGrok3                        GatewayLanguageModelID = "xai/grok-3"
	GatewayLanguageModelXaiGrok3Fast                    GatewayLanguageModelID = "xai/grok-3-fast"
	GatewayLanguageModelXaiGrok3Mini                    GatewayLanguageModelID = "xai/grok-3-mini"
	GatewayLanguageModelXaiGrok3MiniFast                GatewayLanguageModelID = "xai/grok-3-mini-fast"
	GatewayLanguageModelXaiGrok4                        GatewayLanguageModelID = "xai/grok-4"
	GatewayLanguageModelXaiGrok4FastNonReasoning        GatewayLanguageModelID = "xai/grok-4-fast-non-reasoning"
	GatewayLanguageModelXaiGrok4FastReasoning           GatewayLanguageModelID = "xai/grok-4-fast-reasoning"
	GatewayLanguageModelXaiGrok41FastNonReasoning       GatewayLanguageModelID = "xai/grok-4.1-fast-non-reasoning"
	GatewayLanguageModelXaiGrok41FastReasoning          GatewayLanguageModelID = "xai/grok-4.1-fast-reasoning"
	GatewayLanguageModelXaiGrok420MultiAgent            GatewayLanguageModelID = "xai/grok-4.20-multi-agent"
	GatewayLanguageModelXaiGrok420MultiAgentBeta        GatewayLanguageModelID = "xai/grok-4.20-multi-agent-beta"
	GatewayLanguageModelXaiGrok420NonReasoning          GatewayLanguageModelID = "xai/grok-4.20-non-reasoning"
	GatewayLanguageModelXaiGrok420NonReasoningBeta      GatewayLanguageModelID = "xai/grok-4.20-non-reasoning-beta"
	GatewayLanguageModelXaiGrok420Reasoning             GatewayLanguageModelID = "xai/grok-4.20-reasoning"
	GatewayLanguageModelXaiGrok420ReasoningBeta         GatewayLanguageModelID = "xai/grok-4.20-reasoning-beta"
	GatewayLanguageModelXaiGrok43                       GatewayLanguageModelID = "xai/grok-4.3"
	GatewayLanguageModelXaiGrokCodeFast1                GatewayLanguageModelID = "xai/grok-code-fast-1"
	GatewayLanguageModelXiaomiMimoV2Flash               GatewayLanguageModelID = "xiaomi/mimo-v2-flash"
	GatewayLanguageModelXiaomiMimoV2Pro                 GatewayLanguageModelID = "xiaomi/mimo-v2-pro"
	GatewayLanguageModelXiaomiMimoV25                   GatewayLanguageModelID = "xiaomi/mimo-v2.5"
	GatewayLanguageModelXiaomiMimoV25Pro                GatewayLanguageModelID = "xiaomi/mimo-v2.5-pro"
	GatewayLanguageModelZaiGlm45                        GatewayLanguageModelID = "zai/glm-4.5"
	GatewayLanguageModelZaiGlm45Air                     GatewayLanguageModelID = "zai/glm-4.5-air"
	GatewayLanguageModelZaiGlm45v                       GatewayLanguageModelID = "zai/glm-4.5v"
	GatewayLanguageModelZaiGlm46                        GatewayLanguageModelID = "zai/glm-4.6"
	GatewayLanguageModelZaiGlm46v                       GatewayLanguageModelID = "zai/glm-4.6v"
	GatewayLanguageModelZaiGlm46vFlash                  GatewayLanguageModelID = "zai/glm-4.6v-flash"
	GatewayLanguageModelZaiGlm47                        GatewayLanguageModelID = "zai/glm-4.7"
	GatewayLanguageModelZaiGlm47Flash                   GatewayLanguageModelID = "zai/glm-4.7-flash"
	GatewayLanguageModelZaiGlm47Flashx                  GatewayLanguageModelID = "zai/glm-4.7-flashx"
	GatewayLanguageModelZaiGlm5                         GatewayLanguageModelID = "zai/glm-5"
	GatewayLanguageModelZaiGlm5Turbo                    GatewayLanguageModelID = "zai/glm-5-turbo"
	GatewayLanguageModelZaiGlm51                        GatewayLanguageModelID = "zai/glm-5.1"
	GatewayLanguageModelZaiGlm5vTurbo                   GatewayLanguageModelID = "zai/glm-5v-turbo"
)

// GatewayLanguageModelIDs lists the known gateway language model IDs from the refreshed settings catalog.
var GatewayLanguageModelIDs = []GatewayLanguageModelID{
	GatewayLanguageModelAlibabaQwen314b,
	GatewayLanguageModelAlibabaQwen3235b,
	GatewayLanguageModelAlibabaQwen330b,
	GatewayLanguageModelAlibabaQwen332b,
	GatewayLanguageModelAlibabaQwen36MaxPreview,
	GatewayLanguageModelAlibabaQwen3235bA22bThinking,
	GatewayLanguageModelAlibabaQwen3Coder,
	GatewayLanguageModelAlibabaQwen3Coder30bA3b,
	GatewayLanguageModelAlibabaQwen3CoderNext,
	GatewayLanguageModelAlibabaQwen3CoderPlus,
	GatewayLanguageModelAlibabaQwen3Max,
	GatewayLanguageModelAlibabaQwen3MaxPreview,
	GatewayLanguageModelAlibabaQwen3MaxThinking,
	GatewayLanguageModelAlibabaQwen3Next80bA3bInstruct,
	GatewayLanguageModelAlibabaQwen3Next80bA3bThinking,
	GatewayLanguageModelAlibabaQwen3Vl235bA22bInstruct,
	GatewayLanguageModelAlibabaQwen3VlInstruct,
	GatewayLanguageModelAlibabaQwen3VlThinking,
	GatewayLanguageModelAlibabaQwen35Flash,
	GatewayLanguageModelAlibabaQwen35Plus,
	GatewayLanguageModelAlibabaQwen3627b,
	GatewayLanguageModelAlibabaQwen36Plus,
	GatewayLanguageModelAmazonNova2Lite,
	GatewayLanguageModelAmazonNovaLite,
	GatewayLanguageModelAmazonNovaMicro,
	GatewayLanguageModelAmazonNovaPro,
	GatewayLanguageModelAnthropicClaude3Haiku,
	GatewayLanguageModelAnthropicClaude35Haiku,
	GatewayLanguageModelAnthropicClaudeHaiku45,
	GatewayLanguageModelAnthropicClaudeOpus4,
	GatewayLanguageModelAnthropicClaudeOpus41,
	GatewayLanguageModelAnthropicClaudeOpus45,
	GatewayLanguageModelAnthropicClaudeOpus46,
	GatewayLanguageModelAnthropicClaudeOpus47,
	GatewayLanguageModelAnthropicClaudeSonnet4,
	GatewayLanguageModelAnthropicClaudeSonnet45,
	GatewayLanguageModelAnthropicClaudeSonnet46,
	GatewayLanguageModelArceeAiTrinityLargePreview,
	GatewayLanguageModelArceeAiTrinityLargeThinking,
	GatewayLanguageModelArceeAiTrinityMini,
	GatewayLanguageModelBytedanceSeed16,
	GatewayLanguageModelBytedanceSeed18,
	GatewayLanguageModelCohereCommandA,
	GatewayLanguageModelDeepseekDeepseekR1,
	GatewayLanguageModelDeepseekDeepseekV3,
	GatewayLanguageModelDeepseekDeepseekV31,
	GatewayLanguageModelDeepseekDeepseekV31Terminus,
	GatewayLanguageModelDeepseekDeepseekV32,
	GatewayLanguageModelDeepseekDeepseekV32Thinking,
	GatewayLanguageModelDeepseekDeepseekV4Flash,
	GatewayLanguageModelDeepseekDeepseekV4Pro,
	GatewayLanguageModelGoogleGemini20Flash,
	GatewayLanguageModelGoogleGemini20FlashLite,
	GatewayLanguageModelGoogleGemini25Flash,
	GatewayLanguageModelGoogleGemini25FlashImage,
	GatewayLanguageModelGoogleGemini25FlashLite,
	GatewayLanguageModelGoogleGemini25Pro,
	GatewayLanguageModelGoogleGemini3Flash,
	GatewayLanguageModelGoogleGemini3ProImage,
	GatewayLanguageModelGoogleGemini3ProPreview,
	GatewayLanguageModelGoogleGemini31FlashImagePreview,
	GatewayLanguageModelGoogleGemini31FlashLite,
	GatewayLanguageModelGoogleGemini31FlashLitePreview,
	GatewayLanguageModelGoogleGemini31ProPreview,
	GatewayLanguageModelGoogleGemma426bA4bIt,
	GatewayLanguageModelGoogleGemma431bIt,
	GatewayLanguageModelInceptionMercury2,
	GatewayLanguageModelInceptionMercuryCoderSmall,
	GatewayLanguageModelInterfazeInterfazeBeta,
	GatewayLanguageModelKwaipilotKatCoderProV1,
	GatewayLanguageModelKwaipilotKatCoderProV2,
	GatewayLanguageModelMeituanLongcatFlashChat,
	GatewayLanguageModelMeituanLongcatFlashThinking2601,
	GatewayLanguageModelMetaLlama3170b,
	GatewayLanguageModelMetaLlama318b,
	GatewayLanguageModelMetaLlama3211b,
	GatewayLanguageModelMetaLlama321b,
	GatewayLanguageModelMetaLlama323b,
	GatewayLanguageModelMetaLlama3290b,
	GatewayLanguageModelMetaLlama3370b,
	GatewayLanguageModelMetaLlama4Maverick,
	GatewayLanguageModelMetaLlama4Scout,
	GatewayLanguageModelMinimaxMinimaxM2,
	GatewayLanguageModelMinimaxMinimaxM21,
	GatewayLanguageModelMinimaxMinimaxM21Lightning,
	GatewayLanguageModelMinimaxMinimaxM25,
	GatewayLanguageModelMinimaxMinimaxM25Highspeed,
	GatewayLanguageModelMinimaxMinimaxM27,
	GatewayLanguageModelMinimaxMinimaxM27Highspeed,
	GatewayLanguageModelMistralCodestral,
	GatewayLanguageModelMistralDevstral2,
	GatewayLanguageModelMistralDevstralSmall,
	GatewayLanguageModelMistralDevstralSmall2,
	GatewayLanguageModelMistralMagistralMedium,
	GatewayLanguageModelMistralMagistralSmall,
	GatewayLanguageModelMistralMinistral14b,
	GatewayLanguageModelMistralMinistral3b,
	GatewayLanguageModelMistralMinistral8b,
	GatewayLanguageModelMistralMistralLarge3,
	GatewayLanguageModelMistralMistralMedium,
	GatewayLanguageModelMistralMistralNemo,
	GatewayLanguageModelMistralMistralSmall,
	GatewayLanguageModelMistralPixtral12b,
	GatewayLanguageModelMistralPixtralLarge,
	GatewayLanguageModelMoonshotaiKimiK2,
	GatewayLanguageModelMoonshotaiKimiK2Thinking,
	GatewayLanguageModelMoonshotaiKimiK2ThinkingTurbo,
	GatewayLanguageModelMoonshotaiKimiK2Turbo,
	GatewayLanguageModelMoonshotaiKimiK25,
	GatewayLanguageModelMoonshotaiKimiK26,
	GatewayLanguageModelMorphMorphV3Fast,
	GatewayLanguageModelMorphMorphV3Large,
	GatewayLanguageModelNvidiaNemotron3Nano30bA3b,
	GatewayLanguageModelNvidiaNemotron3Super120bA12b,
	GatewayLanguageModelNvidiaNemotronNano12bV2Vl,
	GatewayLanguageModelNvidiaNemotronNano9bV2,
	GatewayLanguageModelOpenaiGpt35Turbo,
	GatewayLanguageModelOpenaiGpt35TurboInstruct,
	GatewayLanguageModelOpenaiGpt4Turbo,
	GatewayLanguageModelOpenaiGpt41,
	GatewayLanguageModelOpenaiGpt41Mini,
	GatewayLanguageModelOpenaiGpt41Nano,
	GatewayLanguageModelOpenaiGpt4o,
	GatewayLanguageModelOpenaiGpt4oMini,
	GatewayLanguageModelOpenaiGpt4oMiniSearchPreview,
	GatewayLanguageModelOpenaiGpt5,
	GatewayLanguageModelOpenaiGpt5Chat,
	GatewayLanguageModelOpenaiGpt5Codex,
	GatewayLanguageModelOpenaiGpt5Mini,
	GatewayLanguageModelOpenaiGpt5Nano,
	GatewayLanguageModelOpenaiGpt5Pro,
	GatewayLanguageModelOpenaiGpt51Codex,
	GatewayLanguageModelOpenaiGpt51CodexMax,
	GatewayLanguageModelOpenaiGpt51CodexMini,
	GatewayLanguageModelOpenaiGpt51Instant,
	GatewayLanguageModelOpenaiGpt51Thinking,
	GatewayLanguageModelOpenaiGpt52,
	GatewayLanguageModelOpenaiGpt52Chat,
	GatewayLanguageModelOpenaiGpt52Codex,
	GatewayLanguageModelOpenaiGpt52Pro,
	GatewayLanguageModelOpenaiGpt53Chat,
	GatewayLanguageModelOpenaiGpt53Codex,
	GatewayLanguageModelOpenaiGpt54,
	GatewayLanguageModelOpenaiGpt54Mini,
	GatewayLanguageModelOpenaiGpt54Nano,
	GatewayLanguageModelOpenaiGpt54Pro,
	GatewayLanguageModelOpenaiGpt55,
	GatewayLanguageModelOpenaiGpt55Pro,
	GatewayLanguageModelOpenaiGptOss120b,
	GatewayLanguageModelOpenaiGptOss20b,
	GatewayLanguageModelOpenaiGptOssSafeguard20b,
	GatewayLanguageModelOpenaiO1,
	GatewayLanguageModelOpenaiO3,
	GatewayLanguageModelOpenaiO3DeepResearch,
	GatewayLanguageModelOpenaiO3Mini,
	GatewayLanguageModelOpenaiO3Pro,
	GatewayLanguageModelOpenaiO4Mini,
	GatewayLanguageModelPerplexitySonar,
	GatewayLanguageModelPerplexitySonarPro,
	GatewayLanguageModelPerplexitySonarReasoningPro,
	GatewayLanguageModelXaiGrok3,
	GatewayLanguageModelXaiGrok3Fast,
	GatewayLanguageModelXaiGrok3Mini,
	GatewayLanguageModelXaiGrok3MiniFast,
	GatewayLanguageModelXaiGrok4,
	GatewayLanguageModelXaiGrok4FastNonReasoning,
	GatewayLanguageModelXaiGrok4FastReasoning,
	GatewayLanguageModelXaiGrok41FastNonReasoning,
	GatewayLanguageModelXaiGrok41FastReasoning,
	GatewayLanguageModelXaiGrok420MultiAgent,
	GatewayLanguageModelXaiGrok420MultiAgentBeta,
	GatewayLanguageModelXaiGrok420NonReasoning,
	GatewayLanguageModelXaiGrok420NonReasoningBeta,
	GatewayLanguageModelXaiGrok420Reasoning,
	GatewayLanguageModelXaiGrok420ReasoningBeta,
	GatewayLanguageModelXaiGrok43,
	GatewayLanguageModelXaiGrokCodeFast1,
	GatewayLanguageModelXiaomiMimoV2Flash,
	GatewayLanguageModelXiaomiMimoV2Pro,
	GatewayLanguageModelXiaomiMimoV25,
	GatewayLanguageModelXiaomiMimoV25Pro,
	GatewayLanguageModelZaiGlm45,
	GatewayLanguageModelZaiGlm45Air,
	GatewayLanguageModelZaiGlm45v,
	GatewayLanguageModelZaiGlm46,
	GatewayLanguageModelZaiGlm46v,
	GatewayLanguageModelZaiGlm46vFlash,
	GatewayLanguageModelZaiGlm47,
	GatewayLanguageModelZaiGlm47Flash,
	GatewayLanguageModelZaiGlm47Flashx,
	GatewayLanguageModelZaiGlm5,
	GatewayLanguageModelZaiGlm5Turbo,
	GatewayLanguageModelZaiGlm51,
	GatewayLanguageModelZaiGlm5vTurbo,
}

// GatewayEmbeddingModelID constants mirror gateway-embedding-model-settings.ts.
const (
	GatewayEmbeddingModelAlibabaQwen3Embedding06b           GatewayEmbeddingModelID = "alibaba/qwen3-embedding-0.6b"
	GatewayEmbeddingModelAlibabaQwen3Embedding4b            GatewayEmbeddingModelID = "alibaba/qwen3-embedding-4b"
	GatewayEmbeddingModelAlibabaQwen3Embedding8b            GatewayEmbeddingModelID = "alibaba/qwen3-embedding-8b"
	GatewayEmbeddingModelAmazonTitanEmbedTextV2             GatewayEmbeddingModelID = "amazon/titan-embed-text-v2"
	GatewayEmbeddingModelCohereEmbedV40                     GatewayEmbeddingModelID = "cohere/embed-v4.0"
	GatewayEmbeddingModelGoogleGeminiEmbedding001           GatewayEmbeddingModelID = "google/gemini-embedding-001"
	GatewayEmbeddingModelGoogleGeminiEmbedding2             GatewayEmbeddingModelID = "google/gemini-embedding-2"
	GatewayEmbeddingModelGoogleTextEmbedding005             GatewayEmbeddingModelID = "google/text-embedding-005"
	GatewayEmbeddingModelGoogleTextMultilingualEmbedding002 GatewayEmbeddingModelID = "google/text-multilingual-embedding-002"
	GatewayEmbeddingModelMistralCodestralEmbed              GatewayEmbeddingModelID = "mistral/codestral-embed"
	GatewayEmbeddingModelMistralMistralEmbed                GatewayEmbeddingModelID = "mistral/mistral-embed"
	GatewayEmbeddingModelOpenaiTextEmbedding3Large          GatewayEmbeddingModelID = "openai/text-embedding-3-large"
	GatewayEmbeddingModelOpenaiTextEmbedding3Small          GatewayEmbeddingModelID = "openai/text-embedding-3-small"
	GatewayEmbeddingModelOpenaiTextEmbeddingAda002          GatewayEmbeddingModelID = "openai/text-embedding-ada-002"
	GatewayEmbeddingModelVoyageVoyage3Large                 GatewayEmbeddingModelID = "voyage/voyage-3-large"
	GatewayEmbeddingModelVoyageVoyage35                     GatewayEmbeddingModelID = "voyage/voyage-3.5"
	GatewayEmbeddingModelVoyageVoyage35Lite                 GatewayEmbeddingModelID = "voyage/voyage-3.5-lite"
	GatewayEmbeddingModelVoyageVoyage4                      GatewayEmbeddingModelID = "voyage/voyage-4"
	GatewayEmbeddingModelVoyageVoyage4Large                 GatewayEmbeddingModelID = "voyage/voyage-4-large"
	GatewayEmbeddingModelVoyageVoyage4Lite                  GatewayEmbeddingModelID = "voyage/voyage-4-lite"
	GatewayEmbeddingModelVoyageVoyageCode2                  GatewayEmbeddingModelID = "voyage/voyage-code-2"
	GatewayEmbeddingModelVoyageVoyageCode3                  GatewayEmbeddingModelID = "voyage/voyage-code-3"
	GatewayEmbeddingModelVoyageVoyageFinance2               GatewayEmbeddingModelID = "voyage/voyage-finance-2"
	GatewayEmbeddingModelVoyageVoyageLaw2                   GatewayEmbeddingModelID = "voyage/voyage-law-2"
)

// GatewayEmbeddingModelIDs lists the known gateway embedding model IDs from the refreshed settings catalog.
var GatewayEmbeddingModelIDs = []GatewayEmbeddingModelID{
	GatewayEmbeddingModelAlibabaQwen3Embedding06b,
	GatewayEmbeddingModelAlibabaQwen3Embedding4b,
	GatewayEmbeddingModelAlibabaQwen3Embedding8b,
	GatewayEmbeddingModelAmazonTitanEmbedTextV2,
	GatewayEmbeddingModelCohereEmbedV40,
	GatewayEmbeddingModelGoogleGeminiEmbedding001,
	GatewayEmbeddingModelGoogleGeminiEmbedding2,
	GatewayEmbeddingModelGoogleTextEmbedding005,
	GatewayEmbeddingModelGoogleTextMultilingualEmbedding002,
	GatewayEmbeddingModelMistralCodestralEmbed,
	GatewayEmbeddingModelMistralMistralEmbed,
	GatewayEmbeddingModelOpenaiTextEmbedding3Large,
	GatewayEmbeddingModelOpenaiTextEmbedding3Small,
	GatewayEmbeddingModelOpenaiTextEmbeddingAda002,
	GatewayEmbeddingModelVoyageVoyage3Large,
	GatewayEmbeddingModelVoyageVoyage35,
	GatewayEmbeddingModelVoyageVoyage35Lite,
	GatewayEmbeddingModelVoyageVoyage4,
	GatewayEmbeddingModelVoyageVoyage4Large,
	GatewayEmbeddingModelVoyageVoyage4Lite,
	GatewayEmbeddingModelVoyageVoyageCode2,
	GatewayEmbeddingModelVoyageVoyageCode3,
	GatewayEmbeddingModelVoyageVoyageFinance2,
	GatewayEmbeddingModelVoyageVoyageLaw2,
}

// GatewayImageModelID constants mirror gateway-image-model-settings.ts.
const (
	GatewayImageModelBflFlux2Flex                   GatewayImageModelID = "bfl/flux-2-flex"
	GatewayImageModelBflFlux2Klein4b                GatewayImageModelID = "bfl/flux-2-klein-4b"
	GatewayImageModelBflFlux2Klein9b                GatewayImageModelID = "bfl/flux-2-klein-9b"
	GatewayImageModelBflFlux2Max                    GatewayImageModelID = "bfl/flux-2-max"
	GatewayImageModelBflFlux2Pro                    GatewayImageModelID = "bfl/flux-2-pro"
	GatewayImageModelBflFluxKontextMax              GatewayImageModelID = "bfl/flux-kontext-max"
	GatewayImageModelBflFluxKontextPro              GatewayImageModelID = "bfl/flux-kontext-pro"
	GatewayImageModelBflFluxPro10Fill               GatewayImageModelID = "bfl/flux-pro-1.0-fill"
	GatewayImageModelBflFluxPro11                   GatewayImageModelID = "bfl/flux-pro-1.1"
	GatewayImageModelBflFluxPro11Ultra              GatewayImageModelID = "bfl/flux-pro-1.1-ultra"
	GatewayImageModelBytedanceSeedream40            GatewayImageModelID = "bytedance/seedream-4.0"
	GatewayImageModelBytedanceSeedream45            GatewayImageModelID = "bytedance/seedream-4.5"
	GatewayImageModelBytedanceSeedream50Lite        GatewayImageModelID = "bytedance/seedream-5.0-lite"
	GatewayImageModelGoogleImagen40FastGenerate001  GatewayImageModelID = "google/imagen-4.0-fast-generate-001"
	GatewayImageModelGoogleImagen40Generate001      GatewayImageModelID = "google/imagen-4.0-generate-001"
	GatewayImageModelGoogleImagen40UltraGenerate001 GatewayImageModelID = "google/imagen-4.0-ultra-generate-001"
	GatewayImageModelOpenaiGptImage1                GatewayImageModelID = "openai/gpt-image-1"
	GatewayImageModelOpenaiGptImage1Mini            GatewayImageModelID = "openai/gpt-image-1-mini"
	GatewayImageModelOpenaiGptImage15               GatewayImageModelID = "openai/gpt-image-1.5"
	GatewayImageModelOpenaiGptImage2                GatewayImageModelID = "openai/gpt-image-2"
	GatewayImageModelProdiaFluxFastSchnell          GatewayImageModelID = "prodia/flux-fast-schnell"
	GatewayImageModelRecraftRecraftV2               GatewayImageModelID = "recraft/recraft-v2"
	GatewayImageModelRecraftRecraftV3               GatewayImageModelID = "recraft/recraft-v3"
	GatewayImageModelRecraftRecraftV4               GatewayImageModelID = "recraft/recraft-v4"
	GatewayImageModelRecraftRecraftV4Pro            GatewayImageModelID = "recraft/recraft-v4-pro"
	GatewayImageModelXaiGrokImagineImage            GatewayImageModelID = "xai/grok-imagine-image"
	GatewayImageModelXaiGrokImagineImagePro         GatewayImageModelID = "xai/grok-imagine-image-pro"
)

// GatewayImageModelIDs lists the known gateway image model IDs from the refreshed settings catalog.
var GatewayImageModelIDs = []GatewayImageModelID{
	GatewayImageModelBflFlux2Flex,
	GatewayImageModelBflFlux2Klein4b,
	GatewayImageModelBflFlux2Klein9b,
	GatewayImageModelBflFlux2Max,
	GatewayImageModelBflFlux2Pro,
	GatewayImageModelBflFluxKontextMax,
	GatewayImageModelBflFluxKontextPro,
	GatewayImageModelBflFluxPro10Fill,
	GatewayImageModelBflFluxPro11,
	GatewayImageModelBflFluxPro11Ultra,
	GatewayImageModelBytedanceSeedream40,
	GatewayImageModelBytedanceSeedream45,
	GatewayImageModelBytedanceSeedream50Lite,
	GatewayImageModelGoogleImagen40FastGenerate001,
	GatewayImageModelGoogleImagen40Generate001,
	GatewayImageModelGoogleImagen40UltraGenerate001,
	GatewayImageModelOpenaiGptImage1,
	GatewayImageModelOpenaiGptImage1Mini,
	GatewayImageModelOpenaiGptImage15,
	GatewayImageModelOpenaiGptImage2,
	GatewayImageModelProdiaFluxFastSchnell,
	GatewayImageModelRecraftRecraftV2,
	GatewayImageModelRecraftRecraftV3,
	GatewayImageModelRecraftRecraftV4,
	GatewayImageModelRecraftRecraftV4Pro,
	GatewayImageModelXaiGrokImagineImage,
	GatewayImageModelXaiGrokImagineImagePro,
}

// GatewayVideoModelID constants mirror gateway-video-model-settings.ts.
const (
	GatewayVideoModelAlibabaWanV25T2vPreview      GatewayVideoModelID = "alibaba/wan-v2.5-t2v-preview"
	GatewayVideoModelAlibabaWanV26I2v             GatewayVideoModelID = "alibaba/wan-v2.6-i2v"
	GatewayVideoModelAlibabaWanV26I2vFlash        GatewayVideoModelID = "alibaba/wan-v2.6-i2v-flash"
	GatewayVideoModelAlibabaWanV26R2v             GatewayVideoModelID = "alibaba/wan-v2.6-r2v"
	GatewayVideoModelAlibabaWanV26R2vFlash        GatewayVideoModelID = "alibaba/wan-v2.6-r2v-flash"
	GatewayVideoModelAlibabaWanV26T2v             GatewayVideoModelID = "alibaba/wan-v2.6-t2v"
	GatewayVideoModelBytedanceSeedance20          GatewayVideoModelID = "bytedance/seedance-2.0"
	GatewayVideoModelBytedanceSeedance20Fast      GatewayVideoModelID = "bytedance/seedance-2.0-fast"
	GatewayVideoModelBytedanceSeedanceV10LiteI2v  GatewayVideoModelID = "bytedance/seedance-v1.0-lite-i2v"
	GatewayVideoModelBytedanceSeedanceV10LiteT2v  GatewayVideoModelID = "bytedance/seedance-v1.0-lite-t2v"
	GatewayVideoModelBytedanceSeedanceV10Pro      GatewayVideoModelID = "bytedance/seedance-v1.0-pro"
	GatewayVideoModelBytedanceSeedanceV10ProFast  GatewayVideoModelID = "bytedance/seedance-v1.0-pro-fast"
	GatewayVideoModelBytedanceSeedanceV15Pro      GatewayVideoModelID = "bytedance/seedance-v1.5-pro"
	GatewayVideoModelGoogleVeo30FastGenerate001   GatewayVideoModelID = "google/veo-3.0-fast-generate-001"
	GatewayVideoModelGoogleVeo30Generate001       GatewayVideoModelID = "google/veo-3.0-generate-001"
	GatewayVideoModelGoogleVeo31FastGenerate001   GatewayVideoModelID = "google/veo-3.1-fast-generate-001"
	GatewayVideoModelGoogleVeo31Generate001       GatewayVideoModelID = "google/veo-3.1-generate-001"
	GatewayVideoModelKlingaiKlingV25TurboI2v      GatewayVideoModelID = "klingai/kling-v2.5-turbo-i2v"
	GatewayVideoModelKlingaiKlingV25TurboT2v      GatewayVideoModelID = "klingai/kling-v2.5-turbo-t2v"
	GatewayVideoModelKlingaiKlingV26I2v           GatewayVideoModelID = "klingai/kling-v2.6-i2v"
	GatewayVideoModelKlingaiKlingV26MotionControl GatewayVideoModelID = "klingai/kling-v2.6-motion-control"
	GatewayVideoModelKlingaiKlingV26T2v           GatewayVideoModelID = "klingai/kling-v2.6-t2v"
	GatewayVideoModelKlingaiKlingV30I2v           GatewayVideoModelID = "klingai/kling-v3.0-i2v"
	GatewayVideoModelKlingaiKlingV30T2v           GatewayVideoModelID = "klingai/kling-v3.0-t2v"
	GatewayVideoModelXaiGrokImagineVideo          GatewayVideoModelID = "xai/grok-imagine-video"
)

// GatewayVideoModelIDs lists the known gateway video model IDs from the refreshed settings catalog.
var GatewayVideoModelIDs = []GatewayVideoModelID{
	GatewayVideoModelAlibabaWanV25T2vPreview,
	GatewayVideoModelAlibabaWanV26I2v,
	GatewayVideoModelAlibabaWanV26I2vFlash,
	GatewayVideoModelAlibabaWanV26R2v,
	GatewayVideoModelAlibabaWanV26R2vFlash,
	GatewayVideoModelAlibabaWanV26T2v,
	GatewayVideoModelBytedanceSeedance20,
	GatewayVideoModelBytedanceSeedance20Fast,
	GatewayVideoModelBytedanceSeedanceV10LiteI2v,
	GatewayVideoModelBytedanceSeedanceV10LiteT2v,
	GatewayVideoModelBytedanceSeedanceV10Pro,
	GatewayVideoModelBytedanceSeedanceV10ProFast,
	GatewayVideoModelBytedanceSeedanceV15Pro,
	GatewayVideoModelGoogleVeo30FastGenerate001,
	GatewayVideoModelGoogleVeo30Generate001,
	GatewayVideoModelGoogleVeo31FastGenerate001,
	GatewayVideoModelGoogleVeo31Generate001,
	GatewayVideoModelKlingaiKlingV25TurboI2v,
	GatewayVideoModelKlingaiKlingV25TurboT2v,
	GatewayVideoModelKlingaiKlingV26I2v,
	GatewayVideoModelKlingaiKlingV26MotionControl,
	GatewayVideoModelKlingaiKlingV26T2v,
	GatewayVideoModelKlingaiKlingV30I2v,
	GatewayVideoModelKlingaiKlingV30T2v,
	GatewayVideoModelXaiGrokImagineVideo,
}

// GatewayRerankingModelID constants mirror gateway-reranking-model-settings.ts.
const (
	GatewayRerankingModelCohereRerankV35    GatewayRerankingModelID = "cohere/rerank-v3.5"
	GatewayRerankingModelCohereRerankV4Fast GatewayRerankingModelID = "cohere/rerank-v4-fast"
	GatewayRerankingModelCohereRerankV4Pro  GatewayRerankingModelID = "cohere/rerank-v4-pro"
	GatewayRerankingModelVoyageRerank25     GatewayRerankingModelID = "voyage/rerank-2.5"
	GatewayRerankingModelVoyageRerank25Lite GatewayRerankingModelID = "voyage/rerank-2.5-lite"
)

// GatewayRerankingModelIDs lists the known gateway reranking model IDs from the refreshed settings catalog.
var GatewayRerankingModelIDs = []GatewayRerankingModelID{
	GatewayRerankingModelCohereRerankV35,
	GatewayRerankingModelCohereRerankV4Fast,
	GatewayRerankingModelCohereRerankV4Pro,
	GatewayRerankingModelVoyageRerank25,
	GatewayRerankingModelVoyageRerank25Lite,
}
