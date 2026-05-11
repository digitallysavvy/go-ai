package prompt

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNormalizeFileContentData(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{
		Data:      []byte("hello"),
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("NormalizeFileContent() error = %v", err)
	}
	if got.FileData.Type != types.FileDataTypeData {
		t.Fatalf("FileData.Type = %q, want %q", got.FileData.Type, types.FileDataTypeData)
	}
	if string(got.FileData.Data) != "hello" {
		t.Fatalf("FileData.Data = %q, want hello", string(got.FileData.Data))
	}
	if got.FileData.MediaType != "text/plain" {
		t.Fatalf("FileData.MediaType = %q, want text/plain", got.FileData.MediaType)
	}
}

func TestNormalizeFileContentURL(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{
		URL:       "https://example.com/file.pdf",
		MediaType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("NormalizeFileContent() error = %v", err)
	}
	if got.FileData.Type != types.FileDataTypeURL {
		t.Fatalf("FileData.Type = %q, want %q", got.FileData.Type, types.FileDataTypeURL)
	}
	if got.FileData.URL != "https://example.com/file.pdf" {
		t.Fatalf("FileData.URL = %q", got.FileData.URL)
	}
}

func TestNormalizeFileContentDataURL(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("hello"))
	got, err := NormalizeFileContent(types.FileContent{
		FileData: types.FileData{
			Type: types.FileDataTypeURL,
			URL:  "data:text/plain;base64," + body,
		},
	})
	if err != nil {
		t.Fatalf("NormalizeFileContent() error = %v", err)
	}
	if got.FileData.Type != types.FileDataTypeData {
		t.Fatalf("FileData.Type = %q, want %q", got.FileData.Type, types.FileDataTypeData)
	}
	if string(got.FileData.Data) != "hello" {
		t.Fatalf("FileData.Data = %q, want hello", string(got.FileData.Data))
	}
	if got.FileData.MediaType != "text/plain" {
		t.Fatalf("FileData.MediaType = %q, want text/plain", got.FileData.MediaType)
	}
}

func TestNormalizeFileContentReferenceAndText(t *testing.T) {
	ref, err := NormalizeFileContent(types.FileContent{Reference: "file-123", MediaType: "application/pdf"})
	if err != nil {
		t.Fatalf("NormalizeFileContent(reference) error = %v", err)
	}
	if ref.FileData.Type != types.FileDataTypeReference || types.ProviderReferenceString(ref.FileData.Reference) != "file-123" {
		t.Fatalf("reference FileData = %+v", ref.FileData)
	}

	text, err := NormalizeFileContent(types.FileContent{Text: "inline", MediaType: "text/plain"})
	if err != nil {
		t.Fatalf("NormalizeFileContent(text) error = %v", err)
	}
	if text.FileData.Type != types.FileDataTypeText || text.FileData.Text != "inline" {
		t.Fatalf("text FileData = %+v", text.FileData)
	}
}

func TestNormalizeFileContentPreservesProviderReferenceMap(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{
		MediaType: "application/pdf",
		FileData: types.FileData{
			Type: types.FileDataTypeReference,
			Reference: types.ProviderReference{
				"openai": "file-openai",
				"xai":    "file-xai",
			},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeFileContent() error = %v", err)
	}
	if got.Reference != "" {
		t.Fatalf("legacy Reference = %q, want empty so provider-specific resolution is not lost", got.Reference)
	}
	if got.FileData.Reference["openai"] != "file-openai" || got.FileData.Reference["xai"] != "file-xai" {
		t.Fatalf("FileData.Reference not preserved: %#v", got.FileData.Reference)
	}
}

func TestNormalizeImageContentShim(t *testing.T) {
	got, err := NormalizeImageContent(types.ImageContent{
		Image:    []byte{1, 2, 3},
		MimeType: "image/png",
	})
	if err != nil {
		t.Fatalf("NormalizeImageContent() error = %v", err)
	}
	if got.FileData.Type != types.FileDataTypeData {
		t.Fatalf("FileData.Type = %q, want data", got.FileData.Type)
	}
	if got.MediaType != "image/png" {
		t.Fatalf("MediaType = %q, want image/png", got.MediaType)
	}
}

func TestNormalizeReasoningFileContentRejectsReferenceAndText(t *testing.T) {
	for _, dataType := range []types.FileDataType{types.FileDataTypeReference, types.FileDataTypeText} {
		_, err := NormalizeReasoningFileContent(types.ReasoningFileContent{
			FileData: types.FileData{Type: dataType, Reference: map[string]string{"openai": "file-123"}, Text: "inline"},
		})
		var constraintErr *ReasoningFileConstraintError
		if !errors.As(err, &constraintErr) {
			t.Fatalf("NormalizeReasoningFileContent(%q) error = %T, want ReasoningFileConstraintError", dataType, err)
		}
		if constraintErr.Type != dataType {
			t.Fatalf("constraint type = %q, want %q", constraintErr.Type, dataType)
		}
	}
}

func TestNormalizeFileContentMalformedDataURL(t *testing.T) {
	_, err := NormalizeFileContent(types.FileContent{
		FileData: types.FileData{Type: types.FileDataTypeURL, URL: "data:text/plain;base64,not-base64!!!"},
	})
	var invalid *InvalidInlineDataURLError
	if !errors.As(err, &invalid) {
		t.Fatalf("NormalizeFileContent() error = %T, want InvalidInlineDataURLError", err)
	}
}

func TestNormalizeFileContentRejectsNonHTTPRemoteURL(t *testing.T) {
	_, err := NormalizeFileContent(types.FileContent{
		FileData: types.FileData{Type: types.FileDataTypeURL, URL: "file:///tmp/document.pdf"},
	})
	if err == nil {
		t.Fatal("NormalizeFileContent() error = nil, want non-http URL rejection")
	}
}

func TestNormalizeToolResultFileContentUsesFileData(t *testing.T) {
	got, err := NormalizeToolResultFileContent(types.FileContentBlock{
		URL:       "https://example.com/tool-output.pdf",
		MediaType: "application/pdf",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"detail": "high"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeToolResultFileContent() error = %v", err)
	}
	if got.FileData.Type != types.FileDataTypeURL {
		t.Fatalf("FileData.Type = %q, want url", got.FileData.Type)
	}
	if got.ProviderOptions["openai"] == nil {
		t.Fatal("ProviderOptions were not preserved")
	}
}

func TestNormalizePromptRejectsSystemMessagesByDefault(t *testing.T) {
	_, err := NormalizePrompt(types.Prompt{
		Messages: []types.Message{
			{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "be concise"}},
			},
		},
	}, false)
	var unsupported *UnsupportedSystemMessageError
	if !errors.As(err, &unsupported) {
		t.Fatalf("NormalizePrompt() error = %T, want UnsupportedSystemMessageError", err)
	}
}

func TestNormalizePromptAllowsSystemMessagesWithOptIn(t *testing.T) {
	got, err := NormalizePrompt(types.Prompt{
		Messages: []types.Message{
			{
				Role:    types.RoleSystem,
				Content: []types.ContentPart{types.TextContent{Text: "be concise"}},
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("NormalizePrompt() error = %v", err)
	}
	if got.Messages[0].Role != types.RoleSystem {
		t.Fatalf("Role = %q, want system", got.Messages[0].Role)
	}
}

func TestNormalizePromptConvertsImageAndToolImageToFile(t *testing.T) {
	got, err := NormalizePrompt(types.Prompt{
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png"},
					types.ToolResultContent{
						ToolCallID: "call-1",
						ToolName:   "render",
						Output: &types.ToolResultOutput{
							Type: types.ToolResultOutputContent,
							Content: []types.ToolResultContentBlock{
								types.ImageContentBlock{Data: []byte{4, 5}, MediaType: "image/jpeg"},
							},
						},
					},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("NormalizePrompt() error = %v", err)
	}
	file, ok := got.Messages[0].Content[0].(types.FileContent)
	if !ok {
		t.Fatalf("first content type = %T, want FileContent", got.Messages[0].Content[0])
	}
	if file.FileData.Type != types.FileDataTypeData || file.MediaType != "image/png" {
		t.Fatalf("normalized image file = %+v", file)
	}
	toolResult := got.Messages[0].Content[1].(types.ToolResultContent)
	if _, ok := toolResult.Output.Content[0].(types.FileContentBlock); !ok {
		t.Fatalf("tool result content type = %T, want FileContentBlock", toolResult.Output.Content[0])
	}
}
