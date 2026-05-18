package types

import (
	"encoding/base64"
	"encoding/json"
)

// MessageRole represents the role of a message sender in a conversation
type MessageRole string

const (
	// RoleSystem represents system instructions
	RoleSystem MessageRole = "system"
	// RoleUser represents user input
	RoleUser MessageRole = "user"
	// RoleAssistant represents model responses
	RoleAssistant MessageRole = "assistant"
	// RoleTool represents tool execution results
	RoleTool MessageRole = "tool"
)

// Message represents a single message in a conversation
type Message struct {
	// Role of the message sender
	Role MessageRole `json:"role"`

	// Content parts of the message (text, images, tool results, etc.)
	Content []ContentPart `json:"content"`

	// ToolCalls holds tool calls made in this message (assistant messages only).
	// Providers that require tool calls as a top-level field (e.g. OpenAI) use
	// this field when converting messages to their wire format.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Optional name for the message sender
	Name string `json:"name,omitempty"`

	// ProviderOptions holds provider-specific options for the whole message.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ContentPart represents a part of message content
// This is an interface to support different content types
type ContentPart interface {
	// ContentType returns the type of content ("text", "image", "tool-result", etc.)
	ContentType() string
}

// TextContent represents text content in a message
type TextContent struct {
	Text string `json:"text"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider.
	// Used by Google/Vertex providers to carry thoughtSignature for text parts
	// that are associated with model reasoning.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (t TextContent) ContentType() string {
	return "text"
}

// ReasoningContent represents reasoning/thinking content in a message.
// This is used by models that expose their reasoning process (e.g., OpenAI o1, Anthropic Claude with thinking).
//
// For Anthropic models with extended thinking, Signature and RedactedData carry the
// fields required to re-send this block in message history. When the Anthropic API
// returns a thinking block its response includes a cryptographic Signature; callers
// who pass conversation history back to the API must include that signature intact.
// RedactedData is set instead when the API returns a redacted_thinking block.
type ReasoningContent struct {
	Text string `json:"text"`

	// Signature is the cryptographic signature attached to Anthropic thinking blocks.
	// Required when re-sending a thinking block as part of message history.
	// Populated by the Anthropic provider when parsing API responses.
	Signature string `json:"signature,omitempty"`

	// RedactedData is set for redacted_thinking blocks returned by the Anthropic API.
	// When non-empty, the block was redacted and only this opaque blob is available.
	RedactedData string `json:"redacted_data,omitempty"`

	// EncryptedContent is the opaque encrypted reasoning blob returned by the
	// OpenAI Responses API. When present, callers must forward it verbatim in
	// subsequent turns so the API can reconstruct the reasoning context.
	EncryptedContent string `json:"encrypted_content,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider.
	// Used to carry provider-specific fields (e.g. xAI reasoning item ID).
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ContentType implements ContentPart interface
func (r ReasoningContent) ContentType() string {
	return "reasoning"
}

// ImageContent represents image content in a message
type ImageContent struct {
	// Image data as bytes
	Image []byte `json:"image"`

	// MIME type of the image (e.g., "image/png", "image/jpeg")
	MimeType string `json:"mimeType"`

	// Optional URL if image is hosted remotely
	URL string `json:"url,omitempty"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ContentType implements ContentPart interface
func (i ImageContent) ContentType() string {
	return "image"
}

// FileContent represents file content in a message
type FileContent struct {
	// FileData is the normalized provider-facing tagged file data shape.
	// When unset, legacy Data/URL fields are normalized before provider calls.
	FileData FileData `json:"fileData,omitempty"`

	// File data as bytes
	Data []byte `json:"data,omitempty"`

	// MIME type of the file.
	// Deprecated: use MediaType. Kept for existing callers.
	MimeType string `json:"mimeType,omitempty"`

	// MediaType is the IANA media type of the file.
	MediaType string `json:"mediaType,omitempty"`

	// Optional filename
	Filename string `json:"filename,omitempty"`

	// URL holds a legacy remote file URL. It is normalized into FileData.
	URL string `json:"url,omitempty"`

	// Reference holds a provider file reference. It is normalized into FileData.
	Reference string `json:"reference,omitempty"`

	// Text holds inline text document content. It is normalized into FileData.
	Text string `json:"text,omitempty"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider when
	// this file appears in model output.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (f FileContent) ContentType() string {
	return "file"
}

// SourceContent is a source reference generated alongside model output —
// typically a citation or grounding reference.
// Matches LanguageModelV4Source in the TypeScript SDK.
//
// SourceType is either "url" (web citation) or "document" (file/document citation).
// For "url" sources, URL is required. For "document" sources, MediaType and Title
// are required.
type SourceContent struct {
	// SourceType is "url" or "document".
	SourceType string `json:"sourceType"`

	// ID is the provider-assigned identifier for this source.
	ID string `json:"id"`

	// URL is the web address of the source (sourceType="url").
	URL string `json:"url,omitempty"`

	// MediaType is the IANA media type of the document (sourceType="document").
	MediaType string `json:"mediaType,omitempty"`

	// Title is the human-readable title of the source.
	Title string `json:"title,omitempty"`

	// Filename is the optional filename for document sources (sourceType="document").
	Filename string `json:"filename,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (s SourceContent) ContentType() string {
	return "source"
}

// GeneratedFileContent is a file produced by the model as part of its response
// (e.g., a chart image, an audio clip, or a PDF).
//
// This is distinct from FileContent (which represents a file sent TO the model
// as part of a message). GeneratedFileContent represents output FROM the model.
//
// Data is []byte; encoding/json automatically base64-encodes it when marshaling
// and decodes base64 back to []byte when unmarshaling.
// Matches LanguageModelV4File in the TypeScript SDK.
type GeneratedFileContent struct {
	// MediaType is the IANA media type of the generated file (e.g., "image/png").
	MediaType string `json:"mediaType"`

	// FileData holds the TypeScript-compatible tagged file-data shape when the
	// provider returns a URL or another non-inline file representation.
	FileData FileData `json:"fileData,omitempty"`

	// Data holds the raw file bytes.
	// encoding/json marshals []byte as base64 and unmarshals base64 to []byte.
	Data []byte `json:"data,omitempty"`

	// URL is a convenience mirror for FileData.URL when FileData.Type is "url".
	URL string `json:"url,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (f GeneratedFileContent) ContentType() string {
	return "file"
}

// MarshalJSON emits the TypeScript SDK file shape:
// { mediaType, data: { type: "data"|"url", ... }, providerMetadata? }.
func (f GeneratedFileContent) MarshalJSON() ([]byte, error) {
	type generatedFileJSON struct {
		MediaType        string          `json:"mediaType"`
		Data             interface{}     `json:"data"`
		ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	}
	var data interface{}
	switch {
	case f.FileData.Type == FileDataTypeURL || f.URL != "":
		data = map[string]interface{}{
			"type": "url",
			"url":  firstNonEmptyString(f.FileData.URL, f.URL),
		}
	case f.FileData.Type == FileDataTypeData && f.FileData.DataString != "":
		data = map[string]interface{}{"type": "data", "data": f.FileData.DataString}
	case f.FileData.Type == FileDataTypeData && len(f.FileData.Data) > 0:
		data = map[string]interface{}{"type": "data", "data": base64.StdEncoding.EncodeToString(f.FileData.Data)}
	default:
		data = map[string]interface{}{"type": "data", "data": base64.StdEncoding.EncodeToString(f.Data)}
	}
	return json.Marshal(generatedFileJSON{
		MediaType:        f.MediaType,
		Data:             data,
		ProviderMetadata: f.ProviderMetadata,
	})
}

// UnmarshalJSON accepts both the TypeScript tagged data union and the older Go
// base64-string data shape for backward compatibility.
func (f *GeneratedFileContent) UnmarshalJSON(data []byte) error {
	type generatedFileJSON struct {
		MediaType        string          `json:"mediaType"`
		Data             json.RawMessage `json:"data"`
		URL              string          `json:"url,omitempty"`
		FileData         FileData        `json:"fileData,omitempty"`
		ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	}
	var raw generatedFileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.MediaType = raw.MediaType
	f.URL = raw.URL
	f.FileData = raw.FileData
	f.ProviderMetadata = raw.ProviderMetadata
	if len(raw.Data) == 0 || string(raw.Data) == "null" {
		return nil
	}
	if raw.Data[0] == '"' {
		return json.Unmarshal(raw.Data, &f.Data)
	}
	var tagged struct {
		Type string `json:"type"`
		Data string `json:"data,omitempty"`
		URL  string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(raw.Data, &tagged); err != nil {
		return err
	}
	switch tagged.Type {
	case "url":
		f.URL = tagged.URL
		f.FileData = FileData{Type: FileDataTypeURL, URL: tagged.URL, MediaType: raw.MediaType}
	case "data", "":
		f.FileData = FileData{Type: FileDataTypeData, DataString: tagged.Data, MediaType: raw.MediaType}
		decoded, err := DecodeFileDataString(tagged.Data)
		if err == nil {
			f.Data = decoded
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ToolCallContent represents a tool call in ordered model output content.
// It mirrors the TypeScript SDK's `type: "tool-call"` content part while the
// top-level GenerateResult.ToolCalls field remains available for callers that
// consume tool calls separately.
type ToolCallContent struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`

	// Input preserves the raw JSON-string tool input used by the TypeScript SDK.
	Input string `json:"input,omitempty"`

	// Arguments is the decoded form of Input for idiomatic Go callers.
	Arguments map[string]interface{} `json:"arguments,omitempty"`

	ProviderExecuted bool                   `json:"providerExecuted,omitempty"`
	ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
	ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}

func (t ToolCallContent) ContentType() string {
	return "tool-call"
}

// CustomContent is a provider-specific content block with no standard mapping.
// Kind follows the format "{provider}-{provider-type}" (e.g., "xai-citation").
//
// This type serves dual duty matching both TS SDK roles:
//   - Output (LanguageModelV4CustomContent): ProviderMetadata carries raw JSON
//     returned by the provider.
//   - Input (LanguageModelV4CustomPart in assistant messages): ProviderOptions
//     carries provider-specific options to forward to the provider.
type CustomContent struct {
	// Kind identifies the provider-specific content type.
	// Format: "{provider}-{provider-type}"
	Kind string `json:"kind"`

	// ProviderOptions holds provider-specific options for the input (prompt) direction.
	// Keyed by provider name (e.g., "anthropic", "openai"). Used when this part
	// is included in an assistant message sent back to a provider.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// ProviderMetadata holds raw JSON metadata from the provider (output direction).
	// json.RawMessage round-trips cleanly and avoids interface{} allocations.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (c CustomContent) ContentType() string {
	return "custom"
}

// ReasoningFileContent is a file generated by the model during reasoning.
// Data is []byte; encoding/json automatically base64-encodes it when marshaling
// and decodes base64 back to []byte when unmarshaling — no DataBase64 field needed.
//
// This type serves dual duty matching both TS SDK roles:
//   - Output (LanguageModelV4ReasoningFile): ProviderMetadata carries raw JSON
//     returned by the provider.
//   - Input (LanguageModelV4ReasoningFilePart in assistant messages): ProviderOptions
//     carries provider-specific options to forward to the provider.
type ReasoningFileContent struct {
	// FileData is the normalized provider-facing tagged file data shape. Only
	// data and url variants are valid for reasoning-file parts.
	FileData FileData `json:"fileData,omitempty"`

	// MediaType is the IANA media type of the file (e.g., "image/png").
	MediaType string `json:"mediaType"`

	// Data holds the raw file bytes.
	// encoding/json marshals []byte as base64 and unmarshals base64 to []byte.
	Data []byte `json:"data"`

	// ProviderOptions holds provider-specific options for the input (prompt) direction.
	// Keyed by provider name (e.g., "anthropic", "openai"). Used when this part
	// is included in an assistant message sent back to a provider.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// ProviderMetadata holds optional raw JSON metadata from the provider (output direction).
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

// ContentType implements ContentPart interface
func (r ReasoningFileContent) ContentType() string {
	return "reasoning-file"
}

// ToolResultContent represents a tool execution result in a message
type ToolResultContent struct {
	// ID of the tool call this result corresponds to
	ToolCallID string `json:"toolCallId"`

	// Name of the tool that was executed
	ToolName string `json:"toolName"`

	// Result of the tool execution (can be any type)
	// DEPRECATED: Use Output for new code. Kept for backward compatibility.
	Result interface{} `json:"result,omitempty"`

	// Optional error if tool execution failed
	Error string `json:"error,omitempty"`

	// Structured output (takes precedence over Result)
	// Use this for rich tool outputs with multiple content blocks
	Output *ToolResultOutput `json:"output,omitempty"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ContentType implements ContentPart interface
func (t ToolResultContent) ContentType() string {
	return "tool-result"
}

// ToolResultOutputType represents the type of tool result output
type ToolResultOutputType string

const (
	// ToolResultOutputText represents simple text output
	ToolResultOutputText ToolResultOutputType = "text"

	// ToolResultOutputJSON represents JSON output
	ToolResultOutputJSON ToolResultOutputType = "json"

	// ToolResultOutputContent represents structured content with multiple blocks
	ToolResultOutputContent ToolResultOutputType = "content"

	// ToolResultOutputError represents error output
	ToolResultOutputError ToolResultOutputType = "error"

	// ToolResultOutputExecutionDenied represents a tool call that was denied by
	// the user approval gate before execution. The provider should receive a
	// clear signal that the tool was not run so it can decide how to proceed.
	ToolResultOutputExecutionDenied ToolResultOutputType = "execution-denied"
)

// ToolResultOutput represents structured tool result output
type ToolResultOutput struct {
	// Type of the output
	Type ToolResultOutputType `json:"type"`

	// Value for text/json/error types
	Value interface{} `json:"value,omitempty"`

	// Content blocks for content type (array of content blocks)
	Content []ToolResultContentBlock `json:"content,omitempty"`

	// Reason is an optional human-readable explanation used with
	// ToolResultOutputExecutionDenied to describe why the tool was not run.
	// Forwarded to the provider so the model understands the denial context.
	Reason string `json:"reason,omitempty"`
}

// ToolResultContentBlock represents a content block in tool results
// This interface allows different types of content in tool results
type ToolResultContentBlock interface {
	ToolResultContentType() string
}

// TextContentBlock represents text content in tool results
type TextContentBlock struct {
	// Text content
	Text string `json:"text"`

	// Provider-specific options (e.g., for tool-reference)
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (t TextContentBlock) ToolResultContentType() string {
	return "text"
}

// ImageContentBlock represents an image in tool results
type ImageContentBlock struct {
	// Image data as bytes
	Data []byte `json:"data"`

	// MIME type of the image (e.g., "image/png", "image/jpeg")
	MediaType string `json:"mediaType"`

	// Provider-specific options
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (i ImageContentBlock) ToolResultContentType() string {
	return "image"
}

// FileContentBlock represents a file in tool results.
// Set URL for file-url references (a remote URL, no data bytes required);
// set Data + MediaType for file-data (inline bytes).
type FileContentBlock struct {
	// FileData is the normalized provider-facing tagged file data shape.
	// When unset, legacy Data/URL/Text/Reference fields are normalized.
	FileData FileData `json:"fileData,omitempty"`

	// File data as bytes (file-data).
	Data []byte `json:"data,omitempty"`

	// MIME type of the file.
	MediaType string `json:"mediaType,omitempty"`

	// Optional filename.
	Filename string `json:"filename,omitempty"`

	// URL for file-url references (mutually exclusive with Data).
	URL string `json:"url,omitempty"`

	// Reference holds a provider file reference.
	Reference string `json:"reference,omitempty"`

	// Text holds inline text file content.
	Text string `json:"text,omitempty"`

	// Provider-specific options
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (f FileContentBlock) ToolResultContentType() string {
	return "file"
}

// CustomContentBlock represents provider-specific content
// This is used for features like tool-reference (Anthropic) or other
// provider-specific content types that don't fit standard categories
type CustomContentBlock struct {
	// Provider-specific options that define what this custom block represents
	// For example: map[string]interface{}{"anthropic": map[string]interface{}{"type": "tool-reference", "toolName": "calc"}}
	ProviderOptions map[string]interface{} `json:"providerOptions"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (c CustomContentBlock) ToolResultContentType() string {
	return "custom"
}

// Prompt represents a prompt that can be either a simple string or a list of messages
type Prompt struct {
	// Messages in the conversation (nil if using simple text prompt)
	Messages []Message

	// System message (optional, can be empty)
	System string

	// Simple text prompt (empty if using messages)
	Text string
}

// IsSimple returns true if this is a simple text prompt
func (p Prompt) IsSimple() bool {
	return p.Text != "" && len(p.Messages) == 0
}

// IsMessages returns true if this uses message-based prompts
func (p Prompt) IsMessages() bool {
	return len(p.Messages) > 0
}

// Helper functions for creating tool results

// SimpleTextResult creates a tool result with simple text (backward compatible)
// This is the old style and is maintained for backward compatibility.
//
// Example:
//
//	result := types.SimpleTextResult("call_123", "search", "Found 3 results")
func SimpleTextResult(toolCallID, toolName, result string) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Result:     result,
	}
}

// SimpleJSONResult creates a tool result with JSON value (backward compatible)
//
// Example:
//
//	result := types.SimpleJSONResult("call_123", "calculate", map[string]interface{}{"answer": 42})
func SimpleJSONResult(toolCallID, toolName string, result interface{}) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Result:     result,
	}
}

// ContentResult creates a tool result with structured content blocks (new style)
// This is the recommended way to create tool results with rich content.
//
// Example:
//
//	result := types.ContentResult("call_123", "search",
//	    types.TextContentBlock{Text: "Search results:"},
//	    types.ImageContentBlock{Data: imageBytes, MediaType: "image/png"},
//	)
func ContentResult(toolCallID, toolName string, blocks ...ToolResultContentBlock) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Output: &ToolResultOutput{
			Type:    ToolResultOutputContent,
			Content: blocks,
		},
	}
}

// ErrorResult creates a tool result representing an error
//
// Example:
//
//	result := types.ErrorResult("call_123", "search", "Network timeout")
func ErrorResult(toolCallID, toolName, errorMsg string) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Error:      errorMsg,
		Output: &ToolResultOutput{
			Type:  ToolResultOutputError,
			Value: errorMsg,
		},
	}
}
