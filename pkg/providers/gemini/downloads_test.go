package gemini

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDownloadToolResultFiles_InlinesRemoteFileURL ports the intent of TS
// downloadToolResultFiles (google-download-tool-result-files.ts): Vertex
// function responses only accept inline data, so a tool-result content
// block referencing a remote http(s) URL is downloaded and replaced with
// inline bytes before the message is converted to the wire format.
func TestDownloadToolResultFiles_InlinesRemoteFileURL(t *testing.T) {
	var gotURL string
	var gotMaxBytes int64
	m := &LanguageModel{
		cfg: Config{
			ToolResultDownloadMaxBytes: 1234,
			ToolResultDownload: func(ctx context.Context, url string, maxBytes int64) ([]byte, string, error) {
				gotURL = url
				gotMaxBytes = maxBytes
				return []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}, "image/png", nil
			},
		},
	}

	messages := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "call-1",
					ToolName:   "get_image",
					Output: &types.ToolResultOutput{
						Type: types.ToolResultOutputContent,
						Content: []types.ToolResultContentBlock{
							types.FileContentBlock{
								URL:       "https://example.com/image.png",
								MediaType: "image/png",
							},
						},
					},
				},
			},
		},
	}

	out, err := m.downloadToolResultFiles(context.Background(), messages)
	if err != nil {
		t.Fatalf("downloadToolResultFiles: %v", err)
	}
	if gotURL != "https://example.com/image.png" {
		t.Fatalf("download called with url = %q", gotURL)
	}
	if gotMaxBytes != 1234 {
		t.Fatalf("download called with maxBytes = %d, want 1234", gotMaxBytes)
	}

	tr, ok := out[0].Content[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("expected ToolResultContent, got %T", out[0].Content[0])
	}
	fb, ok := tr.Output.Content[0].(types.FileContentBlock)
	if !ok {
		t.Fatalf("expected FileContentBlock, got %T", tr.Output.Content[0])
	}
	if fb.URL != "" {
		t.Errorf("expected URL cleared after download, got %q", fb.URL)
	}
	if len(fb.Data) == 0 {
		t.Error("expected Data to be populated with downloaded bytes")
	}
	if fb.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png (detected from PNG signature)", fb.MediaType)
	}

	// The original messages slice must not be mutated.
	origFb := messages[0].Content[0].(types.ToolResultContent).Output.Content[0].(types.FileContentBlock)
	if origFb.URL == "" {
		t.Error("original messages slice was mutated (URL cleared)")
	}
}

// TestDownloadToolResultFiles_NoOpWithoutRemoteURLs verifies that messages
// without any remote-URL tool-result file blocks pass through unchanged and
// the downloader is never invoked.
func TestDownloadToolResultFiles_NoOpWithoutRemoteURLs(t *testing.T) {
	called := false
	m := &LanguageModel{
		cfg: Config{
			ToolResultDownloadMaxBytes: DefaultToolResultDownloadMaxBytes,
			ToolResultDownload: func(ctx context.Context, url string, maxBytes int64) ([]byte, string, error) {
				called = true
				return nil, "", nil
			},
		},
	}

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	out, err := m.downloadToolResultFiles(context.Background(), messages)
	if err != nil {
		t.Fatalf("downloadToolResultFiles: %v", err)
	}
	if called {
		t.Error("downloader should not be called when there are no remote file URLs")
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
}
