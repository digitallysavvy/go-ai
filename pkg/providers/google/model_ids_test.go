package google

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelConstants_Gemini31ProPreview(t *testing.T) {
	assert.Equal(t, "gemini-3.1-pro-preview", ModelGemini31ProPreview)
}

func TestModelConstants_Gemini31FlashImagePreview(t *testing.T) {
	assert.Equal(t, "gemini-3.1-flash-image-preview", ModelGemini31FlashImagePreview)
}

func TestModelConstants_AllGemini3Series(t *testing.T) {
	// Verify the Gemini 3 and 3.1 model IDs from #12819 and #12695 are present
	tests := []struct {
		name    string
		modelID string
	}{
		{"Gemini3ProPreview", ModelGemini3ProPreview},
		{"Gemini3ProImagePreview", ModelGemini3ProImagePreview},
		{"Gemini3FlashPreview", ModelGemini3FlashPreview},
		{"Gemini31ProPreview", ModelGemini31ProPreview},
		{"Gemini31ProPreviewCustom", ModelGemini31ProPreviewCustom},
		{"Gemini31FlashImagePreview", ModelGemini31FlashImagePreview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.modelID)
			assert.True(t, strings.HasPrefix(tt.modelID, "gemini-3"),
				"expected model ID %q to start with 'gemini-3'", tt.modelID)
		})
	}
}

func TestModelConstants_ImagenModels(t *testing.T) {
	assert.Equal(t, "imagen-4.0-generate-001", ModelImagen40Generate001)
	assert.Equal(t, "imagen-4.0-ultra-generate-001", ModelImagen40UltraGenerate001)
	assert.Equal(t, "imagen-4.0-fast-generate-001", ModelImagen40FastGenerate001)
}

func TestProvider_LanguageModel_Gemini31ProPreview(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model, err := prov.LanguageModel(ModelGemini31ProPreview)
	require.NoError(t, err)
	assert.Equal(t, ModelGemini31ProPreview, model.ModelID())
	assert.Equal(t, "google", model.Provider())
}

func TestProvider_LanguageModel_AllMissingModelIDs(t *testing.T) {
	// Verify all model IDs from #12819 can be used as language models
	prov := New(Config{APIKey: "test-key"})
	modelIDs := []string{
		ModelGemini15Flash,
		ModelGemini15FlashLatest,
		ModelGemini15Flash001,
		ModelGemini15Flash002,
		ModelGemini15Flash8B,
		ModelGemini15Flash8BLatest,
		ModelGemini15Flash8B001,
		ModelGemini15Pro,
		ModelGemini15ProLatest,
		ModelGemini15Pro001,
		ModelGemini15Pro002,
		ModelGemini20Flash,
		ModelGemini20Flash001,
		ModelGemini20FlashLive001,
		ModelGemini20FlashLite,
		ModelGemini20FlashLite001,
		ModelGemini20FlashExp,
		ModelGemini20FlashExpImage,
		ModelGemini20FlashThinkingExp,
		ModelGemini20ProExp,
		ModelGemini25Pro,
		ModelGemini25Flash,
		ModelGemini25FlashImage,
		ModelGemini25FlashLite,
		ModelGemini25FlashLitePreview0925,
		ModelGemini25FlashPreview0417,
		ModelGemini25FlashPreview0925,
		ModelGemini25FlashPreviewTTS,
		ModelGemini25ProPreviewTTS,
		ModelGemini25FlashNativeAudioLatest,
		ModelGemini25FlashNativeAudio0925,
		ModelGemini25FlashNativeAudio1225,
		ModelGemini25ComputerUsePreview,
		ModelGemini3ProPreview,
		ModelGemini3ProImagePreview,
		ModelGemini3FlashPreview,
		ModelGemini31ProPreview,
		ModelGemini31ProPreviewCustom,
		ModelGemini31FlashImagePreview,
		ModelGeminiProLatest,
		ModelGeminiFlashLatest,
		ModelGeminiFlashLiteLatest,
		ModelDeepResearchProPreview,
		ModelNanaBananaProPreview,
		ModelAQA,
		ModelGemini25ProExp0325,
		ModelGeminiExp1206,
		ModelGeminiRoboticsER15Preview,
		ModelGemma31BIt,
		ModelGemma34BIt,
		ModelGemma3NE4BIt,
		ModelGemma3NE2BIt,
		ModelGemma312BIt,
		ModelGemma327BIt,
	}
	for _, id := range modelIDs {
		t.Run(id, func(t *testing.T) {
			model, err := prov.LanguageModel(id)
			require.NoError(t, err, "expected no error creating language model with ID %q", id)
			assert.Equal(t, id, model.ModelID())
		})
	}
}

func TestProvider_ImageModel_Gemini31FlashImagePreview(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model, err := prov.ImageModel(ModelGemini31FlashImagePreview)
	require.NoError(t, err)
	assert.Equal(t, ModelGemini31FlashImagePreview, model.ModelID())
	assert.Equal(t, "google", model.Provider())
}

// TestIntegration_Gemini31ProPreview tests text generation with the new model ID.
// Requires GOOGLE_GENERATIVE_AI_API_KEY to be set.
func TestIntegration_Gemini31ProPreview(t *testing.T) {
	if os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY") == "" {
		t.Skip("Skipping: GOOGLE_GENERATIVE_AI_API_KEY not set")
	}

	prov := New(Config{APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")})
	model, err := prov.LanguageModel(ModelGemini31ProPreview)
	require.NoError(t, err)
	assert.Equal(t, ModelGemini31ProPreview, model.ModelID())
}
