package telemetry

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/intsafe"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// This file is the Go port of TS's otel/src/gen-ai-format-messages.ts: it
// converts go-ai's internal prompt/content representations into the OTel
// GenAI semantic-convention message shape used for the gen_ai.input.messages,
// gen_ai.output.messages and gen_ai.system_instructions span attributes
// (fc15550, 5ad6abf, 12cfe40). Tests in gen_ai_format_messages_test.go port
// TS's gen-ai-format-messages.test.ts cases as goldens.

// semConvPart is a single {type, ...} part in a SemConv message, mirroring
// TS's SemConvPart discriminated union. Field sets vary by "type", so a plain
// map (rather than a Go struct per variant) mirrors the TS union most
// directly and keeps JSON output byte-identical in shape.
type semConvPart = map[string]interface{}

// semConvInputMessage mirrors TS's SemConvInputMessage ({role, parts}).
type semConvInputMessage struct {
	Role  string        `json:"role"`
	Parts []semConvPart `json:"parts"`
}

// semConvOutputMessage mirrors TS's SemConvOutputMessage ({role, parts, finish_reason}).
type semConvOutputMessage struct {
	Role         string        `json:"role"`
	Parts        []semConvPart `json:"parts"`
	FinishReason string        `json:"finish_reason"`
}

// ---------------------------------------------------------------------------
// mapProviderName / mapOperationName
// ---------------------------------------------------------------------------

type providerNameMapping struct{ prefix, mapped string }

// wellKnownProviderPrefixes is checked longest-prefix-first (declaration
// order matters for multi-segment prefixes like "google.vertex" needing to
// win over the single-segment "google"), mirroring TS mapProviderName.
var wellKnownProviderPrefixes = []providerNameMapping{
	{"google.vertex", "gcp.vertex_ai"},
	{"google.generative-ai", "gcp.gemini"},
	{"google-vertex", "gcp.vertex_ai"},
	{"amazon-bedrock", "aws.bedrock"},
	{"azure-openai", "azure.ai.openai"},
	{"anthropic", "anthropic"},
	{"openai", "openai"},
	{"azure", "azure.ai.inference"},
	{"google", "gcp.gemini"},
	{"mistral", "mistral_ai"},
	{"cohere", "cohere"},
	{"bedrock", "aws.bedrock"},
	{"groq", "groq"},
	{"deepseek", "deepseek"},
	{"perplexity", "perplexity"},
	{"xai", "x_ai"},
}

// mapProviderName maps a go-ai provider string to a well-known
// gen_ai.provider.name value per the OTel GenAI SemConv, mirroring TS's
// mapProviderName (eb70e72, fc15550).
func mapProviderName(provider string) string {
	lower := strings.ToLower(provider)
	for _, m := range wellKnownProviderPrefixes {
		if lower == m.prefix || strings.HasPrefix(lower, m.prefix+".") || strings.HasPrefix(lower, m.prefix+"-") {
			return m.mapped
		}
	}
	return provider
}

var operationNameMapping = map[string]string{
	"ai.generateText":   "invoke_agent",
	"ai.streamText":     "invoke_agent",
	"ai.generateObject": "invoke_agent",
	"ai.streamObject":   "invoke_agent",
	"ai.embed":          "embeddings",
	"ai.embedMany":      "embeddings",
	"ai.rerank":         "rerank",
	// generateSpeech/transcribe have no standardized GenAI operation name
	// yet, so TS maps them to themselves (identity); mapOperationName's
	// default fallback already does this without an explicit entry, but
	// these are listed for parity with TS's mapping object.
	"ai.generateSpeech":   "ai.generateSpeech",
	"ai.transcribe":       "ai.transcribe",
	"ai.streamTranscribe": "ai.streamTranscribe",
}

// mapOperationName maps a go-ai operationId to a gen_ai.operation.name value,
// mirroring TS's mapOperationName (fc15550).
func mapOperationName(operationID string) string {
	if v, ok := operationNameMapping[operationID]; ok {
		return v
	}
	return operationID
}

// ---------------------------------------------------------------------------
// formatSystemInstructions / extractSystemFromPrompt
// ---------------------------------------------------------------------------

// formatSystemInstructions converts a system string into the
// gen_ai.system_instructions SemConv format, mirroring TS's
// formatSystemInstructions. go-ai always carries a single flattened system
// string (unlike TS, which also accepts an array of system messages), so
// only the string variant is ported.
func formatSystemInstructions(system string) []semConvPart {
	if system == "" {
		return nil
	}
	return []semConvPart{{"type": "text", "content": system}}
}

// ---------------------------------------------------------------------------
// Content-part conversion
// ---------------------------------------------------------------------------

// nullableString returns nil for an empty string, matching TS's `?? null`
// pattern for optional ids.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// toolCallArgumentsValue decodes a ToolCallContent's input the same way
// types.ToolCallContent.MarshalJSON does (Arguments when present, else the
// raw JSON string parsed, else nil), matching TS's `part.input`.
func toolCallArgumentsValue(t types.ToolCallContent) interface{} {
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

// toolResultResponseValue derives the "response" field for a tool_call_response
// SemConv part, mirroring TS's output.type switch in convertMessagePartToSemConv.
// go-ai also supports legacy Result/Error fields (pre-dating the structured
// Output union); those are used as a fallback when Output is nil.
func toolResultResponseValue(t types.ToolResultContent) interface{} {
	out := t.Output
	if out == nil {
		if t.Error != "" {
			return t.Error
		}
		return t.Result
	}
	switch out.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText,
		types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		return out.Value
	case types.ToolResultOutputExecutionDenied:
		return map[string]interface{}{"denied": true, "reason": out.Reason}
	default:
		return out
	}
}

// modalityForMediaType mirrors TS's getModality helper.
func modalityForMediaType(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "video/"):
		return "video"
	case strings.HasPrefix(mediaType, "audio/"):
		return "audio"
	default:
		return "image"
	}
}

func fileMediaType(f types.FileContent) string {
	return firstNonEmptyStr(f.MediaType, f.FileData.MediaType, f.MimeType)
}

// convertFilePartToSemConv converts a FileContent part to a "uri" or "blob"
// SemConv part, mirroring TS's 'file' case in convertMessagePartToSemConv.
func convertFilePartToSemConv(f types.FileContent) semConvPart {
	mediaType := fileMediaType(f)
	modality := modalityForMediaType(mediaType)
	mimeType := nullableString(mediaType)

	switch f.FileData.Type {
	case types.FileDataTypeURL:
		return semConvPart{"type": "uri", "modality": modality, "mime_type": mimeType, "uri": firstNonEmptyStr(f.FileData.URL, f.URL)}
	case types.FileDataTypeText:
		return semConvPart{"type": "blob", "modality": modality, "mime_type": mimeType, "content": f.FileData.Text}
	case types.FileDataTypeData:
		content := f.FileData.DataString
		if content == "" && len(f.FileData.Data) > 0 {
			content = base64.StdEncoding.EncodeToString(f.FileData.Data)
		}
		return semConvPart{"type": "blob", "modality": modality, "mime_type": mimeType, "content": content}
	default:
		if f.URL != "" {
			return semConvPart{"type": "uri", "modality": modality, "mime_type": mimeType, "uri": f.URL}
		}
		return semConvPart{"type": "blob", "modality": modality, "mime_type": mimeType, "content": base64.StdEncoding.EncodeToString(f.Data)}
	}
}

// convertMessagePartToSemConv converts a single content part to its SemConv
// shape, mirroring TS's convertMessagePartToSemConv.
func convertMessagePartToSemConv(part types.ContentPart) semConvPart {
	switch p := part.(type) {
	case types.TextContent:
		return semConvPart{"type": "text", "content": p.Text}
	case types.ReasoningContent:
		return semConvPart{"type": "reasoning", "content": p.Text}
	case types.ToolCallContent:
		return semConvPart{
			"type":      "tool_call",
			"id":        nullableString(p.ToolCallID),
			"name":      p.ToolName,
			"arguments": toolCallArgumentsValue(p),
		}
	case types.ToolResultContent:
		return semConvPart{
			"type":     "tool_call_response",
			"id":       nullableString(p.ToolCallID),
			"response": toolResultResponseValue(p),
		}
	case types.FileContent:
		return convertFilePartToSemConv(p)
	case types.ToolApprovalResponseContent:
		return semConvPart{
			"type":        "tool_approval_response",
			"approval_id": p.ApprovalID,
			"approved":    p.Approved,
			"reason":      p.Reason,
		}
	case types.CustomContent:
		return semConvPart{"type": "custom", "kind": p.Kind}
	case types.ReasoningFileContent:
		return semConvPart{"type": "reasoning-file"}
	default:
		return semConvPart{"type": part.ContentType()}
	}
}

// ---------------------------------------------------------------------------
// formatInputMessages
// ---------------------------------------------------------------------------

// systemMessageText extracts the flattened text of a role="system" message's
// content parts (go-ai models a system message's content as a single
// TextContent part).
func systemMessageText(m types.Message) string {
	for _, c := range m.Content {
		if t, ok := c.(types.TextContent); ok {
			return t.Text
		}
	}
	return ""
}

// formatInputMessages converts a normalized message list into the
// gen_ai.input.messages SemConv format, preserving message order (including
// any inline system messages, when AllowSystemInMessages let one through),
// mirroring TS's formatInputMessages.
func formatInputMessages(messages []types.Message) []semConvInputMessage {
	result := make([]semConvInputMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == types.RoleSystem {
			result = append(result, semConvInputMessage{
				Role:  "system",
				Parts: []semConvPart{{"type": "text", "content": systemMessageText(m)}},
			})
			continue
		}
		parts := make([]semConvPart, 0, len(m.Content))
		for _, c := range m.Content {
			parts = append(parts, convertMessagePartToSemConv(c))
		}
		result = append(result, semConvInputMessage{Role: string(m.Role), Parts: parts})
	}
	return result
}

// ---------------------------------------------------------------------------
// formatOutputMessagesFromContent / formatObjectOutputMessages
// ---------------------------------------------------------------------------

// mapFinishReasonSemConv mirrors TS's mapFinishReason.
func mapFinishReasonSemConv(reason string) string {
	switch reason {
	case "stop":
		return "stop"
	case "length":
		return "length"
	case "content-filter":
		return "content_filter"
	case "tool-calls":
		return "tool_call"
	case "error":
		return "error"
	case "other", "unknown":
		return "stop"
	default:
		return reason
	}
}

func generatedFileToSemConv(f types.GeneratedFileContent) semConvPart {
	mediaType := f.MediaType
	content := ""
	switch {
	case f.FileData.Type == types.FileDataTypeData && f.FileData.DataString != "":
		content = f.FileData.DataString
	case f.FileData.Type == types.FileDataTypeData && len(f.FileData.Data) > 0:
		content = base64.StdEncoding.EncodeToString(f.FileData.Data)
	case len(f.Data) > 0:
		content = base64.StdEncoding.EncodeToString(f.Data)
	}
	return semConvPart{"type": "blob", "modality": modalityForMediaType(mediaType), "mime_type": nullableString(mediaType), "content": content}
}

// formatOutputMessagesFromContent converts an ordered content-part list (as
// carried on LanguageModelCallEndEvent.Content / GenerateResult.Content) into
// the gen_ai.output.messages SemConv format, mirroring TS's
// formatOutputMessages. Unlike TS (whose caller pre-buckets by type before
// calling formatOutputMessages), go-ai's content list interleaves types, so
// this function does the bucketing itself; the resulting bucket order
// (reasoning, text, tool calls, tool results, files) matches TS exactly.
func formatOutputMessagesFromContent(content []types.ContentPart, finishReason string) []semConvOutputMessage {
	var textBuilder strings.Builder
	var reasoningParts, toolCallParts, toolResultParts, fileParts []semConvPart

	for _, c := range content {
		switch p := c.(type) {
		case types.TextContent:
			textBuilder.WriteString(p.Text)
		case types.ReasoningContent:
			if p.Text != "" {
				reasoningParts = append(reasoningParts, semConvPart{"type": "reasoning", "content": p.Text})
			}
		case types.ToolCallContent:
			toolCallParts = append(toolCallParts, semConvPart{
				"type":      "tool_call",
				"id":        nullableString(p.ToolCallID),
				"name":      p.ToolName,
				"arguments": toolCallArgumentsValue(p),
			})
		case types.ToolResultContent:
			toolResultParts = append(toolResultParts, semConvPart{
				"type":     "tool_call_response",
				"id":       nullableString(p.ToolCallID),
				"response": toolResultResponseValue(p),
			})
		case types.GeneratedFileContent:
			fileParts = append(fileParts, generatedFileToSemConv(p))
		}
	}

	partsCap := intsafe.AddCap(len(reasoningParts), 1)
	partsCap = intsafe.AddCap(partsCap, len(toolCallParts))
	partsCap = intsafe.AddCap(partsCap, len(toolResultParts))
	partsCap = intsafe.AddCap(partsCap, len(fileParts))
	parts := make([]semConvPart, 0, partsCap)
	parts = append(parts, reasoningParts...)
	if textBuilder.Len() > 0 {
		parts = append(parts, semConvPart{"type": "text", "content": textBuilder.String()})
	}
	parts = append(parts, toolCallParts...)
	parts = append(parts, toolResultParts...)
	parts = append(parts, fileParts...)

	return []semConvOutputMessage{{
		Role:         "assistant",
		Parts:        parts,
		FinishReason: mapFinishReasonSemConv(finishReason),
	}}
}

// formatObjectOutputMessages converts a generateObject/streamObject result's
// serialized object text into the gen_ai.output.messages SemConv format,
// mirroring TS's formatObjectOutputMessages.
func formatObjectOutputMessages(objectText, finishReason string) []semConvOutputMessage {
	return []semConvOutputMessage{{
		Role:         "assistant",
		Parts:        []semConvPart{{"type": "text", "content": objectText}},
		FinishReason: mapFinishReasonSemConv(finishReason),
	}}
}

// promptMessages extracts the message list from a LanguageModelCallStartEvent
// / TelemetryStartEvent's untyped Prompt field, when it holds a types.Prompt
// (the shape genOpts.Prompt / GenerateOptions.Prompt use).
func promptMessages(prompt interface{}) ([]types.Message, bool) {
	switch p := prompt.(type) {
	case types.Prompt:
		return p.Messages, true
	case *types.Prompt:
		if p == nil {
			return nil, false
		}
		return p.Messages, true
	default:
		return nil, false
	}
}
