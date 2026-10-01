package core

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockFilesAPI struct {
	last types.UploadFileOptions
	res  *types.UploadFileResult
	err  error
}

func (m *mockFilesAPI) UploadFile(_ context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	m.last = opts
	return m.res, m.err
}

func TestUploadFile_DetectMediaType(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"mock": "file_1"}, Warnings: []types.Warning{}}}
	_, err := UploadFile(context.Background(), api, UploadFileOptions{
		Data: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if api.last.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q", api.last.MediaType)
	}
}

func TestUploadFile_TextFallback(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"mock": "file_1"}, Warnings: []types.Warning{}}}
	_, err := UploadFile(context.Background(), api, UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeText, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if api.last.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q, want text/plain", api.last.MediaType)
	}
}

func TestUploadFile_Base64StringTextDetection(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: types.ProviderReference{"mock": "file_1"}, Warnings: []types.Warning{}}}
	_, err := UploadFile(context.Background(), api, UploadFileOptions{
		Data: "dGVzdA==",
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if api.last.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q, want text/plain", api.last.MediaType)
	}
}

func TestUploadFile_InvalidBase64StringWithoutMediaTypeFails(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: types.ProviderReference{"mock": "file_1"}, Warnings: []types.Warning{}}}
	_, err := UploadFile(context.Background(), api, UploadFileOptions{
		Data: "not base64!!!",
	})
	if err == nil {
		t.Fatal("expected invalid base64 error")
	}
}

func TestUploadFile_InvalidBase64StringWithMediaTypePassesThrough(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: types.ProviderReference{"mock": "file_1"}, Warnings: []types.Warning{}}}
	_, err := UploadFile(context.Background(), api, UploadFileOptions{
		Data:      "not base64!!!",
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if api.last.Data.DataString != "not base64!!!" {
		t.Fatalf("DataString = %q", api.last.Data.DataString)
	}
}

func TestUploadFile_MissingAPI(t *testing.T) {
	_, err := UploadFile(context.Background(), struct{}{}, UploadFileOptions{Data: []byte{1}})
	if !errors.Is(err, ErrFilesAPINotSupported) {
		t.Fatalf("err = %v", err)
	}
}

type mockFilesProvider struct {
	files provider.FilesAPI
}

func (m mockFilesProvider) Files() provider.FilesAPI { return m.files }

func TestResolveFilesAPIAndTypeDetectionHelpers(t *testing.T) {
	api := &mockFilesAPI{res: &types.UploadFileResult{ProviderReference: types.ProviderReference{"mock": "file_1"}}}
	resolved, err := resolveFilesAPI(mockFilesProvider{files: api})
	if err != nil {
		t.Fatalf("resolveFilesAPI(FilesProvider) error = %v", err)
	}
	if resolved == nil {
		t.Fatal("resolveFilesAPI(FilesProvider) should return FilesAPI")
	}
	if _, err := resolveFilesAPI(mockFilesProvider{files: nil}); !errors.Is(err, ErrFilesAPINotSupported) {
		t.Fatalf("resolveFilesAPI(nil Files()) error = %v", err)
	}

	if got := detectUploadMediaType([]byte("plain text")); got != "text/plain" {
		t.Fatalf("detectUploadMediaType(text) = %q", got)
	}
	if got := detectUploadMediaType([]byte{0x00, 0xFF, 0x10, 0x00}); got != "application/octet-stream" {
		t.Fatalf("detectUploadMediaType(binary) = %q", got)
	}
	if !isLikelyText([]byte("hello\tworld\n")) {
		t.Fatal("isLikelyText(printable) = false, want true")
	}
	if isLikelyText([]byte{0x00, 0x01}) {
		t.Fatal("isLikelyText(binary) = true, want false")
	}
}
