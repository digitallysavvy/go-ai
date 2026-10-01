package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MessageConversionError is returned when a UI message cannot be converted to
// a model message. Mirrors TS MessageConversionError.
type MessageConversionError struct {
	OriginalMessage UIMessage
	Message         string
	Cause           error
}

func (e *MessageConversionError) Error() string { return e.Message }
func (e *MessageConversionError) Unwrap() error { return e.Cause }

// IsMessageConversionError reports whether err is a MessageConversionError.
func IsMessageConversionError(err error) bool {
	var target *MessageConversionError
	return errors.As(err, &target)
}

// ConvertToModelMessagesOptions configures ConvertToModelMessages.
type ConvertToModelMessagesOptions struct {
	// Tools are used to convert tool outputs with Tool.ToModelOutput.
	Tools []types.Tool
	// IgnoreIncompleteToolCalls drops tool parts that are not in a completed
	// state (approval-responded, final output-available, output-error,
	// output-denied).
	IgnoreIncompleteToolCalls bool
	// ConvertDataPart converts data parts to text or file model message
	// parts (types.TextContent / types.FileContent). Returning nil skips
	// the part. Without it, data parts are ignored.
	ConvertDataPart func(part DataUIPart) types.ContentPart
}

// ConvertToModelMessages converts UI messages (for example from a chat
// frontend) into model messages that can be passed to GenerateText /
// StreamText. Mirrors TS convertToModelMessages.
//
// Assistant tool calls are emitted as types.ToolCallContent parts; calls that
// are not provider-executed are also mirrored to Message.ToolCalls for
// providers that read tool calls from that field.
func ConvertToModelMessages(ctx context.Context, messages []UIMessage, opts ...ConvertToModelMessagesOptions) ([]types.Message, error) {
	var options ConvertToModelMessagesOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	if ctx == nil {
		ctx = context.Background()
	}

	warnIfUIMessagesHaveDeprecatedRawInput(messages)

	if options.IgnoreIncompleteToolCalls {
		filtered := make([]UIMessage, len(messages))
		for i, message := range messages {
			message.Parts = filterCompleteToolParts(message.Parts)
			filtered[i] = message
		}
		messages = filtered
	}

	modelMessages := make([]types.Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case UIMessageRoleSystem:
			var text strings.Builder
			providerMetadata := map[string]interface{}{}
			for _, part := range message.Parts {
				textPart, ok := asTextUIPart(part)
				if !ok {
					continue
				}
				text.WriteString(textPart.Text)
				for key, value := range textPart.ProviderMetadata {
					providerMetadata[key] = value
				}
			}
			msg := types.Message{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: text.String()}},
			}
			if len(providerMetadata) > 0 {
				msg.ProviderOptions = providerMetadata
			}
			modelMessages = append(modelMessages, msg)

		case UIMessageRoleUser:
			content := make([]types.ContentPart, 0, len(message.Parts))
			for _, part := range message.Parts {
				if textPart, ok := asTextUIPart(part); ok {
					content = append(content, types.TextContent{Text: textPart.Text, ProviderOptions: textPart.ProviderMetadata})
					continue
				}
				if filePart, ok := asFileUIPart(part); ok {
					fileContent, err := fileUIPartToModel(filePart)
					if err != nil {
						return nil, &MessageConversionError{OriginalMessage: message, Message: err.Error(), Cause: err}
					}
					content = append(content, fileContent)
					continue
				}
				if dataPart, ok := asDataUIPart(part); ok {
					if options.ConvertDataPart != nil {
						if converted := options.ConvertDataPart(*dataPart); converted != nil {
							content = append(content, converted)
						}
					}
				}
			}
			modelMessages = append(modelMessages, types.Message{Role: types.RoleUser, Content: content})

		case UIMessageRoleAssistant:
			if message.Parts == nil {
				break
			}
			var block []UIMessagePart
			processBlock := func() error {
				if len(block) == 0 {
					return nil
				}
				assistant, err := convertUIAssistantBlock(ctx, message, block, options)
				if err != nil {
					return err
				}
				if len(assistant.Content) > 0 {
					modelMessages = append(modelMessages, assistant)
				}
				toolMessage, err := convertUIToolBlock(ctx, block, options)
				if err != nil {
					return err
				}
				if len(toolMessage.Content) > 0 {
					modelMessages = append(modelMessages, toolMessage)
				}
				block = nil
				return nil
			}
			for _, part := range message.Parts {
				switch {
				case IsCustomContentUIPart(part), IsTextUIPart(part), IsReasoningUIPart(part),
					IsReasoningFileUIPart(part), IsFileUIPart(part), IsToolUIPart(part), IsDataUIPart(part):
					block = append(block, part)
				case partType(part) == "step-start":
					if err := processBlock(); err != nil {
						return nil, err
					}
				}
			}
			if err := processBlock(); err != nil {
				return nil, err
			}

		default:
			return nil, &MessageConversionError{
				OriginalMessage: message,
				Message:         fmt.Sprintf("Unsupported role: %s", message.Role),
			}
		}
	}
	return modelMessages, nil
}

// filterCompleteToolParts keeps non-tool parts and tool parts in a completed
// state. Preliminary outputs are treated as incomplete.
func filterCompleteToolParts(parts []UIMessagePart) []UIMessagePart {
	out := make([]UIMessagePart, 0, len(parts))
	for _, part := range parts {
		tool, ok := AsToolUIPart(part)
		if !ok {
			out = append(out, part)
			continue
		}
		switch tool.State {
		case ToolStateApprovalResponded, ToolStateOutputError, ToolStateOutputDenied:
			out = append(out, part)
		case ToolStateOutputAvailable:
			if tool.Preliminary == nil || !*tool.Preliminary {
				out = append(out, part)
			}
		}
	}
	return out
}

func convertUIAssistantBlock(ctx context.Context, message UIMessage, block []UIMessagePart, options ConvertToModelMessagesOptions) (types.Message, error) {
	msg := types.Message{Role: types.RoleAssistant}
	for _, part := range block {
		switch {
		case IsTextUIPart(part):
			p, _ := asTextUIPart(part)
			msg.Content = append(msg.Content, types.TextContent{Text: p.Text, ProviderOptions: p.ProviderMetadata})
		case IsCustomContentUIPart(part):
			p, _ := asCustomUIPart(part)
			msg.Content = append(msg.Content, types.CustomContent{Kind: p.Kind, ProviderOptions: p.ProviderMetadata})
		case IsFileUIPart(part):
			p, _ := asFileUIPart(part)
			fileContent, err := fileUIPartToModel(p)
			if err != nil {
				return msg, &MessageConversionError{OriginalMessage: message, Message: err.Error(), Cause: err}
			}
			msg.Content = append(msg.Content, fileContent)
		case IsReasoningFileUIPart(part):
			p, _ := asReasoningFileUIPart(part)
			if err := validateUIPartURL(p.URL); err != nil {
				return msg, &MessageConversionError{OriginalMessage: message, Message: err.Error(), Cause: err}
			}
			msg.Content = append(msg.Content, types.ReasoningFileContent{
				FileData:        types.FileData{Type: types.FileDataTypeURL, URL: p.URL, MediaType: p.MediaType},
				MediaType:       p.MediaType,
				ProviderOptions: p.ProviderMetadata,
			})
		case IsReasoningUIPart(part):
			p, _ := asReasoningUIPart(part)
			msg.Content = append(msg.Content, types.ReasoningContent{Text: p.Text, ProviderOptions: p.ProviderMetadata})
		case IsToolUIPart(part):
			p, _ := AsToolUIPart(part)
			if p.State == ToolStateInputStreaming {
				continue
			}
			toolName := GetToolName(p)
			callProviderMetadata := p.CallProviderMetadata
			if callProviderMetadata == nil && p.State == ToolStateOutputError {
				callProviderMetadata = p.ResultProviderMetadata
			}
			input := p.Input
			if p.State == ToolStateOutputError && input == nil {
				input = p.RawInput
			}
			call := uiToolCallContent(p.ToolCallID, toolName, input, isTrue(p.ProviderExecuted), callProviderMetadata)
			msg.Content = append(msg.Content, call)
			if !isTrue(p.ProviderExecuted) {
				msg.ToolCalls = append(msg.ToolCalls, toolCallFromContentPart(call))
			}

			if p.Approval != nil {
				msg.Content = append(msg.Content, types.ToolApprovalRequestContent{
					ApprovalID:       p.Approval.ID,
					ToolCallID:       p.ToolCallID,
					IsAutomatic:      isTrue(p.Approval.IsAutomatic),
					Reason:           p.Approval.RequestReason,
					InputSchemaInput: p.Approval.InputSchemaInput,
					Signature:        p.Approval.Signature,
				})
			}

			if isTrue(p.ProviderExecuted) &&
				(p.State == ToolStateOutputAvailable || p.State == ToolStateOutputError) {
				resultProviderMetadata := p.ResultProviderMetadata
				if resultProviderMetadata == nil {
					resultProviderMetadata = p.CallProviderMetadata
				}
				var output interface{} = p.Output
				errorMode := "none"
				if p.State == ToolStateOutputError {
					output = p.ErrorText
					errorMode = "json"
				}
				modelOutput, err := createUIToolModelOutput(ctx, p.ToolCallID, p.Input, output, findUITool(options.Tools, toolName), errorMode)
				if err != nil {
					return msg, err
				}
				msg.Content = append(msg.Content, types.ToolResultContent{
					ToolCallID:       p.ToolCallID,
					ToolName:         toolName,
					Output:           modelOutput,
					ProviderExecuted: true,
					ProviderOptions:  resultProviderMetadata,
				})
			}
		case IsDataUIPart(part):
			p, _ := asDataUIPart(part)
			if options.ConvertDataPart != nil {
				if converted := options.ConvertDataPart(*p); converted != nil {
					msg.Content = append(msg.Content, converted)
				}
			}
		}
	}
	return msg, nil
}

func convertUIToolBlock(ctx context.Context, block []UIMessagePart, options ConvertToModelMessagesOptions) (types.Message, error) {
	msg := types.Message{Role: types.RoleTool}
	for _, part := range block {
		p, ok := AsToolUIPart(part)
		if !ok {
			continue
		}
		hasApprovalResponse := p.Approval != nil && p.Approval.Approved != nil
		if isTrue(p.ProviderExecuted) && !hasApprovalResponse {
			continue
		}
		toolName := GetToolName(p)

		if hasApprovalResponse {
			msg.Content = append(msg.Content, types.ToolApprovalResponseContent{
				ApprovalID:       p.Approval.ID,
				Approved:         *p.Approval.Approved,
				Reason:           p.Approval.Reason,
				ProviderExecuted: isTrue(p.ProviderExecuted),
			})
		}

		// Synthetic execution-denied result for denied tool approvals.
		if p.State == ToolStateApprovalResponded && hasApprovalResponse && !*p.Approval.Approved {
			msg.Content = append(msg.Content, types.ToolResultContent{
				ToolCallID: p.ToolCallID,
				ToolName:   toolName,
				Output: &types.ToolResultOutput{
					Type:   types.ToolResultOutputExecutionDenied,
					Reason: p.Approval.Reason,
				},
				ProviderOptions: p.CallProviderMetadata,
			})
		}

		// Provider-executed results are already in the assistant content.
		if isTrue(p.ProviderExecuted) {
			continue
		}

		switch p.State {
		case ToolStateOutputDenied:
			reason := "Tool call execution denied."
			if p.Approval != nil && p.Approval.Reason != "" {
				reason = p.Approval.Reason
			}
			msg.Content = append(msg.Content, types.ToolResultContent{
				ToolCallID:      p.ToolCallID,
				ToolName:        toolName,
				Output:          &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: reason},
				ProviderOptions: p.CallProviderMetadata,
			})
		case ToolStateOutputError, ToolStateOutputAvailable:
			var output interface{} = p.Output
			errorMode := "none"
			if p.State == ToolStateOutputError {
				output = p.ErrorText
				errorMode = "text"
			}
			modelOutput, err := createUIToolModelOutput(ctx, p.ToolCallID, p.Input, output, findUITool(options.Tools, toolName), errorMode)
			if err != nil {
				return msg, err
			}
			msg.Content = append(msg.Content, types.ToolResultContent{
				ToolCallID:      p.ToolCallID,
				ToolName:        toolName,
				Output:          modelOutput,
				ProviderOptions: p.CallProviderMetadata,
			})
		}
	}
	return msg, nil
}

func uiToolCallContent(toolCallID, toolName string, input interface{}, providerExecuted bool, providerOptions map[string]interface{}) types.ToolCallContent {
	call := types.ToolCallContent{
		ToolCallID:       toolCallID,
		ToolName:         toolName,
		ProviderExecuted: providerExecuted,
		ProviderOptions:  providerOptions,
	}
	switch v := input.(type) {
	case nil:
	case string:
		call.Input = v
	case map[string]interface{}:
		call.Arguments = v
		if data, err := json.Marshal(v); err == nil {
			call.Input = string(data)
		}
	default:
		if data, err := json.Marshal(v); err == nil {
			call.Input = string(data)
		}
	}
	return call
}

// createUIToolModelOutput mirrors TS createToolModelOutput.
func createUIToolModelOutput(ctx context.Context, toolCallID string, input, output interface{}, tool *types.Tool, errorMode string) (*types.ToolResultOutput, error) {
	switch errorMode {
	case "text":
		return &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: uiErrorMessage(output)}, nil
	case "json":
		return &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: toJSONValue(output)}, nil
	}
	if tool != nil && tool.ToModelOutput != nil {
		args, _ := input.(map[string]interface{})
		if args == nil && input != nil {
			args, _ = toArgumentsMap(input)
		}
		return tool.ToModelOutput(ctx, types.ToModelOutputOptions{
			ToolCallID: toolCallID,
			Input:      args,
			Output:     output,
			// Result is a deprecated alias of Output, kept for
			// ToModelOutput implementations still reading it.
			Result: output, //nolint:staticcheck
		})
	}
	if s, ok := output.(string); ok {
		return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: s}, nil
	}
	return &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: toJSONValue(output)}, nil
}

// uiErrorMessage mirrors TS getErrorMessage.
func uiErrorMessage(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "unknown error"
	case string:
		return v
	case error:
		return v.Error()
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// toJSONValue normalizes a value to plain JSON data by round-tripping it.
func toJSONValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func findUITool(tools []types.Tool, name string) *types.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func fileUIPartToModel(p *FileUIPart) (types.FileContent, error) {
	content := types.FileContent{
		MediaType:       p.MediaType,
		Filename:        p.Filename,
		ProviderOptions: p.ProviderMetadata,
	}
	if p.ProviderReference != nil {
		content.FileData = types.FileData{
			Type:      types.FileDataTypeReference,
			Reference: types.ProviderReference(p.ProviderReference),
			MediaType: p.MediaType,
		}
		return content, nil
	}
	if err := validateUIPartURL(p.URL); err != nil {
		return content, err
	}
	content.FileData = types.FileData{Type: types.FileDataTypeURL, URL: p.URL, MediaType: p.MediaType}
	return content, nil
}

// validateUIPartURL mirrors `new URL(part.url)`, which rejects relative or
// malformed URLs.
func validateUIPartURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("Invalid URL: %s", raw) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

func isTrue(b *bool) bool { return b != nil && *b }

func asTextUIPart(part UIMessagePart) (*TextUIPart, bool) {
	switch p := part.(type) {
	case *TextUIPart:
		return p, p != nil
	case TextUIPart:
		return &p, true
	}
	return nil, false
}

func asCustomUIPart(part UIMessagePart) (*CustomContentUIPart, bool) {
	switch p := part.(type) {
	case *CustomContentUIPart:
		return p, p != nil
	case CustomContentUIPart:
		return &p, true
	}
	return nil, false
}

func asReasoningUIPart(part UIMessagePart) (*ReasoningUIPart, bool) {
	switch p := part.(type) {
	case *ReasoningUIPart:
		return p, p != nil
	case ReasoningUIPart:
		return &p, true
	}
	return nil, false
}

func asFileUIPart(part UIMessagePart) (*FileUIPart, bool) {
	switch p := part.(type) {
	case *FileUIPart:
		return p, p != nil
	case FileUIPart:
		return &p, true
	}
	return nil, false
}

func asReasoningFileUIPart(part UIMessagePart) (*ReasoningFileUIPart, bool) {
	switch p := part.(type) {
	case *ReasoningFileUIPart:
		return p, p != nil
	case ReasoningFileUIPart:
		return &p, true
	}
	return nil, false
}

func asDataUIPart(part UIMessagePart) (*DataUIPart, bool) {
	switch p := part.(type) {
	case *DataUIPart:
		return p, p != nil && IsDataUIPart(p)
	case DataUIPart:
		return &p, IsDataUIPart(p)
	}
	return nil, false
}
