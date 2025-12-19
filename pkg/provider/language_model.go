package provider

import (
	"context"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModel represents a language model (V3 specification)
// This is the core interface that all language model providers must implement
type LanguageModel interface {
	// Metadata methods
	SpecificationVersion() string // Returns "v3" for V3 models
	Provider() string             // Returns the provider name (e.g., "openai", "anthropic")
	ModelID() string              // Returns the model ID (e.g., "gpt-4", "claude-3-opus")

	// Capability methods
	SupportsTools() bool            // Whether the model supports tool calling
	SupportsStructuredOutput() bool // Whether the model supports structured output (JSON mode)
	SupportsImageInput() bool       // Whether the model accepts image inputs

	// Generation methods
	DoGenerate(ctx context.Context, opts *GenerateOptions) (*types.GenerateResult, error)
	DoStream(ctx context.Context, opts *GenerateOptions) (TextStream, error)
}

// GenerateOptions contains all options for text generation
type GenerateOptions struct {
	// Prompt for the model (either text or messages)
	Prompt types.Prompt

	// Temperature controls randomness (0.0 to 2.0, typically)
	Temperature *float64

	// Maximum number of tokens to generate
	MaxTokens *int

	// TopP (nucleus sampling) parameter
	TopP *float64

	// TopK parameter (for providers that support it)
	TopK *int

	// Frequency penalty (reduces repetition)
	FrequencyPenalty *float64

	// Presence penalty (encourages topic diversity)
	PresencePenalty *float64

	// Stop sequences that halt generation
	StopSequences []string

	// Tools available for the model to call
	Tools []types.Tool

	// Tool choice strategy
	ToolChoice types.ToolChoice

	// Response format (for structured output)
	ResponseFormat *ResponseFormat

	// Seed for deterministic generation
	Seed *int

	// Custom headers to send with the request
	Headers map[string]string

	// Maximum number of automatic tool call steps
	MaxSteps *int
}

// ResponseFormat specifies the format of the response
// Updated in v6.0 to support name and description for provider guidance
type ResponseFormat struct {
	// Type of response format ("text", "json", "json_object", "json_schema")
	Type string

	// Schema for JSON response (when Type is "json" or "json_schema")
	// Can be a map[string]interface{} (JSON Schema) or schema.Schema
	Schema interface{}

	// Name is an optional name for the output
	// Used by some providers (e.g., OpenAI, Anthropic) for additional LLM guidance
	Name string

	// Description is an optional description of the expected output
	// Used by some providers for additional LLM guidance
	Description string
}

// TextStream represents a streaming text response
type TextStream interface {
	io.ReadCloser

	// Next returns the next chunk in the stream
	// Returns io.EOF when the stream is complete
	Next() (*StreamChunk, error)

	// Err returns any error that occurred during streaming
	Err() error
}

// StreamChunk represents a single chunk in a text stream
type StreamChunk struct {
	// Type of chunk
	Type ChunkType

	// Text content (when Type is ChunkTypeText)
	Text string

	// Tool call (when Type is ChunkTypeToolCall)
	ToolCall *types.ToolCall

	// Usage information (when Type is ChunkTypeUsage or ChunkTypeFinish)
	Usage *types.Usage

	// Finish reason (when Type is ChunkTypeFinish)
	FinishReason types.FinishReason
}

// ChunkType represents the type of stream chunk
type ChunkType string

const (
	// ChunkTypeText indicates a text content chunk
	ChunkTypeText ChunkType = "text"

	// ChunkTypeToolCall indicates a tool call chunk
	ChunkTypeToolCall ChunkType = "tool-call"

	// ChunkTypeUsage indicates a usage information chunk
	ChunkTypeUsage ChunkType = "usage"

	// ChunkTypeFinish indicates the final chunk with finish reason
	ChunkTypeFinish ChunkType = "finish"

	// ChunkTypeError indicates an error occurred
	ChunkTypeError ChunkType = "error"
)

// EmbeddingModel represents an embedding model
type EmbeddingModel interface {
	// Metadata
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// MaxEmbeddingsPerCall returns the maximum number of embeddings that can be
	// generated in a single API call. Returns 0 or negative for unlimited.
	MaxEmbeddingsPerCall() int

	// SupportsParallelCalls returns whether the model can handle multiple
	// embedding calls in parallel (for batch processing).
	SupportsParallelCalls() bool

	// Embedding methods
	DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error)
	DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error)
}

// ImageModel represents an image generation model
type ImageModel interface {
	// Metadata
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// Image generation
	DoGenerate(ctx context.Context, opts *ImageGenerateOptions) (*types.ImageResult, error)
}

// ImageGenerateOptions contains options for image generation
type ImageGenerateOptions struct {
	// Text prompt for image generation
	Prompt string

	// Number of images to generate
	N *int

	// Size of the image (e.g., "1024x1024")
	Size string

	// Quality setting
	Quality string

	// Style setting
	Style string
}

// SpeechModel represents a speech synthesis model
type SpeechModel interface {
	// Metadata
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// Speech synthesis
	DoGenerate(ctx context.Context, opts *SpeechGenerateOptions) (*types.SpeechResult, error)
}

// SpeechGenerateOptions contains options for speech synthesis
type SpeechGenerateOptions struct {
	// Text to convert to speech
	Text string

	// Voice to use
	Voice string

	// Speed of speech (0.25 to 4.0)
	Speed *float64
}

// TranscriptionModel represents a speech-to-text model
type TranscriptionModel interface {
	// Metadata
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// Transcription
	DoTranscribe(ctx context.Context, opts *TranscriptionOptions) (*types.TranscriptionResult, error)
}

// TranscriptionOptions contains options for speech-to-text
type TranscriptionOptions struct {
	// Audio data to transcribe
	Audio []byte

	// MIME type of the audio
	MimeType string

	// Language of the audio (optional)
	Language string

	// Whether to include timestamps
	Timestamps bool
}
