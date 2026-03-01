package googlevertex

// Language model ID constants for Google Vertex AI
// Vertex AI supports the same Gemini models as Google Generative AI
// but accessed through Vertex AI endpoints with additional enterprise features.
const (
	// Gemini 1.5 series
	ModelGemini15Pro    = "gemini-1.5-pro"
	ModelGemini15Flash  = "gemini-1.5-flash"
	ModelGemini15Flash8B = "gemini-1.5-flash-8b"

	// Gemini 2.0 series
	ModelGemini20Flash    = "gemini-2.0-flash"
	ModelGemini20FlashExp = "gemini-2.0-flash-exp"
	ModelGemini20FlashLite = "gemini-2.0-flash-lite"

	// Gemini 2.5 series
	ModelGemini25Pro         = "gemini-2.5-pro"
	ModelGemini25Flash       = "gemini-2.5-flash"
	ModelGemini25FlashImage  = "gemini-2.5-flash-image"

	// Gemini 3 series — added in #12819
	ModelGemini3ProPreview      = "gemini-3-pro-preview"
	ModelGemini3ProImagePreview = "gemini-3-pro-image-preview"
	ModelGemini3FlashPreview    = "gemini-3-flash-preview"

	// Gemini 3.1 series — added in #12695 and #12883
	ModelGemini31ProPreview        = "gemini-3.1-pro-preview"         // language model (#12695)
	ModelGemini31FlashImagePreview = "gemini-3.1-flash-image-preview" // image model (#12883)

	// Legacy models
	// Deprecated: Use ModelGemini15Pro instead
	ModelGeminiPro = "gemini-pro"
	// Deprecated: Use ModelGemini15Pro instead (supports multimodal by default)
	ModelGeminiProVision = "gemini-pro-vision"
)

// Imagen model ID constants for Google Vertex AI image generation
const (
	ModelImagen30Generate001     = "imagen-3.0-generate-001"
	ModelImagen30Generate002     = "imagen-3.0-generate-002"
	ModelImagen30FastGenerate001 = "imagen-3.0-fast-generate-001"
	ModelImagen40Generate001     = "imagen-4.0-generate-001"
	ModelImagen40UltraGenerate001 = "imagen-4.0-ultra-generate-001"
	ModelImagen40FastGenerate001  = "imagen-4.0-fast-generate-001"
)

// VertexImageSize constants for the sampleImageSize parameter in Vertex AI image generation.
// Controls the output resolution of generated images.
const (
	VertexImageSize1K = "1K"
	VertexImageSize2K = "2K"
)
