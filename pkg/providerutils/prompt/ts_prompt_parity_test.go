package prompt

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var pngBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}

func TestNormalizeSniffsImageMediaTypeFromBytes(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.ImageContent{Image: pngBytes},
		types.FileContent{Data: pngBytes, MediaType: "application/octet-stream"},
	}}}}
	out, err := NormalizePromptWithDownloadSupport(context.Background(), prompt, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range out.Messages[0].Content {
		f := p.(types.FileContent)
		if f.MediaType != "image/png" || f.FileData.MediaType != "image/png" {
			t.Fatalf("part %d media type = %q/%q", i, f.MediaType, f.FileData.MediaType)
		}
	}
}

func TestNormalizeDataURLMediaTypeWins(t *testing.T) {
	f, err := NormalizeFileContent(types.FileContent{
		MediaType: "application/octet-stream",
		FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "data:text/plain;base64,aGk="},
	})
	if err != nil || f.MediaType != "text/plain" {
		t.Fatalf("mediaType=%q err=%v", f.MediaType, err)
	}
}

func TestDownloadsSkipAssistantFileParts(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.FileContent{URL: "https://example.com/a.png", MediaType: "image/png"}}},
	}}
	var seen []string
	_, err := NormalizePromptWithDownloadSupport(context.Background(), prompt, false, func(_ context.Context, reqs []DownloadRequest) ([]*DownloadResult, error) {
		for _, r := range reqs {
			seen = append(seen, r.URL)
		}
		return make([]*DownloadResult, len(reqs)), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("assistant file URLs were planned for download: %v", seen)
	}
}
