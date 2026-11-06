package gemini

import (
	"context"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultToolResultDownloadMaxBytes is the Vertex default cap for downloading
// remote tool-result files (TS toolResultDownloads.maxBytes default, 7 MiB).
const DefaultToolResultDownloadMaxBytes = 7 * 1024 * 1024

func defaultToolResultDownload(ctx context.Context, url string, maxBytes int64) ([]byte, string, error) {
	opts := fileutil.DefaultDownloadOptions()
	opts.MaxSize = maxBytes
	res, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}
	contentType := res.ContentType
	if contentType == "" && strings.HasPrefix(strings.ToLower(url), "data:") {
		// fetch() reports the data: URL default media type.
		contentType = "text/plain;charset=US-ASCII"
	}
	return res.Data, contentType, nil
}

// downloadToolResultFiles ports TS downloadToolResultFiles: Vertex function
// responses only accept inline file data, so remote file URLs inside
// tool-result content outputs (tool messages and assistant provider-executed
// results) are downloaded before prompt conversion.
func (m *LanguageModel) downloadToolResultFiles(ctx context.Context, messages []types.Message, supportedURLs map[string][]*regexp.Regexp) ([]types.Message, error) {
	download := m.cfg.ToolResultDownload
	if download == nil {
		download = defaultToolResultDownload
	}
	maxBytes := m.cfg.ToolResultDownloadMaxBytes

	out := make([]types.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if msg.Role != types.RoleAssistant && msg.Role != types.RoleTool {
			continue
		}
		var content []types.ContentPart
		changed := false
		for j, part := range msg.Content {
			var tr types.ToolResultContent
			switch p := part.(type) {
			case types.ToolResultContent:
				tr = p
			case *types.ToolResultContent:
				if p == nil {
					continue
				}
				tr = *p
			default:
				continue
			}
			if tr.Output == nil || tr.Output.Type != types.ToolResultOutputContent {
				continue
			}
			newBlocks, blockChanged, err := downloadContentBlocks(ctx, tr.Output.Content, download, maxBytes, supportedURLs)
			if err != nil {
				return nil, err
			}
			if !blockChanged {
				continue
			}
			if !changed {
				content = append([]types.ContentPart(nil), msg.Content...)
				changed = true
			}
			output := *tr.Output
			output.Content = newBlocks
			tr.Output = &output
			content[j] = tr
		}
		if changed {
			out[i].Content = content
		}
	}
	return out, nil
}

func downloadContentBlocks(ctx context.Context, blocks []types.ToolResultContentBlock, download func(context.Context, string, int64) ([]byte, string, error), maxBytes int64, supportedURLs map[string][]*regexp.Regexp) ([]types.ToolResultContentBlock, bool, error) {
	var out []types.ToolResultContentBlock
	changed := false
	for i, block := range blocks {
		var fb types.FileContentBlock
		switch b := block.(type) {
		case types.FileContentBlock:
			fb = b
		case *types.FileContentBlock:
			if b == nil {
				continue
			}
			fb = *b
		default:
			continue
		}
		url := fb.URL
		if url == "" {
			url = fb.FileData.URL
		}
		if url == "" {
			// No remote URL on this block at all (e.g. already-inline data) —
			// nothing to validate or download.
			continue
		}
		if providerutils.MatchesSupportedURL(supportedURLs, fb.MediaType, url) {
			// Natively supported by the model (e.g. gs:// on Vertex Gemini
			// 3+): forwarded as-is, never downloaded (TS isUrlSupported
			// check in downloadToolResultOutput).
			continue
		}
		// Validate the URL scheme before deciding whether to download it,
		// matching TS's downloadToolResultFiles: any file URL not already
		// natively supported by the model is passed to downloadBlob, which
		// validates it via fetchUntrustedUrl/validateDownloadUrl and throws
		// DownloadError("URL scheme must be http, https, or data, got
		// <scheme>") for anything other than http/https/data
		// (packages/provider-utils/src/validate-download-url.ts). The
		// previous Go code silently skipped (continued past) any URL whose
		// scheme wasn't http(s) instead of surfacing that error — including
		// genuinely unsupported schemes like ftp:// or file://, which then
		// silently reached the provider as an unusable reference instead of
		// failing the request.
		if err := fileutil.ValidateDownloadURL(url); err != nil {
			return nil, false, err
		}
		// A data: URL is not "supported" by isUrlSupported (the supported
		// patterns are gs:// only), so TS sends it through downloadBlob
		// (fetch decodes it) and inlines the bytes, for base64 and
		// non-base64 data URLs and for every Vertex model generation.
		data, contentType, err := download(ctx, url, maxBytes)
		if err != nil {
			return nil, false, err
		}
		mediaType := detectImageMediaType(data)
		if mediaType == "" {
			mediaType = fb.MediaType
			if blob := blobMediaType(contentType); blob != "" && !isFullMediaType(fb.MediaType) {
				mediaType = blob
			}
		}
		if !changed {
			out = append([]types.ToolResultContentBlock(nil), blocks...)
			changed = true
		}
		fb.URL = ""
		fb.FileData = types.FileData{}
		fb.Data = data
		fb.MediaType = mediaType
		out[i] = fb
	}
	if !changed {
		return blocks, false, nil
	}
	return out, true, nil
}

func isFullMediaType(mediaType string) bool {
	parts := strings.Split(mediaType, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && parts[1] != "*"
}

// detectImageMediaType mirrors TS detectMediaType({topLevelType:'image'}):
// returns "" when no known image signature matches.
func detectImageMediaType(data []byte) string {
	mediaType, _ := fileutil.DetectMediaTypeSignature(data, "image")
	return mediaType
}

// blobMediaType mirrors the Blob constructor's `type` normalization used by
// TS downloadBlob: the Content-Type is lowercased and kept whole (parameters
// included), and a value with characters outside U+0020-U+007E becomes "".
func blobMediaType(contentType string) string {
	for i := 0; i < len(contentType); i++ {
		if contentType[i] < 0x20 || contentType[i] > 0x7E {
			return ""
		}
	}
	return strings.ToLower(contentType)
}
