package types

import "time"

// StepModel identifies the model that produced a generation step.
type StepModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// StepRequest contains metadata about the HTTP request sent to the provider.
type StepRequest struct {
	// Body is the raw request body sent to the provider (for debugging).
	Body interface{} `json:"body,omitempty"`

	// Messages are the model messages sent in this step when explicitly included.
	Messages []Message `json:"messages,omitempty"`
}

// StepResponse contains metadata about the response received from the provider.
type StepResponse struct {
	// ID is the provider-assigned response identifier.
	ID string `json:"id,omitempty"`

	// Timestamp is when the provider started generating the response.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// ModelID is the model that handled the request.
	ModelID string `json:"modelId,omitempty"`

	// Headers are the raw HTTP response headers from the provider.
	Headers map[string]string `json:"headers,omitempty"`

	// Messages are the response messages generated in this step (assistant + tool messages).
	Messages []Message `json:"messages,omitempty"`

	// Body is the raw response body from the provider (for debugging).
	Body interface{} `json:"body,omitempty"`
}

// StepPerformance contains deterministic performance statistics for a model step.
// It mirrors the TypeScript AI SDK StepResult.performance shape.
type StepPerformance struct {
	// StepTimeMs is total wall-clock time spent on the step, including client-side tool execution.
	StepTimeMs int64 `json:"stepTimeMs"`

	// ResponseTimeMs is the wall-clock duration of the model response in milliseconds.
	ResponseTimeMs int64 `json:"responseTimeMs"`

	// ToolExecutionMs contains client-side tool execution durations keyed by tool call ID.
	ToolExecutionMs map[string]int64 `json:"toolExecutionMs"`

	// TokensPerSecond is average output tokens per second. It is 0 when the
	// value cannot be represented as a finite number.
	TokensPerSecond float64 `json:"tokensPerSecond"`

	// TimeToFirstTokenMs is populated for streaming steps when the first content
	// token/chunk timing is known.
	TimeToFirstTokenMs *int64 `json:"timeToFirstTokenMs,omitempty"`
}

// GenerateResult contains the result of a text generation operation
type GenerateResult struct {
	// Generated text content
	Text string `json:"text"`

	// Content parts from the response beyond the primary text (e.g. ReasoningContent
	// blocks from Anthropic extended thinking). Callers can include these parts in
	// subsequent message history to round-trip thinking blocks through the API.
	Content []ContentPart `json:"content,omitempty"`

	// Tool calls made by the model (if any)
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	// Reason why generation finished
	FinishReason FinishReason `json:"finishReason"`

	// Token usage information
	Usage Usage `json:"usage"`

	// Context management information (Anthropic-specific)
	// Contains statistics about automatic conversation history cleanup
	ContextManagement interface{} `json:"contextManagement,omitempty"`

	// Raw request sent to the provider (for debugging)
	RawRequest interface{} `json:"rawRequest,omitempty"`

	// Raw response from the provider (for debugging)
	RawResponse interface{} `json:"rawResponse,omitempty"`

	// Warnings from the provider
	Warnings []Warning `json:"warnings,omitempty"`

	// ProviderMetadata holds provider-specific metadata keyed by provider name.
	// Example: map[string]interface{}{"googleVertex": map[string]interface{}{"finishMessage": "..."}}
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// ResponseHeaders are the raw HTTP response headers from the provider.
	// Populated for HTTP-based providers; nil for others.
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`

	// ResponseMetadata contains normalized response metadata when the provider
	// exposes it on non-streaming calls.
	ResponseMetadata *ResponseMetadata `json:"response,omitempty"`
}

// EmbeddingResponse contains metadata about the HTTP response from the embedding provider.
type EmbeddingResponse struct {
	// Headers are the raw response headers from the provider.
	Headers map[string][]string `json:"headers,omitempty"`

	// Body is the raw response body from the provider (for debugging).
	Body interface{} `json:"body,omitempty"`
}

// EmbeddingResult contains the result of an embedding operation
type EmbeddingResult struct {
	// Embedding vector
	Embedding []float64 `json:"embedding"`

	// Usage information
	Usage EmbeddingUsage `json:"usage"`

	// Warnings are any non-fatal warnings emitted by the provider (e.g. unsupported settings).
	Warnings []Warning `json:"warnings,omitempty"`

	// Response holds provider HTTP response metadata (headers, body).
	Response EmbeddingResponse `json:"response,omitempty"`
}

// EmbeddingsResult contains the results of a batch embedding operation
type EmbeddingsResult struct {
	// Embeddings for each input text
	Embeddings [][]float64 `json:"embeddings"`

	// Usage information
	Usage EmbeddingUsage `json:"usage"`

	// Warnings are any non-fatal warnings emitted by the provider.
	Warnings []Warning `json:"warnings,omitempty"`

	// Responses holds per-request HTTP response metadata. One entry per batch call
	// (most providers make a single call for the whole batch).
	Responses []EmbeddingResponse `json:"responses,omitempty"`
}

// ImageResult contains the result of an image generation operation
type ImageResult struct {
	// Generated image data
	Image []byte `json:"image"`

	// Generated images. Providers that support multiple images should return
	// all images here while keeping Image populated with the first image for
	// compatibility.
	Images [][]byte `json:"images,omitempty"`

	// Base64Image is the first generated image as a provider-returned base64
	// string, when the provider returns base64 directly.
	Base64Image string `json:"base64Image,omitempty"`

	// Base64Images contains all provider-returned base64 image strings, when
	// the provider returns base64 directly.
	Base64Images []string `json:"base64Images,omitempty"`

	// MIME type of the image
	MimeType string `json:"mimeType"`

	// Optional URL if image is hosted
	URL string `json:"url,omitempty"`

	// Usage information
	Usage ImageUsage `json:"usage"`

	// Warnings from the provider
	Warnings []Warning `json:"warnings,omitempty"`

	// ProviderMetadata holds provider-specific metadata (e.g. cost tracking).
	// Keyed by provider name (e.g. "xai").
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// Response contains provider response metadata such as model ID and headers.
	Response *ResponseMetadata `json:"response,omitempty"`
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

// VideoResult contains the result of a video generation operation
type VideoResult struct {
	// Generated video data
	Video []byte `json:"video"`

	// MIME type of the video
	MimeType string `json:"mimeType"`

	// Optional URL if video is hosted
	URL string `json:"url,omitempty"`

	// Usage information
	Usage VideoUsage `json:"usage"`

	// Warnings from the provider
	Warnings []Warning `json:"warnings,omitempty"`
}

// GeneratedFile represents a generated file (video, audio, image, etc.)
type GeneratedFile struct {
	// Data is the raw file data
	Data []byte `json:"data,omitempty"`

	// URL is the URL to the file (if available)
	URL string `json:"url,omitempty"`

	// MediaType is the MIME type of the file (e.g., "video/mp4", "image/png")
	MediaType string `json:"mediaType"`
}

// StepResult represents the result of a single step in multi-step generation.
type StepResult struct {
	// CallID uniquely identifies the generateText/streamText call this step belongs to.
	CallID string `json:"callId,omitempty"`

	// Step number (0-indexed), matching the TypeScript SDK.
	StepNumber int `json:"stepNumber"`

	// Model identifies the provider and model ID that produced this step.
	Model StepModel `json:"model"`

	// Text generated in this step
	Text string `json:"text"`

	// Content contains all standardized content parts generated in this step.
	Content []ContentPart `json:"content,omitempty"`

	// Reasoning holds the reasoning/thinking content parts produced in this step.
	Reasoning []ReasoningContent `json:"reasoning,omitempty"`

	// ReasoningText is the concatenated text of all reasoning parts in this step.
	ReasoningText string `json:"reasoningText,omitempty"`

	// Files contains model-generated output files from this step.
	Files []GeneratedFileContent `json:"files,omitempty"`

	// Tool calls made in this step
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	// StaticToolCalls are tool calls from non-dynamic (typed) tools.
	StaticToolCalls []ToolCall `json:"staticToolCalls,omitempty"`

	// DynamicToolCalls are tool calls from dynamically registered tools.
	DynamicToolCalls []ToolCall `json:"dynamicToolCalls,omitempty"`

	// Tool results from this step
	ToolResults []ToolResult `json:"toolResults,omitempty"`

	// StaticToolResults are results from non-dynamic (typed) tools.
	StaticToolResults []ToolResult `json:"staticToolResults,omitempty"`

	// DynamicToolResults are results from dynamically registered tools.
	DynamicToolResults []ToolResult `json:"dynamicToolResults,omitempty"`

	// Finish reason for this step
	FinishReason FinishReason `json:"finishReason"`

	// Raw finish reason from the provider
	RawFinishReason string `json:"rawFinishReason,omitempty"`

	// Usage for this step
	Usage Usage `json:"usage"`

	// Performance contains deterministic timing and token-rate statistics for this step.
	Performance StepPerformance `json:"performance"`

	// Context management information (Anthropic-specific)
	ContextManagement interface{} `json:"contextManagement,omitempty"`

	// Warnings from this step
	Warnings []Warning `json:"warnings,omitempty"`

	// Sources contains citation or grounding references for this step.
	Sources []SourceContent `json:"sources,omitempty"`

	// Request contains metadata about the request sent to the provider.
	Request StepRequest `json:"request,omitempty"`

	// Response contains metadata about the response from the provider.
	Response StepResponse `json:"response,omitempty"`

	// ResponseMessages contains the assistant and tool messages generated in this step.
	// Deprecated: use Response.Messages instead.
	ResponseMessages []Message `json:"responseMessages,omitempty"`

	// ProviderMetadata holds provider-specific metadata for this step.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// ToolsContext is the per-tool context map in effect for this step.
	ToolsContext map[string]interface{} `json:"toolsContext,omitempty"`

	// RuntimeContext is the user-defined context in effect for this step.
	RuntimeContext interface{} `json:"runtimeContext,omitempty"`
}
