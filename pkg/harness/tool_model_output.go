package harness

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolModelOutputContentType discriminates ToolModelOutputContent.
type ToolModelOutputContentType string

const (
	ToolModelOutputContentText  ToolModelOutputContentType = "text"
	ToolModelOutputContentImage ToolModelOutputContentType = "image"
)

// ToolModelOutputContent is one text or inline-image block of a tool's
// model-facing output, in the shape adapters hand to their runtime. Mirrors
// TS `HarnessToolModelOutputContent`.
type ToolModelOutputContent struct {
	Type ToolModelOutputContentType `json:"type"`
	// Text is set for Type "text".
	Text string `json:"text,omitempty"`
	// Data (base64) and MediaType are set for Type "image".
	Data      string `json:"data,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

// ModelToolResult is the `toolResult` payload carried on a
// ToolResultSubmission when a tool defines ToModelOutput: the model-facing
// representation of the raw output. Mirrors TS `submitToolResult`'s
// `toolResult` (`{type:'tool-result', toolCallId, toolName, output}`).
type ModelToolResult struct {
	Type       string                 `json:"type"`
	ToolCallID string                 `json:"toolCallId"`
	ToolName   string                 `json:"toolName"`
	Output     types.ToolResultOutput `json:"output"`
}

func newModelToolResult(toolCallID, toolName string, output types.ToolResultOutput) *ModelToolResult {
	return &ModelToolResult{Type: "tool-result", ToolCallID: toolCallID, ToolName: toolName, Output: output}
}

func unsupportedModelOutput(detail string) error {
	return NewCapabilityUnsupportedError(
		"Harnesses support only text and inline images in tool model output. "+detail, "", nil)
}

// NormalizeToolModelOutput validates a tool's model output for harness
// delivery and canonicalizes inline images (full media type, inline bytes).
// Only text and inline images are supported in "content" outputs; anything
// else returns a *CapabilityUnsupportedError. Non-content outputs pass
// through unchanged. Mirrors TS `normalizeHarnessToolModelOutput`.
func NormalizeToolModelOutput(output types.ToolResultOutput) (types.ToolResultOutput, error) {
	if output.Type != types.ToolResultOutputContent {
		return output, nil
	}
	blocks := make([]types.ToolResultContentBlock, 0, len(output.Content))
	for _, block := range output.Content {
		switch b := block.(type) {
		case types.TextContentBlock:
			blocks = append(blocks, b)
		case types.ImageContentBlock:
			img, err := normalizeInlineImage(b.Data, b.MediaType, b.ProviderOptions, "image-data")
			if err != nil {
				return types.ToolResultOutput{}, err
			}
			blocks = append(blocks, img)
		case types.FileContentBlock:
			// TS: a file whose data is not inline fails with its data type,
			// "Unsupported content 'file' (<mediaType>, <data.type>).".
			if dataType := fileBlockDataType(b); dataType != types.FileDataTypeData {
				return types.ToolResultOutput{}, unsupportedModelOutput(fmt.Sprintf("Unsupported content 'file' (%s, %s).", b.MediaType, dataType))
			}
			data, mediaType := fileBlockInline(b)
			img, err := normalizeInlineImage(data, mediaType, b.ProviderOptions, "file")
			if err != nil {
				return types.ToolResultOutput{}, err
			}
			blocks = append(blocks, img)
		default:
			return types.ToolResultOutput{}, unsupportedModelOutput(fmt.Sprintf("Unsupported content '%s'.", block.ToolResultContentType()))
		}
	}
	out := output
	out.Content = blocks
	return out, nil
}

func topLevelMediaType(mediaType string) string {
	top, _, _ := strings.Cut(mediaType, "/")
	return top
}

// fileBlockDataType is the TS `file.data.type` of a file block.
func fileBlockDataType(b types.FileContentBlock) types.FileDataType {
	switch {
	case !b.FileData.IsZero():
		return b.FileData.Type
	case b.URL != "":
		return types.FileDataTypeURL
	case b.Reference != "":
		return types.FileDataTypeReference
	case b.Text != "":
		return types.FileDataTypeText
	}
	return types.FileDataTypeData
}

// fileBlockInline is the inline bytes and media type of a file block whose
// data is inline.
func fileBlockInline(b types.FileContentBlock) ([]byte, string) {
	data := b.Data
	if len(data) == 0 {
		data = b.FileData.Data
	}
	if len(data) == 0 && b.FileData.DataString != "" {
		if decoded, err := types.DecodeFileDataString(b.FileData.DataString); err == nil {
			data = decoded
		}
	}
	mediaType := b.MediaType
	if mediaType == "" {
		mediaType = b.FileData.MediaType
	}
	return data, mediaType
}

// isFullMediaType mirrors TS isFullMediaType: `type/subtype` with a
// non-empty subtype that is not "*".
func isFullMediaType(mediaType string) bool {
	_, subtype, ok := strings.Cut(mediaType, "/")
	return ok && subtype != "" && subtype != "*"
}

// normalizeInlineImage checks the top-level media type first (TS reports
// the part's type, partType, for a non-image), then detects the media type
// of an image whose media type is not a full one.
func normalizeInlineImage(data []byte, mediaType string, providerOptions map[string]interface{}, partType string) (types.ImageContentBlock, error) {
	// TS getTopLevelMediaType(mediaType) !== 'image': an empty media type
	// fails here too.
	if topLevelMediaType(mediaType) != "image" {
		return types.ImageContentBlock{}, unsupportedModelOutput(fmt.Sprintf("Unsupported content '%s' (%s, data).", partType, mediaType))
	}
	if !isFullMediaType(mediaType) {
		detected, ok := fileutil.DetectMediaTypeSignature(data, "image")
		if !ok {
			return types.ImageContentBlock{}, unsupportedModelOutput("Cannot detect the inline image media type.")
		}
		mediaType = detected
	}
	return types.ImageContentBlock{Data: data, MediaType: mediaType, ProviderOptions: providerOptions}, nil
}

// ConvertToolModelOutput converts a (normalized) tool model output into the
// text/inline-image blocks and error flag an adapter forwards to its runtime.
// Mirrors TS `convertHarnessToolModelOutput`. Adapters use this to build the
// runtime's tool-result from ToolResultSubmission.ToolResult.
func ConvertToolModelOutput(output types.ToolResultOutput) ([]ToolModelOutputContent, bool, error) {
	textOf := func(v interface{}) string {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	jsonOf := func(v interface{}) string {
		b, err := jsstr.Marshal(v)
		if err != nil {
			return "null"
		}
		return string(b)
	}
	switch output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText:
		return []ToolModelOutputContent{{Type: ToolModelOutputContentText, Text: textOf(output.Value)}},
			output.Type == types.ToolResultOutputErrorText, nil
	case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		return []ToolModelOutputContent{{Type: ToolModelOutputContentText, Text: jsonOf(output.Value)}},
			output.Type == types.ToolResultOutputErrorJSON, nil
	case types.ToolResultOutputError:
		return []ToolModelOutputContent{{Type: ToolModelOutputContentText, Text: textOf(output.Value)}}, true, nil
	case types.ToolResultOutputExecutionDenied:
		// TS output.reason ?? 'Tool execution denied.'
		reason := output.ReasonOr("Tool execution denied.")
		return []ToolModelOutputContent{{Type: ToolModelOutputContentText, Text: reason}}, true, nil
	case types.ToolResultOutputContent:
		out := make([]ToolModelOutputContent, 0, len(output.Content))
		for _, block := range output.Content {
			switch b := block.(type) {
			case types.TextContentBlock:
				out = append(out, ToolModelOutputContent{Type: ToolModelOutputContentText, Text: b.Text})
			case types.ImageContentBlock:
				// TS getNormalizedInlineImage: a full image/* media type only.
				if !isNormalizedImageMediaType(b.MediaType) {
					return nil, false, unsupportedConvertContent("image-data")
				}
				out = append(out, ToolModelOutputContent{Type: ToolModelOutputContentImage, Data: base64Std(b.Data), MediaType: b.MediaType})
			case types.FileContentBlock:
				// TS accepts a normalized `file` part: inline data with a full
				// image/* media type.
				data, mediaType := fileBlockInline(b)
				if fileBlockDataType(b) != types.FileDataTypeData || !isNormalizedImageMediaType(mediaType) {
					return nil, false, unsupportedConvertContent(block.ToolResultContentType())
				}
				out = append(out, ToolModelOutputContent{Type: ToolModelOutputContentImage, Data: base64Std(data), MediaType: mediaType})
			default:
				// TS throws HarnessBridgeCapabilityUnsupportedError with this exact
				// text (the host translates it to the capability error).
				return nil, false, unsupportedConvertContent(block.ToolResultContentType())
			}
		}
		return out, false, nil
	}
	return nil, false, unsupportedModelOutput(fmt.Sprintf("unsupported output type '%s'.", output.Type))
}

// isNormalizedImageMediaType is TS getNormalizedInlineImage's media type
// check: starts with "image/" and is not "image/*".
func isNormalizedImageMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") && mediaType != "image/*"
}

// unsupportedConvertContent is the TS HarnessBridgeCapabilityUnsupportedError
// text of convertHarnessToolModelOutput (the host translates it to the
// capability error).
func unsupportedConvertContent(partType string) error {
	return NewCapabilityUnsupportedError(fmt.Sprintf(
		"Harnesses support only text and inline images in tool model output; unsupported content '%s'.", partType), "", nil)
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
