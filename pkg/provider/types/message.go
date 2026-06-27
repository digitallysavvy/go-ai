package types

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (t TextContent) MarshalJSON() ([]byte, error) {
	type textContentAlias TextContent
	return json.Marshal(struct {
		Type string `json:"type"`
		textContentAlias
	}{
		Type:             t.ContentType(),
		textContentAlias: textContentAlias(t),
	})
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (r ReasoningContent) MarshalJSON() ([]byte, error) {
	type reasoningContentAlias ReasoningContent
	return json.Marshal(struct {
		Type string `json:"type"`
		reasoningContentAlias
	}{
		Type:                  r.ContentType(),
		reasoningContentAlias: reasoningContentAlias(r),
	})
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (i ImageContent) MarshalJSON() ([]byte, error) {
	type imageContentAlias ImageContent
	return json.Marshal(struct {
		Type string `json:"type"`
		imageContentAlias
	}{
		Type:              i.ContentType(),
		imageContentAlias: imageContentAlias(i),
	})
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (f FileContent) MarshalJSON() ([]byte, error) {
	type fileContentAlias FileContent
	return json.Marshal(struct {
		Type string `json:"type"`
		fileContentAlias
	}{
		Type:             f.ContentType(),
		fileContentAlias: fileContentAlias(f),
	})
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (s SourceContent) MarshalJSON() ([]byte, error) {
	type sourceContentAlias SourceContent
	return json.Marshal(struct {
		Type string `json:"type"`
		sourceContentAlias
	}{
		Type:               s.ContentType(),
		sourceContentAlias: sourceContentAlias(s),
	})
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

	// ProviderOptions holds provider-specific options when replaying this file
	// in an assistant message.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ContentType implements ContentPart interface
func (f GeneratedFileContent) ContentType() string {
	return "file"
}

// MarshalJSON emits the TypeScript SDK file shape:
// { mediaType, data: { type: "data"|"url", ... }, providerMetadata? }.
func (f GeneratedFileContent) MarshalJSON() ([]byte, error) {
	type generatedFileJSON struct {
		Type             string                 `json:"type"`
		MediaType        string                 `json:"mediaType"`
		Data             interface{}            `json:"data"`
		ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
		ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
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
		Type:             f.ContentType(),
		MediaType:        f.MediaType,
		Data:             data,
		ProviderMetadata: f.ProviderMetadata,
		ProviderOptions:  f.ProviderOptions,
	})
}

// UnmarshalJSON accepts both the TypeScript tagged data union and the older Go
// base64-string data shape for backward compatibility.
func (f *GeneratedFileContent) UnmarshalJSON(data []byte) error {
	type generatedFileJSON struct {
		MediaType        string                 `json:"mediaType"`
		Data             json.RawMessage        `json:"data"`
		URL              string                 `json:"url,omitempty"`
		FileData         FileData               `json:"fileData,omitempty"`
		ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
		ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
	}
	var raw generatedFileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.MediaType = raw.MediaType
	f.URL = raw.URL
	f.FileData = raw.FileData
	f.ProviderMetadata = raw.ProviderMetadata
	f.ProviderOptions = raw.ProviderOptions
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
	Title      string `json:"title,omitempty"`

	// Input preserves the raw JSON-string tool input used by the TypeScript SDK.
	Input string `json:"input,omitempty"`

	// Arguments is the decoded form of Input for idiomatic Go callers.
	Arguments map[string]interface{} `json:"arguments,omitempty"`

	ProviderExecuted bool                   `json:"providerExecuted,omitempty"`
	ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
	ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
	ToolMetadata     map[string]interface{} `json:"toolMetadata,omitempty"`
	Dynamic          bool                   `json:"dynamic,omitempty"`
	Invalid          bool                   `json:"invalid,omitempty"`
	Error            interface{}            `json:"error,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}

func (t ToolCallContent) ContentType() string {
	return "tool-call"
}

// MarshalJSON emits the TypeScript SDK content-part shape. Arguments remains
// available to Go callers and provider converters, but JSON uses input.
func (t ToolCallContent) MarshalJSON() ([]byte, error) {
	type toolCallContentJSON struct {
		Type             string                 `json:"type"`
		ToolCallID       string                 `json:"toolCallId"`
		ToolName         string                 `json:"toolName"`
		Title            string                 `json:"title,omitempty"`
		Input            interface{}            `json:"input,omitempty"`
		ProviderExecuted bool                   `json:"providerExecuted,omitempty"`
		ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
		ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
		ToolMetadata     map[string]interface{} `json:"toolMetadata,omitempty"`
		Dynamic          bool                   `json:"dynamic,omitempty"`
		Invalid          bool                   `json:"invalid,omitempty"`
		Error            interface{}            `json:"error,omitempty"`
		ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
	}
	return json.Marshal(toolCallContentJSON{
		Type:             t.ContentType(),
		ToolCallID:       t.ToolCallID,
		ToolName:         t.ToolName,
		Title:            t.Title,
		Input:            toolCallContentInput(t),
		ProviderExecuted: t.ProviderExecuted,
		ProviderOptions:  t.ProviderOptions,
		ProviderMetadata: t.ProviderMetadata,
		ToolMetadata:     t.ToolMetadata,
		Dynamic:          t.Dynamic,
		Invalid:          t.Invalid,
		Error:            t.Error,
		ThoughtSignature: t.ThoughtSignature,
	})
}

func toolCallContentInput(t ToolCallContent) interface{} {
	if t.Arguments != nil {
		return t.Arguments
	}
	if t.Input == "" {
		return nil
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(t.Input), &parsed); err == nil {
		return parsed
	}
	return t.Input
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (c CustomContent) MarshalJSON() ([]byte, error) {
	type customContentAlias CustomContent
	return json.Marshal(struct {
		Type string `json:"type"`
		customContentAlias
	}{
		Type:               c.ContentType(),
		customContentAlias: customContentAlias(c),
	})
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

// MarshalJSON emits the TypeScript SDK discriminated content-part shape.
func (r ReasoningFileContent) MarshalJSON() ([]byte, error) {
	type reasoningFileContentAlias ReasoningFileContent
	return json.Marshal(struct {
		Type string `json:"type"`
		reasoningFileContentAlias
	}{
		Type:                      r.ContentType(),
		reasoningFileContentAlias: reasoningFileContentAlias(r),
	})
}

// ToolResultContent represents a tool execution result in a message
type ToolResultContent struct {
	// ID of the tool call this result corresponds to
	ToolCallID string `json:"toolCallId"`

	// Name of the tool that was executed
	ToolName string `json:"toolName"`

	// Title is a short, human-readable title for the tool result.
	Title string `json:"title,omitempty"`

	// Input contains the tool input associated with this result.
	Input map[string]interface{} `json:"input,omitempty"`

	// Result of the tool execution (can be any type)
	// DEPRECATED: Use Output for new code. Kept for backward compatibility.
	Result interface{} `json:"result,omitempty"`

	// Optional error if tool execution failed
	Error string `json:"error,omitempty"`

	// Structured output (takes precedence over Result)
	// Use this for rich tool outputs with multiple content blocks
	Output *ToolResultOutput `json:"output,omitempty"`

	// ProviderExecuted indicates the result was produced by the model provider
	// and belongs in the assistant response content instead of a separate tool message.
	ProviderExecuted bool `json:"providerExecuted,omitempty"`

	// ProviderOptions holds provider-specific options for the input direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	// ProviderMetadata holds provider-specific metadata from output content.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`

	// ToolMetadata carries tool-specific metadata associated with this result.
	ToolMetadata map[string]interface{} `json:"toolMetadata,omitempty"`

	// Dynamic indicates this result belongs to a dynamic tool call.
	Dynamic bool `json:"dynamic,omitempty"`

	// Preliminary indicates this is an intermediate streamed result rather than
	// the final result for the tool call.
	Preliminary bool `json:"preliminary,omitempty"`
}

// ContentType implements ContentPart interface
func (t ToolResultContent) ContentType() string {
	return "tool-result"
}

// MarshalJSON emits the TypeScript SDK content-part shape. Result is retained
// as a deprecated Go field, but JSON uses output when present.
func (t ToolResultContent) MarshalJSON() ([]byte, error) {
	type toolResultContentJSON struct {
		Type             string                 `json:"type"`
		ToolCallID       string                 `json:"toolCallId"`
		ToolName         string                 `json:"toolName"`
		Title            string                 `json:"title,omitempty"`
		Input            map[string]interface{} `json:"input,omitempty"`
		Result           interface{}            `json:"result,omitempty"`
		Error            string                 `json:"error,omitempty"`
		Output           *ToolResultOutput      `json:"output,omitempty"`
		ProviderExecuted bool                   `json:"providerExecuted,omitempty"`
		ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
		ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
		ToolMetadata     map[string]interface{} `json:"toolMetadata,omitempty"`
		Dynamic          bool                   `json:"dynamic,omitempty"`
		Preliminary      bool                   `json:"preliminary,omitempty"`
	}
	return json.Marshal(toolResultContentJSON{
		Type:             t.ContentType(),
		ToolCallID:       t.ToolCallID,
		ToolName:         t.ToolName,
		Title:            t.Title,
		Input:            t.Input,
		Result:           t.Result,
		Error:            t.Error,
		Output:           t.Output,
		ProviderExecuted: t.ProviderExecuted,
		ProviderOptions:  t.ProviderOptions,
		ProviderMetadata: t.ProviderMetadata,
		ToolMetadata:     t.ToolMetadata,
		Dynamic:          t.Dynamic,
		Preliminary:      t.Preliminary,
	})
}

// ToolErrorContent represents a failed tool execution in ordered content.
type ToolErrorContent struct {
	ToolCallID       string                 `json:"toolCallId"`
	ToolName         string                 `json:"toolName"`
	Title            string                 `json:"title,omitempty"`
	Input            map[string]interface{} `json:"input,omitempty"`
	Error            interface{}            `json:"error"`
	ProviderExecuted bool                   `json:"providerExecuted,omitempty"`
	ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
	ProviderMetadata json.RawMessage        `json:"providerMetadata,omitempty"`
	ToolMetadata     map[string]interface{} `json:"toolMetadata,omitempty"`
	Dynamic          bool                   `json:"dynamic,omitempty"`
}

func (t ToolErrorContent) ContentType() string {
	return "tool-error"
}

// ToolApprovalRequestContent indicates that a tool call requires approval before execution.
type ToolApprovalRequestContent struct {
	ApprovalID  string   `json:"approvalId"`
	ToolCallID  string   `json:"toolCallId"`
	ToolCall    ToolCall `json:"toolCall,omitempty"`
	Signature   string   `json:"signature,omitempty"`
	IsAutomatic bool     `json:"isAutomatic,omitempty"`
}

func (t ToolApprovalRequestContent) ContentType() string {
	return "tool-approval-request"
}

// MarshalJSON omits the full tool call when the part has been normalized for
// provider replay. Public result content still includes ToolCall when present.
func (t ToolApprovalRequestContent) MarshalJSON() ([]byte, error) {
	type toolApprovalRequestContentJSON struct {
		Type        string           `json:"type"`
		ApprovalID  string           `json:"approvalId"`
		ToolCallID  string           `json:"toolCallId,omitempty"`
		ToolCall    *ToolCallContent `json:"toolCall,omitempty"`
		Signature   string           `json:"signature,omitempty"`
		IsAutomatic bool             `json:"isAutomatic,omitempty"`
	}
	out := toolApprovalRequestContentJSON{
		Type:        t.ContentType(),
		ApprovalID:  t.ApprovalID,
		Signature:   t.Signature,
		IsAutomatic: t.IsAutomatic,
	}
	if !toolCallIsZero(t.ToolCall) {
		toolCall := toolCallContentFromToolCall(t.ToolCall)
		out.ToolCall = &toolCall
	} else {
		out.ToolCallID = t.ToolCallID
	}
	return json.Marshal(out)
}

// ToolApprovalResponseContent indicates that approval was granted or denied.
type ToolApprovalResponseContent struct {
	ApprovalID       string   `json:"approvalId"`
	ToolCallID       string   `json:"toolCallId,omitempty"`
	ToolCall         ToolCall `json:"toolCall,omitempty"`
	Approved         bool     `json:"approved"`
	Reason           string   `json:"reason,omitempty"`
	ProviderExecuted bool     `json:"providerExecuted,omitempty"`
}

func (t ToolApprovalResponseContent) ContentType() string {
	return "tool-approval-response"
}

// MarshalJSON omits the full tool call when the part has been normalized for
// provider replay. Public result content still includes ToolCall when present.
func (t ToolApprovalResponseContent) MarshalJSON() ([]byte, error) {
	type toolApprovalResponseContentJSON struct {
		Type             string           `json:"type"`
		ApprovalID       string           `json:"approvalId"`
		ToolCall         *ToolCallContent `json:"toolCall,omitempty"`
		Approved         bool             `json:"approved"`
		Reason           string           `json:"reason,omitempty"`
		ProviderExecuted bool             `json:"providerExecuted,omitempty"`
	}
	out := toolApprovalResponseContentJSON{
		Type:             t.ContentType(),
		ApprovalID:       t.ApprovalID,
		Approved:         t.Approved,
		Reason:           t.Reason,
		ProviderExecuted: t.ProviderExecuted,
	}
	if !toolCallIsZero(t.ToolCall) {
		toolCall := toolCallContentFromToolCall(t.ToolCall)
		out.ToolCall = &toolCall
	}
	return json.Marshal(out)
}

func toolCallContentFromToolCall(call ToolCall) ToolCallContent {
	return ToolCallContent{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.RawArguments,
		Arguments:        call.Arguments,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: providerMetadataRawFromMap(call.ProviderMetadata),
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
		Invalid:          call.Invalid,
		Error:            toolCallErrorValue(call.Error),
		ThoughtSignature: call.ThoughtSignature,
	}
}

func toolCallErrorValue(err error) interface{} {
	if err == nil {
		return nil
	}
	return err.Error()
}

func providerMetadataRawFromMap(metadata map[string]interface{}) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return raw
}

func toolCallIsZero(call ToolCall) bool {
	return call.ID == "" &&
		call.ToolName == "" &&
		call.Title == "" &&
		len(call.Arguments) == 0 &&
		!call.ProviderExecuted &&
		len(call.ProviderMetadata) == 0 &&
		len(call.ToolMetadata) == 0 &&
		call.ThoughtSignature == "" &&
		!call.Dynamic &&
		!call.Invalid &&
		call.Error == nil
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

	// ToolResultOutputErrorText represents a text error output.
	ToolResultOutputErrorText ToolResultOutputType = "error-text"

	// ToolResultOutputErrorJSON represents a JSON error output.
	ToolResultOutputErrorJSON ToolResultOutputType = "error-json"

	// ToolResultOutputExecutionDenied represents a tool call that was denied by
	// the user approval gate before execution. The provider should receive a
	// clear signal that the tool was not run so it can decide how to proceed.
	ToolResultOutputExecutionDenied ToolResultOutputType = "execution-denied"
)

var ErrMissingProviderReferenceContext = errors.New("cannot marshal legacy file reference without provider reference map")

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

	// ProviderOptions holds provider-specific options for the output direction.
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// MarshalJSON emits the TypeScript SDK ToolResultOutput union shape. Content
// output uses "value" for the block array; the Content field is the idiomatic
// Go mirror used by provider converters.
func (o ToolResultOutput) MarshalJSON() ([]byte, error) {
	type outputJSON struct {
		Type            ToolResultOutputType   `json:"type"`
		Value           json.RawMessage        `json:"value,omitempty"`
		Reason          string                 `json:"reason,omitempty"`
		ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
	}
	out := outputJSON{
		Type:            o.Type,
		Reason:          o.Reason,
		ProviderOptions: o.ProviderOptions,
	}
	marshalValue := func(value interface{}) error {
		if value == nil {
			out.Value = json.RawMessage("null")
			return nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		out.Value = data
		return nil
	}
	switch o.Type {
	case ToolResultOutputContent:
		if o.Value != nil {
			if err := marshalValue(o.Value); err != nil {
				return nil, err
			}
		} else if o.Content != nil {
			if err := marshalValue(o.Content); err != nil {
				return nil, err
			}
		} else {
			if err := marshalValue([]ToolResultContentBlock{}); err != nil {
				return nil, err
			}
		}
	case ToolResultOutputExecutionDenied:
	default:
		if err := marshalValue(o.Value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON accepts the TypeScript SDK ToolResultOutput union shape and the
// older Go "content" field for backward compatibility.
func (o *ToolResultOutput) UnmarshalJSON(data []byte) error {
	type outputJSON struct {
		Type            ToolResultOutputType   `json:"type"`
		Value           json.RawMessage        `json:"value"`
		Content         json.RawMessage        `json:"content"`
		Reason          string                 `json:"reason"`
		ProviderOptions map[string]interface{} `json:"providerOptions"`
	}
	var raw outputJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	o.Type = raw.Type
	o.Reason = raw.Reason
	o.ProviderOptions = raw.ProviderOptions
	o.Value = nil
	o.Content = nil

	if raw.Type == ToolResultOutputContent {
		contentRaw := raw.Value
		if len(contentRaw) == 0 {
			contentRaw = raw.Content
		}
		if len(contentRaw) == 0 || string(contentRaw) == "null" {
			o.Content = []ToolResultContentBlock{}
			return nil
		}
		blocks, err := unmarshalToolResultContentBlocks(contentRaw)
		if err != nil {
			return err
		}
		o.Content = blocks
		return nil
	}
	if len(raw.Value) > 0 {
		if err := json.Unmarshal(raw.Value, &o.Value); err != nil {
			return err
		}
	}
	return nil
}

func unmarshalToolResultContentBlocks(data []byte) ([]ToolResultContentBlock, error) {
	var rawBlocks []json.RawMessage
	if err := json.Unmarshal(data, &rawBlocks); err != nil {
		return nil, err
	}
	blocks := make([]ToolResultContentBlock, 0, len(rawBlocks))
	for _, rawBlock := range rawBlocks {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawBlock, &envelope); err != nil {
			return nil, err
		}
		switch envelope.Type {
		case "text":
			var block TextContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		case "file":
			var block FileContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		case "file-data", "file-url", "file-id", "file-reference", "image-data", "image-url", "image-file-id", "image-file-reference":
			block, err := unmarshalLegacyToolResultFileContentBlock(envelope.Type, rawBlock)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		case "image":
			var block ImageContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		case "custom":
			var block CustomContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		default:
			var block RawToolResultContentBlock
			if err := json.Unmarshal(rawBlock, &block); err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func unmarshalLegacyToolResultFileContentBlock(blockType string, data []byte) (FileContentBlock, error) {
	type legacyBlock struct {
		Data              string                 `json:"data"`
		URL               string                 `json:"url"`
		FileID            interface{}            `json:"fileId"`
		ProviderReference ProviderReference      `json:"providerReference"`
		MediaType         string                 `json:"mediaType"`
		Filename          string                 `json:"filename"`
		ProviderOptions   map[string]interface{} `json:"providerOptions"`
	}
	var raw legacyBlock
	if err := json.Unmarshal(data, &raw); err != nil {
		return FileContentBlock{}, err
	}
	block := FileContentBlock{
		MediaType:       raw.MediaType,
		Filename:        raw.Filename,
		ProviderOptions: raw.ProviderOptions,
	}
	switch blockType {
	case "file-data", "image-data":
		block.FileData = FileData{Type: FileDataTypeData, DataString: raw.Data}
		if block.MediaType == "" && strings.HasPrefix(blockType, "image-") {
			block.MediaType = "image"
		}
	case "file-url", "image-url":
		block.FileData = FileData{Type: FileDataTypeURL, URL: raw.URL}
		block.URL = raw.URL
		if block.MediaType == "" {
			if blockType == "image-url" {
				block.MediaType = "image"
			} else {
				block.MediaType = inferToolResultMediaTypeFromURL(raw.URL)
			}
		}
	case "file-id", "image-file-id":
		block.FileData = FileData{Type: FileDataTypeReference, Reference: providerReferenceFromLegacyFileID(raw.FileID)}
		block.MediaType = legacyReferenceMediaType(blockType, block.MediaType)
	case "file-reference", "image-file-reference":
		block.FileData = FileData{Type: FileDataTypeReference, Reference: raw.ProviderReference}
		block.MediaType = legacyReferenceMediaType(blockType, block.MediaType)
	}
	return block, nil
}

func providerReferenceFromLegacyFileID(fileID interface{}) ProviderReference {
	switch value := fileID.(type) {
	case string:
		return ProviderReference{"": value}
	case map[string]interface{}:
		ref := make(ProviderReference, len(value))
		for provider, id := range value {
			if text, ok := id.(string); ok {
				ref[provider] = text
			}
		}
		return ref
	default:
		return nil
	}
}

func legacyReferenceMediaType(blockType, mediaType string) string {
	if mediaType != "" {
		return mediaType
	}
	if strings.HasPrefix(blockType, "image-") {
		return "image"
	}
	return "application"
}

func inferToolResultMediaTypeFromURL(value string) string {
	path := value
	if idx := strings.IndexAny(path, "?#"); idx >= 0 {
		path = path[:idx]
	}
	idx := strings.LastIndex(path, ".")
	if idx < 0 || idx == len(path)-1 {
		return "application/octet-stream"
	}
	switch strings.ToLower(path[idx+1:]) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "avif":
		return "image/avif"
	case "heic":
		return "image/heic"
	case "bmp":
		return "image/bmp"
	case "tiff", "tif":
		return "image/tiff"
	case "pdf":
		return "application/pdf"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

// ToolResultContentBlock represents a content block in tool results
// This interface allows different types of content in tool results
type ToolResultContentBlock interface {
	ToolResultContentType() string
}

// RawToolResultContentBlock preserves unknown tool-result content variants.
// TypeScript's mapToolResultOutput returns unknown variants unchanged.
type RawToolResultContentBlock struct {
	Type   string
	Fields map[string]json.RawMessage
}

func (r RawToolResultContentBlock) ToolResultContentType() string {
	return r.Type
}

func (r RawToolResultContentBlock) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(r.Fields)+1)
	for key, value := range r.Fields {
		fields[key] = value
	}
	if r.Type != "" {
		typeData, err := json.Marshal(r.Type)
		if err != nil {
			return nil, err
		}
		fields["type"] = typeData
	}
	return json.Marshal(fields)
}

func (r *RawToolResultContentBlock) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	r.Fields = fields
	if typeData, ok := fields["type"]; ok {
		_ = json.Unmarshal(typeData, &r.Type)
	}
	return nil
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

// MarshalJSON emits the TypeScript SDK discriminated tool-result content shape.
func (t TextContentBlock) MarshalJSON() ([]byte, error) {
	type textContentBlockAlias TextContentBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		textContentBlockAlias
	}{
		Type:                  t.ToolResultContentType(),
		textContentBlockAlias: textContentBlockAlias(t),
	})
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

// MarshalJSON emits the modern TypeScript SDK file tool-result shape for image
// content. Legacy image-* aliases are accepted on input but not emitted.
func (i ImageContentBlock) MarshalJSON() ([]byte, error) {
	type imageContentBlockJSON struct {
		Type            string                 `json:"type"`
		Data            FileData               `json:"data"`
		MediaType       string                 `json:"mediaType"`
		ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
	}
	return json.Marshal(imageContentBlockJSON{
		Type:            "file",
		Data:            FileData{Type: FileDataTypeData, Data: i.Data},
		MediaType:       i.MediaType,
		ProviderOptions: i.ProviderOptions,
	})
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

// MarshalJSON emits the TypeScript SDK tagged file tool-result shape.
func (f FileContentBlock) MarshalJSON() ([]byte, error) {
	type fileContentBlockJSON struct {
		Type            string                 `json:"type"`
		Data            interface{}            `json:"data"`
		MediaType       string                 `json:"mediaType"`
		Filename        string                 `json:"filename,omitempty"`
		ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
	}
	data := interface{}(f.FileData)
	switch {
	case !f.FileData.IsZero():
	case f.URL != "":
		data = FileData{Type: FileDataTypeURL, URL: f.URL}
	case f.Reference != "":
		return nil, ErrMissingProviderReferenceContext
	case f.Text != "":
		data = FileData{Type: FileDataTypeText, Text: f.Text}
	default:
		data = FileData{Type: FileDataTypeData, Data: f.Data}
	}
	return json.Marshal(fileContentBlockJSON{
		Type:            f.ToolResultContentType(),
		Data:            data,
		MediaType:       firstNonEmptyString(f.MediaType, f.FileData.MediaType),
		Filename:        f.Filename,
		ProviderOptions: f.ProviderOptions,
	})
}

// UnmarshalJSON accepts the TypeScript SDK tagged file tool-result shape.
func (f *FileContentBlock) UnmarshalJSON(data []byte) error {
	type fileContentBlockJSON struct {
		Data            FileData               `json:"data"`
		MediaType       string                 `json:"mediaType"`
		Filename        string                 `json:"filename"`
		ProviderOptions map[string]interface{} `json:"providerOptions"`
	}
	var raw fileContentBlockJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.FileData = raw.Data
	f.MediaType = raw.MediaType
	f.Filename = raw.Filename
	f.ProviderOptions = raw.ProviderOptions
	switch raw.Data.Type {
	case FileDataTypeURL:
		f.URL = raw.Data.URL
	case FileDataTypeReference:
		if len(raw.Data.Reference) == 1 {
			for _, value := range raw.Data.Reference {
				f.Reference = value
			}
		}
	case FileDataTypeText:
		f.Text = raw.Data.Text
	case FileDataTypeData:
		f.Data = raw.Data.Data
	}
	return nil
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

// MarshalJSON emits the TypeScript SDK custom tool-result content shape.
func (c CustomContentBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type            string                 `json:"type"`
		ProviderOptions map[string]interface{} `json:"providerOptions"`
	}{
		Type:            c.ToolResultContentType(),
		ProviderOptions: c.ProviderOptions,
	})
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
			Type:  ToolResultOutputErrorText,
			Value: errorMsg,
		},
	}
}
