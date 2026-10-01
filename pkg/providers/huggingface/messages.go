package huggingface

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// hfInputMessage mirrors one entry of the "input" array sent to the
// Responses API. Content is either a plain string (system messages) or an
// array of hfContentPart values (user/assistant messages), matching TS's
// untyped `Array<any>` input shape.
type hfInputMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// hfContentPart mirrors a single input_text/input_image/output_text content
// entry.
type hfContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// convertToHuggingFaceResponsesInput mirrors TS
// convertToHuggingFaceResponsesMessages. It builds the "input" array sent to
// the Responses API from the full prompt (Prompt.System, if set, becomes a
// leading system message; Prompt.Messages is then converted role by role).
func convertToHuggingFaceResponsesInput(prompt types.Prompt) ([]interface{}, []types.Warning, error) {
	var input []interface{}
	var warnings []types.Warning

	if prompt.System != "" {
		input = append(input, hfInputMessage{Role: "system", Content: prompt.System})
	}

	for _, msg := range prompt.Messages {
		switch msg.Role {
		case types.RoleSystem:
			input = append(input, hfInputMessage{Role: "system", Content: hfExtractText(msg.Content)})

		case types.RoleUser:
			content, err := convertHFUserContent(msg.Content)
			if err != nil {
				return nil, warnings, err
			}
			input = append(input, hfInputMessage{Role: "user", Content: content})

		case types.RoleAssistant:
			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					input = append(input, hfInputMessage{
						Role:    "assistant",
						Content: []hfContentPart{{Type: "output_text", Text: p.Text}},
					})
				case types.ReasoningContent:
					// TS includes reasoning content in the message text too
					// (comment: "include reasoning content in the message text").
					input = append(input, hfInputMessage{
						Role:    "assistant",
						Content: []hfContentPart{{Type: "output_text", Text: p.Text}},
					})
				case types.ToolCallContent, types.ToolResultContent:
					// Tool calls/results are handled by the Responses API itself.
				}
			}

		case types.RoleTool:
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "tool messages"})
		}
	}

	if input == nil {
		input = []interface{}{}
	}
	return input, warnings, nil
}

// hfExtractText concatenates the text of every TextContent part, used to
// recover a plain-string system message from a Go Message's []ContentPart.
func hfExtractText(parts []types.ContentPart) string {
	var sb strings.Builder
	for _, part := range parts {
		if tc, ok := part.(types.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// convertHFUserContent mirrors the 'user' case of TS
// convertToHuggingFaceResponsesMessages.
func convertHFUserContent(parts []types.ContentPart) ([]interface{}, error) {
	result := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			result = append(result, hfContentPart{Type: "input_text", Text: p.Text})

		case types.FileContent:
			imagePart, err := hfConvertFileToImagePart(p)
			if err != nil {
				return nil, err
			}
			result = append(result, imagePart)

		case types.ImageContent:
			imagePart, err := hfConvertImageContentToImagePart(p)
			if err != nil {
				return nil, err
			}
			result = append(result, imagePart)
		}
	}
	return result, nil
}

// hfConvertImageContentToImagePart handles the legacy ImageContent part
// type. TS's LanguageModelV4Prompt has no separate image part (only file
// parts), but the Go SDK's Prompt may still carry ImageContent for parity
// with older callers; it is treated the same as an image file part.
func hfConvertImageContentToImagePart(img types.ImageContent) (hfContentPart, error) {
	if img.URL != "" {
		return hfContentPart{Type: "input_image", ImageURL: img.URL}, nil
	}
	mediaType := img.MimeType
	if mediaType == "" {
		mediaType = "image/jpeg"
	}
	full, err := hfResolveFullMediaType(mediaType, img.Image)
	if err != nil {
		return hfContentPart{}, err
	}
	return hfContentPart{
		Type:     "input_image",
		ImageURL: fmt.Sprintf("data:%s;base64,%s", full, base64.StdEncoding.EncodeToString(img.Image)),
	}, nil
}

// hfConvertFileToImagePart mirrors the 'file' case of TS
// convertToHuggingFaceResponsesMessages's user-content switch exactly,
// including its error ordering: reference/text data kinds always throw
// first, regardless of media type; only url/data kinds are then checked
// against the top-level media type.
func hfConvertFileToImagePart(file types.FileContent) (hfContentPart, error) {
	fd := hfNormalizeFileData(file)
	// mediaType mirrors TS's part.mediaType: the part's own top-level media
	// type field is authoritative (FileData.MediaType is a secondary,
	// provider-facing field used only when the outer field is unset).
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = file.MimeType
	}
	if mediaType == "" {
		mediaType = fd.MediaType
	}

	switch fd.Type {
	case types.FileDataTypeReference:
		return hfContentPart{}, &providererrors.UnsupportedFunctionalityError{Functionality: "file parts with provider references"}
	case types.FileDataTypeText:
		return hfContentPart{}, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
	case types.FileDataTypeURL:
		if hfTopLevelMediaType(mediaType) != "image" {
			return hfContentPart{}, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("file part media type %s", mediaType)}
		}
		return hfContentPart{Type: "input_image", ImageURL: fd.URL}, nil
	default: // types.FileDataTypeData or unset -- inline bytes
		if hfTopLevelMediaType(mediaType) != "image" {
			return hfContentPart{}, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("file part media type %s", mediaType)}
		}
		full, err := hfResolveFullMediaType(mediaType, fd.Data)
		if err != nil {
			return hfContentPart{}, err
		}
		return hfContentPart{
			Type:     "input_image",
			ImageURL: fmt.Sprintf("data:%s;base64,%s", full, base64.StdEncoding.EncodeToString(fd.Data)),
		}, nil
	}
}

// hfNormalizeFileData resolves a FileContent's tagged FileData shape,
// falling back to the legacy Data/URL/Reference/Text fields when FileData is
// unset (mirrors openresponses' normalizeFileContentData pattern).
func hfNormalizeFileData(file types.FileContent) types.FileData {
	if !file.FileData.IsZero() {
		return file.FileData
	}
	switch {
	case file.Reference != "":
		return types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"huggingface": file.Reference}, MediaType: file.MediaType}
	case file.Text != "":
		return types.FileData{Type: types.FileDataTypeText, Text: file.Text, MediaType: file.MediaType}
	case file.URL != "":
		return types.FileData{Type: types.FileDataTypeURL, URL: file.URL, MediaType: file.MediaType}
	default:
		return types.FileData{Type: types.FileDataTypeData, Data: file.Data, MediaType: file.MediaType}
	}
}

// hfIsFullMediaType mirrors TS isFullMediaType: true when mediaType has a
// non-empty, non-wildcard subtype (e.g. "image/png", but not "image" or
// "image/*").
func hfIsFullMediaType(mediaType string) bool {
	idx := strings.Index(mediaType, "/")
	if idx == -1 {
		return false
	}
	subtype := mediaType[idx+1:]
	return subtype != "" && subtype != "*"
}

// hfTopLevelMediaType mirrors TS getTopLevelMediaType: the portion of
// mediaType before "/", or the whole string when there is no "/".
func hfTopLevelMediaType(mediaType string) string {
	idx := strings.Index(mediaType, "/")
	if idx == -1 {
		return mediaType
	}
	return mediaType[:idx]
}

// hfResolveFullMediaType mirrors TS resolveFullMediaType: a full media type
// (e.g. "image/png") is returned as-is; otherwise (e.g. "image" or
// "image/*") the subtype is sniffed from the inline bytes.
func hfResolveFullMediaType(mediaType string, data []byte) (string, error) {
	if hfIsFullMediaType(mediaType) {
		return mediaType, nil
	}
	detected := fileutil.DetectMediaType(data)
	if detected.Category == "image" && detected.MimeType != "" {
		return detected.MimeType, nil
	}
	return "", &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", mediaType),
	}
}
