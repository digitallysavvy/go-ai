package core

import (
	"context"
	"errors"
	"testing"

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

func TestUploadFile_MissingAPI(t *testing.T) {
	_, err := UploadFile(context.Background(), struct{}{}, UploadFileOptions{Data: []byte{1}})
	if !errors.Is(err, ErrFilesAPINotSupported) {
		t.Fatalf("err = %v", err)
	}
}
