package xai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertXaiUsage_Basic(t *testing.T) {
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.InputTokens)
	assert.Equal(t, int64(100), *result.InputTokens)
	assert.NotNil(t, result.OutputTokens)
	assert.Equal(t, int64(50), *result.OutputTokens)
	assert.NotNil(t, result.TotalTokens)
	assert.Equal(t, int64(150), *result.TotalTokens)
}

func TestConvertXaiUsage_WithCachedTokens_DirectField(t *testing.T) {
	cachedTokens := 30
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CachedTokens:     &cachedTokens,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.InputDetails)
	assert.NotNil(t, result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(30), *result.InputDetails.CacheReadTokens)
	assert.NotNil(t, result.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(70), *result.InputDetails.NoCacheTokens)
}

func TestConvertXaiUsage_WithCachedTokens_NestedField(t *testing.T) {
	cachedTokens := 30
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{
			CachedTokens: &cachedTokens,
		},
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.InputDetails)
	assert.NotNil(t, result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(30), *result.InputDetails.CacheReadTokens)
	assert.NotNil(t, result.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(70), *result.InputDetails.NoCacheTokens)
}

func TestConvertXaiUsage_WithReasoningTokens_DirectField(t *testing.T) {
	reasoningTokens := 20
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ReasoningTokens:  &reasoningTokens,
	}

	result := convertXaiUsage(usage)

	// Reasoning tokens are ADDITIVE in Chat API
	assert.Equal(t, int64(70), *result.OutputTokens) // 50 + 20

	assert.NotNil(t, result.OutputDetails)
	assert.NotNil(t, result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.NotNil(t, result.OutputDetails.TextTokens)
	assert.Equal(t, int64(50), *result.OutputDetails.TextTokens) // CompletionTokens
}

func TestConvertXaiUsage_WithReasoningTokens_NestedField(t *testing.T) {
	reasoningTokens := 20
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{
			ReasoningTokens: &reasoningTokens,
		},
	}

	result := convertXaiUsage(usage)

	// Reasoning tokens are ADDITIVE in Chat API
	assert.Equal(t, int64(70), *result.OutputTokens) // 50 + 20

	assert.NotNil(t, result.OutputDetails)
	assert.NotNil(t, result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.NotNil(t, result.OutputDetails.TextTokens)
	assert.Equal(t, int64(50), *result.OutputDetails.TextTokens) // CompletionTokens
}

func TestConvertXaiUsage_WithImageInputTokens(t *testing.T) {
	imageInputTokens := 40
	textInputTokens := 60
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ImageInputTokens: &imageInputTokens,
		TextInputTokens:  &textInputTokens,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.Raw)
	assert.Equal(t, int64(40), result.Raw["image_input_tokens"])
	assert.Equal(t, int64(60), result.Raw["text_input_tokens"])
}

func TestConvertXaiUsage_WithAllTokenTypes(t *testing.T) {
	cachedTokens := 30
	reasoningTokens := 20
	imageInputTokens := 40
	textInputTokens := 30
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CachedTokens:     &cachedTokens,
		ReasoningTokens:  &reasoningTokens,
		ImageInputTokens: &imageInputTokens,
		TextInputTokens:  &textInputTokens,
	}

	result := convertXaiUsage(usage)

	// Check basic tokens (with reasoning additive)
	assert.Equal(t, int64(100), *result.InputTokens)
	assert.Equal(t, int64(70), *result.OutputTokens)    // 50 + 20
	assert.Equal(t, int64(170), *result.TotalTokens)    // 100 + 70

	// Check input details (cached tokens - inclusive)
	assert.NotNil(t, result.InputDetails)
	assert.Equal(t, int64(30), *result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(70), *result.InputDetails.NoCacheTokens)

	// Check output details (reasoning tokens - additive)
	assert.NotNil(t, result.OutputDetails)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(50), *result.OutputDetails.TextTokens)

	// Check multimodal tokens in raw
	assert.NotNil(t, result.Raw)
	assert.Equal(t, int64(40), result.Raw["image_input_tokens"])
	assert.Equal(t, int64(30), result.Raw["text_input_tokens"])
}

func TestConvertXaiUsage_RawData(t *testing.T) {
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.Raw)
	assert.Equal(t, 100, result.Raw["prompt_tokens"])
	assert.Equal(t, 50, result.Raw["completion_tokens"])
	assert.Equal(t, 150, result.Raw["total_tokens"])
}

func TestConvertXaiUsage_RawData_WithDetails(t *testing.T) {
	cachedTokens := 30
	reasoningTokens := 20
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{
			CachedTokens: &cachedTokens,
		},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{
			ReasoningTokens: &reasoningTokens,
		},
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.Raw)
	assert.Contains(t, result.Raw, "prompt_tokens_details")
	assert.Contains(t, result.Raw, "completion_tokens_details")
}

func TestConvertXaiUsage_PreferDirectFields(t *testing.T) {
	// Test that direct fields take precedence over nested fields
	directCached := 30
	nestedCached := 20
	directReasoning := 25
	nestedReasoning := 15

	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CachedTokens:     &directCached,
		ReasoningTokens:  &directReasoning,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{
			CachedTokens: &nestedCached,
		},
		CompletionTokensDetails: &struct {
			ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
			AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
			RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
		}{
			ReasoningTokens: &nestedReasoning,
		},
	}

	result := convertXaiUsage(usage)

	// Should use direct fields, not nested fields
	assert.Equal(t, int64(30), *result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(25), *result.OutputDetails.ReasoningTokens)
}

func TestConvertXaiUsage_ZeroValues(t *testing.T) {
	usage := xaiUsage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}

	result := convertXaiUsage(usage)

	assert.Equal(t, int64(0), *result.InputTokens)
	assert.Equal(t, int64(0), *result.OutputTokens)
	assert.Equal(t, int64(0), *result.TotalTokens)
}

func TestConvertXaiUsage_ImageOnlyTokens(t *testing.T) {
	imageInputTokens := 100
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ImageInputTokens: &imageInputTokens,
		TextInputTokens:  nil,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.Raw)
	assert.Equal(t, int64(100), result.Raw["image_input_tokens"])
	assert.NotContains(t, result.Raw, "text_input_tokens")
}

func TestConvertXaiUsage_TextOnlyTokens(t *testing.T) {
	textInputTokens := 100
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ImageInputTokens: nil,
		TextInputTokens:  &textInputTokens,
	}

	result := convertXaiUsage(usage)

	assert.NotNil(t, result.Raw)
	assert.Equal(t, int64(100), result.Raw["text_input_tokens"])
	assert.NotContains(t, result.Raw, "image_input_tokens")
}

// Test inclusivity logic: cached_tokens <= prompt_tokens (inclusive/overlapping)
func TestConvertXaiUsage_InclusiveCachedTokens(t *testing.T) {
	cachedTokens := 150
	usage := xaiUsage{
		PromptTokens:     200,
		CompletionTokens: 50,
		TotalTokens:      250,
		CachedTokens:     &cachedTokens,
	}

	result := convertXaiUsage(usage)

	// Input should remain 200 (cached are part of this total)
	assert.Equal(t, int64(200), *result.InputTokens)

	// No-cache should be 200 - 150 = 50
	assert.NotNil(t, result.InputDetails)
	assert.Equal(t, int64(50), *result.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(150), *result.InputDetails.CacheReadTokens)

	// Total should be recalculated: 200 + 50 = 250
	assert.Equal(t, int64(250), *result.TotalTokens)
}

// Test exclusivity logic: cached_tokens > prompt_tokens (exclusive/non-overlapping)
func TestConvertXaiUsage_ExclusiveCachedTokens(t *testing.T) {
	cachedTokens := 4328
	usage := xaiUsage{
		PromptTokens:     4142,
		CompletionTokens: 1,
		TotalTokens:      4143,
		CachedTokens:     &cachedTokens,
	}

	result := convertXaiUsage(usage)

	// Input should be sum: 4142 + 4328 = 8470
	assert.Equal(t, int64(8470), *result.InputTokens)

	// No-cache should be all prompt tokens: 4142
	assert.NotNil(t, result.InputDetails)
	assert.Equal(t, int64(4142), *result.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(4328), *result.InputDetails.CacheReadTokens)

	// Total should be recalculated: 8470 + 1 = 8471
	assert.Equal(t, int64(8471), *result.TotalTokens)
}

// Test reasoning tokens are ADDITIVE in Chat API
func TestConvertXaiUsage_ReasoningTokensAdditive(t *testing.T) {
	reasoningTokens := 228
	usage := xaiUsage{
		PromptTokens:     100,
		CompletionTokens: 1,
		TotalTokens:      101,
		ReasoningTokens:  &reasoningTokens,
	}

	result := convertXaiUsage(usage)

	// Output should be sum: 1 + 228 = 229
	assert.Equal(t, int64(229), *result.OutputTokens)

	// Output details
	assert.NotNil(t, result.OutputDetails)
	assert.Equal(t, int64(1), *result.OutputDetails.TextTokens)
	assert.Equal(t, int64(228), *result.OutputDetails.ReasoningTokens)

	// Total should be recalculated: 100 + 229 = 329
	assert.Equal(t, int64(329), *result.TotalTokens)
}

// Test full scenario with cached (exclusive) and reasoning (additive)
func TestConvertXaiUsage_FullScenarioWithInclusivity(t *testing.T) {
	cachedTokens := 5000   // Exclusive (> 4000)
	reasoningTokens := 300 // Additive
	textInputTokens := 3000
	imageInputTokens := 1000
	usage := xaiUsage{
		PromptTokens:     4000,
		CompletionTokens: 100,
		TotalTokens:      4100,
		CachedTokens:     &cachedTokens,
		ReasoningTokens:  &reasoningTokens,
		TextInputTokens:  &textInputTokens,
		ImageInputTokens: &imageInputTokens,
	}

	result := convertXaiUsage(usage)

	// Input: 4000 + 5000 = 9000 (exclusive)
	assert.Equal(t, int64(9000), *result.InputTokens)

	// Output: 100 + 300 = 400 (additive)
	assert.Equal(t, int64(400), *result.OutputTokens)

	// Total: 9000 + 400 = 9400
	assert.Equal(t, int64(9400), *result.TotalTokens)

	// Input details
	assert.NotNil(t, result.InputDetails)
	assert.Equal(t, int64(4000), *result.InputDetails.NoCacheTokens)
	assert.Equal(t, int64(5000), *result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(3000), *result.InputDetails.TextTokens)
	assert.Equal(t, int64(1000), *result.InputDetails.ImageTokens)

	// Output details
	assert.NotNil(t, result.OutputDetails)
	assert.Equal(t, int64(100), *result.OutputDetails.TextTokens)
	assert.Equal(t, int64(300), *result.OutputDetails.ReasoningTokens)
}
