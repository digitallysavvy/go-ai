package prompt

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// InvalidURLError is returned when a URL file part's URL does not parse as a
// WHATWG URL. Its text is that of the TypeError TS `new URL(url)` throws.
type InvalidURLError struct {
	URL string
}

func (e *InvalidURLError) Error() string {
	return "Invalid URL"
}

// ReasoningFileConstraintError is returned when a reasoning-file part uses a
// FileData variant that cannot represent model-produced reasoning output.
type ReasoningFileConstraintError struct {
	Type types.FileDataType
}

func (e *ReasoningFileConstraintError) Error() string {
	// TS convertToLanguageModelPrompt: `Unsupported reasoning-file data type: ${data.type}`.
	return fmt.Sprintf("Unsupported reasoning-file data type: %s", e.Type)
}

// UnsupportedSystemMessageError is returned when a messages prompt contains a
// system-role message without the explicit AllowSystemMessages opt-in.
type UnsupportedSystemMessageError struct{}

func (e *UnsupportedSystemMessageError) Error() string {
	// TS standardizePrompt's InvalidPromptError message ("Invalid prompt: " prefix included).
	return "Invalid prompt: " + unsupportedSystemMessageText
}

const unsupportedSystemMessageText = "System messages are not allowed in the prompt or messages fields. Use the instructions option instead."

// Unwrap exposes the TS InvalidPromptError the condition corresponds to, so
// errors.As matches *InvalidPromptError as well as this type.
func (e *UnsupportedSystemMessageError) Unwrap() error {
	return &providererrors.InvalidPromptError{Message: unsupportedSystemMessageText}
}

// ValidatePromptInput is the first part of TS standardizePrompt: a call must
// carry a prompt or messages, not both, and the messages must not be empty.
// (An empty prompt string counts as unset in Go.)
func ValidatePromptInput(prompt string, messages []types.Message) error {
	switch {
	case prompt != "" && len(messages) > 0:
		return &providererrors.InvalidPromptError{Prompt: prompt, Message: "prompt and messages cannot be defined at the same time"}
	case prompt == "" && messages == nil:
		return &providererrors.InvalidPromptError{Prompt: prompt, Message: "prompt or messages must be defined"}
	case prompt == "" && len(messages) == 0:
		return &providererrors.InvalidPromptError{Prompt: prompt, Message: "messages must not be empty"}
	}
	return nil
}

// NormalizePrompt applies the core prompt conversion rules before providers see
// the request: reject system messages by default, rewrite deprecated image parts
// to file parts, normalize file data shorthands, and enforce reasoning-file
// constraints.
//
// NormalizePrompt intentionally does NOT combine consecutive tool-role
// messages (see MergeConsecutiveToolMessages): pkg/ai and pkg/agent also call
// this (indirectly, via NormalizePromptWithDownloadSupport) to seed the
// running conversation history used across an entire tool loop -- e.g.
// resumeToolApprovals (pkg/ai/tool_approval_resume.go) scans that history
// message-by-message to find and re-validate tool-approval-response parts by
// their ORIGINAL message boundaries. TS's convertToLanguageModelPrompt (which
// does merge) is called only once per model call, right before building the
// LanguageModelV4Prompt handed to doGenerate -- never to prepare the
// ModelMessage-shaped history used for approval bookkeeping. Go's equivalent
// terminal point is NormalizePromptWithDownloadSupport, called fresh for each
// step immediately before assembling the provider.GenerateOptions passed to
// model.DoGenerate/DoStream; that is where the merge is applied.
func NormalizePrompt(prompt types.Prompt, allowSystemMessages bool) (types.Prompt, error) {
	messages, err := NormalizeMessages(prompt.Messages, allowSystemMessages)
	if err != nil {
		return types.Prompt{}, err
	}
	prompt.Messages = messages
	return prompt, nil
}

// URLSupportChecker reports whether a model supports passing the URL directly
// for the given media type.
type URLSupportChecker func(mediaType, url string) bool

// DownloadRequest describes a URL that the prompt conversion path is planning
// to hand to experimental_download.
type DownloadRequest struct {
	URL                   string
	IsURLSupportedByModel bool
}

// DownloadResult is the optional replacement data returned for a planned
// download. A nil result leaves the original URL untouched.
type DownloadResult struct {
	Data      []byte
	MediaType string
}

// DownloadFunction mirrors the TypeScript SDK experimental_download contract:
// one batch of planned URL downloads in prompt order, with nil results meaning
// pass-through.
type DownloadFunction func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error)

// URLDownloadFunction is the legacy single-URL download shape.
type URLDownloadFunction func(context.Context, string) ([]byte, error)

// AdaptURLDownload converts the legacy single-URL shape into the planned batch
// contract. Supported URLs are left untouched.
func AdaptURLDownload(download URLDownloadFunction) DownloadFunction {
	if download == nil {
		return nil
	}
	return func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
		results := make([]*DownloadResult, len(requests))
		for i, request := range requests {
			if request.IsURLSupportedByModel {
				continue
			}
			data, err := download(ctx, request.URL)
			if err != nil {
				return nil, err
			}
			results[i] = &DownloadResult{Data: data}
		}
		return results, nil
	}
}

// NormalizePromptWithDownloads applies the same prompt conversion rules as
// NormalizePrompt and downloads remote file URLs that the model cannot consume
// directly. This mirrors the TypeScript SDK's prompt conversion path for user
// files and tool-result file URLs before messages are sent to a model.
func NormalizePromptWithDownloads(ctx context.Context, prompt types.Prompt, allowSystemMessages bool, download URLDownloadFunction) (types.Prompt, error) {
	return NormalizePromptWithDownloadSupport(ctx, prompt, allowSystemMessages, AdaptURLDownload(download), nil)
}

// NormalizePromptWithDownloadSupport is NormalizePromptWithDownloads plus
// TypeScript-compatible URL support planning. Supported URLs are left as URLs;
// unsupported URLs are converted to inline data via download.
//
// This is the Go equivalent of TS's convertToLanguageModelPrompt: it is
// called fresh for every step, immediately before the resulting Prompt is
// handed to a provider's DoGenerate/DoStream (pkg/ai's per-step loop,
// pkg/agent's executeStep), so it is the right place -- and, in the standard
// pipeline, the ONLY place -- to combine consecutive tool-role messages
// (MergeConsecutiveToolMessages) before ANY provider sees them, matching TS
// where that combining happens once in the shared core for every provider.
func NormalizePromptWithDownloadSupport(ctx context.Context, prompt types.Prompt, allowSystemMessages bool, download DownloadFunction, isURLSupported URLSupportChecker) (types.Prompt, error) {
	normalized, err := NormalizePrompt(prompt, allowSystemMessages)
	if err != nil {
		return types.Prompt{}, err
	}
	messages := normalized.Messages
	if download != nil {
		if messages, err = DownloadUnsupportedFileURLs(ctx, normalized.Messages, download, isURLSupported); err != nil {
			return types.Prompt{}, err
		}
	}
	if messages, err = sniffImageMediaTypes(messages); err != nil {
		return types.Prompt{}, err
	}
	normalized.Messages = MergeConsecutiveToolMessages(removeEmptyTextParts(messages))
	return normalized, nil
}

// removeEmptyTextParts is TS convertToLanguageModelMessage's empty text
// filter (convert-to-language-model-prompt.ts): a user message loses every
// text part with empty text, an assistant message every empty text part
// without provider options (`part.providerOptions != null` keeps it, so a
// replayed Google thoughtSignature on an empty text part reaches the
// provider). Go messages always hold parts, so TS's string-content form,
// which is never filtered, has no counterpart. Messages are copied before
// changes.
func removeEmptyTextParts(messages []types.Message) []types.Message {
	var out []types.Message
	for i, message := range messages {
		if message.Role != types.RoleUser && message.Role != types.RoleAssistant {
			continue
		}
		drop := func(part types.ContentPart) bool {
			var text *types.TextContent
			switch p := part.(type) {
			case types.TextContent:
				text = &p
			case *types.TextContent:
				if p == nil {
					return false
				}
				text = p
			default:
				return false
			}
			if text.Text != "" {
				return false
			}
			return message.Role == types.RoleUser || text.ProviderOptions == nil
		}
		var kept []types.ContentPart
		changed := false
		for j, part := range message.Content {
			if drop(part) {
				if !changed {
					kept = append(make([]types.ContentPart, 0, len(message.Content)), message.Content[:j]...)
					changed = true
				}
				continue
			}
			if changed {
				kept = append(kept, part)
			}
		}
		if !changed {
			continue
		}
		if out == nil {
			out = append([]types.Message(nil), messages...)
		}
		out[i].Content = kept
	}
	if out == nil {
		return messages
	}
	return out
}

// sniffImageMediaTypes mirrors the tail of TS convertPartToLanguageModelPart:
// inline file data whose bytes carry an image signature gets that image media
// type, replacing whatever was declared (including the bare "image" default of
// a legacy image part). It applies to user file parts and tool-result file
// content, not to assistant file parts. Messages are copied before changes.
// Base64 string data (FileData.DataString) is sniffed too (TS detectMediaType
// takes a string); a string that is not base64 fails the conversion, as TS's
// decode throws.
func sniffImageMediaTypes(messages []types.Message) ([]types.Message, error) {
	var out []types.Message
	for i, message := range messages {
		if message.Role != types.RoleUser && message.Role != types.RoleTool && message.Role != types.RoleAssistant {
			continue
		}
		var parts []types.ContentPart
		for j, part := range message.Content {
			if !isDownloadablePart(message.Role, part) {
				continue
			}
			var replaced types.ContentPart
			var err error
			switch typed := part.(type) {
			case types.FileContent:
				var f types.FileContent
				var ok bool
				if f, ok, err = sniffFileContent(typed); ok {
					replaced = f
				}
			case *types.FileContent:
				if typed != nil {
					var f types.FileContent
					var ok bool
					if f, ok, err = sniffFileContent(*typed); ok {
						replaced = &f
					}
				}
			case types.ToolResultContent:
				var t types.ToolResultContent
				var ok bool
				if t, ok, err = sniffToolResult(typed); ok {
					replaced = t
				}
			case *types.ToolResultContent:
				if typed != nil {
					var t types.ToolResultContent
					var ok bool
					if t, ok, err = sniffToolResult(*typed); ok {
						replaced = &t
					}
				}
			}
			if err != nil {
				return nil, err
			}
			if replaced == nil {
				continue
			}
			if out == nil {
				out = append([]types.Message(nil), messages...)
			}
			if parts == nil {
				parts = append([]types.ContentPart(nil), message.Content...)
			}
			parts[j] = replaced
		}
		if parts != nil {
			out[i].Content = parts
		}
	}
	if out == nil {
		return messages, nil
	}
	return out, nil
}

// sniffImageType detects an image media type from inline data: the bytes, or
// the base64 string when there are none.
func sniffImageType(data types.FileData) (string, bool, error) {
	if len(data.Data) == 0 {
		if data.DataString == "" {
			return "", false, nil
		}
		return fileutil.DetectMediaTypeSignatureBase64(data.DataString, "image")
	}
	detected, ok := fileutil.DetectMediaTypeSignature(data.Data, "image")
	return detected, ok, nil
}

func sniffFileContent(f types.FileContent) (types.FileContent, bool, error) {
	if f.FileData.Type != types.FileDataTypeData {
		return f, false, nil
	}
	detected, ok, err := sniffImageType(f.FileData)
	if err != nil {
		return f, false, err
	}
	if !ok || (f.MediaType == detected && f.FileData.MediaType == detected) {
		return f, false, nil
	}
	f.MediaType = detected
	f.FileData.MediaType = detected
	if f.MimeType != "" {
		f.MimeType = detected
	}
	return f, true, nil
}

func sniffToolResult(t types.ToolResultContent) (types.ToolResultContent, bool, error) {
	if t.Output == nil || t.Output.Type != types.ToolResultOutputContent {
		return t, false, nil
	}
	var blocks []types.ToolResultContentBlock
	for i, block := range t.Output.Content {
		var b types.FileContentBlock
		var isPtr bool
		switch typed := block.(type) {
		case types.FileContentBlock:
			b = typed
		case *types.FileContentBlock:
			if typed == nil {
				continue
			}
			b, isPtr = *typed, true
		default:
			continue
		}
		if b.FileData.Type != types.FileDataTypeData {
			continue
		}
		detected, ok, err := sniffImageType(b.FileData)
		if err != nil {
			return t, false, err
		}
		if !ok || (b.MediaType == detected && b.FileData.MediaType == detected) {
			continue
		}
		b.MediaType = detected
		b.FileData.MediaType = detected
		if blocks == nil {
			blocks = append([]types.ToolResultContentBlock(nil), t.Output.Content...)
		}
		if isPtr {
			blocks[i] = &b
		} else {
			blocks[i] = b
		}
	}
	if blocks == nil {
		return t, false, nil
	}
	output := *t.Output
	output.Content = blocks
	t.Output = &output
	return t, true, nil
}

// ImageContentPartDeprecation is the TS convertToLanguageModelPrompt
// deprecation warning for an "image" part (types.ImageContent) of a user
// message (setting `"image" content part`, code
// AISDK_DEP_IMAGE_CONTENT_PART).
const ImageContentPartDeprecation = `The "image" content part type is deprecated. Use a "file" part with mediaType: 'image' (or a more specific image/* subtype) instead.`

// DeprecatedContentWarnings returns the deprecation warnings TS
// convertToLanguageModelPrompt logs (logWarnings, without provider or model)
// when it converts message: one per "image" part of a user message, and one
// per tool-result content item of a legacy type ("file-data", "file-url",
// "image-url", ...; see types.FileContentBlock.DeprecationWarning) in an
// assistant or tool message, in part order. Core functions log them once per
// message that has any.
func DeprecatedContentWarnings(message types.Message) []types.Warning {
	var warnings []types.Warning
	switch message.Role {
	case types.RoleUser:
		for _, part := range message.Content {
			switch typed := part.(type) {
			case types.ImageContent:
			case *types.ImageContent:
				if typed == nil {
					continue
				}
			default:
				continue
			}
			warnings = append(warnings, types.Warning{
				Type:    "deprecated",
				Setting: `"image" content part`,
				Message: ImageContentPartDeprecation,
			})
		}
	case types.RoleAssistant, types.RoleTool:
		for _, part := range message.Content {
			var output *types.ToolResultOutput
			switch typed := part.(type) {
			case types.ToolResultContent:
				output = typed.Output
			case *types.ToolResultContent:
				if typed != nil {
					output = typed.Output
				}
			}
			if output == nil || output.Type != types.ToolResultOutputContent {
				continue
			}
			for _, block := range output.Content {
				var warning types.Warning
				var ok bool
				switch typed := block.(type) {
				case types.FileContentBlock:
					warning, ok = typed.DeprecationWarning()
				case *types.FileContentBlock:
					if typed != nil {
						warning, ok = typed.DeprecationWarning()
					}
				}
				if ok {
					warnings = append(warnings, warning)
				}
			}
		}
	}
	return warnings
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

// DownloadToolResultFiles returns a copy of messages where unsupported
// tool-result file URL blocks have been converted to inline data blocks using
// download. It is kept for compatibility with earlier Go-AI callers.
func DownloadToolResultFiles(ctx context.Context, messages []types.Message, download func(context.Context, string) ([]byte, error)) ([]types.Message, error) {
	return DownloadUnsupportedFileURLs(ctx, messages, AdaptURLDownload(download), nil)
}

// DownloadUnsupportedFileURLs returns a copy of messages where unsupported user
// file URLs and tool-result file URLs have been converted to inline data blocks.
func DownloadUnsupportedFileURLs(ctx context.Context, messages []types.Message, download DownloadFunction, isURLSupported URLSupportChecker) ([]types.Message, error) {
	if len(messages) == 0 || download == nil {
		return messages, nil
	}
	requests := collectDownloadRequests(messages, isURLSupported)
	if len(requests) == 0 {
		return messages, nil
	}
	results, err := download(ctx, requests)
	if err != nil {
		return nil, err
	}
	if results == nil {
		return messages, nil
	}
	if len(results) != len(requests) {
		return nil, fmt.Errorf("download returned %d results for %d requests", len(results), len(requests))
	}
	// Keyed by the planned (normalized) URL, as TS keys downloaded assets by
	// url.toString().
	downloaded := make(map[string]*DownloadResult, len(results))
	for i, result := range results {
		if result != nil {
			downloaded[requests[i].URL] = result
		}
	}
	if len(downloaded) == 0 {
		return messages, nil
	}
	out := applyDownloadedFiles(messages, downloaded)
	return out, nil
}

func collectDownloadRequests(messages []types.Message, isURLSupported URLSupportChecker) []DownloadRequest {
	var requests []DownloadRequest
	for _, message := range messages {
		for _, part := range message.Content {
			if !isDownloadablePart(message.Role, part) {
				continue
			}
			collectContentPartDownloadRequests(part, isURLSupported, &requests)
		}
	}
	return requests
}

// isDownloadablePart mirrors TS downloadAssets: file parts are downloaded only
// from user messages, and tool-result file content only from tool and
// assistant messages. File parts inside assistant messages are passed through
// untouched.
func isDownloadablePart(role types.MessageRole, part types.ContentPart) bool {
	switch part.(type) {
	case types.FileContent, *types.FileContent:
		return role == types.RoleUser
	case types.ToolResultContent, *types.ToolResultContent:
		return role == types.RoleTool || role == types.RoleAssistant
	}
	return false
}

func collectContentPartDownloadRequests(part types.ContentPart, isURLSupported URLSupportChecker, requests *[]DownloadRequest) {
	switch typed := part.(type) {
	case types.FileContent:
		appendFileDownloadRequest(typed.FileData, typed.URL, firstNonEmpty(typed.MediaType, typed.MimeType), isURLSupported, requests)
	case *types.FileContent:
		if typed != nil {
			appendFileDownloadRequest(typed.FileData, typed.URL, firstNonEmpty(typed.MediaType, typed.MimeType), isURLSupported, requests)
		}
	case types.ToolResultContent:
		collectToolResultDownloadRequests(typed, isURLSupported, requests)
	case *types.ToolResultContent:
		if typed != nil {
			collectToolResultDownloadRequests(*typed, isURLSupported, requests)
		}
	}
}

func collectToolResultDownloadRequests(part types.ToolResultContent, isURLSupported URLSupportChecker, requests *[]DownloadRequest) {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputContent {
		return
	}
	for _, block := range part.Output.Content {
		switch typed := block.(type) {
		case types.FileContentBlock:
			appendFileDownloadRequest(typed.FileData, typed.URL, typed.MediaType, isURLSupported, requests)
		case *types.FileContentBlock:
			if typed != nil {
				appendFileDownloadRequest(typed.FileData, typed.URL, typed.MediaType, isURLSupported, requests)
			}
		}
	}
}

func appendFileDownloadRequest(fileData types.FileData, fallbackURL, fallbackMediaType string, isURLSupported URLSupportChecker, requests *[]DownloadRequest) {
	url := fallbackURL
	mediaType := fallbackMediaType
	if fileData.Type == types.FileDataTypeURL {
		url = fileData.URL
		mediaType = firstNonEmpty(mediaType, fileData.MediaType)
	}
	if url == "" {
		return
	}
	url = downloadKey(url)
	*requests = append(*requests, DownloadRequest{
		URL:                   url,
		IsURLSupportedByModel: mediaType != "" && isURLSupported != nil && isURLSupported(mediaType, url),
	})
}

func applyDownloadedFiles(messages []types.Message, downloaded map[string]*DownloadResult) []types.Message {
	out := make([]types.Message, len(messages))
	for i, message := range messages {
		converted := message
		if len(message.Content) > 0 {
			converted.Content = applyDownloadedContent(message.Role, message.Content, downloaded)
		}
		out[i] = converted
	}
	return out
}

func applyDownloadedContent(role types.MessageRole, parts []types.ContentPart, downloaded map[string]*DownloadResult) []types.ContentPart {
	out := make([]types.ContentPart, len(parts))
	for i, part := range parts {
		if !isDownloadablePart(role, part) {
			out[i] = part
			continue
		}
		switch typed := part.(type) {
		case types.FileContent:
			converted := applyDownloadedFileContent(typed, downloaded)
			out[i] = converted
		case *types.FileContent:
			if typed == nil {
				out[i] = part
				continue
			}
			converted := applyDownloadedFileContent(*typed, downloaded)
			out[i] = &converted
		case types.ToolResultContent:
			converted := applyDownloadedToolResultPart(typed, downloaded)
			out[i] = converted
		case *types.ToolResultContent:
			if typed == nil {
				out[i] = part
				continue
			}
			converted := applyDownloadedToolResultPart(*typed, downloaded)
			out[i] = &converted
		default:
			out[i] = part
		}
	}
	return out
}

func applyDownloadedToolResultPart(part types.ToolResultContent, downloaded map[string]*DownloadResult) types.ToolResultContent {
	if part.Output == nil || part.Output.Type != types.ToolResultOutputContent {
		return part
	}
	blocks := make([]types.ToolResultContentBlock, 0, len(part.Output.Content))
	for _, block := range part.Output.Content {
		converted := applyDownloadedToolResultBlock(block, downloaded)
		blocks = append(blocks, converted)
	}
	output := *part.Output
	output.Content = blocks
	part.Output = &output
	return part
}

func applyDownloadedToolResultBlock(block types.ToolResultContentBlock, downloaded map[string]*DownloadResult) types.ToolResultContentBlock {
	switch typed := block.(type) {
	case types.FileContentBlock:
		return applyDownloadedFileContentBlock(typed, downloaded)
	case *types.FileContentBlock:
		if typed == nil {
			return block
		}
		converted := applyDownloadedFileContentBlock(*typed, downloaded)
		return &converted
	default:
		return block
	}
}

func applyDownloadedFileContent(file types.FileContent, downloaded map[string]*DownloadResult) types.FileContent {
	url := file.URL
	mediaType := firstNonEmpty(file.MediaType, file.MimeType)
	if file.FileData.Type == types.FileDataTypeURL {
		url = file.FileData.URL
		mediaType = firstNonEmpty(mediaType, file.FileData.MediaType)
	}
	result := downloaded[downloadKey(url)]
	if result == nil {
		return file
	}
	if result.MediaType != "" && (mediaType == "" || !isFullMediaType(mediaType)) {
		mediaType = result.MediaType
	}
	file.FileData = types.FileData{Type: types.FileDataTypeData, Data: result.Data, MediaType: mediaType}
	file.Data = result.Data
	file.URL = ""
	file.Reference = ""
	file.Text = ""
	file.MediaType = mediaType
	file.MimeType = ""
	return file
}

func applyDownloadedFileContentBlock(block types.FileContentBlock, downloaded map[string]*DownloadResult) types.FileContentBlock {
	url := block.URL
	mediaType := block.MediaType
	if block.FileData.Type == types.FileDataTypeURL {
		url = block.FileData.URL
		mediaType = firstNonEmpty(mediaType, block.FileData.MediaType)
	}
	result := downloaded[downloadKey(url)]
	if result == nil {
		return block
	}
	if result.MediaType != "" && (mediaType == "" || !isFullMediaType(mediaType)) {
		mediaType = result.MediaType
	}
	block.FileData = types.FileData{Type: types.FileDataTypeData, Data: result.Data, MediaType: mediaType}
	block.Data = result.Data
	block.URL = ""
	block.Reference = ""
	block.Text = ""
	block.MediaType = mediaType
	return block
}

// downloadKey is the URL a download is planned and matched by: the WHATWG
// serialization (TS url.toString()), which NormalizeFileData has already
// applied on the standard path. A URL that does not parse is used as given.
func downloadKey(url string) string {
	if href, err := urlnorm.Href(url); err == nil {
		return href
	}
	return url
}

func isFullMediaType(mediaType string) bool {
	// TS isFullMediaType: a slash followed by a non-empty subtype other than
	// the "*" wildcard (the top-level type is not checked).
	_, subtype, ok := strings.Cut(mediaType, "/")
	return ok && subtype != "" && subtype != "*"
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
			if typed == nil {
				continue
			}
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
			if typed == nil {
				continue
			}
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
			if typed == nil {
				continue
			}
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
			if typed == nil {
				continue
			}
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
		// TS file parts carry a URL object and providers send
		// url.toString(): the WHATWG serialization of the URL. A URL that
		// does not parse is TS's `new URL(url)` TypeError.
		parsed, err := urlnorm.Parse(data.URL)
		if err != nil {
			return types.FileData{}, &InvalidURLError{URL: data.URL}
		}
		href := parsed.String()
		if parsed.Scheme == "data" {
			// TS: url.protocol === 'data:' -> splitDataUrl(url.toString()).
			return ParseDataURL(href)
		}
		data.URL = href
		if data.MediaType == "" {
			data.MediaType = fileutil.InferMediaTypeFromURL(href, "application/octet-stream")
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
	if part.FileData.IsZero() {
		normalized = keepOriginalURL(normalized, part.URL)
	}
	part.FileData = normalized
	if data.Type == types.FileDataTypeURL && normalized.Type == types.FileDataTypeData {
		// TS: mediaType = dataUrlMediaType ?? part.mediaType, so the media
		// type embedded in a data: URL wins over the declared one, even when
		// it is the empty string of `data:;base64,...`.
		part.MediaType = normalized.MediaType
		part.MimeType = normalized.MediaType
	} else {
		part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType, part.MimeType)
		part.MimeType = firstNonEmpty(part.MimeType, part.MediaType)
	}
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
		if ref := normalized.Reference[""]; ref != "" {
			part.Reference = ref
		}
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
		// The legacy URL string (TS `image: string`) is normalized like a
		// FileContent.URL string, keeping the original when it differs.
		file.URL = part.URL
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
	if block.FileData.IsZero() {
		normalized = keepOriginalURL(normalized, block.URL)
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
		if ref := normalized.Reference[""]; ref != "" {
			block.Reference = ref
		}
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
			if typed == nil {
				continue
			}
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
			if typed == nil {
				continue
			}
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
	if data.Type == types.FileDataTypeURL && normalized.Type == types.FileDataTypeData {
		// TS mediaType: mediaType ?? part.mediaType: a data: URL's media
		// type wins over the declared one.
		part.MediaType = normalized.MediaType
	} else {
		part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType)
	}
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
	// TS splitDataUrl: `data:;base64,...` has the empty media type "" (not
	// text/plain), and "" is not nullish so it wins over the declared one.
	mediaType := parts[0]
	if err := validateMediaType(mediaType); err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	if len(parts) == 1 || parts[len(parts)-1] != "base64" {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: fmt.Errorf("only base64 data URLs are supported")}
	}
	// TS keeps the text after the first comma and later decodes it with atob
	// after mapping the URL-safe alphabet (convertBase64ToUint8Array): the
	// forgiving decode accepts whitespace and missing padding.
	if i := strings.IndexByte(body, ','); i >= 0 {
		body = body[:i]
	}
	decoded, err := fileutil.ConvertBase64ToBytes(body)
	if err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	return types.FileData{Type: types.FileDataTypeData, Data: decoded, MediaType: mediaType}, nil
}

// keepOriginalURL mirrors TS convertUrlStringToFilePartData for a legacy URL
// string field (FileContent.URL, ImageContent.URL, FileContentBlock.URL, the
// Go form of a TS string URL): when the normalized URL differs from the
// string as given, the string is kept as OriginalURL. A tagged FileData URL
// (TS `{ type: 'url', url: URL }`) keeps only the OriginalURL its caller set.
func keepOriginalURL(normalized types.FileData, raw string) types.FileData {
	if normalized.Type == types.FileDataTypeURL && raw != "" && normalized.URL != raw {
		normalized.OriginalURL = raw
	}
	return normalized
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
