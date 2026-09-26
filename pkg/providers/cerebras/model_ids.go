package cerebras

// Language model ID constants for Cerebras inference models.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://inference-docs.cerebras.ai/models/overview for the full list.
//
// Cerebras accepts any model ID string; this list mirrors the TypeScript
// SDK's CerebrasChatModelId autocomplete set exactly (production models
// only). Previously listed preview/deprecated model IDs (llama3.1-8b,
// qwen-3-235b-a22b-instruct-2507, qwen-3-235b-a22b-thinking-2507,
// zai-glm-4.6, zai-glm-4.7) were removed from the TS catalog and are removed
// here too.
const (
	// ModelGPTOSS120B — GPT OSS 120B model (production)
	ModelGPTOSS120B = "gpt-oss-120b"

	// ModelGemma4_31B — Gemma 4 31B model (production)
	ModelGemma4_31B = "gemma-4-31b"
)
