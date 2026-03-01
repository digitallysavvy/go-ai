package cerebras

// Language model ID constants for Cerebras inference models.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://inference-docs.cerebras.ai/models/overview for the full list.
//
// Note: Previously deprecated model IDs (e.g., llama-3.1-70b, llama-3.3-70b) have been
// removed. Only current production and preview models are listed here.
const (
	// ─── Production models ───────────────────────────────────────────────────

	// ModelLlama31_8B — Llama 3.1 8B instruction-tuned model (production)
	ModelLlama31_8B = "llama3.1-8b"

	// ModelGPTOSS120B — GPT OSS 120B model (production)
	ModelGPTOSS120B = "gpt-oss-120b"

	// ─── Preview models ──────────────────────────────────────────────────────

	// ModelQwen3_235BA22BInstruct2507 — Qwen 3 235B A22B instruct model (preview)
	ModelQwen3_235BA22BInstruct2507 = "qwen-3-235b-a22b-instruct-2507"

	// ModelQwen3_235BA22BThinking2507 — Qwen 3 235B A22B thinking model (preview)
	ModelQwen3_235BA22BThinking2507 = "qwen-3-235b-a22b-thinking-2507"

	// ModelZaiGLM4_6 — ZAI GLM 4.6 model (preview)
	ModelZaiGLM4_6 = "zai-glm-4.6"

	// ModelZaiGLM4_7 — ZAI GLM 4.7 model (preview)
	ModelZaiGLM4_7 = "zai-glm-4.7"
)
