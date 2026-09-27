package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockFullFilesAPI implements FilesAPI plus all three optional v4
// capabilities (FileMetadataGetter, FileDownloader, FileDeleter).
type mockFullFilesAPI struct {
	mockUploadFilesAPI

	metaOpts provider.FileMetadataOptions
	metaRes  *provider.FileMetadataResult
	metaErr  error

	downloadOpts provider.DownloadFileOptions
	downloadRes  *provider.DownloadFileResult
	downloadErr  error

	deleteOpts provider.DeleteFileOptions
	deleteRes  *provider.DeleteFileResult
	deleteErr  error
}

func (m *mockFullFilesAPI) GetFileMetadata(_ context.Context, opts provider.FileMetadataOptions) (*provider.FileMetadataResult, error) {
	m.metaOpts = opts
	return m.metaRes, m.metaErr
}

func (m *mockFullFilesAPI) DownloadFile(_ context.Context, opts provider.DownloadFileOptions) (*provider.DownloadFileResult, error) {
	m.downloadOpts = opts
	return m.downloadRes, m.downloadErr
}

func (m *mockFullFilesAPI) DeleteFile(_ context.Context, opts provider.DeleteFileOptions) (*provider.DeleteFileResult, error) {
	m.deleteOpts = opts
	return m.deleteRes, m.deleteErr
}

func TestGetFileMetadata_ForwardsToProvider(t *testing.T) {
	byteSize := int64(42)
	api := &mockFullFilesAPI{metaRes: &provider.FileMetadataResult{
		ProviderReference: types.ProviderReference{"openai": "file_1"},
		ByteSize:          &byteSize,
	}}

	res, err := GetFileMetadata(context.Background(), GetFileMetadataOptions{
		API:     api,
		File:    types.ProviderReference{"openai": "file_1"},
		Headers: map[string]string{"X-Test": "1"},
	})
	if err != nil {
		t.Fatalf("GetFileMetadata() error = %v", err)
	}
	if res.ByteSize == nil || *res.ByteSize != 42 {
		t.Fatalf("ByteSize = %v, want 42", res.ByteSize)
	}
	if api.metaOpts.Headers["X-Test"] != "1" {
		t.Fatalf("Headers not forwarded: %+v", api.metaOpts.Headers)
	}
}

func TestGetFileMetadata_UnsupportedWhenProviderLacksCapability(t *testing.T) {
	api := &mockUploadFilesAPI{}

	_, err := GetFileMetadata(context.Background(), GetFileMetadataOptions{API: api})
	if !errors.Is(err, ErrFileMetadataNotSupported) {
		t.Fatalf("err = %v, want ErrFileMetadataNotSupported", err)
	}
}

func TestDownloadFile_ForwardsToProviderAndStreamsContent(t *testing.T) {
	content := io.NopCloser(strings.NewReader("file bytes"))
	api := &mockFullFilesAPI{downloadRes: &provider.DownloadFileResult{
		Content:   content,
		MediaType: "text/plain",
	}}

	res, err := DownloadFile(context.Background(), DownloadFileOptions{
		API:  api,
		File: types.ProviderReference{"openai": "file_1"},
	})
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	defer res.Content.Close()
	data, err := io.ReadAll(res.Content)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "file bytes" {
		t.Fatalf("content = %q", string(data))
	}
	if res.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q", res.MediaType)
	}
}

func TestDownloadFile_UnsupportedWhenProviderLacksCapability(t *testing.T) {
	api := &mockUploadFilesAPI{}

	_, err := DownloadFile(context.Background(), DownloadFileOptions{API: api})
	if !errors.Is(err, ErrFileDownloadNotSupported) {
		t.Fatalf("err = %v, want ErrFileDownloadNotSupported", err)
	}
}

func TestDeleteFile_ForwardsToProvider(t *testing.T) {
	api := &mockFullFilesAPI{deleteRes: &provider.DeleteFileResult{
		ProviderReference: types.ProviderReference{"openai": "file_1"},
		Deleted:           true,
	}}

	res, err := DeleteFile(context.Background(), DeleteFileOptions{
		API:  api,
		File: types.ProviderReference{"openai": "file_1"},
	})
	if err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if !res.Deleted {
		t.Fatalf("Deleted = false, want true")
	}
	if api.deleteOpts.File["openai"] != "file_1" {
		t.Fatalf("File not forwarded: %+v", api.deleteOpts.File)
	}
}

func TestDeleteFile_UnsupportedWhenProviderLacksCapability(t *testing.T) {
	api := &mockUploadFilesAPI{}

	_, err := DeleteFile(context.Background(), DeleteFileOptions{API: api})
	if !errors.Is(err, ErrFileDeleteNotSupported) {
		t.Fatalf("err = %v, want ErrFileDeleteNotSupported", err)
	}
}

func TestUploadFile_ForwardsHeaders(t *testing.T) {
	api := &mockUploadFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"openai": "file_1"}}}

	_, err := UploadFile(context.Background(), UploadFileOptions{
		API:     api,
		Data:    []byte("hello"),
		Headers: map[string]string{"X-Test": "1"},
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if api.last.Headers["X-Test"] != "1" {
		t.Fatalf("Headers not forwarded: %+v", api.last.Headers)
	}
}

// TestFilesV4Results_JSONTagsAreCamelCase locks in camelCase JSON field
// names for the Files v4 result types, matching TypeScript's
// FilesV4GetFileMetadataResult/FilesV4DeleteFileResult field names and the
// sibling types.UploadFileResult convention already established in
// pkg/provider/types/upload.go.
func TestFilesV4Results_JSONTagsAreCamelCase(t *testing.T) {
	byteSize := int64(5)
	metaJSON, err := json.Marshal(provider.FileMetadataResult{
		ProviderReference: types.ProviderReference{"openai": "file_1"},
		Filename:          "a.txt",
		MediaType:         "text/plain",
		ByteSize:          &byteSize,
	})
	if err != nil {
		t.Fatalf("Marshal(FileMetadataResult) error = %v", err)
	}
	for _, key := range []string{`"providerReference"`, `"filename"`, `"mediaType"`, `"byteSize"`} {
		if !strings.Contains(string(metaJSON), key) {
			t.Errorf("FileMetadataResult JSON = %s, want %s", metaJSON, key)
		}
	}

	deleteJSON, err := json.Marshal(provider.DeleteFileResult{
		ProviderReference: types.ProviderReference{"openai": "file_1"},
		Deleted:           true,
	})
	if err != nil {
		t.Fatalf("Marshal(DeleteFileResult) error = %v", err)
	}
	for _, key := range []string{`"providerReference"`, `"deleted"`} {
		if !strings.Contains(string(deleteJSON), key) {
			t.Errorf("DeleteFileResult JSON = %s, want %s", deleteJSON, key)
		}
	}

	downloadJSON, err := json.Marshal(provider.DownloadFileResult{MediaType: "text/plain"})
	if err != nil {
		t.Fatalf("Marshal(DownloadFileResult) error = %v", err)
	}
	if !strings.Contains(string(downloadJSON), `"mediaType"`) {
		t.Errorf("DownloadFileResult JSON = %s, want mediaType", downloadJSON)
	}
	if strings.Contains(string(downloadJSON), "Content") {
		t.Errorf("DownloadFileResult JSON = %s, want Content excluded", downloadJSON)
	}
}
