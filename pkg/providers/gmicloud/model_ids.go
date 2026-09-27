package gmicloud

// Language model ID constants for GMI Cloud chat models.
// Catalog: GET https://api.gmi-serving.com/v1/models
// Mirrors packages/gmicloud/src/gmicloud-chat-options.ts (GmicloudChatModelId)
// at ai@7.0.113. Any model ID string is accepted; these constants exist for
// convenience and IDE support.
const (
	ModelDeepSeekV4FlashChat = "deepseek-ai/DeepSeek-V4-Flash-0731"
	ModelQwen38Max           = "Qwen/Qwen3.8-Max"
	ModelKimiK3              = "moonshotai/kimi-k3"
	ModelGLM52FP8            = "zai-org/GLM-5.2-FP8"
	ModelMiniMaxM3           = "MiniMaxAI/MiniMax-M3"
)
