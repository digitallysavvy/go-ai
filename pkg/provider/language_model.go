package provider

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

	// Provider-specific options
	// These are passed directly to the provider and can contain any provider-specific settings
	// Example: map[string]interface{}{"openai": map[string]interface{}{"promptCacheRetention": "24h"}}
	ProviderOptions map[string]interface{}

	// Telemetry configuration for observability
	// Providers can use this to instrument their API calls with OpenTelemetry spans
	Telemetry *telemetry.Settings
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

// TextStream represents a streaming text response.
//
// TextStream uses a Next()-based pattern for consuming stream chunks.
// This provides better type safety and is more idiomatic Go compared to
// the io.Reader interface.
//
// Example usage:
//
//	stream, err := model.DoStream(ctx, options)
//	if err != nil {
//	    return err
//	}
//	defer stream.Close()
//
//	for {
//	    chunk, err := stream.Next()
//	    if err != nil {
//	        if err.Error() == "EOF" {
//	            break
//	        }
//	        return err
//	    }
//
//	    switch chunk.Type {
//	    case provider.ChunkTypeText:
//	        fmt.Print(chunk.Text)
//	    case provider.ChunkTypeError:
//	        return fmt.Errorf("stream error: %v", chunk)
//	    }
//	}
//
// Note: The legacy io.Reader pattern (stream.Read()) is not supported.
// Use the Next() method shown above.
type TextStream interface {
	// Next returns the next chunk in the stream.
	// Returns io.EOF when the stream is complete.
	// Consumers should call Next() in a loop until io.EOF is returned.
	Next() (*StreamChunk, error)

	// Err returns any error that occurred during streaming.
	// Returns nil if the stream completed successfully or if the error was io.EOF.
	Err() error

	// Close closes the underlying stream and releases resources.
	// It's safe to call Close multiple times.
	// Calling Close will cause future Next() calls to return io.EOF.
	Close() error
}

// StreamChunk represents a single chunk in a text stream
type StreamChunk struct {
	// Type of chunk
	Type ChunkType

	// Text content (when Type is ChunkTypeText)
	Text string

	// Reasoning content (when Type is ChunkTypeReasoning)
	Reasoning string

	// Tool call (when Type is ChunkTypeToolCall)
	ToolCall *types.ToolCall

	// Usage information (when Type is ChunkTypeUsage or ChunkTypeFinish)
	Usage *types.Usage

	// Finish reason (when Type is ChunkTypeFinish)
	FinishReason types.FinishReason

	// Context management information (Anthropic-specific)
	// Contains statistics about automatic conversation history cleanup
	// Available when Type is ChunkTypeFinish or ChunkTypeMetadata
	ContextManagement interface{}

	// AbortReason provides context when a stream is aborted
	// This can be due to timeouts, user cancellation, or errors
	// Useful for debugging and understanding stream termination
	AbortReason string
}

// ChunkType represents the type of stream chunk
type ChunkType string

const (
	// ChunkTypeText indicates a text content chunk
	ChunkTypeText ChunkType = "text"

	// ChunkTypeReasoning indicates a reasoning/thinking content chunk
	ChunkTypeReasoning ChunkType = "reasoning"

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
	// Text prompt for image generation (optional for some operations like upscaling)
	Prompt string

	// Number of images to generate
	N *int

	// Size of the image (e.g., "1024x1024")
	Size string

	// Aspect ratio (e.g., "16:9", "1:1")
	AspectRatio string

	// Seed for reproducible generation
	Seed *int

	// Quality setting
	Quality string

	// Style setting
	Style string

	// Files for image editing or variation generation
	Files []ImageFile

	// Mask for inpainting operations
	Mask *ImageFile

	// Provider-specific options
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation
	AbortSignal context.Context

	// Additional HTTP headers
	Headers map[string]string
}

// ImageFile represents an image file for editing or variations
type ImageFile struct {
	// Type is "file" or "url"
	Type string

	// URL for type="url"
	URL string

	// Data for type="file" (base64 or binary)
	Data []byte

	// MediaType for type="file" (e.g., "image/png")
	MediaType string
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
