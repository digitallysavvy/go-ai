package googlevertex

// Gemini model IDs supported by Google Vertex AI
// These models use the same Gemini models as Google Generative AI
// but are accessed through Vertex AI endpoints with additional enterprise features
const (
	// ModelGemini15Pro is the Gemini 1.5 Pro model
	// Best for complex reasoning, planning, and understanding
	ModelGemini15Pro = "gemini-1.5-pro"

	// ModelGemini15Flash is the Gemini 1.5 Flash model
	// Optimized for speed and efficiency with good quality
	ModelGemini15Flash = "gemini-1.5-flash"

	// ModelGemini15Flash8B is the lightweight Gemini 1.5 Flash 8B model
	// Smallest and fastest model for high-volume, low-latency use cases
	ModelGemini15Flash8B = "gemini-1.5-flash-8b"

	// ModelGemini20FlashExp is the experimental Gemini 2.0 Flash model
	// Preview of next-generation capabilities
	ModelGemini20FlashExp = "gemini-2.0-flash-exp"

	// ModelGeminiPro is the legacy Gemini Pro model
	// Deprecated: Use ModelGemini15Pro instead
	ModelGeminiPro = "gemini-pro"

	// ModelGeminiProVision is the legacy Gemini Pro Vision model
	// Deprecated: Use ModelGemini15Pro instead (supports multimodal by default)
	ModelGeminiProVision = "gemini-pro-vision"
)
