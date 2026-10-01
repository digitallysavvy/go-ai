package perplexity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// This file mirrors TS convert-to-perplexity-input.ts: it converts a
// types.Message list into the Agent API's `input` array (Responses-API-style
// items), replacing the pre-migration convertToPerplexityMessages. The Agent
// API only accepts image file input -- PDFs and other file types that the
// pre-migration Sonar Chat Completions API supported are now rejected with
// UnsupportedFunctionalityError, matching TS.

// convertToPerplexityInput mirrors TS convertToPerplexityInput(prompt).
func convertToPerplexityInput(messages []types.Message) ([]map[string]interface{}, []types.Warning, error) {
	input := make([]map[string]interface{}, 0, len(messages))
	var warnings []types.Warning

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			input = append(input, map[string]interface{}{
				"type":    "message",
				"role":    "system",
				"content": perplexityTextContent(msg.Content),
			})

		case types.RoleUser:
			converted := make([]map[string]interface{}, 0, len(msg.Content))
			isTextOnly := true
			for _, part := range msg.Content {
				item, err := convertPerplexityUserPart(part)
				if err != nil {
					return nil, nil, err
				}
				if item == nil {
					continue
				}
				if item["type"] != "input_text" {
					isTextOnly = false
				}
				converted = append(converted, item)
			}
			var content interface{}
			if isTextOnly {
				var b strings.Builder
				for _, item := range converted {
					if text, ok := item["text"].(string); ok {
						b.WriteString(text)
					}
				}
				content = b.String()
			} else {
				content = converted
			}
			input = append(input, map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": content,
			})

		case types.RoleAssistant:
			var textBuilder strings.Builder
			for _, part := range msg.Content {
				if text, ok := part.(types.TextContent); ok {
					textBuilder.WriteString(text.Text)
				}
			}
			if textBuilder.Len() > 0 {
				input = append(input, map[string]interface{}{
					"type":    "message",
					"role":    "assistant",
					"content": textBuilder.String(),
				})
			}

			for _, part := range msg.Content {
				switch p := part.(type) {
				case types.TextContent:
					// already folded into the assistant message above.
				case types.ToolCallContent:
					item := map[string]interface{}{
						"type":      "function_call",
						"call_id":   p.ToolCallID,
						"name":      p.ToolName,
						"arguments": perplexityToolCallArguments(p),
					}
					if sig := perplexityThoughtSignature(p.ProviderOptions); sig != "" {
						item["thought_signature"] = sig
					}
					input = append(input, item)
				case types.ToolResultContent:
					out, err := serializePerplexityToolOutput(p)
					if err != nil {
						return nil, nil, err
					}
					item := map[string]interface{}{
						"type":    "function_call_output",
						"call_id": p.ToolCallID,
						"name":    p.ToolName,
						"output":  out,
					}
					if sig := perplexityThoughtSignature(p.ProviderOptions); sig != "" {
						item["thought_signature"] = sig
					}
					input = append(input, item)
				case types.ReasoningContent:
					warnings = append(warnings, types.Warning{
						Type:    "unsupported",
						Feature: "reasoning content in prompt",
					})
				case types.FileContent:
					return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "assistant file parts"}
				case types.ReasoningFileContent:
					return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "assistant reasoning-file parts"}
				case types.CustomContent:
					return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "assistant custom parts"}
				}
			}

		case types.RoleTool:
			for _, part := range msg.Content {
				if _, ok := part.(types.ToolApprovalResponseContent); ok {
					return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "tool approval responses"}
				}
				toolResult, ok := part.(types.ToolResultContent)
				if !ok {
					continue
				}
				out, err := serializePerplexityToolOutput(toolResult)
				if err != nil {
					return nil, nil, err
				}
				item := map[string]interface{}{
					"type":    "function_call_output",
					"call_id": toolResult.ToolCallID,
					"name":    toolResult.ToolName,
					"output":  out,
				}
				if sig := perplexityThoughtSignature(toolResult.ProviderOptions); sig != "" {
					item["thought_signature"] = sig
				}
				input = append(input, item)
			}

		default:
			return nil, nil, fmt.Errorf("perplexity: unsupported role %q", msg.Role)
		}
	}

	return input, warnings, nil
}

func perplexityTextContent(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if text, ok := part.(types.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// convertPerplexityUserPart converts one user-message content part into an
// Agent API input_text/input_image item. Only image file parts are
// supported; every other file media type is rejected, matching TS's
// getTopLevelMediaType(part.mediaType) !== 'image' check (the Agent API
// dropped PDF/video support present in the pre-migration Sonar Chat
// Completions API).
func convertPerplexityUserPart(part types.ContentPart) (map[string]interface{}, error) {
	switch p := part.(type) {
	case types.TextContent:
		return map[string]interface{}{"type": "input_text", "text": p.Text}, nil

	case types.FileContent:
		declaredMediaType := firstNonEmptyPerplexity(p.MediaType, p.MimeType, p.FileData.MediaType)
		if topLevelMediaType(declaredMediaType) != "image" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("file part media type %s", declaredMediaType),
			}
		}

		normalized, err := prompt.NormalizeFileContent(p)
		if err != nil {
			return nil, err
		}

		switch normalized.FileData.Type {
		case types.FileDataTypeURL:
			return map[string]interface{}{"type": "input_image", "image_url": normalized.FileData.URL}, nil
		case types.FileDataTypeData:
			fullMediaType, err := resolvePerplexityImageMediaType(declaredMediaType, normalized.Data)
			if err != nil {
				return nil, err
			}
			url := fmt.Sprintf("data:%s;base64,%s", fullMediaType, base64.StdEncoding.EncodeToString(normalized.Data))
			return map[string]interface{}{"type": "input_image", "image_url": url}, nil
		case types.FileDataTypeReference:
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "file parts with provider references"}
		case types.FileDataTypeText:
			return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "text file parts"}
		}
	}
	return nil, nil
}

// isFullMediaType reports whether mediaType is already a complete
// "type/subtype" media type (i.e. has a non-empty, non-wildcard subtype).
func isFullMediaType(mediaType string) bool {
	idx := strings.Index(mediaType, "/")
	if idx == -1 {
		return false
	}
	subtype := mediaType[idx+1:]
	return subtype != "" && subtype != "*"
}

// resolvePerplexityImageMediaType resolves a bare/wildcard image media type
// via signature detection from inline bytes, mirroring TS resolveFullMediaType
// for the image-only case the Agent API supports.
func resolvePerplexityImageMediaType(mediaType string, data []byte) (string, error) {
	if isFullMediaType(mediaType) {
		return mediaType, nil
	}
	if detected, ok := fileutil.DetectMediaTypeSignature(data, "image"); ok {
		return detected, nil
	}
	return "", &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", mediaType),
	}
}

func topLevelMediaType(mediaType string) string {
	if idx := strings.Index(mediaType, "/"); idx >= 0 {
		return mediaType[:idx]
	}
	return mediaType
}

func firstNonEmptyPerplexity(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// perplexityToolCallArguments mirrors TS `JSON.stringify(part.input ?? {})`
// for a re-sent assistant tool-call part: Arguments (a parsed map) takes
// precedence, then Input (a raw JSON string) is parsed and re-serialized, and
// an empty object is used as the fallback -- matching how
// types.ToolCallContent.MarshalJSON's toolCallContentInput resolves the same
// field for wire serialization elsewhere.
func perplexityToolCallArguments(part types.ToolCallContent) string {
	var value interface{} = map[string]interface{}{}
	if part.Arguments != nil {
		value = part.Arguments
	} else if part.Input != "" {
		var parsed interface{}
		if err := json.Unmarshal([]byte(part.Input), &parsed); err == nil {
			value = parsed
		} else {
			value = part.Input
		}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// perplexityThoughtSignature mirrors TS getThoughtSignature: it reads
// providerOptions.perplexity.thoughtSignature, returning "" (unset) unless
// the value is a non-null string.
func perplexityThoughtSignature(providerOptions map[string]interface{}) string {
	if providerOptions == nil {
		return ""
	}
	perp, ok := providerOptions["perplexity"].(map[string]interface{})
	if !ok {
		return ""
	}
	if s, ok := perp["thoughtSignature"].(string); ok {
		return s
	}
	return ""
}

// perplexityToolOutput is the TS LanguageModelV4ToolResultOutput resolved
// from a Go ToolResultContent (Output, or the legacy Result/Error fields).
type perplexityToolOutput struct {
	kind    string // text, json, error-text, error-json, content, execution-denied
	value   interface{}
	content []types.ToolResultContentBlock
	reason  string
}

func resolvePerplexityToolOutput(p types.ToolResultContent) perplexityToolOutput {
	if p.Output == nil {
		if p.Error != "" {
			return perplexityToolOutput{kind: "error-text", value: p.Error}
		}
		switch v := p.Result.(type) {
		case string:
			return perplexityToolOutput{kind: "text", value: v}
		case types.ToolResultOutput:
			return resolvePerplexityToolOutput(types.ToolResultContent{Output: &v})
		case *types.ToolResultOutput:
			if v != nil {
				return resolvePerplexityToolOutput(types.ToolResultContent{Output: v})
			}
		}
		return perplexityToolOutput{kind: "json", value: p.Result}
	}

	o := p.Output
	out := perplexityToolOutput{value: o.Value, content: o.Content, reason: o.Reason}
	switch o.Type {
	case types.ToolResultOutputError:
		if _, ok := o.Value.(string); ok {
			out.kind = "error-text"
		} else {
			out.kind = "error-json"
		}
	case "":
		out.kind = "json"
	default:
		out.kind = string(o.Type)
	}
	return out
}

// serializePerplexityToolOutput mirrors TS serializeToolOutput
// (perplexity-language-model-prompt.ts): text/error-text forward the value
// verbatim, json/error-json JSON.stringify the value, execution-denied uses
// the denial reason (or a default message), and content concatenates text
// blocks -- rejecting any non-text block, since the Agent API's
// function_call_output only accepts a plain string.
func serializePerplexityToolOutput(p types.ToolResultContent) (string, error) {
	out := resolvePerplexityToolOutput(p)
	switch out.kind {
	case "text", "error-text":
		if s, ok := out.value.(string); ok {
			return s, nil
		}
		b, err := json.Marshal(out.value)
		return string(b), err
	case "execution-denied":
		if out.reason != "" {
			return out.reason, nil
		}
		return "Tool call execution denied.", nil
	case "content":
		for _, block := range out.content {
			if _, ok := block.(types.TextContentBlock); !ok {
				return "", &providererrors.UnsupportedFunctionalityError{
					Functionality: "file and custom tool result content",
				}
			}
		}
		var b strings.Builder
		for _, block := range out.content {
			if tb, ok := block.(types.TextContentBlock); ok {
				b.WriteString(tb.Text)
			}
		}
		return b.String(), nil
	default: // json, error-json
		b, err := json.Marshal(out.value)
		return string(b), err
	}
}
