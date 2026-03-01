package google

// Language model ID constants for Google Generative AI (Gemini)
// Reflects the full model list from packages/google/src/google-generative-ai-options.ts
// Updated to include all models from commit #12819 and #12695.
const (
	// Gemini 1.5 series
	ModelGemini15Flash          = "gemini-1.5-flash"
	ModelGemini15FlashLatest    = "gemini-1.5-flash-latest"
	ModelGemini15Flash001       = "gemini-1.5-flash-001"
	ModelGemini15Flash002       = "gemini-1.5-flash-002"
	ModelGemini15Flash8B        = "gemini-1.5-flash-8b"
	ModelGemini15Flash8BLatest  = "gemini-1.5-flash-8b-latest"
	ModelGemini15Flash8B001     = "gemini-1.5-flash-8b-001"
	ModelGemini15Pro            = "gemini-1.5-pro"
	ModelGemini15ProLatest      = "gemini-1.5-pro-latest"
	ModelGemini15Pro001         = "gemini-1.5-pro-001"
	ModelGemini15Pro002         = "gemini-1.5-pro-002"

	// Gemini 2.0 series
	ModelGemini20Flash            = "gemini-2.0-flash"
	ModelGemini20Flash001         = "gemini-2.0-flash-001"
	ModelGemini20FlashLive001     = "gemini-2.0-flash-live-001"
	ModelGemini20FlashLite        = "gemini-2.0-flash-lite"
	ModelGemini20FlashLite001     = "gemini-2.0-flash-lite-001"
	ModelGemini20FlashExp         = "gemini-2.0-flash-exp"
	ModelGemini20FlashExpImage    = "gemini-2.0-flash-exp-image-generation"
	ModelGemini20FlashThinkingExp = "gemini-2.0-flash-thinking-exp-01-21"
	ModelGemini20ProExp           = "gemini-2.0-pro-exp-02-05"

	// Gemini 2.5 series
	ModelGemini25Pro                      = "gemini-2.5-pro"
	ModelGemini25Flash                    = "gemini-2.5-flash"
	ModelGemini25FlashImage               = "gemini-2.5-flash-image"
	ModelGemini25FlashLite                = "gemini-2.5-flash-lite"
	ModelGemini25FlashLitePreview0925     = "gemini-2.5-flash-lite-preview-09-2025"
	ModelGemini25FlashPreview0417         = "gemini-2.5-flash-preview-04-17"
	ModelGemini25FlashPreview0925         = "gemini-2.5-flash-preview-09-2025"
	ModelGemini25FlashPreviewTTS          = "gemini-2.5-flash-preview-tts"
	ModelGemini25ProPreviewTTS            = "gemini-2.5-pro-preview-tts"
	ModelGemini25FlashNativeAudioLatest   = "gemini-2.5-flash-native-audio-latest"
	ModelGemini25FlashNativeAudio0925     = "gemini-2.5-flash-native-audio-preview-09-2025"
	ModelGemini25FlashNativeAudio1225     = "gemini-2.5-flash-native-audio-preview-12-2025"
	ModelGemini25ComputerUsePreview       = "gemini-2.5-computer-use-preview-10-2025"

	// Gemini 3 series — added in #12819
	ModelGemini3ProPreview      = "gemini-3-pro-preview"
	ModelGemini3ProImagePreview = "gemini-3-pro-image-preview"
	ModelGemini3FlashPreview    = "gemini-3-flash-preview"

	// Gemini 3.1 series — added in #12695 and #12883
	ModelGemini31ProPreview        = "gemini-3.1-pro-preview"         // language model (#12695)
	ModelGemini31ProPreviewCustom  = "gemini-3.1-pro-preview-customtools" // (#12819)
	ModelGemini31FlashImagePreview = "gemini-3.1-flash-image-preview" // image model (#12883)

	// Latest alias models — added in #12819
	ModelGeminiProLatest       = "gemini-pro-latest"
	ModelGeminiFlashLatest     = "gemini-flash-latest"
	ModelGeminiFlashLiteLatest = "gemini-flash-lite-latest"

	// Specialized models — added in #12819
	ModelDeepResearchProPreview = "deep-research-pro-preview-12-2025"
	ModelNanaBananaProPreview   = "nano-banana-pro-preview"
	ModelAQA                   = "aqa"

	// Experimental models — added in #12819
	ModelGemini25ProExp0325          = "gemini-2.5-pro-exp-03-25"
	ModelGeminiExp1206               = "gemini-exp-1206"
	ModelGeminiRoboticsER15Preview   = "gemini-robotics-er-1.5-preview"

	// Gemma open models — added in #12819
	ModelGemma31BIt  = "gemma-3-1b-it"
	ModelGemma34BIt  = "gemma-3-4b-it"
	ModelGemma3NE4BIt = "gemma-3n-e4b-it"
	ModelGemma3NE2BIt = "gemma-3n-e2b-it"
	ModelGemma312BIt  = "gemma-3-12b-it"
	ModelGemma327BIt  = "gemma-3-27b-it"
)

// Imagen model IDs for Google Generative AI image generation (use :predict API)
const (
	ModelImagen40Generate001     = "imagen-4.0-generate-001"
	ModelImagen40UltraGenerate001 = "imagen-4.0-ultra-generate-001"
	ModelImagen40FastGenerate001  = "imagen-4.0-fast-generate-001"
)

// Gemini image model IDs for Google Generative AI (use :generateContent API)
// These are multimodal output language models that produce images.
// The constants below are aliases to the language model constants above for clarity.
// ModelGemini25FlashImage, ModelGemini3ProImagePreview, and ModelGemini31FlashImagePreview
// can all be used with provider.ImageModel() directly.
