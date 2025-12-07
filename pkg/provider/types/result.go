package types

// GenerateResult contains the result of a text generation operation
type GenerateResult struct {
	// Generated text content
	Text string `json:"text"`

	// Tool calls made by the model (if any)
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	// Reason why generation finished
	FinishReason FinishReason `json:"finishReason"`

	// Token usage information
	Usage Usage `json:"usage"`

	// Raw request sent to the provider (for debugging)
	RawRequest interface{} `json:"rawRequest,omitempty"`

	// Raw response from the provider (for debugging)
	RawResponse interface{} `json:"rawResponse,omitempty"`

	// Warnings from the provider
	Warnings []Warning `json:"warnings,omitempty"`
}

// EmbeddingResult contains the result of an embedding operation
type EmbeddingResult struct {
	// Embedding vector
	Embedding []float64 `json:"embedding"`

	// Usage information
	Usage EmbeddingUsage `json:"usage"`
}

// EmbeddingsResult contains the results of a batch embedding operation
type EmbeddingsResult struct {
	// Embeddings for each input text
	Embeddings [][]float64 `json:"embeddings"`

	// Usage information
	Usage EmbeddingUsage `json:"usage"`
}

// ImageResult contains the result of an image generation operation
type ImageResult struct {
	// Generated image data
	Image []byte `json:"image"`

	// MIME type of the image
	MimeType string `json:"mimeType"`

	// Optional URL if image is hosted
	URL string `json:"url,omitempty"`

	// Usage information
	Usage ImageUsage `json:"usage"`

	// Warnings from the provider
	Warnings []Warning `json:"warnings,omitempty"`
}

// SpeechResult contains the result of a speech synthesis operation
type SpeechResult struct {
	// Audio data
	Audio []byte `json:"audio"`

	// MIME type of the audio
	MimeType string `json:"mimeType"`

	// Usage information
	Usage SpeechUsage `json:"usage"`
}

// TranscriptionResult contains the result of a speech-to-text operation
type TranscriptionResult struct {
	// Transcribed text
	Text string `json:"text"`

	// Optional timestamps for words or segments
	Timestamps []TranscriptionTimestamp `json:"timestamps,omitempty"`

	// Usage information
	Usage TranscriptionUsage `json:"usage"`
}

// TranscriptionTimestamp represents a timestamp in a transcription
type TranscriptionTimestamp struct {
	// Text for this segment
	Text string `json:"text"`

	// Start time in seconds
	Start float64 `json:"start"`

	// End time in seconds
	End float64 `json:"end"`
}

// StepResult represents the result of a single step in multi-step generation
// Used for tool calling loops and agent workflows
type StepResult struct {
	// Step number (1-indexed)
	StepNumber int `json:"stepNumber"`

	// Text generated in this step
	Text string `json:"text"`

	// Tool calls made in this step
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	// Tool results from this step
	ToolResults []ToolResult `json:"toolResults,omitempty"`

	// Finish reason for this step
	FinishReason FinishReason `json:"finishReason"`

	// Usage for this step
	Usage Usage `json:"usage"`

	// Warnings from this step
	Warnings []Warning `json:"warnings,omitempty"`
}
