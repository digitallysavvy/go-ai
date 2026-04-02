package provider

import (
	"context"
	"encoding/json"
	"time"

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

	// Reasoning controls how much thinking effort the model applies.
	// nil means the field is unset (inherits the provider's own default).
	// Use types.ReasoningDefault to explicitly omit the field from API requests.
	// Providers map this to their native reasoning APIs (see types.ReasoningLevel).
	Reasoning *types.ReasoningLevel

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

	// ID identifies the content block this chunk belongs to.
	// Set on ChunkTypeTextStart/End, ChunkTypeReasoningStart/End, and their
	// corresponding delta chunks.  Matches the TypeScript SDK's `id` field on
	// text-start, text-delta, text-end, reasoning-start, reasoning-delta, and
	// reasoning-end stream parts.  Empty for chunk types that don't use block IDs.
	ID string

	// Text content (when Type is ChunkTypeText).
	// ChunkTypeTextStart and ChunkTypeTextEnd carry only an ID, no text.
	Text string

	// Reasoning content (when Type is ChunkTypeReasoning).
	// ChunkTypeReasoningStart and ChunkTypeReasoningEnd carry only an ID, no text.
	Reasoning string

	// Tool call (when Type is ChunkTypeToolCall)
	ToolCall *types.ToolCall

	// Tool result (when Type is ChunkTypeToolResult).
	// Synthetic chunks emitted by streamText after deferred tool execution.
	ToolResult *types.ToolResult

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

	// Warnings carries provider warnings surfaced on stream-start chunks.
	Warnings []types.Warning

	// CustomContent carries provider-specific content when Type is ChunkTypeCustom.
	CustomContent *types.CustomContent

	// ReasoningFileContent carries a reasoning-generated file when Type is
	// ChunkTypeReasoningFile.
	ReasoningFileContent *types.ReasoningFileContent

	// SourceContent carries a citation or grounding reference when Type is
	// ChunkTypeSource.
	SourceContent *types.SourceContent

	// GeneratedFileContent carries a model-generated output file when Type is
	// ChunkTypeFile.
	GeneratedFileContent *types.GeneratedFileContent

	// ProviderMetadata carries provider-specific metadata attached to a stream
	// part.  Present on block-boundary chunk types (ChunkTypeTextStart,
	// ChunkTypeText, ChunkTypeTextEnd, ChunkTypeReasoningStart,
	// ChunkTypeReasoning, ChunkTypeReasoningEnd) when the provider returns
	// additional metadata alongside the content delta.  Matches the TS SDK's
	// optional `providerMetadata?: SharedV4ProviderMetadata` field on those
	// stream part types.
	ProviderMetadata json.RawMessage

	// ResponseMetadata is set when Type == ChunkTypeResponseMetadata.
	// Carries the provider-level HTTP response metadata emitted early in the
	// stream (after headers arrive, before content begins).
	ResponseMetadata *ResponseMetadata
}

// ResponseMetadata is the payload of a ChunkTypeResponseMetadata chunk.
// Providers that know their response ID / model / timestamp before streaming
// content should emit exactly one of these as the first meaningful chunk.
// Mirrors the 'response-metadata' chunk type in the TypeScript SDK.
type ResponseMetadata struct {
	// ID is the provider-assigned response ID (e.g. "chatcmpl-abc123").
	ID string

	// Timestamp is when the provider started generating the response.
	// Zero value means the provider did not supply a timestamp.
	Timestamp time.Time

	// ModelID is the model that handled the request. May differ from the
	// model requested (e.g. when using model aliases).
	ModelID string

	// Headers are the raw HTTP response headers.
	Headers map[string]string
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

	// ChunkTypeToolResult is a synthetic chunk emitted by streamText after
	// deferred tool execution.  It carries the result of a tool call and is
	// forwarded to OnChunk consumers just like any other chunk, matching the
	// TypeScript AI SDK's tool-result forwarding behaviour.
	ChunkTypeToolResult ChunkType = "tool-result"

	// ChunkTypeTextStart marks the beginning of a text content block.
	// ID identifies which block subsequent ChunkTypeText and ChunkTypeTextEnd
	// chunks belong to.  Matches TS SDK's "text-start" stream part.
	ChunkTypeTextStart ChunkType = "text-start"

	// ChunkTypeTextEnd marks the end of a text content block identified by ID.
	// Matches TS SDK's "text-end" stream part.
	ChunkTypeTextEnd ChunkType = "text-end"

	// ChunkTypeReasoningStart marks the beginning of a reasoning/thinking block.
	// ID identifies which block subsequent ChunkTypeReasoning and
	// ChunkTypeReasoningEnd chunks belong to.  Matches TS SDK's "reasoning-start".
	ChunkTypeReasoningStart ChunkType = "reasoning-start"

	// ChunkTypeReasoningEnd marks the end of a reasoning/thinking block
	// identified by ID.  Matches TS SDK's "reasoning-end" stream part.
	ChunkTypeReasoningEnd ChunkType = "reasoning-end"

	// ChunkTypeCustom indicates a provider-specific content chunk that does not
	// map to any standardized type.  The chunk carries a CustomContent part.
	ChunkTypeCustom ChunkType = "custom"

	// ChunkTypeReasoningFile indicates a file generated by the model during
	// reasoning.  The chunk carries a ReasoningFileContent part.
	ChunkTypeReasoningFile ChunkType = "reasoning-file"

	// ChunkTypeSource indicates a citation or grounding reference emitted
	// alongside model output (e.g., XAI/Perplexity citations, Google grounding).
	// The chunk carries a SourceContent part.
	ChunkTypeSource ChunkType = "source"

	// ChunkTypeFile indicates a model-generated output file (e.g., a chart image
	// or audio clip produced by the model).  The chunk carries a
	// GeneratedFileContent part.
	ChunkTypeFile ChunkType = "file"

	// ChunkTypeToolInputStart marks the beginning of streaming tool input for a
	// custom function tool call.  The ToolCall field contains the tool call ID and
	// name.  Subsequent ChunkTypeToolInputDelta chunks carry incremental JSON and
	// ChunkTypeToolInputEnd marks completion.  A final ChunkTypeToolCall is always
	// emitted after ChunkTypeToolInputEnd with the fully assembled arguments.
	//
	// Only emitted for user-defined function tools (type: tool_use), not for
	// provider-executed tools (type: server_tool_use).
	ChunkTypeToolInputStart ChunkType = "tool-input-start"

	// ChunkTypeToolInputDelta carries an incremental chunk of tool input JSON
	// during eager input streaming.  The Text field contains the raw partial JSON
	// fragment exactly as received from the API.  Follows ChunkTypeToolInputStart.
	ChunkTypeToolInputDelta ChunkType = "tool-input-delta"

	// ChunkTypeToolInputEnd marks the end of tool input streaming for a custom
	// function tool.  The ToolCall field carries the tool call ID.  A
	// ChunkTypeToolCall chunk with the fully parsed arguments always follows.
	ChunkTypeToolInputEnd ChunkType = "tool-input-end"

	// ChunkTypeStreamStart is the first chunk emitted by a stream that has
	// pre-stream warnings (e.g. unsupported-setting).
	// The Warnings field carries the warnings; the chunk has no text content.
	// Consumers check chunk.Type == ChunkTypeStreamStart and read chunk.Warnings.
	ChunkTypeStreamStart ChunkType = "stream-start"

	// ChunkTypeResponseMetadata carries provider-level response metadata emitted
	// early in the stream (e.g. after HTTP response headers are received).
	// Providers that know their response ID / model / timestamp before streaming
	// content should emit exactly one of these as the first meaningful chunk.
	// Mirrors the 'response-metadata' chunk type in the TypeScript SDK.
	ChunkTypeResponseMetadata ChunkType = "response-metadata"
)

// EmbedModelOptions contains options forwarded to the embedding provider on each call.
// Mirrors the TS SDK's ProviderOptions pattern for generateText/generateImage.
type EmbedModelOptions struct {
	// ProviderOptions holds provider-specific options keyed by provider name.
	// Example: map[string]interface{}{"openai": map[string]interface{}{"dimensions": 256}}
	ProviderOptions map[string]interface{}

	// Headers are additional HTTP headers to send with the request.
	Headers map[string]string
}

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
	// opts may be nil — callers that don't need provider options pass nil.
	DoEmbed(ctx context.Context, input string, opts *EmbedModelOptions) (*types.EmbeddingResult, error)
	DoEmbedMany(ctx context.Context, inputs []string, opts *EmbedModelOptions) (*types.EmbeddingsResult, error)
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
