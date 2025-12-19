package types

// Usage represents token or resource usage for an API call
// Updated to match TypeScript AI SDK v6.0 with detailed token tracking
type Usage struct {
	// Number of tokens in the prompt/input
	InputTokens *int64 `json:"inputTokens,omitempty"`

	// Detailed breakdown of input tokens
	InputDetails *InputTokenDetails `json:"inputTokenDetails,omitempty"`

	// Number of tokens in the completion/output
	OutputTokens *int64 `json:"outputTokens,omitempty"`

	// Detailed breakdown of output tokens
	OutputDetails *OutputTokenDetails `json:"outputTokenDetails,omitempty"`

	// Total tokens (input + output)
	TotalTokens *int64 `json:"totalTokens,omitempty"`

	// Raw provider-specific usage data
	// Contains any additional usage information specific to the provider
	Raw map[string]interface{} `json:"raw,omitempty"`
}

// InputTokenDetails provides a detailed breakdown of input token usage
// Particularly useful for tracking prompt caching benefits
type InputTokenDetails struct {
	// Tokens that were not cached (processed normally)
	NoCacheTokens *int64 `json:"noCacheTokens,omitempty"`

	// Tokens that were read from cache (faster, cheaper)
	CacheReadTokens *int64 `json:"cacheReadTokens,omitempty"`

	// Tokens that were written to cache (for future use)
	CacheWriteTokens *int64 `json:"cacheWriteTokens,omitempty"`
}

// OutputTokenDetails provides a detailed breakdown of output token usage
// Particularly useful for tracking reasoning token usage in models like OpenAI o1
type OutputTokenDetails struct {
	// Tokens used for regular text generation
	TextTokens *int64 `json:"textTokens,omitempty"`

	// Tokens used for internal reasoning (not shown in output)
	// Used by reasoning models like OpenAI o1, o3
	ReasoningTokens *int64 `json:"reasoningTokens,omitempty"`
}

// Add adds another Usage to this one and returns a new Usage
func (u Usage) Add(other Usage) Usage {
	result := Usage{
		InputTokens:  addInt64Ptr(u.InputTokens, other.InputTokens),
		OutputTokens: addInt64Ptr(u.OutputTokens, other.OutputTokens),
		TotalTokens:  addInt64Ptr(u.TotalTokens, other.TotalTokens),
	}

	// Merge input details
	if u.InputDetails != nil || other.InputDetails != nil {
		result.InputDetails = &InputTokenDetails{
			NoCacheTokens:    addInt64Ptr(getInputDetail(u, "NoCacheTokens"), getInputDetail(other, "NoCacheTokens")),
			CacheReadTokens:  addInt64Ptr(getInputDetail(u, "CacheReadTokens"), getInputDetail(other, "CacheReadTokens")),
			CacheWriteTokens: addInt64Ptr(getInputDetail(u, "CacheWriteTokens"), getInputDetail(other, "CacheWriteTokens")),
		}
	}

	// Merge output details
	if u.OutputDetails != nil || other.OutputDetails != nil {
		result.OutputDetails = &OutputTokenDetails{
			TextTokens:      addInt64Ptr(getOutputDetail(u, "TextTokens"), getOutputDetail(other, "TextTokens")),
			ReasoningTokens: addInt64Ptr(getOutputDetail(u, "ReasoningTokens"), getOutputDetail(other, "ReasoningTokens")),
		}
	}

	// Merge raw data (simple merge, later values override)
	if len(u.Raw) > 0 || len(other.Raw) > 0 {
		result.Raw = make(map[string]interface{})
		for k, v := range u.Raw {
			result.Raw[k] = v
		}
		for k, v := range other.Raw {
			result.Raw[k] = v
		}
	}

	return result
}

// Helper functions for Add method
func addInt64Ptr(a, b *int64) *int64 {
	if a == nil && b == nil {
		return nil
	}
	var aVal, bVal int64
	if a != nil {
		aVal = *a
	}
	if b != nil {
		bVal = *b
	}
	result := aVal + bVal
	return &result
}

func getInputDetail(u Usage, field string) *int64 {
	if u.InputDetails == nil {
		return nil
	}
	switch field {
	case "NoCacheTokens":
		return u.InputDetails.NoCacheTokens
	case "CacheReadTokens":
		return u.InputDetails.CacheReadTokens
	case "CacheWriteTokens":
		return u.InputDetails.CacheWriteTokens
	}
	return nil
}

func getOutputDetail(u Usage, field string) *int64 {
	if u.OutputDetails == nil {
		return nil
	}
	switch field {
	case "TextTokens":
		return u.OutputDetails.TextTokens
	case "ReasoningTokens":
		return u.OutputDetails.ReasoningTokens
	}
	return nil
}

// Legacy helper functions for backward compatibility
// These work with the new structure but return simple int values

// GetInputTokens returns the input token count (0 if nil)
func (u Usage) GetInputTokens() int64 {
	if u.InputTokens == nil {
		return 0
	}
	return *u.InputTokens
}

// GetOutputTokens returns the output token count (0 if nil)
func (u Usage) GetOutputTokens() int64 {
	if u.OutputTokens == nil {
		return 0
	}
	return *u.OutputTokens
}

// GetTotalTokens returns the total token count (0 if nil)
func (u Usage) GetTotalTokens() int64 {
	if u.TotalTokens == nil {
		return 0
	}
	return *u.TotalTokens
}

// EmbeddingUsage represents usage for embedding operations
type EmbeddingUsage struct {
	// Number of tokens in the input text
	InputTokens int `json:"inputTokens"`

	// Total tokens
	TotalTokens int `json:"totalTokens"`
}

// ImageUsage represents usage for image generation operations
type ImageUsage struct {
	// Number of images generated
	ImageCount int `json:"imageCount"`
}

// SpeechUsage represents usage for speech synthesis operations
type SpeechUsage struct {
	// Number of characters processed
	CharacterCount int `json:"characterCount"`
}

// TranscriptionUsage represents usage for speech-to-text operations
type TranscriptionUsage struct {
	// Duration of audio in seconds
	DurationSeconds float64 `json:"durationSeconds"`
}

// Warning represents a warning message from the provider
type Warning struct {
	// Type of warning
	Type string `json:"type"`

	// Warning message
	Message string `json:"message"`
}

// FinishReason represents why the model stopped generating
type FinishReason string

const (
	// FinishReasonStop indicates the model generated a natural stop sequence
	FinishReasonStop FinishReason = "stop"

	// FinishReasonLength indicates the generation reached the max token limit
	FinishReasonLength FinishReason = "length"

	// FinishReasonContentFilter indicates content was filtered
	FinishReasonContentFilter FinishReason = "content-filter"

	// FinishReasonToolCalls indicates the model wants to call tools
	FinishReasonToolCalls FinishReason = "tool-calls"

	// FinishReasonError indicates an error occurred
	FinishReasonError FinishReason = "error"

	// FinishReasonOther indicates another reason
	FinishReasonOther FinishReason = "other"
)

// ResponseMetadata contains metadata about the model's response
type ResponseMetadata struct {
	// Model ID that generated the response
	ModelID string `json:"modelId,omitempty"`

	// Provider-specific metadata
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
}
