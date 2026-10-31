package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockUploadFilesAPI struct {
	last types.UploadFileOptions
	res  *types.UploadFileResult
	err  error
}

func (m *mockUploadFilesAPI) UploadFile(_ context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	m.last = opts
	return m.res, m.err
}

type mockUploadFilesProvider struct {
	api provider.FilesAPI
}

func (m mockUploadFilesProvider) Files() provider.FilesAPI {
	return m.api
}

func TestUploadFile_RootAPIUsesProviderFiles(t *testing.T) {
	filesAPI := &mockUploadFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"openai": "file_1"}}}
	provider := mockUploadFilesProvider{api: filesAPI}

	_, err := UploadFile(context.Background(), UploadFileOptions{
		API:      provider,
		Data:     []byte("hello"),
		Filename: "hello.txt",
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}

	if filesAPI.last.Filename != "hello.txt" {
		t.Fatalf("Filename = %q", filesAPI.last.Filename)
	}
	if filesAPI.last.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q, want text/plain", filesAPI.last.MediaType)
	}
	if got := string(filesAPI.last.Data.Data); got != "hello" {
		t.Fatalf("Data = %q", got)
	}
}

func TestUploadFile_StringShorthandMatchesTypeScriptDataString(t *testing.T) {
	api := &mockUploadFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"openai": "file_1"}}}

	_, err := UploadFile(context.Background(), UploadFileOptions{
		API:       api,
		Data:      "aGVsbG8=",
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}

	if api.last.Data.Type != types.FileDataTypeData {
		t.Fatalf("Data.Type = %q", api.last.Data.Type)
	}
	if api.last.Data.DataString != "aGVsbG8=" {
		t.Fatalf("DataString = %q", api.last.Data.DataString)
	}
	if len(api.last.Data.Data) != 0 {
		t.Fatalf("Data bytes should be empty for string shorthand")
	}
}

func TestUploadFile_InvalidBase64StringWithoutMediaTypeFails(t *testing.T) {
	api := &mockUploadFilesAPI{res: &types.UploadFileResult{ProviderReference: map[string]string{"openai": "file_1"}}}

	_, err := UploadFile(context.Background(), UploadFileOptions{
		API:  api,
		Data: "not base64!!!",
	})
	if err == nil {
		t.Fatal("expected invalid base64 error")
	}
}

func TestUploadFile_UnsupportedAPI(t *testing.T) {
	_, err := UploadFile(context.Background(), UploadFileOptions{API: struct{}{}, Data: []byte{1}})
	if !errors.Is(err, ErrFilesAPINotSupported) {
		t.Fatalf("err = %v", err)
	}
}

// TS detectMediaType only knows magic numbers: HTML stays text/plain (net/http
// sniffing would have reported text/html; charset=utf-8).
func TestUploadFile_HTMLIsTextPlain(t *testing.T) {
	filesAPI := &mockUploadFilesAPI{res: &types.UploadFileResult{}}
	_, err := UploadFile(context.Background(), UploadFileOptions{API: filesAPI, Data: []byte("<!DOCTYPE html><html></html>")})
	if err != nil {
		t.Fatal(err)
	}
	if filesAPI.last.MediaType != "text/plain" {
		t.Fatalf("MediaType = %q", filesAPI.last.MediaType)
	}
}

type closeTracker struct {
	*strings.Reader
	closed bool
}

func (c *closeTracker) Close() error { c.closed = true; return nil }

func TestUploadFile_FailedUploadClosesStream(t *testing.T) {
	stream := &closeTracker{Reader: strings.NewReader("x")}
	filesAPI := &mockUploadFilesAPI{err: errors.New("boom")}
	_, err := UploadFile(context.Background(), UploadFileOptions{
		API:  filesAPI,
		Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream},
	})
	if err == nil || !stream.closed {
		t.Fatalf("err=%v closed=%v", err, stream.closed)
	}
}
