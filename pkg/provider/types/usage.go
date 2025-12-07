package types

// Usage represents token or resource usage for an API call
type Usage struct {
	// Number of tokens in the prompt/input
	InputTokens int `json:"inputTokens"`

	// Number of tokens in the completion/output
	OutputTokens int `json:"outputTokens"`

	// Total tokens (input + output)
	TotalTokens int `json:"totalTokens"`
}

// Add adds another Usage to this one and returns a new Usage
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + other.InputTokens,
		OutputTokens: u.OutputTokens + other.OutputTokens,
		TotalTokens:  u.TotalTokens + other.TotalTokens,
	}
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
