package bedrock

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// isMistralModel reports whether modelID identifies a Mistral model on
// Bedrock. Mistral models require 9-character alphanumeric tool call IDs.
func isMistralModel(modelID string) bool {
	return strings.Contains(modelID, "mistral.")
}

// validMistralToolCallIDPattern matches TS /^[a-zA-Z0-9]{9}$/.
var validMistralToolCallIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{9}$`)

const base62Characters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const normalizedToolCallIDLength = 9

// normalizeToolCallID rewrites toolCallID into Mistral's required 9-char
// alphanumeric form when isMistral is true, otherwise returns it unchanged.
// Passthrough IDs already matching ^[a-zA-Z0-9]{9}$ are preserved; anything
// else is deterministically hashed via FNV-1a (64-bit) and base62-encoded,
// mirroring TS normalize-tool-call-id.ts convertToBase62Hash exactly (same
// hash constants, same base62 alphabet/order, same digit-extraction loop).
func normalizeToolCallID(toolCallID string, isMistral bool) string {
	if !isMistral {
		return toolCallID
	}
	if validMistralToolCallIDPattern.MatchString(toolCallID) {
		return toolCallID
	}
	return convertToBase62Hash(toolCallID)
}

// convertToBase62Hash ports TS normalize-tool-call-id.ts#convertToBase62Hash.
// FNV-1a 64-bit is computed over UTF-16 code units (mirroring
// value.charCodeAt(i)) and is deterministic across runtimes; Go's uint64
// arithmetic wraps modulo 2^64 exactly like TS's `& fnv64BitMask` after each
// multiplication, so no explicit masking is needed.
func convertToBase62Hash(value string) string {
	const fnvOffsetBasis64 uint64 = 14695981039346656037
	const fnvPrime64 uint64 = 1099511628211

	hash := fnvOffsetBasis64
	for _, codeUnit := range utf16.Encode([]rune(value)) {
		hash ^= uint64(codeUnit)
		hash *= fnvPrime64
	}

	base62Length := uint64(len(base62Characters))
	normalizedToolCallIDSpace := uint64(1)
	for i := 0; i < normalizedToolCallIDLength; i++ {
		normalizedToolCallIDSpace *= base62Length
	}

	base62Value := hash % normalizedToolCallIDSpace
	result := make([]byte, normalizedToolCallIDLength)
	for i := normalizedToolCallIDLength - 1; i >= 0; i-- {
		result[i] = base62Characters[base62Value%base62Length]
		base62Value /= base62Length
	}
	return string(result)
}

// sanitizeToolName ports TS convert-to-amazon-bedrock-chat-messages.ts#sanitizeToolName.
func sanitizeToolName(toolName string) string {
	sanitized := invalidToolNameCharsPattern.ReplaceAllString(toolName, "")
	if sanitized == "" {
		return "_"
	}
	return sanitized
}

var invalidToolNameCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
var whitespacePattern = regexp.MustCompile(`\s+`)
var invalidDocumentNameCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9 ()\[\]-]`)

// sanitizeDocumentName ports TS
// convert-to-amazon-bedrock-chat-messages.ts#sanitizeDocumentName: strip the
// extension, collapse whitespace, remove invalid characters, trim, cut to 200
// characters, and trim again.
func sanitizeDocumentName(filename string) string {
	name := stripBedrockFileExtension(filename)
	name = whitespacePattern.ReplaceAllString(name, " ")
	name = invalidDocumentNameCharsPattern.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}
	return strings.TrimSpace(name)
}

func stripBedrockFileExtension(filename string) string {
	if dot := strings.Index(filename, "."); dot >= 0 {
		return filename[:dot]
	}
	return filename
}

// toBedrockToolInput wraps a non-object tool-call input as
// {"rawInvalidInput": value}, matching TS
// convert-to-amazon-bedrock-chat-messages.ts#toBedrockToolInput (Bedrock
// requires tool input to be an object).
func toBedrockToolInput(input string, arguments map[string]interface{}) map[string]interface{} {
	if arguments != nil {
		return arguments
	}
	if input == "" {
		return map[string]interface{}{}
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(input), &decoded); err == nil {
		if m, ok := decoded.(map[string]interface{}); ok {
			return m
		}
		return map[string]interface{}{"rawInvalidInput": decoded}
	}
	return map[string]interface{}{"rawInvalidInput": input}
}

// ─── Media type / format mapping ──────────────────────────────────────────

func bedrockImageFormat(mediaType string) (string, error) {
	switch mediaType {
	case "image/jpeg":
		return "jpeg", nil
	case "image/png":
		return "png", nil
	case "image/gif":
		return "gif", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image mime type: %s, expected one of: image/jpeg, image/png, image/gif, image/webp", mediaType)
	}
}

func bedrockDocumentFormat(mediaType string) (string, error) {
	switch mediaType {
	case "application/pdf":
		return "pdf", nil
	case "text/csv":
		return "csv", nil
	case "application/msword":
		return "doc", nil
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx", nil
	case "application/vnd.ms-excel":
		return "xls", nil
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx", nil
	case "text/html":
		return "html", nil
	case "text/plain":
		return "txt", nil
	case "text/markdown":
		return "md", nil
	default:
		return "", fmt.Errorf("unsupported file mime type: %s, expected one of: application/pdf, text/csv, application/msword, application/vnd.openxmlformats-officedocument.wordprocessingml.document, application/vnd.ms-excel, application/vnd.openxmlformats-officedocument.spreadsheetml.sheet, text/html, text/plain, text/markdown", mediaType)
	}
}

// bedrockVideoFormat maps a video MIME type to a Bedrock video format,
// mirroring TS BEDROCK_VIDEO_MIME_TYPES.
func bedrockVideoFormat(mediaType string) (string, error) {
	switch mediaType {
	case "video/x-matroska":
		return "mkv", nil
	case "video/quicktime":
		return "mov", nil
	case "video/mp4":
		return "mp4", nil
	case "video/webm":
		return "webm", nil
	case "video/x-flv":
		return "flv", nil
	case "video/mpeg":
		return "mpeg", nil
	case "video/mpg":
		return "mpg", nil
	case "video/wmv", "video/x-ms-wmv":
		return "wmv", nil
	case "video/3gpp":
		return "three_gp", nil
	default:
		return "", fmt.Errorf("unsupported video mime type: %s", mediaType)
	}
}

func isFullMediaType(mediaType string) bool {
	slash := strings.Index(mediaType, "/")
	return slash >= 0 && slash < len(mediaType)-1 && mediaType[slash+1:] != "*"
}

func topLevelMediaType(mediaType string) string {
	if slash := strings.Index(mediaType, "/"); slash >= 0 {
		return mediaType[:slash]
	}
	return mediaType
}

func fileContentMediaType(file types.FileContent) string {
	if file.MediaType != "" {
		return file.MediaType
	}
	if file.MimeType != "" {
		return file.MimeType
	}
	return file.FileData.MediaType
}

type bedrockFilePayload struct {
	bytes       string
	raw         []byte
	hasRawBytes bool
	hasData     bool
	// s3URI is set when the file references an s3:// URL rather than inline
	// bytes.
	s3URI string
}

func fileContentPayload(file types.FileContent) bedrockFilePayload {
	if strings.HasPrefix(file.URL, "s3://") {
		return bedrockFilePayload{s3URI: file.URL}
	}
	if file.FileData.Type == types.FileDataTypeURL && strings.HasPrefix(file.FileData.URL, "s3://") {
		return bedrockFilePayload{s3URI: file.FileData.URL}
	}
	if file.Data != nil {
		return bedrockFilePayload{bytes: base64Encode(file.Data), raw: file.Data, hasRawBytes: true, hasData: true}
	}
	switch file.FileData.Type {
	case types.FileDataTypeData:
		if file.FileData.DataString != "" {
			payload := bedrockFilePayload{bytes: file.FileData.DataString, hasData: true}
			if decoded, err := types.DecodeFileDataString(file.FileData.DataString); err == nil {
				payload.raw = decoded
				payload.hasRawBytes = true
			}
			return payload
		}
		if file.FileData.Data != nil {
			return bedrockFilePayload{bytes: base64Encode(file.FileData.Data), raw: file.FileData.Data, hasRawBytes: true, hasData: true}
		}
		return bedrockFilePayload{hasRawBytes: true, hasData: true}
	case types.FileDataTypeText:
		raw := []byte(file.FileData.Text)
		return bedrockFilePayload{bytes: base64Encode(raw), raw: raw, hasRawBytes: true, hasData: true}
	}
	if file.FileData.Data != nil {
		return bedrockFilePayload{bytes: base64Encode(file.FileData.Data), raw: file.FileData.Data, hasRawBytes: true, hasData: true}
	}
	if file.Text != "" {
		raw := []byte(file.Text)
		return bedrockFilePayload{bytes: base64Encode(raw), raw: raw, hasRawBytes: true, hasData: true}
	}
	return bedrockFilePayload{}
}

func toolResultFileContentPayload(block types.FileContentBlock) (string, bedrockFilePayload, error) {
	if block.Reference != "" || block.FileData.Type == types.FileDataTypeReference {
		return "", bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data of type: reference")
	}
	mediaType := block.MediaType
	if mediaType == "" {
		mediaType = block.FileData.MediaType
	}

	url := block.URL
	if url == "" && block.FileData.Type == types.FileDataTypeURL {
		url = block.FileData.URL
	}
	if url != "" {
		if !strings.HasPrefix(url, "s3://") {
			return "", bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data of type: url")
		}
		return mediaType, bedrockFilePayload{s3URI: url}, nil
	}

	if block.Data != nil {
		return mediaType, bedrockFilePayload{bytes: base64Encode(block.Data), raw: block.Data, hasRawBytes: true, hasData: true}, nil
	}
	if block.FileData.Type == types.FileDataTypeData {
		if block.FileData.DataString != "" {
			payload := bedrockFilePayload{bytes: block.FileData.DataString, hasData: true}
			if decoded, err := types.DecodeFileDataString(block.FileData.DataString); err == nil {
				payload.raw = decoded
				payload.hasRawBytes = true
			}
			return mediaType, payload, nil
		}
		if block.FileData.Data != nil {
			return mediaType, bedrockFilePayload{bytes: base64Encode(block.FileData.Data), raw: block.FileData.Data, hasRawBytes: true, hasData: true}, nil
		}
		return mediaType, bedrockFilePayload{hasRawBytes: true, hasData: true}, nil
	}
	if block.FileData.Data != nil {
		return mediaType, bedrockFilePayload{bytes: base64Encode(block.FileData.Data), raw: block.FileData.Data, hasRawBytes: true, hasData: true}, nil
	}
	return mediaType, bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data for media type: %s", mediaType)
}

func resolveBedrockDataMediaType(mediaType string, payload bedrockFilePayload) (string, error) {
	if isFullMediaType(mediaType) {
		return mediaType, nil
	}
	if !payload.hasRawBytes {
		return "", fmt.Errorf("file of media type %q must specify subtype since it could not be auto-detected", mediaType)
	}
	switch topLevelMediaType(mediaType) {
	case "image":
		switch {
		case len(payload.raw) >= 4 && payload.raw[0] == 0x89 && payload.raw[1] == 0x50 && payload.raw[2] == 0x4e && payload.raw[3] == 0x47:
			return "image/png", nil
		case len(payload.raw) >= 3 && payload.raw[0] == 0x47 && payload.raw[1] == 0x49 && payload.raw[2] == 0x46:
			return "image/gif", nil
		case len(payload.raw) >= 2 && payload.raw[0] == 0xff && payload.raw[1] == 0xd8:
			return "image/jpeg", nil
		case len(payload.raw) >= 12 && payload.raw[0] == 0x52 && payload.raw[1] == 0x49 && payload.raw[2] == 0x46 && payload.raw[3] == 0x46 && payload.raw[8] == 0x57 && payload.raw[9] == 0x45 && payload.raw[10] == 0x42 && payload.raw[11] == 0x50:
			return "image/webp", nil
		}
	case "application":
		if len(payload.raw) >= 4 && payload.raw[0] == 0x25 && payload.raw[1] == 0x50 && payload.raw[2] == 0x44 && payload.raw[3] == 0x46 {
			return "application/pdf", nil
		}
	}
	return "", fmt.Errorf("file of media type %q must specify subtype since it could not be auto-detected", mediaType)
}

// mediaSourceBlock builds an image/video `source` block: either inline bytes
// or an s3Location, matching TS getAmazonBedrockMediaSource.
func mediaSourceBlock(payload bedrockFilePayload) map[string]interface{} {
	if payload.s3URI != "" {
		return map[string]interface{}{"s3Location": map[string]interface{}{"uri": payload.s3URI}}
	}
	return map[string]interface{}{"bytes": payload.bytes}
}

// ─── Cache points ──────────────────────────────────────────────────────────

func bedrockProviderOptionMap(providerOptions map[string]interface{}, providerName string) map[string]interface{} {
	raw, ok := providerOptions[providerName].(map[string]interface{})
	if !ok {
		return nil
	}
	return raw
}

func bedrockCachePoint(providerOptions map[string]interface{}) (map[string]interface{}, bool) {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		cachePoint, ok := raw["cachePoint"]
		if !ok || cachePoint == nil {
			continue
		}
		if disabled, ok := cachePoint.(bool); ok && !disabled {
			return nil, false
		}
		if cfg, ok := cachePoint.(map[string]interface{}); ok {
			return cloneMap(cfg), true
		}
		return map[string]interface{}{"type": "default"}, true
	}
	return nil, false
}

func appendCachePointBlock(blocks []map[string]interface{}, providerOptions map[string]interface{}) []map[string]interface{} {
	cachePoint, ok := bedrockCachePoint(providerOptions)
	if !ok {
		return blocks
	}
	return append(blocks, map[string]interface{}{"cachePoint": cachePoint})
}

// contentPartProviderOptions extracts the ProviderOptions map from a content
// part via type switch, since types.ContentPart only guarantees ContentType().
func contentPartProviderOptions(part types.ContentPart) map[string]interface{} {
	switch p := part.(type) {
	case types.TextContent:
		return p.ProviderOptions
	case types.ImageContent:
		return p.ProviderOptions
	case types.FileContent:
		return p.ProviderOptions
	case types.ReasoningContent:
		return p.ProviderOptions
	case types.ToolCallContent:
		return p.ProviderOptions
	case types.ToolResultContent:
		return p.ProviderOptions
	default:
		return nil
	}
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// addAnthropicBeta merges beta into the existing additionalModelRequestFields
// "anthropic_beta" value (nil, []interface{}, or []string — the latter two
// are the shapes a caller-supplied providerOptions.amazonBedrock.anthropicBeta
// or a prior append can produce), returning a new []interface{} with beta
// appended if it is not already present. Mirrors TS's `betas: Set<string>`
// dedup semantics (amazon-bedrock-chat-language-model.ts:381-390).
func addAnthropicBeta(existing interface{}, beta string) []interface{} {
	var betas []interface{}
	switch v := existing.(type) {
	case []interface{}:
		betas = append(betas, v...)
	case []string:
		for _, s := range v {
			betas = append(betas, s)
		}
	}
	for _, b := range betas {
		if s, ok := b.(string); ok && s == beta {
			return betas
		}
	}
	return append(betas, beta)
}

// ─── Reasoning metadata ─────────────────────────────────────────────────────

func bedrockReasoningMetadata(part types.ReasoningContent) (signature string, hasSignature bool, redactedData string, hasRedactedData bool, redactedContent string, hasRedactedContent bool) {
	signature = part.Signature
	hasSignature = part.Signature != ""
	redactedData = part.RedactedData
	hasRedactedData = part.RedactedData != ""
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(part.ProviderOptions, providerName)
		if raw == nil {
			continue
		}
		if value, ok := raw["signature"].(string); ok {
			signature = value
			hasSignature = true
		}
		if value, ok := raw["redactedData"].(string); ok {
			redactedData = value
			hasRedactedData = true
		}
		if value, ok := raw["redactedContent"].(string); ok {
			redactedContent = value
			hasRedactedContent = true
		}
		break
	}
	return
}

// ─── Guard content / citations provider options ────────────────────────────

func bedrockTextGuardContentOptions(providerOptions map[string]interface{}) (guard bool, qualifiers []string) {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		guard, _ = raw["guardContent"].(bool)
		if qs, ok := raw["guardContentQualifiers"].([]interface{}); ok {
			for _, q := range qs {
				if s, ok := q.(string); ok {
					qualifiers = append(qualifiers, s)
				}
			}
		}
		return guard, qualifiers
	}
	return false, nil
}

func bedrockImageGuardContentOptions(providerOptions map[string]interface{}) bool {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		guard, _ := raw["guardContent"].(bool)
		return guard
	}
	return false
}

func bedrockCitationsEnabled(providerOptions map[string]interface{}) bool {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		citations, ok := raw["citations"].(map[string]interface{})
		if !ok {
			return false
		}
		enabled, _ := citations["enabled"].(bool)
		return enabled
	}
	return false
}

// ─── Message grouping ──────────────────────────────────────────────────────

type bedrockBlockType int

const (
	bedrockBlockSystem bedrockBlockType = iota
	bedrockBlockUser
	bedrockBlockAssistant
)

type bedrockMessageBlock struct {
	Type     bedrockBlockType
	Messages []types.Message
}

// groupIntoBedrockBlocks ports TS
// convert-to-amazon-bedrock-chat-messages.ts#groupIntoBlocks: consecutive
// messages of the same "shape" (user+tool count as one shape) are grouped so
// they can be merged into a single Bedrock message.
func groupIntoBedrockBlocks(messages []types.Message) []bedrockMessageBlock {
	var blocks []bedrockMessageBlock
	var current *bedrockMessageBlock

	push := func(blockType bedrockBlockType) *bedrockMessageBlock {
		blocks = append(blocks, bedrockMessageBlock{Type: blockType})
		return &blocks[len(blocks)-1]
	}

	for _, msg := range messages {
		var wantType bedrockBlockType
		switch msg.Role {
		case types.RoleSystem:
			wantType = bedrockBlockSystem
		case types.RoleAssistant:
			wantType = bedrockBlockAssistant
		case types.RoleUser, types.RoleTool:
			wantType = bedrockBlockUser
		default:
			wantType = bedrockBlockUser
		}
		if current == nil || current.Type != wantType {
			current = push(wantType)
		}
		current.Messages = append(current.Messages, msg)
	}

	return blocks
}

// convertToBedrockChatMessages ports TS
// convert-to-amazon-bedrock-chat-messages.ts#convertToAmazonBedrockChatMessages.
func convertToBedrockChatMessages(messages []types.Message, isMistral bool) (system []map[string]interface{}, out []map[string]interface{}, err error) {
	blocks := groupIntoBedrockBlocks(messages)

	documentCounter := 0
	generateDocumentName := func() string {
		documentCounter++
		return fmt.Sprintf("document-%d", documentCounter)
	}
	getDocumentName := func(filename string) string {
		if filename != "" {
			if sanitized := sanitizeDocumentName(filename); sanitized != "" {
				return sanitized
			}
		}
		return generateDocumentName()
	}

	for blockIdx, block := range blocks {
		isLastBlock := blockIdx == len(blocks)-1

		switch block.Type {
		case bedrockBlockSystem:
			if len(out) > 0 {
				return nil, nil, fmt.Errorf("multiple system messages that are separated by user/assistant messages are not supported")
			}
			for _, msg := range block.Messages {
				for _, part := range msg.Content {
					if text, ok := part.(types.TextContent); ok {
						system = append(system, map[string]interface{}{"text": text.Text})
						system = appendCachePointBlock(system, text.ProviderOptions)
					}
				}
				system = appendCachePointBlock(system, msg.ProviderOptions)
			}

		case bedrockBlockUser:
			content := []map[string]interface{}{}
			for _, msg := range block.Messages {
				switch msg.Role {
				case types.RoleUser:
					for _, part := range msg.Content {
						switch p := part.(type) {
						case types.TextContent:
							guard, qualifiers := bedrockTextGuardContentOptions(p.ProviderOptions)
							if guard {
								textBlock := map[string]interface{}{"text": p.Text}
								if len(qualifiers) > 0 {
									textBlock["qualifiers"] = qualifiers
								}
								content = append(content, map[string]interface{}{"guardContent": map[string]interface{}{"text": textBlock}})
							} else {
								content = append(content, map[string]interface{}{"text": p.Text})
							}
						case types.ImageContent:
							block, err := bedrockUserImageBlock(p)
							if err != nil {
								return nil, nil, err
							}
							content = append(content, block)
						case types.FileContent:
							block, ok, err := bedrockUserFileBlock(p, getDocumentName)
							if err != nil {
								return nil, nil, err
							}
							if ok {
								content = append(content, block)
							}
						}
						content = appendCachePointBlock(content, contentPartProviderOptions(part))
					}
				case types.RoleTool:
					for _, part := range msg.Content {
						toolResult, ok := part.(types.ToolResultContent)
						if !ok {
							continue
						}
						resultContent, err := bedrockToolResultContent(toolResult, getDocumentName)
						if err != nil {
							return nil, nil, err
						}
						content = append(content, map[string]interface{}{
							"toolResult": map[string]interface{}{
								"toolUseId": normalizeToolCallID(toolResult.ToolCallID, isMistral),
								"content":   resultContent,
							},
						})
						content = appendCachePointBlock(content, toolResult.ProviderOptions)
					}
				}
				content = appendCachePointBlock(content, msg.ProviderOptions)
			}
			out = appendToBedrockUserMessage(out, content)

		case bedrockBlockAssistant:
			var assistantContent []map[string]interface{}
			var toolResultContent []map[string]interface{}

			flushAssistant := func() {
				hasNonCache := false
				for _, b := range assistantContent {
					if _, isCache := b["cachePoint"]; !isCache {
						hasNonCache = true
						break
					}
				}
				if hasNonCache {
					out = append(out, map[string]interface{}{"role": "assistant", "content": assistantContent})
				}
				assistantContent = nil
			}
			flushToolResult := func() {
				if len(toolResultContent) > 0 {
					out = appendToBedrockUserMessage(out, toolResultContent)
					toolResultContent = nil
				}
			}

			for msgIdx, msg := range block.Messages {
				isLastMessage := msgIdx == len(block.Messages)-1
				hasReasoning := false
				for _, part := range msg.Content {
					if _, ok := part.(types.ReasoningContent); ok {
						hasReasoning = true
						break
					}
				}

				var lastPartWasToolResult bool
				for partIdx, part := range msg.Content {
					isLastContentPart := partIdx == len(msg.Content)-1
					_, isToolResult := part.(types.ToolResultContent)
					if !isToolResult {
						flushToolResult()
					}
					lastPartWasToolResult = isToolResult

					switch p := part.(type) {
					case types.TextContent:
						if strings.TrimSpace(p.Text) == "" && !hasReasoning {
							break
						}
						text := p.Text
						if isLastBlock && isLastMessage && isLastContentPart {
							text = strings.TrimSpace(text)
						}
						assistantContent = append(assistantContent, map[string]interface{}{"text": text})
					case types.ReasoningContent:
						signature, hasSignature, redactedData, hasRedactedData, redactedContent, hasRedactedContent := bedrockReasoningMetadata(p)
						switch {
						case hasSignature:
							assistantContent = append(assistantContent, map[string]interface{}{
								"reasoningContent": map[string]interface{}{
									"reasoningText": map[string]interface{}{"text": p.Text, "signature": signature},
								},
							})
						case hasRedactedContent:
							assistantContent = append(assistantContent, map[string]interface{}{
								"reasoningContent": map[string]interface{}{"redactedContent": redactedContent},
							})
						case hasRedactedData:
							assistantContent = append(assistantContent, map[string]interface{}{
								"reasoningContent": map[string]interface{}{"redactedReasoning": map[string]interface{}{"data": redactedData}},
							})
						}
						// Unsigned reasoning is intentionally not replayed.
					case types.ToolCallContent:
						assistantContent = append(assistantContent, map[string]interface{}{
							"toolUse": map[string]interface{}{
								"toolUseId": normalizeToolCallID(p.ToolCallID, isMistral),
								"name":      sanitizeToolName(p.ToolName),
								"input":     toBedrockToolInput(p.Input, p.Arguments),
							},
						})
					case types.ToolResultContent:
						flushAssistant()
						resultContent, err := bedrockToolResultContent(p, getDocumentName)
						if err != nil {
							return nil, nil, err
						}
						toolResultContent = append(toolResultContent, map[string]interface{}{
							"toolResult": map[string]interface{}{
								"toolUseId": normalizeToolCallID(p.ToolCallID, isMistral),
								"content":   resultContent,
							},
						})
					}

					if isToolResult {
						toolResultContent = appendCachePointBlock(toolResultContent, contentPartProviderOptions(part))
					} else {
						assistantContent = appendCachePointBlock(assistantContent, contentPartProviderOptions(part))
					}
				}

				if lastPartWasToolResult {
					toolResultContent = appendCachePointBlock(toolResultContent, msg.ProviderOptions)
				} else {
					assistantContent = appendCachePointBlock(assistantContent, msg.ProviderOptions)
				}
			}

			flushToolResult()
			flushAssistant()
		}
	}

	return system, out, nil
}

// appendToBedrockUserMessage merges content into the previous message when it
// is a user message that already holds a toolResult block (so a trailing
// assistant tool result and the following user content share a single
// Bedrock message and role alternation is preserved), otherwise appends a new
// user message. Ports TS appendToUserMessage.
func appendToBedrockUserMessage(messages []map[string]interface{}, content []map[string]interface{}) []map[string]interface{} {
	if len(content) == 0 {
		return messages
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if last["role"] == "user" {
			if existing, ok := last["content"].([]map[string]interface{}); ok {
				hasToolResult := false
				for _, block := range existing {
					if _, ok := block["toolResult"]; ok {
						hasToolResult = true
						break
					}
				}
				if hasToolResult {
					last["content"] = append(existing, content...)
					return messages
				}
			}
		}
	}
	return append(messages, map[string]interface{}{"role": "user", "content": content})
}

func bedrockUserImageBlock(p types.ImageContent) (map[string]interface{}, error) {
	if p.Image == nil {
		if strings.HasPrefix(p.URL, "s3://") {
			format, err := bedrockImageFormat(p.MimeType)
			if err != nil {
				return nil, err
			}
			imageBlock := map[string]interface{}{
				"image": map[string]interface{}{
					"format": format,
					"source": map[string]interface{}{"s3Location": map[string]interface{}{"uri": p.URL}},
				},
			}
			return imageBlock, nil
		}
		if p.URL != "" {
			return nil, fmt.Errorf("image URL data is not supported")
		}
		return nil, fmt.Errorf("image content requires inline data")
	}
	format, err := bedrockImageFormat(p.MimeType)
	if err != nil {
		return nil, err
	}
	imageBlock := map[string]interface{}{
		"format": format,
		"source": map[string]interface{}{"bytes": base64Encode(p.Image)},
	}
	if bedrockImageGuardContentOptions(p.ProviderOptions) {
		return map[string]interface{}{"guardContent": map[string]interface{}{"image": imageBlock}}, nil
	}
	return map[string]interface{}{"image": imageBlock}, nil
}

func bedrockUserFileBlock(file types.FileContent, getDocumentName func(string) string) (map[string]interface{}, bool, error) {
	if file.Reference != "" || file.FileData.Type == types.FileDataTypeReference {
		return nil, false, fmt.Errorf("file parts with provider references are not supported")
	}

	mediaType := fileContentMediaType(file)
	payload := fileContentPayload(file)

	if payload.s3URI != "" {
		switch topLevelMediaType(mediaType) {
		case "image":
			format, err := bedrockImageFormat(mediaType)
			if err != nil {
				return nil, false, err
			}
			return map[string]interface{}{"image": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}}, true, nil
		case "video":
			format, err := bedrockVideoFormat(mediaType)
			if err != nil {
				return nil, false, err
			}
			return map[string]interface{}{"video": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}}, true, nil
		default:
			return nil, false, fmt.Errorf("File URL data is not supported for media type: %s", mediaType) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	if file.URL != "" || file.FileData.Type == types.FileDataTypeURL {
		return nil, false, fmt.Errorf("file URL data is not supported")
	}

	if file.Text != "" || file.FileData.Type == types.FileDataTypeText {
		if !isFullMediaType(mediaType) {
			mediaType = "text/plain"
		}
	} else if payload.hasData {
		resolved, err := resolveBedrockDataMediaType(mediaType, payload)
		if err != nil {
			return nil, false, err
		}
		mediaType = resolved
	}
	if !payload.hasData {
		return nil, false, nil
	}

	switch topLevelMediaType(mediaType) {
	case "image":
		format, err := bedrockImageFormat(mediaType)
		if err != nil {
			return nil, false, err
		}
		return map[string]interface{}{"image": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}}, true, nil
	case "video":
		format, err := bedrockVideoFormat(mediaType)
		if err != nil {
			return nil, false, err
		}
		return map[string]interface{}{"video": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}}, true, nil
	}

	format, err := bedrockDocumentFormat(mediaType)
	if err != nil {
		return nil, false, err
	}
	document := map[string]interface{}{
		"format": format,
		"name":   getDocumentName(file.Filename),
		"source": map[string]interface{}{"bytes": payload.bytes},
	}
	if bedrockCitationsEnabled(file.ProviderOptions) {
		document["citations"] = map[string]interface{}{"enabled": true}
	}
	return map[string]interface{}{"document": document}, true, nil
}

// bedrockToolResultContent ports TS convert-to-amazon-bedrock-chat-messages.ts#convertToolResultOutput.
func bedrockToolResultContent(part types.ToolResultContent, getDocumentName func(string) string) ([]map[string]interface{}, error) {
	if part.Output == nil {
		if part.Error != "" {
			return []map[string]interface{}{{"text": part.Error}}, nil
		}
		if part.Result != nil {
			return []map[string]interface{}{{"text": fmt.Sprint(part.Result)}}, nil
		}
		return []map[string]interface{}{{"text": ""}}, nil
	}
	switch part.Output.Type {
	case types.ToolResultOutputContent:
		out := make([]map[string]interface{}, 0, len(part.Output.Content))
		for _, block := range part.Output.Content {
			switch b := block.(type) {
			case types.TextContentBlock:
				out = append(out, map[string]interface{}{"text": b.Text})
			case types.ImageContentBlock:
				format, err := bedrockImageFormat(b.MediaType)
				if err != nil {
					return nil, err
				}
				out = append(out, map[string]interface{}{
					"image": map[string]interface{}{"format": format, "source": map[string]interface{}{"bytes": base64Encode(b.Data)}},
				})
			case types.FileContentBlock:
				mediaType, payload, err := toolResultFileContentPayload(b)
				if err != nil {
					return nil, err
				}
				if payload.s3URI == "" {
					mediaType, err = resolveBedrockDataMediaType(mediaType, payload)
					if err != nil {
						return nil, err
					}
				}
				switch topLevelMediaType(mediaType) {
				case "image":
					format, err := bedrockImageFormat(mediaType)
					if err != nil {
						return nil, err
					}
					out = append(out, map[string]interface{}{"image": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}})
				case "video":
					format, err := bedrockVideoFormat(mediaType)
					if err != nil {
						return nil, err
					}
					out = append(out, map[string]interface{}{"video": map[string]interface{}{"format": format, "source": mediaSourceBlock(payload)}})
				default:
					if payload.s3URI != "" {
						return nil, fmt.Errorf(`unsupported tool result file data of type "url"`)
					}
					format, err := bedrockDocumentFormat(mediaType)
					if err != nil {
						return nil, err
					}
					document := map[string]interface{}{
						"format": format,
						"name":   getDocumentName(b.Filename),
						"source": map[string]interface{}{"bytes": payload.bytes},
					}
					if bedrockCitationsEnabled(b.ProviderOptions) {
						document["citations"] = map[string]interface{}{"enabled": true}
					}
					out = append(out, map[string]interface{}{"document": document})
				}
			default:
				return nil, fmt.Errorf("unsupported tool content part type: %s", block.ToolResultContentType())
			}
		}
		return out, nil
	case types.ToolResultOutputText, types.ToolResultOutputError, types.ToolResultOutputErrorText:
		// Ports TS convertToolResultOutput's 'text'/'error-text' case
		// (convert-to-amazon-bedrock-chat-messages.ts:682-684): raw value
		// passthrough, no JSON encoding. ToolResultOutputError has no TS
		// equivalent; it is a Go-only generic error shape, treated the same as
		// error-text (a message string) rather than JSON-encoded.
		return []map[string]interface{}{{"text": fmt.Sprint(part.Output.Value)}}, nil
	case types.ToolResultOutputExecutionDenied:
		reason := part.Output.Reason
		if reason == "" {
			reason = "Tool call execution denied."
		}
		return []map[string]interface{}{{"text": reason}}, nil
	case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		fallthrough
	default:
		// Ports TS's 'json'/'error-json'/default case (ts:687-690):
		// JSON.stringify(output.value).
		data, err := json.Marshal(part.Output.Value)
		if err != nil {
			return []map[string]interface{}{{"text": fmt.Sprint(part.Output.Value)}}, nil
		}
		return []map[string]interface{}{{"text": string(data)}}, nil
	}
}
