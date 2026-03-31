package xai

// Language model ID constants for xAI Grok language models.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://docs.x.ai/docs for the full list.
//
// Removed model IDs (XAI shut down their APIs — do not re-add):
//   - "grok-2"              (use grok-3 or later)
//   - "grok-2-vision-1212"  (use a current multimodal model instead)
const (
	// ModelGrokBeta — Grok Beta language model (default)
	ModelGrokBeta = "grok-beta"

	// ModelGrok3 — Grok 3 language model
	ModelGrok3 = "grok-3"

	// ModelGrok3Latest — Grok 3 language model (latest alias)
	ModelGrok3Latest = "grok-3-latest"

	// ModelGrok3Mini — Grok 3 Mini language model (faster, cheaper)
	ModelGrok3Mini = "grok-3-mini"

	// ModelGrok3MiniLatest — Grok 3 Mini language model (latest alias)
	ModelGrok3MiniLatest = "grok-3-mini-latest"

	// ModelGrok4 — Grok 4 language model
	ModelGrok4 = "grok-4"

	// ModelGrok40709 — Grok 4 dated release (2025-07-09)
	ModelGrok40709 = "grok-4-0709"

	// ModelGrok4Latest — Grok 4 language model (latest alias)
	ModelGrok4Latest = "grok-4-latest"

	// ModelGrok4FastReasoning — Grok 4 fast reasoning model
	ModelGrok4FastReasoning = "grok-4-fast-reasoning"

	// ModelGrok4FastNonReasoning — Grok 4 fast non-reasoning model
	ModelGrok4FastNonReasoning = "grok-4-fast-non-reasoning"

	// ModelGrok41FastReasoning — Grok 4.1 fast reasoning model
	ModelGrok41FastReasoning = "grok-4-1-fast-reasoning"

	// ModelGrok41FastNonReasoning — Grok 4.1 fast non-reasoning model
	ModelGrok41FastNonReasoning = "grok-4-1-fast-non-reasoning"

	// ModelGrokCodeFast1 — Grok Code fast model
	ModelGrokCodeFast1 = "grok-code-fast-1"

	// ModelGrok420MultiAgent — Grok 4.20 multi-agent model (GA)
	ModelGrok420MultiAgent = "grok-4.20-multi-agent"

	// ModelGrok420MultiAgent0309 — Grok 4.20 multi-agent dated release (2025-03-09)
	ModelGrok420MultiAgent0309 = "grok-4.20-multi-agent-0309"

	// ModelGrok420NonReasoning — Grok 4.20 non-reasoning model (GA)
	ModelGrok420NonReasoning = "grok-4.20-non-reasoning"

	// ModelGrok4200309NonReasoning — Grok 4.20 non-reasoning dated release (2025-03-09)
	ModelGrok4200309NonReasoning = "grok-4.20-0309-non-reasoning"

	// ModelGrok420Reasoning — Grok 4.20 reasoning model (GA)
	ModelGrok420Reasoning = "grok-4.20-reasoning"

	// ModelGrok4200309Reasoning — Grok 4.20 reasoning dated release (2025-03-09)
	ModelGrok4200309Reasoning = "grok-4.20-0309-reasoning"
)

// Image model ID constants for xAI Grok image generation models.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://docs.x.ai/docs for the full list.
const (
	// ModelGrok2Image — Grok 2 image generation model (latest alias)
	ModelGrok2Image = "grok-2-image"

	// ModelGrok2Image1212 — Grok 2 image generation model (dated release)
	ModelGrok2Image1212 = "grok-2-image-1212"

	// ModelGrokImagineImage — Grok Imagine standard image generation model
	ModelGrokImagineImage = "grok-imagine-image"

	// ModelGrokImagineImagePro — Grok Imagine Pro image generation model (higher quality)
	ModelGrokImagineImagePro = "grok-imagine-image-pro"
)
