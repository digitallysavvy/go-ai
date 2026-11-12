package telemetry

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// legacyPromptMessagesJSON is TS stringifyForTelemetry(promptMessages) for
// the legacy step span's ai.prompt.messages (otel stringify-for-telemetry.ts
// 18-35): the step's LanguageModelV4Prompt, which TS builds with
// convertToLanguageModelPrompt (convert-to-language-model-prompt.ts). The
// instructions lead as system messages ({role:'system', content:<text>}),
// then every message in the V4 shape: no Go-only keys (tool_calls, name,
// fileData, mimeType, signature, result, ...), file part data as a string
// (base64 bytes, the URL, the text or the provider reference), consecutive
// tool messages combined and empty tool messages dropped. ok is false when
// the prompt has no message.
//
// Go has no V4 prompt type: the input is the step's []types.Message, which
// the SDK has already normalized for the model call (file media types
// resolved, legacy image parts turned into file parts). Nested tool-result
// content keeps the Go ToolResultOutput JSON.
func legacyPromptMessagesJSON(system string, instructionMessages, messages []types.Message) (string, bool) {
	out := make([]*jsonorder.Object, 0, len(messages)+len(instructionMessages)+1)
	if len(instructionMessages) > 0 {
		for _, m := range instructionMessages {
			out = append(out, legacyV4SystemMessage(legacyMessageText(m), m.ProviderOptions))
		}
	} else if system != "" {
		out = append(out, legacyV4SystemMessage(system, nil))
	}

	var lastTool *legacyV4ToolMessage
	for _, m := range messages {
		if m.Role == types.RoleTool {
			content := legacyV4ToolContent(m.Content)
			if lastTool != nil {
				lastTool.append(content, m.ProviderOptions)
				continue
			}
			// The combined message is built once its run of tool messages
			// ends; its slot is reserved here.
			out = append(out, nil)
			lastTool = &legacyV4ToolMessage{index: len(out) - 1, content: content, providerOptions: m.ProviderOptions}
			continue
		}
		if lastTool != nil {
			out[lastTool.index] = lastTool.object()
			lastTool = nil
		}
		out = append(out, legacyV4Message(m))
	}
	if lastTool != nil {
		out[lastTool.index] = lastTool.object()
	}

	// TS: filter out empty tool messages.
	kept := out[:0]
	for _, o := range out {
		if o == nil {
			continue
		}
		if role, _ := o.Get("role"); role == string(types.RoleTool) {
			if c, _ := o.Get("content"); len(c.([]interface{})) == 0 {
				continue
			}
		}
		kept = append(kept, o)
	}
	if len(kept) == 0 {
		return "", false
	}
	list := make([]interface{}, len(kept))
	for i, o := range kept {
		list[i] = o
	}
	return jsonorder.Stringify(list), true
}

// legacyV4ToolMessage is a tool message being combined with the ones that
// follow it (TS "combine consecutive tool messages into a single tool
// message").
type legacyV4ToolMessage struct {
	index           int
	content         []interface{}
	providerOptions map[string]interface{}
}

// append is the TS combine step: the last part of the combined message takes
// the combined message's providerOptions merged under its own, and the
// combined message then takes the new message's providerOptions (merged
// with its own when it had no part yet).
func (t *legacyV4ToolMessage) append(content []interface{}, providerOptions map[string]interface{}) {
	var lastPart *jsonorder.Object
	if n := len(t.content); n > 0 {
		lastPart, _ = t.content[n-1].(*jsonorder.Object)
	}
	if lastPart != nil && t.providerOptions != nil {
		existing, _ := lastPart.Get("providerOptions")
		existingMap, _ := existing.(map[string]interface{})
		lastPart.Set("providerOptions", legacyMergeObjects(t.providerOptions, existingMap))
	}
	t.content = append(t.content, content...)
	if lastPart == nil {
		t.providerOptions = legacyMergeObjects(t.providerOptions, providerOptions)
	} else {
		t.providerOptions = providerOptions
	}
}

func (t *legacyV4ToolMessage) object() *jsonorder.Object {
	o := jsonorder.New()
	o.Set("role", string(types.RoleTool))
	o.Set("content", t.content)
	if t.providerOptions != nil {
		o.Set("providerOptions", t.providerOptions)
	}
	return o
}

// legacyMergeObjects is TS mergeObjects: a deep merge where overrides win;
// nil when both are nil.
func legacyMergeObjects(base, overrides map[string]interface{}) map[string]interface{} {
	if base == nil && overrides == nil {
		return nil
	}
	if base == nil {
		return overrides
	}
	if overrides == nil {
		return base
	}
	out := make(map[string]interface{}, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		bm, bok := out[k].(map[string]interface{})
		om, ook := v.(map[string]interface{})
		if bok && ook {
			out[k] = legacyMergeObjects(bm, om)
			continue
		}
		out[k] = v
	}
	return out
}

func legacyV4SystemMessage(text string, providerOptions map[string]interface{}) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("role", string(types.RoleSystem))
	o.Set("content", text)
	if providerOptions != nil {
		o.Set("providerOptions", providerOptions)
	}
	return o
}

// legacyMessageText is the text of a Go system message (its text parts
// concatenated), the TS SystemModelMessage string content.
func legacyMessageText(m types.Message) string {
	var b strings.Builder
	for _, part := range m.Content {
		switch p := part.(type) {
		case types.TextContent:
			b.WriteString(p.Text)
		case *types.TextContent:
			if p != nil {
				b.WriteString(p.Text)
			}
		}
	}
	return b.String()
}

func legacyV4Message(m types.Message) *jsonorder.Object {
	if m.Role == types.RoleSystem {
		return legacyV4SystemMessage(legacyMessageText(m), m.ProviderOptions)
	}
	content := []interface{}{}
	for _, part := range m.Content {
		var p interface{}
		if m.Role == types.RoleAssistant {
			p = legacyV4AssistantPart(part)
		} else {
			p = legacyV4UserPart(part)
		}
		if p != nil {
			content = append(content, p)
		}
	}
	o := jsonorder.New()
	o.Set("role", string(m.Role))
	o.Set("content", content)
	if m.ProviderOptions != nil {
		o.Set("providerOptions", m.ProviderOptions)
	}
	return o
}

func setProviderOptions(o *jsonorder.Object, providerOptions map[string]interface{}) *jsonorder.Object {
	if providerOptions != nil {
		o.Set("providerOptions", providerOptions)
	}
	return o
}

func legacyV4TextPart(typ, text string, providerOptions map[string]interface{}) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("type", typ)
	o.Set("text", text)
	return setProviderOptions(o, providerOptions)
}

// legacyV4UserPart converts a user message part (TS
// convertPartToLanguageModelPart after convertImagePartToFilePart); empty
// text parts are removed.
func legacyV4UserPart(part types.ContentPart) interface{} {
	switch p := deref(part).(type) {
	case types.TextContent:
		if p.Text == "" {
			return nil
		}
		return legacyV4TextPart("text", p.Text, p.ProviderOptions)
	case types.FileContent:
		o := jsonorder.New()
		o.Set("type", "file")
		o.Set("mediaType", fileMediaType(p))
		if p.Filename != "" {
			o.Set("filename", p.Filename)
		}
		o.Set("data", legacyFileDataValue(p))
		return setProviderOptions(o, p.ProviderOptions)
	case types.ImageContent:
		mediaType := p.MimeType
		if mediaType == "" {
			mediaType = "image"
		}
		o := jsonorder.New()
		o.Set("type", "file")
		o.Set("mediaType", mediaType)
		if p.URL != "" {
			o.Set("data", p.URL)
		} else {
			o.Set("data", base64.StdEncoding.EncodeToString(p.Image))
		}
		return setProviderOptions(o, p.ProviderOptions)
	}
	return legacyV4AssistantPart(part)
}

// legacyV4AssistantPart converts an assistant message part (TS
// convertToLanguageModelMessage 'assistant'): empty text parts without
// provider options and tool approval requests are removed.
func legacyV4AssistantPart(part types.ContentPart) interface{} {
	switch p := deref(part).(type) {
	case types.TextContent:
		if p.Text == "" && p.ProviderOptions == nil {
			return nil
		}
		return legacyV4TextPart("text", p.Text, p.ProviderOptions)
	case types.ReasoningContent:
		return legacyV4TextPart("reasoning", p.Text, p.ProviderOptions)
	case types.CustomContent:
		o := jsonorder.New()
		o.Set("type", "custom")
		o.Set("kind", p.Kind)
		return setProviderOptions(o, p.ProviderOptions)
	case types.FileContent:
		o := jsonorder.New()
		o.Set("type", "file")
		o.Set("data", legacyFileDataValue(p))
		if p.Filename != "" {
			o.Set("filename", p.Filename)
		}
		o.Set("mediaType", fileMediaType(p))
		return setProviderOptions(o, p.ProviderOptions)
	case types.ReasoningFileContent:
		o := jsonorder.New()
		o.Set("type", "reasoning-file")
		o.Set("data", legacyReasoningFileData(p))
		o.Set("mediaType", firstNonEmptyStr(p.MediaType, p.FileData.MediaType))
		return setProviderOptions(o, p.ProviderOptions)
	case types.ToolCallContent:
		o := jsonorder.New()
		o.Set("type", "tool-call")
		o.Set("toolCallId", p.ToolCallID)
		o.Set("toolName", p.ToolName)
		o.Set("input", legacyToolCallInput(p))
		if p.ProviderExecuted {
			o.Set("providerExecuted", true)
		}
		return setProviderOptions(o, p.ProviderOptions)
	case types.ToolResultContent:
		return legacyV4ToolResult(p)
	case types.ToolErrorContent:
		return legacyV4ToolError(p)
	case types.ToolApprovalRequestContent:
		return nil
	case types.ToolApprovalResponseContent:
		return legacyV4ApprovalResponse(p)
	}
	return part
}

// legacyV4ToolContent converts a tool message's parts: tool results, and
// approval responses of provider-executed tools only.
func legacyV4ToolContent(parts []types.ContentPart) []interface{} {
	out := []interface{}{}
	for _, part := range parts {
		switch p := deref(part).(type) {
		case types.ToolResultContent:
			out = append(out, legacyV4ToolResult(p))
		case types.ToolErrorContent:
			out = append(out, legacyV4ToolError(p))
		case types.ToolApprovalResponseContent:
			if p.ProviderExecuted {
				out = append(out, legacyV4ApprovalResponse(p))
			}
		}
	}
	return out
}

func legacyV4ApprovalResponse(p types.ToolApprovalResponseContent) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("type", "tool-approval-response")
	o.Set("approvalId", p.ApprovalID)
	o.Set("approved", p.Approved)
	if p.HasReason() {
		o.Set("reason", p.Reason)
	}
	return o
}

func legacyV4ToolResult(p types.ToolResultContent) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("type", "tool-result")
	o.Set("toolCallId", p.ToolCallID)
	o.Set("toolName", p.ToolName)
	o.Set("output", legacyToolResultOutput(p))
	return setProviderOptions(o, p.ProviderOptions)
}

func legacyV4ToolError(p types.ToolErrorContent) *jsonorder.Object {
	text := p.ErrorText
	if text == "" {
		switch e := p.Error.(type) {
		case string:
			text = e
		case error:
			text = e.Error()
		case nil:
		default:
			text = fmt.Sprint(e)
		}
	}
	o := jsonorder.New()
	o.Set("type", "tool-result")
	o.Set("toolCallId", p.ToolCallID)
	o.Set("toolName", p.ToolName)
	o.Set("output", types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: text})
	return setProviderOptions(o, p.ProviderOptions)
}

// legacyToolResultOutput is the TS ToolResultOutput of a tool result: the
// structured Output, else the legacy Result / Error fields (error-text for
// an error, text for a string result, json otherwise).
func legacyToolResultOutput(p types.ToolResultContent) interface{} {
	if p.Output == nil {
		if p.Error != "" {
			return types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: p.Error}
		}
		switch v := p.Result.(type) {
		case string:
			return types.ToolResultOutput{Type: types.ToolResultOutputText, Value: v}
		case types.ToolResultOutput:
			return legacyToolResultOutput(types.ToolResultContent{Output: &v})
		case *types.ToolResultOutput:
			if v != nil {
				return legacyToolResultOutput(types.ToolResultContent{Output: v})
			}
		}
		return types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: p.Result}
	}
	out := *p.Output
	switch out.Type {
	case types.ToolResultOutputError:
		if _, ok := out.Value.(string); ok {
			out.Type = types.ToolResultOutputErrorText
		} else {
			out.Type = types.ToolResultOutputErrorJSON
		}
	case "":
		out.Type = types.ToolResultOutputJSON
	}
	return out
}

// legacyToolCallInput is the V4 tool-call input: the parsed JSON input in
// wire key order, else the decoded Arguments, else {} (TS always carries an
// input). Text that is not JSON (an invalid call) stays the raw string.
func legacyToolCallInput(p types.ToolCallContent) interface{} {
	if p.Input != "" {
		if v, ok := jsonorder.Parse([]byte(p.Input)); ok {
			return v
		}
		if p.Arguments == nil {
			return p.Input
		}
	}
	if p.Arguments != nil {
		return p.Arguments
	}
	return map[string]interface{}{}
}

// legacyFileDataValue is TS serializeFileData for a file part: base64 for
// inline bytes (a base64 string as given), the URL, the inline text, or the
// provider reference.
func legacyFileDataValue(f types.FileContent) interface{} {
	switch f.FileData.Type {
	case types.FileDataTypeData:
		if f.FileData.DataString != "" {
			return f.FileData.DataString
		}
		return base64.StdEncoding.EncodeToString(f.FileData.Data)
	case types.FileDataTypeURL:
		return firstNonEmptyStr(f.FileData.URL, f.URL)
	case types.FileDataTypeText:
		return f.FileData.Text
	case types.FileDataTypeReference:
		return f.FileData.Reference
	}
	switch {
	case f.URL != "":
		return f.URL
	case f.Text != "" && len(f.Data) == 0:
		return f.Text
	case f.Reference != "" && len(f.Data) == 0:
		return f.Reference
	}
	return base64.StdEncoding.EncodeToString(f.Data)
}

// legacyReasoningFileData is the V4 reasoning-file data, which
// stringifyForTelemetry leaves tagged: {type:'data', data:<base64>} or
// {type:'url', url}.
func legacyReasoningFileData(r types.ReasoningFileContent) *jsonorder.Object {
	o := jsonorder.New()
	if r.FileData.Type == types.FileDataTypeURL {
		o.Set("type", "url")
		o.Set("url", r.FileData.URL)
		return o
	}
	o.Set("type", "data")
	switch {
	case r.FileData.DataString != "":
		o.Set("data", r.FileData.DataString)
	case len(r.FileData.Data) > 0:
		o.Set("data", base64.StdEncoding.EncodeToString(r.FileData.Data))
	default:
		o.Set("data", base64.StdEncoding.EncodeToString(r.Data))
	}
	return o
}

// deref turns a pointer content part into its value form.
func deref(part types.ContentPart) types.ContentPart {
	switch p := part.(type) {
	case *types.TextContent:
		if p != nil {
			return *p
		}
	case *types.FileContent:
		if p != nil {
			return *p
		}
	case *types.ImageContent:
		if p != nil {
			return *p
		}
	case *types.ReasoningContent:
		if p != nil {
			return *p
		}
	case *types.ToolCallContent:
		if p != nil {
			return *p
		}
	case *types.ToolResultContent:
		if p != nil {
			return *p
		}
	}
	return part
}

// legacyModelMessages is the TS ModelMessage[] JSON shape of the root
// ai.prompt messages: role, content and providerOptions only (no Go-only
// tool_calls or name). promptString, when set, is the caller's string
// prompt, which TS standardizePrompt keeps as the user message's string
// content.
func legacyModelMessages(messages []types.Message, promptString *string) []interface{} {
	if promptString != nil {
		o := jsonorder.New()
		o.Set("role", string(types.RoleUser))
		o.Set("content", *promptString)
		return []interface{}{o}
	}
	out := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		o := jsonorder.New()
		o.Set("role", string(m.Role))
		content := m.Content
		if content == nil {
			content = []types.ContentPart{}
		}
		o.Set("content", content)
		if m.ProviderOptions != nil {
			o.Set("providerOptions", m.ProviderOptions)
		}
		out = append(out, o)
	}
	return out
}
