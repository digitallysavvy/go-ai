package googlevertex

// Language model ID constants for Google Vertex AI
// Reflects the full model list from packages/google-vertex/src/google-vertex-options.ts
// Vertex AI supports the same Gemini models as Google Generative AI
// but accessed through Vertex AI endpoints with additional enterprise features.
const (
	EmbeddingModelGeminiEmbedding2        = "gemini-embedding-2"
	EmbeddingModelGeminiEmbedding2Preview = "gemini-embedding-2-preview"
)

// Gemini TTS speech model ID constants for Google Vertex AI.
const (
	SpeechModelGemini25FlashTTS            = "gemini-2.5-flash-tts"
	SpeechModelGemini25ProTTS              = "gemini-2.5-pro-tts"
	SpeechModelGemini25FlashLitePreviewTTS = "gemini-2.5-flash-lite-preview-tts"
	SpeechModelGemini31FlashTTSPreview     = "gemini-3.1-flash-tts-preview"
)

const (
	InteractionsAgentDeepResearchPreview042026 = "deep-research-preview-04-2026"
	InteractionsAgentDeepResearchMax042026     = "deep-research-max-preview-04-2026"
)

const (
	// Gemini 1.0 series (legacy)
	ModelGemini10Pro          = "gemini-1.0-pro"
	ModelGemini10Pro001       = "gemini-1.0-pro-001"
	ModelGemini10Pro002       = "gemini-1.0-pro-002"
	ModelGemini10ProVision001 = "gemini-1.0-pro-vision-001"

	// Gemini 1.5 series
	ModelGemini15Pro      = "gemini-1.5-pro"
	ModelGemini15Pro001   = "gemini-1.5-pro-001"
	ModelGemini15Pro002   = "gemini-1.5-pro-002"
	ModelGemini15Flash    = "gemini-1.5-flash"
	ModelGemini15Flash001 = "gemini-1.5-flash-001"
	ModelGemini15Flash002 = "gemini-1.5-flash-002"
	ModelGemini15Flash8B  = "gemini-1.5-flash-8b"

	// Gemini 2.0 series
	ModelGemini20Flash     = "gemini-2.0-flash"
	ModelGemini20Flash001  = "gemini-2.0-flash-001"
	ModelGemini20FlashExp  = "gemini-2.0-flash-exp"
	ModelGemini20FlashLite = "gemini-2.0-flash-lite"
	ModelGemini20ProExp    = "gemini-2.0-pro-exp-02-05"

	// Gemini 2.5 series
	ModelGemini25Pro        = "gemini-2.5-pro"
	ModelGemini25Flash      = "gemini-2.5-flash"
	ModelGemini25FlashImage = "gemini-2.5-flash-image"
	ModelGemini25FlashLite  = "gemini-2.5-flash-lite"

	// Gemini 3 series — added in #12819
	ModelGemini3ProPreview      = "gemini-3-pro-preview"
	ModelGemini3ProImagePreview = "gemini-3-pro-image-preview"
	ModelGemini3FlashPreview    = "gemini-3-flash-preview"

	// Gemini 3.1 series — added in #12695 and #12883
	ModelGemini31ProPreview        = "gemini-3.1-pro-preview"         // language model (#12695)
	ModelGemini31FlashLitePreview  = "gemini-3.1-flash-lite-preview"  // (#12883)
	ModelGemini31FlashImagePreview = "gemini-3.1-flash-image-preview" // image model (#12883)

	// Preview models
	ModelGemini20FlashLitePreview0205 = "gemini-2.0-flash-lite-preview-02-05"
	ModelGemini25FlashLitePreview0925 = "gemini-2.5-flash-lite-preview-09-2025"
	ModelGemini25FlashPreview0925     = "gemini-2.5-flash-preview-09-2025"

	// Legacy models
	// Deprecated: Use ModelGemini15Pro instead
	ModelGeminiPro = "gemini-pro"
	// Deprecated: Use ModelGemini15Pro instead (supports multimodal by default)
	ModelGeminiProVision = "gemini-pro-vision"
)

// Imagen model ID constants for Google Vertex AI image generation
const (
	ModelImagen30Generate001      = "imagen-3.0-generate-001"
	ModelImagen30Generate002      = "imagen-3.0-generate-002"
	ModelImagen30FastGenerate001  = "imagen-3.0-fast-generate-001"
	ModelImagen40Generate001      = "imagen-4.0-generate-001"
	ModelImagen40UltraGenerate001 = "imagen-4.0-ultra-generate-001"
	ModelImagen40FastGenerate001  = "imagen-4.0-fast-generate-001"
)

// VertexImageSize constants for the sampleImageSize parameter in Vertex AI image generation.
// Controls the output resolution of generated images.
const (
	VertexImageSize1K = "1K"
	VertexImageSize2K = "2K"
)
