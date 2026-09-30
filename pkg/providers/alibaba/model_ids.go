package alibaba

// Language model ID constants for Alibaba Qwen models.
// Use these constants instead of raw strings to avoid typos and get IDE
// support. Any model ID is accepted by Provider.LanguageModel; this list
// mirrors the TypeScript SDK's AlibabaChatModelId documentation set.
// See https://www.alibabacloud.com/help/en/model-studio/models
const (
	// ─── Commercial edition — hybrid-thinking mode (disabled by default) ──────

	// ModelQwen3_7Max — Qwen3.7 Max (commercial, hybrid-thinking)
	ModelQwen3_7Max = "qwen3.7-max"
	// ModelQwen3Max — Qwen3 Max (commercial, hybrid-thinking)
	ModelQwen3Max = "qwen3-max"
	// ModelQwen3MaxPreview — Qwen3 Max preview
	ModelQwen3MaxPreview = "qwen3-max-preview"
	// ModelQwenPlus — Qwen Plus
	ModelQwenPlus = "qwen-plus"
	// ModelQwenPlusLatest — Qwen Plus (latest)
	ModelQwenPlusLatest = "qwen-plus-latest"
	// ModelQwenFlash — Qwen Flash
	ModelQwenFlash = "qwen-flash"
	// ModelQwenTurbo — Qwen Turbo
	ModelQwenTurbo = "qwen-turbo"
	// ModelQwenTurboLatest — Qwen Turbo (latest)
	ModelQwenTurboLatest = "qwen-turbo-latest"

	// ─── Open-source edition — hybrid-thinking mode (enabled by default) ─────

	// ModelQwen3_235BA22B — Qwen3 235B A22B
	ModelQwen3_235BA22B = "qwen3-235b-a22b"
	// ModelQwen3_32B — Qwen3 32B
	ModelQwen3_32B = "qwen3-32b"
	// ModelQwen3_30BA3B — Qwen3 30B A3B
	ModelQwen3_30BA3B = "qwen3-30b-a3b"
	// ModelQwen3_14B — Qwen3 14B
	ModelQwen3_14B = "qwen3-14b"

	// ─── Thinking-only mode ───────────────────────────────────────────────────

	// ModelQwen3Next80BA3BThinking — Qwen3-Next 80B A3B (thinking-only)
	ModelQwen3Next80BA3BThinking = "qwen3-next-80b-a3b-thinking"
	// ModelQwen3_235BA22BThinking2507 — Qwen3 235B A22B thinking (2507)
	ModelQwen3_235BA22BThinking2507 = "qwen3-235b-a22b-thinking-2507"
	// ModelQwen3_30BA3BThinking2507 — Qwen3 30B A3B thinking (2507)
	ModelQwen3_30BA3BThinking2507 = "qwen3-30b-a3b-thinking-2507"
	// ModelQwqPlus — QwQ Plus
	ModelQwqPlus = "qwq-plus"
	// ModelQwqPlusLatest — QwQ Plus (latest)
	ModelQwqPlusLatest = "qwq-plus-latest"
	// ModelQwq32B — QwQ 32B
	ModelQwq32B = "qwq-32b"

	// ─── Code models ──────────────────────────────────────────────────────────

	// ModelQwenCoder — Qwen Coder
	ModelQwenCoder = "qwen-coder"
	// ModelQwen3CoderPlus — Qwen3 Coder Plus
	ModelQwen3CoderPlus = "qwen3-coder-plus"
	// ModelQwen3CoderFlash — Qwen3 Coder Flash
	ModelQwen3CoderFlash = "qwen3-coder-flash"
)
