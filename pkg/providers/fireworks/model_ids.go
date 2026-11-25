package fireworks

// Model ID constants mirror the TypeScript SDK model ID types at 0f3a85f92d (TS catalog
// refactors eaaead397d, a60ddb4a8f, c296443f12). Any model ID string is
// accepted; these constants only document the known IDs.

// Chat model IDs (FireworksChatModelId in fireworks/src/fireworks-chat-options.ts).
const (
	ModelDeepseekV3                  = "accounts/fireworks/models/deepseek-v3"
	ModelLlamaV3p3_70bInstruct       = "accounts/fireworks/models/llama-v3p3-70b-instruct"
	ModelLlamaV3p2_3bInstruct        = "accounts/fireworks/models/llama-v3p2-3b-instruct"
	ModelLlamaV3p1_405bInstruct      = "accounts/fireworks/models/llama-v3p1-405b-instruct"
	ModelLlamaV3p1_8bInstruct        = "accounts/fireworks/models/llama-v3p1-8b-instruct"
	ModelMixtral8x7bInstruct         = "accounts/fireworks/models/mixtral-8x7b-instruct"
	ModelMixtral8x22bInstruct        = "accounts/fireworks/models/mixtral-8x22b-instruct"
	ModelMixtral8x7bInstructHf       = "accounts/fireworks/models/mixtral-8x7b-instruct-hf"
	ModelQwen2p5Coder32bInstruct     = "accounts/fireworks/models/qwen2p5-coder-32b-instruct"
	ModelQwen2p5_72bInstruct         = "accounts/fireworks/models/qwen2p5-72b-instruct"
	ModelQwenQwq32bPreview           = "accounts/fireworks/models/qwen-qwq-32b-preview"
	ModelQwen2Vl72bInstruct          = "accounts/fireworks/models/qwen2-vl-72b-instruct"
	ModelLlamaV3p2_11bVisionInstruct = "accounts/fireworks/models/llama-v3p2-11b-vision-instruct"
	ModelQwq32b                      = "accounts/fireworks/models/qwq-32b"
	ModelYiLarge                     = "accounts/fireworks/models/yi-large"
	ModelKimiK2Instruct              = "accounts/fireworks/models/kimi-k2-instruct"
	ModelKimiK2Thinking              = "accounts/fireworks/models/kimi-k2-thinking"
	ModelKimiK2p6                    = "accounts/fireworks/models/kimi-k2p6"
	ModelMinimaxM2                   = "accounts/fireworks/models/minimax-m2"
)

// Completion model IDs (FireworksCompletionModelId in fireworks/src/fireworks-completion-options.ts).
const (
	ModelLlamaV3_8bInstruct = "accounts/fireworks/models/llama-v3-8b-instruct"
	ModelLlamaV2_34bCode    = "accounts/fireworks/models/llama-v2-34b-code"
)

// Embedding model IDs (FireworksEmbeddingModelId in fireworks/src/fireworks-embedding-options.ts).
const (
	ModelNomicAiNomicEmbedTextV1_5 = "nomic-ai/nomic-embed-text-v1.5"
)

// Image model IDs (FireworksImageModelId in fireworks/src/fireworks-image-options.ts).
const (
	ModelFlux1DevFp8                    = "accounts/fireworks/models/flux-1-dev-fp8"
	ModelFlux1SchnellFp8                = "accounts/fireworks/models/flux-1-schnell-fp8"
	ModelFluxKontextPro                 = "accounts/fireworks/models/flux-kontext-pro"
	ModelFluxKontextMax                 = "accounts/fireworks/models/flux-kontext-max"
	ModelPlaygroundV2_5_1024pxAesthetic = "accounts/fireworks/models/playground-v2-5-1024px-aesthetic"
	ModelJapaneseStableDiffusionXl      = "accounts/fireworks/models/japanese-stable-diffusion-xl"
	ModelPlaygroundV2_1024pxAesthetic   = "accounts/fireworks/models/playground-v2-1024px-aesthetic"
	ModelSSD1B                          = "accounts/fireworks/models/SSD-1B"
	ModelStableDiffusionXl1024V1_0      = "accounts/fireworks/models/stable-diffusion-xl-1024-v1-0"
)
