package gemini

import (
	"bytes"
	"context"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	return res.Data, res.ContentType, nil
}

// downloadToolResultFiles ports TS downloadToolResultFiles: Vertex function
// responses only accept inline file data, so remote file URLs inside
// tool-result content outputs (tool messages and assistant provider-executed
// results) are downloaded before prompt conversion.
func (m *LanguageModel) downloadToolResultFiles(ctx context.Context, messages []types.Message) ([]types.Message, error) {
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
			newBlocks, blockChanged, err := downloadContentBlocks(ctx, tr.Output.Content, download, maxBytes)
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

func downloadContentBlocks(ctx context.Context, blocks []types.ToolResultContentBlock, download func(context.Context, string, int64) ([]byte, string, error), maxBytes int64) ([]types.ToolResultContentBlock, bool, error) {
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
		if strings.HasPrefix(strings.ToLower(url), "data:") {
			// Inline data URL: valid, but nothing to download over the
			// network — leave the block as-is (mirrors TS, where a data: URL
			// is always accepted by isUrlSupported/the provider directly and
			// so never reaches downloadBlob in practice).
			continue
		}
		data, contentType, err := download(ctx, url, maxBytes)
		if err != nil {
			return nil, false, err
		}
		mediaType := detectImageMediaType(data)
		if mediaType == "" {
			mediaType = fb.MediaType
			if contentType != "" && !isFullMediaType(fb.MediaType) {
				mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
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
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF")):
		return "image/gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	case bytes.HasPrefix(data, []byte{0x49, 0x49, 0x2A, 0x00}), bytes.HasPrefix(data, []byte{0x4D, 0x4D, 0x00, 0x2A}):
		return "image/tiff"
	case len(data) >= 12 && bytes.Equal(data[4:12], []byte("ftypavif")):
		return "image/avif"
	case len(data) >= 12 && (bytes.Equal(data[4:12], []byte("ftypheic")) || bytes.Equal(data[4:12], []byte("ftypheix"))):
		return "image/heic"
	}
	return ""
}
