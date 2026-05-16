package prompt

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"strings"

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

// ReasoningFileConstraintError is returned when a reasoning-file part uses a
// FileData variant that cannot represent model-produced reasoning output.
type ReasoningFileConstraintError struct {
	Type types.FileDataType
}

func (e *ReasoningFileConstraintError) Error() string {
	return fmt.Sprintf("reasoning-file data type %q is not supported; allowed types are %q and %q", e.Type, types.FileDataTypeData, types.FileDataTypeURL)
}

// UnsupportedSystemMessageError is returned when a messages prompt contains a
// system-role message without the explicit AllowSystemMessages opt-in.
type UnsupportedSystemMessageError struct{}

func (e *UnsupportedSystemMessageError) Error() string {
	return "system messages are not allowed in the messages field; use the system option instead or set AllowSystemMessages"
}

// NormalizePrompt applies the core prompt conversion rules before providers see
// the request: reject system messages by default, rewrite deprecated image parts
// to file parts, normalize file data shorthands, and enforce reasoning-file
// constraints.
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
func NormalizePromptWithDownloadSupport(ctx context.Context, prompt types.Prompt, allowSystemMessages bool, download DownloadFunction, isURLSupported URLSupportChecker) (types.Prompt, error) {
	normalized, err := NormalizePrompt(prompt, allowSystemMessages)
	if err != nil {
		return types.Prompt{}, err
	}
	if download == nil {
		return normalized, nil
	}
	messages, err := DownloadUnsupportedFileURLs(ctx, normalized.Messages, download, isURLSupported)
	if err != nil {
		return types.Prompt{}, err
	}
	normalized.Messages = messages
	return normalized, nil
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
			collectContentPartDownloadRequests(part, isURLSupported, &requests)
		}
	}
	return requests
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
			converted.Content = applyDownloadedContent(message.Content, downloaded)
		}
		out[i] = converted
	}
	return out
}

func applyDownloadedContent(parts []types.ContentPart, downloaded map[string]*DownloadResult) []types.ContentPart {
	out := make([]types.ContentPart, len(parts))
	for i, part := range parts {
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
	result := downloaded[url]
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
	result := downloaded[url]
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

func isFullMediaType(mediaType string) bool {
	parts := strings.Split(mediaType, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
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
		if strings.HasPrefix(data.URL, "data:") {
			return ParseDataURL(data.URL)
		}
		if err := validateRemoteFileURL(data.URL); err != nil {
			return types.FileData{}, err
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
	part.FileData = normalized
	part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType, part.MimeType)
	part.MimeType = firstNonEmpty(part.MimeType, part.MediaType)
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
		file.FileData = types.FileData{Type: types.FileDataTypeURL, URL: part.URL, MediaType: mediaType}
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
	part.MediaType = firstNonEmpty(part.MediaType, normalized.MediaType)
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
	mediaType := parts[0]
	if mediaType == "" {
		mediaType = "text/plain"
	}
	if err := validateMediaType(mediaType); err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	if len(parts) == 1 || parts[len(parts)-1] != "base64" {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: fmt.Errorf("only base64 data URLs are supported")}
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return types.FileData{}, &InvalidInlineDataURLError{Value: value, Cause: err}
	}
	return types.FileData{Type: types.FileDataTypeData, Data: decoded, MediaType: mediaType}, nil
}

func validateRemoteFileURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("remote file URL must use http or https scheme")
	}
	if parsed.Host == "" {
		return fmt.Errorf("remote file URL must include a host")
	}
	return nil
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
