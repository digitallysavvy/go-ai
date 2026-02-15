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

	assert.NotNil(t, result.OutputDetails)
	assert.NotNil(t, result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.NotNil(t, result.OutputDetails.TextTokens)
	assert.Equal(t, int64(30), *result.OutputDetails.TextTokens)
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

	assert.NotNil(t, result.OutputDetails)
	assert.NotNil(t, result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.NotNil(t, result.OutputDetails.TextTokens)
	assert.Equal(t, int64(30), *result.OutputDetails.TextTokens)
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

	// Check basic tokens
	assert.Equal(t, int64(100), *result.InputTokens)
	assert.Equal(t, int64(50), *result.OutputTokens)
	assert.Equal(t, int64(150), *result.TotalTokens)

	// Check input details (cached tokens)
	assert.NotNil(t, result.InputDetails)
	assert.Equal(t, int64(30), *result.InputDetails.CacheReadTokens)
	assert.Equal(t, int64(70), *result.InputDetails.NoCacheTokens)

	// Check output details (reasoning tokens)
	assert.NotNil(t, result.OutputDetails)
	assert.Equal(t, int64(20), *result.OutputDetails.ReasoningTokens)
	assert.Equal(t, int64(30), *result.OutputDetails.TextTokens)

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
