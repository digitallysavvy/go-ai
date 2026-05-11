package prompt

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// InvalidInlineDataURLError is returned when a data: URL cannot be parsed into
// inline file bytes.
type InvalidInlineDataURLError struct {
	Value string
	Cause error
}

func (e *InvalidInlineDataURLError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("invalid inline data URL %q: %v", e.Value, e.Cause)
	}
	return fmt.Sprintf("invalid inline data URL %q", e.Value)
}

func (e *InvalidInlineDataURLError) Unwrap() error {
	return e.Cause
}

// ReasoningFileConstraintError is returned when a reasoning-file part uses a
// FileData variant that cannot represent model-produced reasoning output.
type ReasoningFileConstraintError struct {
	Type types.FileDataType
}

func (e *ReasoningFileConstraintError) Error() string {
	return fmt.Sprintf("reasoning-file data type %q is not supported; allowed types are %q and %q", e.Type, types.FileDataTypeData, types.FileDataTypeURL)
}

// UnsupportedSystemMessageError is returned when a messages prompt contains a
// system-role message without the explicit AllowSystemMessages opt-in.
type UnsupportedSystemMessageError struct{}

func (e *UnsupportedSystemMessageError) Error() string {
	return "system messages are not allowed in the messages field; use the system option instead or set AllowSystemMessages"
}

// NormalizePrompt applies the core prompt conversion rules before providers see
// the request: reject system messages by default, rewrite deprecated image parts
// to file parts, normalize file data shorthands, and enforce reasoning-file
// constraints.
func NormalizePrompt(prompt types.Prompt, allowSystemMessages bool) (types.Prompt, error) {
	messages, err := NormalizeMessages(prompt.Messages, allowSystemMessages)
	if err != nil {
		return types.Prompt{}, err
	}
	prompt.Messages = messages
	return prompt, nil
}

// NormalizeMessages rewrites message content into provider-facing v4 content.
func NormalizeMessages(messages []types.Message, allowSystemMessages bool) ([]types.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	normalizedMessages := make([]types.Message, len(messages))
	for i, message := range messages {
		if message.Role == types.RoleSystem && !allowSystemMessages {
			return nil, &UnsupportedSystemMessageError{}
		}

		normalizedMessage := message
		content, err := NormalizeContentParts(message.Content)
		if err != nil {
			return nil, err
		}
		normalizedMessage.Content = content
		normalizedMessages[i] = normalizedMessage
	}
	return normalizedMessages, nil
}

// NormalizeContentParts rewrites content parts into provider-facing v4 content.
func NormalizeContentParts(parts []types.ContentPart) ([]types.ContentPart, error) {
	if len(parts) == 0 {
		return parts, nil
	}

	normalized := make([]types.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch typed := part.(type) {
		case types.ImageContent:
			file, err := NormalizeImageContent(typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case *types.ImageContent:
			file, err := NormalizeImageContent(*typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case types.FileContent:
			file, err := NormalizeFileContent(typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case *types.FileContent:
			file, err := NormalizeFileContent(*typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case types.ReasoningFileContent:
			file, err := NormalizeReasoningFileContent(typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case *types.ReasoningFileContent:
			file, err := NormalizeReasoningFileContent(*typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, file)
		case types.ToolResultContent:
			toolResult, err := NormalizeToolResultContent(typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, toolResult)
		case *types.ToolResultContent:
			toolResult, err := NormalizeToolResultContent(*typed)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, toolResult)
		default:
			normalized = append(normalized, part)
		}
	}
	return normalized, nil
}

// NormalizeFileData validates and normalizes a tagged FileData value. It also
// converts data: URLs passed through the URL variant into inline data bytes.
func NormalizeFileData(data types.FileData) (types.FileData, error) {
	switch data.Type {
	case "":
		return data, nil
	case types.FileDataTypeData:
		if data.URL != "" && strings.HasPrefix(data.URL, "data:") {
			return types.FileData{}, &InvalidInlineDataURLError{
				Value: data.URL,
				Cause: fmt.Errorf("data URLs must use the %q variant, not %q", types.FileDataTypeURL, types.FileDataTypeData),
			}
		}
		if err := validateMediaType(data.MediaType); err != nil {
			return types.FileData{}, err
		}
		return data, nil
	case types.FileDataTypeURL:
		if strings.HasPrefix(data.URL, "data:") {
			return ParseDataURL(data.URL)
		}
		if err := validateRemoteFileURL(data.URL); err != nil {
			return types.FileData{}, err
		}
		if err := validateMediaType(data.MediaType); err != nil {
			return types.FileData{}, err
		}
		return data, nil
	case types.FileDataTypeReference:
		if len(data.Reference) == 0 {
			return types.FileData{}, fmt.Errorf("file reference is required")
		}
		if err := validateMediaType(data.MediaType); err != nil {
			return types.FileData{}, err
		}
		return data, nil
	case types.FileDataTypeText:
		if err := validateMediaType(data.MediaType); err != nil {
			return types.FileData{}, err
		}
		return data, nil
	default:
		return types.FileData{}, fmt.Errorf("unsupported file data type %q", data.Type)
	}
}

// NormalizeFileContent accepts the current public Go file shape plus the new
// FileData field and returns a provider-facing FileContent with FileData set.
func NormalizeFileContent(part types.FileContent) (types.FileContent, error) {
	mediaType := firstNonEmpty(part.MediaType, part.MimeType, part.FileData.MediaType)
	data := part.FileData
	if data.IsZero() {
		switch {
		case len(part.Data) > 0:
			data = types.FileData{Type: types.FileDataTypeData, Data: part.Data, MediaType: mediaType}
		case part.URL != "":
			data = types.FileData{Type: types.FileDataTypeURL, URL: part.URL, MediaType: mediaType}
		case part.Reference != "":
			data = types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"": part.Reference}, MediaType: mediaType}
		case part.Text != "":
			data = types.FileData{Type: types.FileDataTypeText, Text: part.Text, MediaType: mediaType}
		}
	} else if data.MediaType == "" {
		data.MediaType = mediaType
	}

	normalized, err := NormalizeFileData(data)
	if err != nil {
		return types.FileContent{}, err
	}
	part.FileData = normalized
	part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType, part.MimeType)
	part.MimeType = firstNonEmpty(part.MimeType, part.MediaType)
	part.Data = nil
	part.URL = ""
	part.Reference = ""
	part.Text = ""
	switch normalized.Type {
	case types.FileDataTypeData:
		part.Data = normalized.Data
	case types.FileDataTypeURL:
		part.URL = normalized.URL
	case types.FileDataTypeReference:
		part.Reference = types.ProviderReferenceString(normalized.Reference)
	case types.FileDataTypeText:
		part.Text = normalized.Text
	}
	return part, nil
}

// NormalizeImageContent is the deprecated image-part interop shim. It rewrites
// an ImageContent into the equivalent FileContent with an image media type.
func NormalizeImageContent(part types.ImageContent) (types.FileContent, error) {
	mediaType := firstNonEmpty(part.MimeType, "image")
	file := types.FileContent{
		MediaType:       mediaType,
		MimeType:        mediaType,
		ProviderOptions: part.ProviderOptions,
	}
	if part.URL != "" {
		file.FileData = types.FileData{Type: types.FileDataTypeURL, URL: part.URL, MediaType: mediaType}
	} else {
		file.FileData = types.FileData{Type: types.FileDataTypeData, Data: part.Image, MediaType: mediaType}
		file.Data = part.Image
	}
	return NormalizeFileContent(file)
}

// NormalizeToolResultFileContent aligns tool-result file blocks with the same
// FileData shape used by top-level file content.
func NormalizeToolResultFileContent(block types.FileContentBlock) (types.FileContentBlock, error) {
	mediaType := firstNonEmpty(block.MediaType, block.FileData.MediaType)
	data := block.FileData
	if data.IsZero() {
		switch {
		case len(block.Data) > 0:
			data = types.FileData{Type: types.FileDataTypeData, Data: block.Data, MediaType: mediaType}
		case block.URL != "":
			data = types.FileData{Type: types.FileDataTypeURL, URL: block.URL, MediaType: mediaType}
		case block.Reference != "":
			data = types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"": block.Reference}, MediaType: mediaType}
		case block.Text != "":
			data = types.FileData{Type: types.FileDataTypeText, Text: block.Text, MediaType: mediaType}
		}
	} else if data.MediaType == "" {
		data.MediaType = mediaType
	}

	normalized, err := NormalizeFileData(data)
	if err != nil {
		return types.FileContentBlock{}, err
	}
	block.FileData = normalized
	block.MediaType = firstNonEmpty(block.MediaType, normalized.MediaType)
	block.Data = nil
	block.URL = ""
	block.Reference = ""
	block.Text = ""
	switch normalized.Type {
	case types.FileDataTypeData:
		block.Data = normalized.Data
	case types.FileDataTypeURL:
		block.URL = normalized.URL
	case types.FileDataTypeReference:
		block.Reference = types.ProviderReferenceString(normalized.Reference)
	case types.FileDataTypeText:
		block.Text = normalized.Text
	}
	return block, nil
}

// NormalizeToolResultContent rewrites structured tool-result file and image
// blocks to the same FileData shape used by top-level file content.
func NormalizeToolResultContent(part types.ToolResultContent) (types.ToolResultContent, error) {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputContent {
		return part, nil
	}

	blocks := make([]types.ToolResultContentBlock, 0, len(part.Output.Content))
	for _, block := range part.Output.Content {
		switch typed := block.(type) {
		case types.FileContentBlock:
			normalized, err := NormalizeToolResultFileContent(typed)
			if err != nil {
				return types.ToolResultContent{}, err
			}
			blocks = append(blocks, normalized)
		case *types.FileContentBlock:
			normalized, err := NormalizeToolResultFileContent(*typed)
			if err != nil {
				return types.ToolResultContent{}, err
			}
			blocks = append(blocks, normalized)
		case types.ImageContentBlock:
			normalized, err := NormalizeToolResultFileContent(types.FileContentBlock{
				Data:            typed.Data,
				MediaType:       firstNonEmpty(typed.MediaType, "image"),
				ProviderOptions: typed.ProviderOptions,
			})
			if err != nil {
				return types.ToolResultContent{}, err
			}
			blocks = append(blocks, normalized)
		case *types.ImageContentBlock:
			normalized, err := NormalizeToolResultFileContent(types.FileContentBlock{
				Data:            typed.Data,
				MediaType:       firstNonEmpty(typed.MediaType, "image"),
				ProviderOptions: typed.ProviderOptions,
			})
			if err != nil {
				return types.ToolResultContent{}, err
			}
			blocks = append(blocks, normalized)
		default:
			blocks = append(blocks, block)
		}
	}

	output := *part.Output
	output.Content = blocks
	part.Output = &output
	return part, nil
}

// NormalizeReasoningFileContent validates the reasoning-file data constraint:
// only data and url variants are allowed.
func NormalizeReasoningFileContent(part types.ReasoningFileContent) (types.ReasoningFileContent, error) {
	mediaType := firstNonEmpty(part.MediaType, part.FileData.MediaType)
	data := part.FileData
	if data.IsZero() {
		data = types.FileData{Type: types.FileDataTypeData, Data: part.Data, MediaType: mediaType}
	} else if data.MediaType == "" {
		data.MediaType = mediaType
	}
	normalized, err := NormalizeFileData(data)
	if err != nil {
		return types.ReasoningFileContent{}, err
	}
	if normalized.Type != types.FileDataTypeData && normalized.Type != types.FileDataTypeURL {
		return types.ReasoningFileContent{}, &ReasoningFileConstraintError{Type: normalized.Type}
	}
	part.FileData = normalized
	part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType)
	part.Data = nil
	if normalized.Type == types.FileDataTypeData {
		part.Data = normalized.Data
	}
	return part, nil
}

// ParseDataURL converts an RFC 2397 base64 data URL into a data FileData value.
func ParseDataURL(value string) (types.FileData, error) {
	prefix, rest, ok := strings.Cut(value, ":")
	if !ok || prefix != "data" {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: fmt.Errorf("missing data: prefix")}
	}
	meta, body, ok := strings.Cut(rest, ",")
	if !ok {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: fmt.Errorf("missing comma separator")}
	}
	parts := strings.Split(meta, ";")
	mediaType := parts[0]
	if mediaType == "" {
		mediaType = "text/plain"
	}
	if err := validateMediaType(mediaType); err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	if len(parts) == 1 || parts[len(parts)-1] != "base64" {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: fmt.Errorf("only base64 data URLs are supported")}
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	return types.FileData{Type: types.FileDataTypeData, Data: decoded, MediaType: mediaType}, nil
}

func validateRemoteFileURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("remote file URL must use http or https scheme")
	}
	if parsed.Host == "" {
		return fmt.Errorf("remote file URL must include a host")
	}
	return nil
}

func validateMediaType(mediaType string) error {
	if mediaType == "" {
		return nil
	}
	if strings.Count(mediaType, "/") == 0 {
		return nil
	}
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return err
	}
	if parsed == "" || strings.HasSuffix(parsed, "/") {
		return fmt.Errorf("invalid media type %q", mediaType)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
