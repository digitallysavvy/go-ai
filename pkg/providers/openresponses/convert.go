package openresponses

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ConvertToOpenResponsesInput converts AI SDK messages to Open Responses
// format using the OpenAI provider-reference key. Provider request paths should
// call ConvertToOpenResponsesInputForProvider so missing provider references
// can be returned as errors instead of ignored for compatibility.
func ConvertToOpenResponsesInput(messages []types.Message, system string) (interface{}, string, []types.Warning) {
	input, instructions, warnings, _ := ConvertToOpenResponsesInputForProvider(messages, system, "openai")
	return input, instructions, warnings
}

// ConvertToOpenResponsesInputForProvider converts messages and resolves
// provider references using the active provider key. It mirrors the TypeScript
// SDK provider adapters, which throw NoSuchProviderReferenceError when a file
// reference does not contain an ID for the current provider.
func ConvertToOpenResponsesInputForProvider(messages []types.Message, system string, providerName string) (interface{}, string, []types.Warning, error) {
	var input []interface{}
	var warnings []types.Warning
	var systemMessages []string
	if providerName == "" {
		providerName = "openai"
	}

	// Collect system messages
	if system != "" {
		systemMessages = append(systemMessages, system)
	}

	// Convert each message
	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			// System messages become instructions - extract text from content parts
			for _, part := range msg.Content {
				if textContent, ok := part.(types.TextContent); ok {
					systemMessages = append(systemMessages, textContent.Text)
				}
			}

		case types.RoleUser:
			userContent, err := convertUserContent(msg.Content, &warnings, providerName)
			if err != nil {
				return nil, "", warnings, err
			}
			input = append(input, MessageItem{
				Type:    "message",
				Role:    "user",
				Content: userContent,
			})

		case types.RoleAssistant:
			assistantContent, toolCalls := convertAssistantContent(msg.Content)

			// Add assistant message if it has text content
			if len(assistantContent) > 0 {
				input = append(input, MessageItem{
					Type:    "message",
					Role:    "assistant",
					Content: assistantContent,
				})
			}

			// Add tool calls as separate items
			input = append(input, toolCalls...)

		case types.RoleTool:
			// Convert tool results
			toolResults, err := convertToolResults(msg.Content, &warnings, providerName)
			if err != nil {
				return nil, "", warnings, err
			}
			input = append(input, toolResults...)
		}
	}

	// Combine system messages into instructions
	var instructions string
	if len(systemMessages) > 0 {
		instructions = strings.Join(systemMessages, "\n")
	}

	return input, instructions, warnings, nil
}

// convertUserContent converts user message content to Open Responses format
func convertUserContent(content []types.ContentPart, warnings *[]types.Warning, providerName string) ([]interface{}, error) {
	var result []interface{}

	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			result = append(result, InputTextContent{
				Type: "input_text",
				Text: p.Text,
			})

		case types.ImageContent:
			imageURL := convertImageContentToURL(p, warnings)
			if imageURL != "" {
				result = append(result, InputImageContent{
					Type:     "input_image",
					ImageURL: imageURL,
				})
			}

		case types.FileContent:
			file, err := normalizeFileContentData(p, providerName)
			if err != nil {
				return nil, err
			}
			mediaType := file.MediaType
			if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
				imageURL := convertFileToImageURL(file, warnings)
				if imageURL != "" {
					result = append(result, InputImageContent{Type: "input_image", ImageURL: imageURL})
				}
				continue
			}
			switch {
			case file.URL != "":
				result = append(result, InputFileContent{Type: "input_file", FileURL: file.URL})
			case file.Reference != "":
				result = append(result, InputFileContent{Type: "input_file", FileID: file.Reference})
			case len(file.Data) > 0:
				result = append(result, InputFileContent{
					Type:     "input_file",
					FileData: fmt.Sprintf("data:%s;base64,%s", mediaTypeOrDefault(file.MediaType), base64.StdEncoding.EncodeToString(file.Data)),
					Filename: file.Filename,
				})
			case file.Text != "":
				result = append(result, InputTextContent{Type: "input_text", Text: file.Text})
			}
		}
	}

	return result, nil
}

// convertImageContentToURL converts ImageContent to a data URL or returns the URL
func convertImageContentToURL(img types.ImageContent, warnings *[]types.Warning) string {
	// Check for URL first
	if img.URL != "" {
		return img.URL
	}

	// Convert data to base64 data URL
	if len(img.Image) > 0 {
		mediaType := img.MimeType
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		dataStr := base64.StdEncoding.EncodeToString(img.Image)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

// convertFileToImageURL converts FileContent to an image data URL
func convertFileToImageURL(file types.FileContent, warnings *[]types.Warning) string {
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = file.MimeType
	}
	if mediaType == "" || mediaType == "image/*" {
		mediaType = "image/jpeg"
	}

	if file.URL != "" {
		return file.URL
	}

	if file.Reference != "" {
		return file.Reference
	}

	if file.Text != "" {
		return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString([]byte(file.Text)))
	}

	if len(file.Data) > 0 {
		dataStr := base64.StdEncoding.EncodeToString(file.Data)
		return fmt.Sprintf("data:%s;base64,%s", mediaType, dataStr)
	}

	return ""
}

func normalizeFileContentData(file types.FileContent, providerName string) (types.FileContent, error) {
	if file.FileData.IsZero() {
		return file, nil
	}
	switch file.FileData.Type {
	case types.FileDataTypeData:
		file.Data = file.FileData.Data
	case types.FileDataTypeURL:
		file.URL = file.FileData.URL
	case types.FileDataTypeReference:
		ref, err := providerutils.ResolveProviderReference(file.FileData.Reference, providerName)
		if err != nil {
			return types.FileContent{}, err
		}
		file.Reference = ref
	case types.FileDataTypeText:
		file.Text = file.FileData.Text
	}
	if file.FileData.MediaType != "" && file.MediaType == "" {
		file.MediaType = file.FileData.MediaType
	}
	return file, nil
}

func mediaTypeOrDefault(mediaType string) string {
	if mediaType == "" {
		return "application/octet-stream"
	}
	return mediaType
}

// convertAssistantContent converts assistant message content
func convertAssistantContent(content []types.ContentPart) ([]interface{}, []interface{}) {
	var textContent []interface{}
	var toolCalls []interface{}

	for _, part := range content {
		contentType := part.ContentType()

		switch contentType {
		case "text":
			if textPart, ok := part.(types.TextContent); ok {
				textContent = append(textContent, OutputTextContent{
					Type: "output_text",
					Text: textPart.Text,
				})
			}

		case "reasoning":
			// Emit reasoning as a top-level reasoning input item, not as output_text.
			// Only forward when EncryptedContent is present; without it the API cannot
			// reconstruct the reasoning context for multi-turn conversations (#12869).
			if reasoningPart, ok := part.(types.ReasoningContent); ok {
				if reasoningPart.EncryptedContent != "" {
					item := ReasoningInputItem{
						Type:             "reasoning",
						EncryptedContent: reasoningPart.EncryptedContent,
					}
					if reasoningPart.Text != "" {
						item.Summary = []SummaryPart{{Type: "summary_text", Text: reasoningPart.Text}}
					}
					toolCalls = append(toolCalls, item)
				}
				// No EncryptedContent: skip (e.g. Anthropic reasoning blocks, or
				// reasoning from a provider that doesn't use this field).
			}
		}
	}

	return textContent, toolCalls
}

// convertToolResults converts tool results to Open Responses format
func convertToolResults(content []types.ContentPart, warnings *[]types.Warning, providerName string) ([]interface{}, error) {
	var results []interface{}

	for _, part := range content {
		if part.ContentType() == "tool-result" {
			if toolResult, ok := part.(types.ToolResultContent); ok {
				output, err := convertToolResultOutput(toolResult, warnings, providerName)
				if err != nil {
					return nil, err
				}

				results = append(results, FunctionCallOutputItem{
					Type:   "function_call_output",
					CallID: toolResult.ToolCallID,
					Output: output,
				})
			}
		}
	}

	return results, nil
}

// convertToolResultOutput converts tool result output to appropriate format
func convertToolResultOutput(toolResult types.ToolResultContent, warnings *[]types.Warning, providerName string) (interface{}, error) {
	if toolResult.Output != nil {
		return convertStructuredToolResultOutput(*toolResult.Output, warnings, providerName)
	}

	// If there's an error, return the error message
	if toolResult.Error != "" {
		return toolResult.Error, nil
	}

	// If result is nil, return empty string
	if toolResult.Result == nil {
		return "", nil
	}

	// Try to convert result to string if it's a simple type
	switch v := toolResult.Result.(type) {
	case string:
		return v, nil
	case int, int32, int64, float32, float64, bool:
		return fmt.Sprintf("%v", v), nil
	default:
		// For complex types, JSON encode
		jsonBytes, _ := json.Marshal(v)
		return string(jsonBytes), nil
	}
}

func convertStructuredToolResultOutput(output types.ToolResultOutput, warnings *[]types.Warning, providerName string) (interface{}, error) {
	switch output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputError, types.ToolResultOutputErrorText, types.ToolResultOutputErrorJSON:
		if output.Value == nil {
			return "", nil
		}
		if value, ok := output.Value.(string); ok {
			return value, nil
		}
		return fmt.Sprintf("%v", output.Value), nil
	case types.ToolResultOutputExecutionDenied:
		if output.Reason != "" {
			return output.Reason, nil
		}
		return "Tool call execution denied.", nil
	case types.ToolResultOutputJSON:
		jsonBytes, _ := json.Marshal(output.Value)
		return string(jsonBytes), nil
	case types.ToolResultOutputContent:
		parts := make([]interface{}, 0, len(output.Content))
		for _, block := range output.Content {
			switch item := block.(type) {
			case types.TextContentBlock:
				parts = append(parts, InputTextContent{Type: "input_text", Text: item.Text})
			case types.ImageContentBlock:
				mediaType := item.MediaType
				if mediaType == "" {
					mediaType = "image/jpeg"
				}
				parts = append(parts, InputImageContent{
					Type:     "input_image",
					ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(item.Data)),
				})
			case types.FileContentBlock:
				converted, ok, err := convertToolFileContentBlock(item, warnings, providerName)
				if err != nil {
					return nil, err
				}
				if ok {
					parts = append(parts, converted)
				}
			default:
				*warnings = append(*warnings, types.Warning{
					Type:    "other",
					Message: fmt.Sprintf("unsupported tool content part type: %s", block.ToolResultContentType()),
				})
			}
		}
		return parts, nil
	default:
		if output.Value == nil {
			return "", nil
		}
		jsonBytes, _ := json.Marshal(output.Value)
		return string(jsonBytes), nil
	}
}

func convertToolFileContentBlock(block types.FileContentBlock, warnings *[]types.Warning, providerName string) (interface{}, bool, error) {
	file, err := normalizeFileContentBlockData(block, providerName)
	if err != nil {
		return nil, false, err
	}
	mediaType := mediaTypeOrDefault(file.MediaType)
	if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
		switch {
		case file.URL != "":
			return InputImageContent{Type: "input_image", ImageURL: file.URL}, true, nil
		case file.Reference != "":
			return InputImageContent{Type: "input_image", ImageURL: file.Reference}, true, nil
		case len(file.Data) > 0:
			return InputImageContent{
				Type:     "input_image",
				ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data)),
			}, true, nil
		case file.Text != "":
			return InputImageContent{
				Type:     "input_image",
				ImageURL: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString([]byte(file.Text))),
			}, true, nil
		default:
			*warnings = append(*warnings, types.Warning{Type: "other", Message: "unsupported tool content part type: file"})
			return nil, false, nil
		}
	}

	switch {
	case file.URL != "":
		return InputFileContent{Type: "input_file", FileURL: file.URL}, true, nil
	case file.Reference != "":
		return InputFileContent{Type: "input_file", FileID: file.Reference}, true, nil
	case len(file.Data) > 0:
		filename := file.Filename
		if filename == "" {
			filename = "data"
		}
		return InputFileContent{
			Type:     "input_file",
			Filename: filename,
			FileData: fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data)),
		}, true, nil
	case file.Text != "":
		return InputTextContent{Type: "input_text", Text: file.Text}, true, nil
	default:
		*warnings = append(*warnings, types.Warning{Type: "other", Message: "unsupported tool content part type: file"})
		return nil, false, nil
	}
}

func normalizeFileContentBlockData(block types.FileContentBlock, providerName string) (types.FileContentBlock, error) {
	if block.FileData.IsZero() {
		return block, nil
	}
	switch block.FileData.Type {
	case types.FileDataTypeData:
		block.Data = block.FileData.Data
	case types.FileDataTypeURL:
		block.URL = block.FileData.URL
	case types.FileDataTypeReference:
		ref, err := providerutils.ResolveProviderReference(block.FileData.Reference, providerName)
		if err != nil {
			return types.FileContentBlock{}, err
		}
		block.Reference = ref
	case types.FileDataTypeText:
		block.Text = block.FileData.Text
	}
	if block.FileData.MediaType != "" && block.MediaType == "" {
		block.MediaType = block.FileData.MediaType
	}
	return block, nil
}
