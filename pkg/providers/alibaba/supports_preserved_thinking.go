package alibaba

// preservedThinkingModelIDs lists the model IDs that support Alibaba's
// preserved-thinking mode (preserve_thinking), which replays assistant
// reasoning_content from previous turns.
// https://docs.qwencloud.com/developer-guides/text-generation/thinking#preserve-thinking-in-multi-turn
var preservedThinkingModelIDs = map[string]bool{
	"kimi-k2.7-code":           true,
	"qwen3.6-max-preview":      true,
	"qwen3.6-plus":             true,
	"qwen3.6-plus-2026-04-02":  true,
	"qwen3.7-flash":            true,
	"qwen3.7-flash-2026-07-15": true,
	"qwen3.7-max":              true,
	"qwen3.7-max-2026-05-17":   true,
	"qwen3.7-max-2026-05-20":   true,
	"qwen3.7-max-2026-06-08":   true,
	"qwen3.7-max-preview":      true,
	"qwen3.7-plus":             true,
	"qwen3.7-plus-2026-05-26":  true,
	"qwen3.8-flash":            true,
	"qwen3.8-max":              true,
	"qwen3.8-max-0902":         true,
}

// supportsPreservedThinking reports whether modelID supports Alibaba's
// preserved-thinking mode, mirroring the TypeScript SDK's
// supportsPreservedThinking.
func supportsPreservedThinking(modelID string) bool {
	return preservedThinkingModelIDs[modelID]
}
