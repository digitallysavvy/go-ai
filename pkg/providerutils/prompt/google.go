package prompt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GoogleSkipThoughtSignatureValidator is the sentinel value Google documents
// for replaying functionCall parts whose original thoughtSignature is not
// available to the client. See https://ai.google.dev/gemini-api/docs/thought-signatures.
const GoogleSkipThoughtSignatureValidator = "skip_thought_signature_validator"

// GoogleMessagesOptions configures ConvertToGoogleMessages. Mirrors the TS
// convertToGoogleMessages options object.
type GoogleMessagesOptions struct {
	// System is an optional system prompt that precedes any system messages.
	System string

	// IsGemmaModel folds system instructions into the first user message.
	IsGemmaModel bool

	// IsGemini3Model enables the thought-signature sentinel for unsigned
	// replayed function calls.
	IsGemini3Model bool

	// ProviderOptionsNames are the keys looked up under part provider options
	// (e.g. thought signatures). Defaults to ["google"]. Vertex uses
	// ["googleVertex", "vertex"].
	ProviderOptionsNames []string

	// SupportsFunctionResponseParts selects the Gemini 3+ multimodal
	// functionResponse.parts format for tool results.
	SupportsFunctionResponseParts bool

	// IncludeFunctionCallIDs controls whether functionCall/functionResponse
	// parts carry an `id`. Vertex rejects them (TS includeFunctionCallIds).
	IncludeFunctionCallIDs bool
}

// GooglePrompt is the converted Gemini prompt.
type GooglePrompt struct {
	// SystemInstruction is nil when there is no system content (or for Gemma).
	SystemInstruction map[string]interface{}
	Contents          []map[string]interface{}
	Warnings          []types.Warning
}

// ToGoogleMessages converts unified messages to Google (Gemini) contents.
//
// Deprecated: use ConvertToGoogleMessages, which also returns the system
// instruction, warnings, and conversion errors.
func ToGoogleMessages(messages []types.Message, supportsFunctionResponseParts bool) []map[string]interface{} {
	out, _ := ConvertToGoogleMessages(messages, GoogleMessagesOptions{
		SupportsFunctionResponseParts: supportsFunctionResponseParts,
		IncludeFunctionCallIDs:        true,
	})
	return out.Contents
}

type googleConverter struct {
	opts         GoogleMessagesOptions
	names        []string
	isVertexLike bool

	missingSignatureToolNames []string
}

// ConvertToGoogleMessages ports TS convertToGoogleMessages
// (packages/google/src/convert-to-google-messages.ts).
func ConvertToGoogleMessages(messages []types.Message, opts GoogleMessagesOptions) (GooglePrompt, error) {
	names := opts.ProviderOptionsNames
	if len(names) == 0 {
		names = []string{"google"}
	}
	c := &googleConverter{opts: opts, names: names, isVertexLike: !containsString(names, "google")}

	var systemParts []map[string]interface{}
	var systemTexts []string
	contents := make([]map[string]interface{}, 0, len(messages))
	systemMessagesAllowed := true

	if opts.System != "" {
		systemParts = append(systemParts, map[string]interface{}{"text": opts.System})
		systemTexts = append(systemTexts, opts.System)
	}

	for _, msg := range messages {
		switch msg.Role {
		case types.RoleSystem:
			if !systemMessagesAllowed {
				return GooglePrompt{}, fmt.Errorf("unsupported functionality: system messages are only supported at the beginning of the conversation")
			}
			text := googleSystemText(msg)
			systemParts = append(systemParts, map[string]interface{}{"text": text})
			systemTexts = append(systemTexts, text)

		case types.RoleAssistant:
			systemMessagesAllowed = false
			parts, err := c.convertAssistant(msg)
			if err != nil {
				return GooglePrompt{}, err
			}
			contents = append(contents, map[string]interface{}{"role": "model", "parts": parts})

		case types.RoleTool:
			systemMessagesAllowed = false
			parts := make([]map[string]interface{}, 0, len(msg.Content))
			for _, part := range msg.Content {
				p, ok := asToolResult(part)
				if !ok {
					continue
				}
				opts := c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata)
				serverToolCallID := stringOpt(opts, "serverToolCallId")
				serverToolType := stringOpt(opts, "serverToolType")
				if serverToolCallID != "" && serverToolType != "" && len(contents) > 0 {
					last := contents[len(contents)-1]
					if last["role"] == "model" {
						lastParts, _ := last["parts"].([]map[string]interface{})
						last["parts"] = append(lastParts, googleServerToolResponse(p, serverToolType, serverToolCallID, stringOpt(opts, "thoughtSignature")))
						continue
					}
				}
				c.appendFunctionResponse(&parts, p)
			}
			contents = append(contents, map[string]interface{}{"role": "user", "parts": parts})

		default:
			systemMessagesAllowed = false
			parts, err := c.convertUser(msg)
			if err != nil {
				return GooglePrompt{}, err
			}
			contents = append(contents, map[string]interface{}{"role": "user", "parts": parts})
		}
	}

	if opts.IsGemmaModel && len(systemTexts) > 0 && len(contents) > 0 && contents[0]["role"] == "user" {
		parts, _ := contents[0]["parts"].([]map[string]interface{})
		prefixed := append([]map[string]interface{}{{"text": strings.Join(systemTexts, "\n\n") + "\n\n"}}, parts...)
		contents[0]["parts"] = prefixed
	}

	result := GooglePrompt{Contents: contents}
	if len(systemParts) > 0 && !opts.IsGemmaModel {
		result.SystemInstruction = map[string]interface{}{"parts": systemParts}
	}

	if len(c.missingSignatureToolNames) > 0 {
		seen := map[string]bool{}
		var unique []string
		for _, name := range c.missingSignatureToolNames {
			if !seen[name] {
				seen[name] = true
				unique = append(unique, "`"+name+"`")
			}
		}
		message := fmt.Sprintf("Replayed %d `functionCall` part(s) for a Gemini 3 model without a `thoughtSignature` (tools: %s). "+
			"Injected the documented `skip_thought_signature_validator` sentinel to keep the request from failing with HTTP 400. "+
			"The likely cause is application code that drops `providerOptions.google.thoughtSignature` when persisting or serializing assistant tool-call messages. "+
			"See https://ai.google.dev/gemini-api/docs/thought-signatures.", len(c.missingSignatureToolNames), strings.Join(unique, ", "))
		result.Warnings = append(result.Warnings, types.Warning{Type: "other", Message: message, Details: message})
	}

	return result, nil
}

func googleSystemText(msg types.Message) string {
	var texts []string
	for _, part := range msg.Content {
		if t, ok := part.(types.TextContent); ok {
			texts = append(texts, t.Text)
		}
	}
	return strings.Join(texts, "")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// readProviderOpts returns the per-part Google options/metadata. ProviderOptions
// take precedence over ProviderMetadata (the output-direction mirror).
func (c *googleConverter) readProviderOpts(providerOptions map[string]interface{}, providerMetadata interface{}) map[string]interface{} {
	sources := []map[string]interface{}{providerOptions, googleMetadataMap(providerMetadata)}
	for _, src := range sources {
		if src == nil {
			continue
		}
		for _, name := range c.names {
			if v, ok := src[name].(map[string]interface{}); ok && v != nil {
				return v
			}
		}
	}
	for _, src := range sources {
		if src == nil {
			continue
		}
		// Cross-namespace fallback (gateway interop).
		if c.isVertexLike {
			if v, ok := src["google"].(map[string]interface{}); ok {
				return v
			}
			continue
		}
		if v, ok := src["googleVertex"].(map[string]interface{}); ok {
			return v
		}
		if v, ok := src["vertex"].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}

func googleMetadataMap(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return m
	case json.RawMessage:
		if len(m) == 0 {
			return nil
		}
		var out map[string]interface{}
		if json.Unmarshal(m, &out) != nil {
			return nil
		}
		return out
	}
	return nil
}

func stringOpt(opts map[string]interface{}, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func asToolResult(part types.ContentPart) (types.ToolResultContent, bool) {
	switch p := part.(type) {
	case types.ToolResultContent:
		return p, true
	case *types.ToolResultContent:
		if p != nil {
			return *p, true
		}
	}
	return types.ToolResultContent{}, false
}

// ── user ─────────────────────────────────────────────────────────────────────

func (c *googleConverter) convertUser(msg types.Message) ([]map[string]interface{}, error) {
	parts := make([]map[string]interface{}, 0, len(msg.Content))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			parts = append(parts, map[string]interface{}{"text": p.Text})
		case types.ImageContent:
			if p.URL != "" {
				mimeType := p.MimeType
				if mimeType == "" || mimeType == "image/*" || mimeType == "image" {
					mimeType = "image/jpeg"
				}
				parts = append(parts, map[string]interface{}{
					"fileData": map[string]interface{}{"mimeType": mimeType, "fileUri": p.URL},
				})
			} else {
				parts = append(parts, map[string]interface{}{
					"inlineData": map[string]interface{}{"mimeType": p.MimeType, "data": base64.StdEncoding.EncodeToString(p.Image)},
				})
			}
		case types.FileContent:
			fp, err := c.fileContentPart(p, false)
			if err != nil {
				return nil, err
			}
			parts = append(parts, fp)
		case types.CustomContent:
			if googleOpts, ok := p.ProviderOptions["google"].(map[string]interface{}); ok {
				block := map[string]interface{}{}
				for k, v := range googleOpts {
					block[k] = v
				}
				parts = append(parts, block)
			}
		}
	}
	return parts, nil
}

type googleFile struct {
	mediaType string
	url       string
	reference string
	text      string
	hasText   bool
	data      []byte
}

func (c *googleConverter) resolveFile(file types.FileContent) googleFile {
	f := googleFile{mediaType: firstNonEmpty(file.MediaType, file.MimeType, file.FileData.MediaType)}
	f.url = firstNonEmpty(file.URL, file.FileData.URL)
	f.reference = file.Reference
	if f.reference == "" && file.FileData.Reference != nil {
		f.reference = file.FileData.Reference["google"]
		if f.reference == "" {
			for _, v := range file.FileData.Reference {
				f.reference = v
				break
			}
		}
	}
	if file.Text != "" || file.FileData.Type == types.FileDataTypeText {
		f.text = firstNonEmpty(file.Text, file.FileData.Text)
		f.hasText = true
	}
	f.data = file.Data
	if len(f.data) == 0 {
		f.data = file.FileData.Data
		if len(f.data) == 0 && file.FileData.DataString != "" {
			if decoded, err := base64.StdEncoding.DecodeString(file.FileData.DataString); err == nil {
				f.data = decoded
			}
		}
	}
	return f
}

// fileContentPart converts a file part. assistant=true applies the TS
// assistant-side rules (URLs rejected, thought flag honored).
func (c *googleConverter) fileContentPart(file types.FileContent, assistant bool) (map[string]interface{}, error) {
	f := c.resolveFile(file)
	switch {
	case f.url != "":
		if assistant {
			return nil, fmt.Errorf("unsupported functionality: File data URLs in assistant messages are not supported")
		}
		return map[string]interface{}{
			"fileData": map[string]interface{}{"mimeType": googleFullMediaType(f.mediaType, f.url), "fileUri": f.url},
		}, nil
	case f.reference != "":
		if c.isVertexLike {
			return nil, fmt.Errorf("unsupported functionality: file parts with provider references")
		}
		return map[string]interface{}{
			"fileData": map[string]interface{}{"mimeType": f.mediaType, "fileUri": f.reference},
		}, nil
	case f.hasText:
		mimeType := "text/plain"
		if isFullMediaType(f.mediaType) {
			mimeType = f.mediaType
		}
		return map[string]interface{}{
			"inlineData": map[string]interface{}{"mimeType": mimeType, "data": base64.StdEncoding.EncodeToString([]byte(f.text))},
		}, nil
	default:
		return map[string]interface{}{
			"inlineData": map[string]interface{}{"mimeType": f.mediaType, "data": base64.StdEncoding.EncodeToString(f.data)},
		}, nil
	}
}

// googleFullMediaType resolves a top-level media type ("image") to a full one
// using the URL extension when possible (TS resolveFullMediaType).
func googleFullMediaType(mediaType, url string) string {
	if isFullMediaType(mediaType) {
		return mediaType
	}
	if mediaType == "image" || mediaType == "image/*" || mediaType == "" {
		lower := strings.ToLower(url)
		switch {
		case strings.HasSuffix(lower, ".png"):
			return "image/png"
		case strings.HasSuffix(lower, ".gif"):
			return "image/gif"
		case strings.HasSuffix(lower, ".webp"):
			return "image/webp"
		}
		if mediaType == "" {
			return mediaType
		}
		return "image/jpeg"
	}
	return mediaType
}

// ── assistant ────────────────────────────────────────────────────────────────

type googleToolCall struct {
	id               string
	toolName         string
	input            interface{}
	providerExecuted bool
	opts             map[string]interface{}
	thoughtSignature string
}

func (c *googleConverter) assistantParts(msg types.Message) []interface{} {
	seen := map[string]bool{}
	out := make([]interface{}, 0, len(msg.Content)+len(msg.ToolCalls))
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			seen[p.ToolCallID] = true
		case *types.ToolCallContent:
			if p != nil {
				seen[p.ToolCallID] = true
			}
		}
		out = append(out, part)
	}
	for _, tc := range msg.ToolCalls {
		if tc.ID != "" && seen[tc.ID] {
			continue
		}
		out = append(out, tc)
	}
	return out
}

func (c *googleConverter) toolCallFrom(part interface{}) (googleToolCall, bool) {
	switch p := part.(type) {
	case *types.ToolCallContent:
		if p == nil {
			return googleToolCall{}, false
		}
		return c.toolCallFrom(*p)
	case types.ToolCallContent:
		var input interface{}
		if p.Arguments != nil {
			input = p.Arguments
		} else if p.Input != "" {
			if err := json.Unmarshal([]byte(p.Input), &input); err != nil {
				input = p.Input
			}
		}
		return googleToolCall{
			id: p.ToolCallID, toolName: p.ToolName, input: input, providerExecuted: p.ProviderExecuted,
			opts: c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata), thoughtSignature: p.ThoughtSignature,
		}, true
	case types.ToolCall:
		var input interface{}
		if p.Arguments != nil {
			input = p.Arguments
		} else if p.RawArguments != "" {
			if err := json.Unmarshal([]byte(p.RawArguments), &input); err != nil {
				input = p.RawArguments
			}
		}
		return googleToolCall{
			id: p.ID, toolName: p.ToolName, input: input, providerExecuted: p.ProviderExecuted,
			opts: c.readProviderOpts(nil, p.ProviderMetadata), thoughtSignature: p.ThoughtSignature,
		}, true
	}
	return googleToolCall{}, false
}

func (c *googleConverter) convertAssistant(msg types.Message) ([]map[string]interface{}, error) {
	parts := make([]map[string]interface{}, 0)
	modelResponseHasSignedFunctionCall := false

	for _, raw := range c.assistantParts(msg) {
		switch p := raw.(type) {
		case types.TextContent:
			if p.Text == "" {
				continue
			}
			part := map[string]interface{}{"text": p.Text}
			if sig := stringOpt(c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata), "thoughtSignature"); sig != "" {
				part["thoughtSignature"] = sig
			}
			parts = append(parts, part)

		case types.ReasoningContent:
			if p.Text == "" {
				continue
			}
			part := map[string]interface{}{"text": p.Text, "thought": true}
			sig := stringOpt(c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata), "thoughtSignature")
			if sig == "" {
				sig = p.Signature
			}
			if sig != "" {
				part["thoughtSignature"] = sig
			}
			parts = append(parts, part)

		case types.ReasoningFileContent:
			if p.FileData.URL != "" {
				return nil, fmt.Errorf("unsupported functionality: File data URLs in assistant messages are not supported")
			}
			data := p.Data
			if len(data) == 0 {
				data = p.FileData.Data
			}
			part := map[string]interface{}{
				"inlineData": map[string]interface{}{"mimeType": firstNonEmpty(p.MediaType, p.FileData.MediaType), "data": base64.StdEncoding.EncodeToString(data)},
				"thought":    true,
			}
			if sig := stringOpt(c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata), "thoughtSignature"); sig != "" {
				part["thoughtSignature"] = sig
			}
			parts = append(parts, part)

		case types.ImageContent:
			parts = append(parts, map[string]interface{}{
				"inlineData": map[string]interface{}{"mimeType": p.MimeType, "data": base64.StdEncoding.EncodeToString(p.Image)},
			})

		case types.FileContent:
			part, err := c.fileContentPart(p, true)
			if err != nil {
				return nil, err
			}
			opts := c.readProviderOpts(p.ProviderOptions, nil)
			if thought, _ := opts["thought"].(bool); thought {
				part["thought"] = true
			}
			if sig := stringOpt(opts, "thoughtSignature"); sig != "" {
				part["thoughtSignature"] = sig
			}
			parts = append(parts, part)

		case types.CustomContent:
			if googleOpts, ok := p.ProviderOptions["google"].(map[string]interface{}); ok {
				block := map[string]interface{}{}
				for k, v := range googleOpts {
					block[k] = v
				}
				parts = append(parts, block)
			}

		case types.ToolResultContent, *types.ToolResultContent:
			tr, _ := asToolResult(raw.(types.ContentPart))
			if part := c.assistantToolResult(tr); part != nil {
				parts = append(parts, part)
			}

		default:
			tc, ok := c.toolCallFrom(raw)
			if !ok {
				continue
			}
			parts = append(parts, c.assistantToolCall(tc, &modelResponseHasSignedFunctionCall))
		}
	}
	return parts, nil
}

func (c *googleConverter) assistantToolCall(tc googleToolCall, hasSigned *bool) map[string]interface{} {
	if tc.providerExecuted && tc.toolName == "code_execution" {
		input, _ := tc.input.(map[string]interface{})
		if s, ok := tc.input.(string); ok {
			_ = json.Unmarshal([]byte(s), &input)
		}
		return map[string]interface{}{
			"executableCode": map[string]interface{}{
				"language": stringOpt(input, "language"),
				"code":     stringOpt(input, "code"),
			},
		}
	}

	thoughtSignature := stringOpt(tc.opts, "thoughtSignature")
	if thoughtSignature == "" {
		thoughtSignature = tc.thoughtSignature
	}
	serverToolCallID := stringOpt(tc.opts, "serverToolCallId")
	serverToolType := stringOpt(tc.opts, "serverToolType")
	isServerToolCall := serverToolCallID != "" && serverToolType != ""
	// Gemini 3 returns a single signature for a parallel function-call response
	// on the first standard function call; later ones legitimately have none.
	skipMitigation := !isServerToolCall && thoughtSignature == "" && *hasSigned
	effective := thoughtSignature
	if effective == "" && c.opts.IsGemini3Model && !skipMitigation {
		c.missingSignatureToolNames = append(c.missingSignatureToolNames, tc.toolName)
		effective = GoogleSkipThoughtSignatureValidator
	}
	if !isServerToolCall && thoughtSignature != "" {
		*hasSigned = true
	}

	var part map[string]interface{}
	if isServerToolCall {
		part = map[string]interface{}{
			"toolCall": map[string]interface{}{"toolType": serverToolType, "args": tc.input, "id": serverToolCallID},
		}
	} else {
		args := tc.input
		if args == nil {
			args = map[string]interface{}{}
		}
		fc := map[string]interface{}{"name": tc.toolName, "args": args}
		if c.opts.IncludeFunctionCallIDs && tc.id != "" {
			fc["id"] = tc.id
		}
		part = map[string]interface{}{"functionCall": fc}
	}
	if effective != "" {
		part["thoughtSignature"] = effective
	}
	return part
}

func (c *googleConverter) assistantToolResult(p types.ToolResultContent) map[string]interface{} {
	out := resolveGoogleToolOutput(p)
	if p.ToolName == "code_execution" && out.kind == "json" {
		value, _ := out.value.(map[string]interface{})
		return map[string]interface{}{
			"codeExecutionResult": map[string]interface{}{
				"outcome": stringOpt(value, "outcome"),
				"output":  stringOpt(value, "output"),
			},
		}
	}
	opts := c.readProviderOpts(p.ProviderOptions, p.ProviderMetadata)
	serverToolCallID := stringOpt(opts, "serverToolCallId")
	serverToolType := stringOpt(opts, "serverToolType")
	if serverToolCallID != "" && serverToolType != "" {
		return googleServerToolResponse(p, serverToolType, serverToolCallID, stringOpt(opts, "thoughtSignature"))
	}
	return nil
}

func googleServerToolResponse(p types.ToolResultContent, toolType, id, thoughtSignature string) map[string]interface{} {
	out := resolveGoogleToolOutput(p)
	var response interface{} = map[string]interface{}{}
	if out.kind == "json" {
		response = out.value
	}
	part := map[string]interface{}{
		"toolResponse": map[string]interface{}{"toolType": toolType, "response": response, "id": id},
	}
	if thoughtSignature != "" {
		part["thoughtSignature"] = thoughtSignature
	}
	return part
}

// ── tool results ─────────────────────────────────────────────────────────────

type googleToolOutput struct {
	kind    string // text, json, error-text, error-json, content, execution-denied
	value   interface{}
	content []types.ToolResultContentBlock
	reason  string
}

func resolveGoogleToolOutput(p types.ToolResultContent) googleToolOutput {
	if p.Output == nil {
		if p.Error != "" {
			return googleToolOutput{kind: "error-text", value: p.Error}
		}
		switch v := p.Result.(type) {
		case string:
			return googleToolOutput{kind: "text", value: v}
		case types.ToolResultOutput:
			return resolveGoogleToolOutput(types.ToolResultContent{Output: &v})
		case *types.ToolResultOutput:
			if v != nil {
				return resolveGoogleToolOutput(types.ToolResultContent{Output: v})
			}
		}
		return googleToolOutput{kind: "json", value: p.Result}
	}
	o := p.Output
	out := googleToolOutput{value: o.Value, content: o.Content, reason: o.Reason}
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

func (c *googleConverter) appendFunctionResponse(parts *[]map[string]interface{}, p types.ToolResultContent) {
	out := resolveGoogleToolOutput(p)
	if out.kind == "content" {
		if c.opts.SupportsFunctionResponseParts {
			c.appendToolResultParts(parts, p.ToolName, p.ToolCallID, out.content)
		} else {
			c.appendLegacyToolResultParts(parts, p.ToolName, p.ToolCallID, out.content)
		}
		return
	}
	var content interface{}
	if out.kind == "execution-denied" {
		content = "Tool call execution denied."
		if out.reason != "" {
			content = out.reason
		}
	} else {
		content = out.value
	}
	*parts = append(*parts, map[string]interface{}{"functionResponse": c.functionResponse(p.ToolName, p.ToolCallID, content)})
}

func (c *googleConverter) functionResponse(toolName, toolCallID string, content interface{}) map[string]interface{} {
	fr := map[string]interface{}{
		"name":     toolName,
		"response": map[string]interface{}{"name": toolName, "content": content},
	}
	if c.opts.IncludeFunctionCallIDs && toolCallID != "" {
		fr["id"] = toolCallID
	}
	return fr
}

var googleDataURLRegex = regexp.MustCompile(`(?s)^data:([^;,]+);base64,(.+)$`)

// appendToolResultParts implements the Gemini 3+ multimodal functionResponse
// format: text goes into response.content, inline files into parts[].
func (c *googleConverter) appendToolResultParts(parts *[]map[string]interface{}, toolName, toolCallID string, blocks []types.ToolResultContentBlock) {
	var textParts []string
	var responseParts []map[string]interface{}

	for _, block := range blocks {
		switch b := block.(type) {
		case types.TextContentBlock:
			textParts = append(textParts, b.Text)
		case types.ImageContentBlock:
			responseParts = append(responseParts, map[string]interface{}{
				"inlineData": map[string]interface{}{"mimeType": b.MediaType, "data": base64.StdEncoding.EncodeToString(b.Data)},
			})
		case types.FileContentBlock:
			f := c.resolveFile(fileBlockToContent(b))
			switch {
			case f.url != "":
				if m := googleDataURLRegex.FindStringSubmatch(f.url); m != nil {
					responseParts = append(responseParts, map[string]interface{}{
						"inlineData": map[string]interface{}{"mimeType": m[1], "data": m[2]},
					})
				} else {
					textParts = append(textParts, googleJSONText(b))
				}
			case len(f.data) > 0:
				responseParts = append(responseParts, map[string]interface{}{
					"inlineData": map[string]interface{}{"mimeType": f.mediaType, "data": base64.StdEncoding.EncodeToString(f.data)},
				})
			default:
				textParts = append(textParts, googleJSONText(b))
			}
		default:
			textParts = append(textParts, googleJSONText(block))
		}
	}

	responseContent := "Tool executed successfully."
	if len(textParts) > 0 {
		responseContent = strings.Join(textParts, "\n")
	}
	fr := c.functionResponse(toolName, toolCallID, responseContent)
	if len(responseParts) > 0 {
		fr["parts"] = responseParts
	}
	*parts = append(*parts, map[string]interface{}{"functionResponse": fr})
}

// appendLegacyToolResultParts implements the pre-Gemini-3 fallback: text
// becomes a functionResponse; inline files become top-level inlineData parts
// followed by a descriptive text part.
func (c *googleConverter) appendLegacyToolResultParts(parts *[]map[string]interface{}, toolName, toolCallID string, blocks []types.ToolResultContentBlock) {
	appendInline := func(mediaType string, data []byte) {
		kind := "file"
		if strings.HasPrefix(mediaType, "image/") || mediaType == "image" {
			kind = "image"
		}
		*parts = append(*parts,
			map[string]interface{}{
				"inlineData": map[string]interface{}{"mimeType": mediaType, "data": base64.StdEncoding.EncodeToString(data)},
			},
			map[string]interface{}{"text": "Tool executed successfully and returned this " + kind + " as a response"},
		)
	}
	for _, block := range blocks {
		switch b := block.(type) {
		case types.TextContentBlock:
			*parts = append(*parts, map[string]interface{}{"functionResponse": c.functionResponse(toolName, toolCallID, b.Text)})
		case types.ImageContentBlock:
			appendInline(b.MediaType, b.Data)
		case types.FileContentBlock:
			f := c.resolveFile(fileBlockToContent(b))
			if f.url == "" && f.reference == "" && !f.hasText && len(f.data) > 0 {
				appendInline(f.mediaType, f.data)
			} else {
				*parts = append(*parts, map[string]interface{}{"text": googleJSONText(b)})
			}
		default:
			*parts = append(*parts, map[string]interface{}{"text": googleJSONText(block)})
		}
	}
}

func fileBlockToContent(b types.FileContentBlock) types.FileContent {
	return types.FileContent{
		FileData: b.FileData, Data: b.Data, MediaType: b.MediaType, Filename: b.Filename,
		URL: b.URL, Reference: b.Reference, Text: b.Text, ProviderOptions: b.ProviderOptions,
	}
}

func googleJSONText(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
