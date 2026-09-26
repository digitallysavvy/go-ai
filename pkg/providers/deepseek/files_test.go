package deepseek

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestDeepSeekFilesUploadFile(t *testing.T) {
	var seenPurpose, seenFilename string
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/files" {
			t.Fatalf("path = %s, want /files", r.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Fatalf("content-type = %s", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("multipart read error: %v", err)
			}
			switch part.FormName() {
			case "purpose":
				b, _ := io.ReadAll(part)
				seenPurpose = string(b)
			case "file":
				seenFilename = part.FileName()
			}
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"file-abc123",
				"object":"file",
				"bytes":3,
				"created_at":1700000000,
				"filename":"pic.png",
				"purpose":"user_data"
			}`)),
		}, nil
	})

	files := &FilesAPI{provider: p}
	result, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType: "image/png",
		Filename:  "pic.png",
	})
	if err != nil {
		t.Fatalf("UploadFile error = %v", err)
	}
	if seenPurpose != "user_data" {
		t.Errorf("purpose field = %q, want user_data", seenPurpose)
	}
	if seenFilename != "pic.png" {
		t.Errorf("filename field = %q, want pic.png", seenFilename)
	}
	if result.ProviderReference["deepseek"] != "file-abc123" {
		t.Errorf("ProviderReference = %#v", result.ProviderReference)
	}
	meta, ok := result.ProviderMetadata["deepseek"].(map[string]interface{})
	if !ok {
		t.Fatalf("ProviderMetadata missing: %#v", result.ProviderMetadata)
	}
	if meta["object"] != "file" {
		t.Errorf("object = %v, want file", meta["object"])
	}
	if meta["purpose"] != "user_data" {
		t.Errorf("purpose = %v", meta["purpose"])
	}
	if meta["bytes"] != int64(3) {
		t.Errorf("bytes = %v, want 3", meta["bytes"])
	}
	if meta["createdAt"] != int64(1700000000) {
		t.Errorf("createdAt = %v", meta["createdAt"])
	}
}

func TestDeepSeekFilesUploadFileUnexpectedObjectDiscriminator(t *testing.T) {
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"id":"file-1","object":"not-a-file"}`)),
		}, nil
	})
	files := &FilesAPI{provider: p}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType: "image/png",
	})
	if err == nil {
		t.Fatal("expected error for unexpected object discriminator")
	}
}

func TestDeepSeekFilesUploadFileTooLarge(t *testing.T) {
	p := New(Config{APIKey: "k"})
	files := &FilesAPI{provider: p}
	big := make([]byte, maxDeepSeekFileSizeBytes+1)
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: big},
		MediaType: "image/png",
	})
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestDeepSeekFilesUploadFileFilenameTooLong(t *testing.T) {
	p := New(Config{APIKey: "k"})
	files := &FilesAPI{provider: p}
	longName := strings.Repeat("a", maxDeepSeekFilenameLength+1) + ".png"
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}},
		MediaType: "image/png",
		Filename:  longName,
	})
	if err == nil {
		t.Fatal("expected error for oversized filename")
	}
}

func TestDeepSeekFilesUploadFileUnsupportedMediaType(t *testing.T) {
	p := New(Config{APIKey: "k"})
	files := &FilesAPI{provider: p}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte("not an image, just plain bytes of text data")},
		MediaType: "application/pdf",
	})
	if err == nil {
		t.Fatal("expected error for unsupported media type")
	}
}

func TestDeepSeekFilesUploadFileGenericMediaTypeWithSupportedExtension(t *testing.T) {
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"id":"file-2","object":"file"}`)),
		}, nil
	})
	files := &FilesAPI{provider: p}
	// application/octet-stream is generic; a supported filename extension
	// should still allow the upload even though the bytes don't sniff as a
	// known image signature.
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0, 0, 0, 0}},
		MediaType: "application/octet-stream",
		Filename:  "photo.png",
	})
	if err != nil {
		t.Fatalf("UploadFile error = %v", err)
	}
}

func TestDeepSeekFilesUploadFileExpiresAfter(t *testing.T) {
	var seenAnchor, seenSeconds string
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		_ = mediaType
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			switch part.FormName() {
			case "expires_after[anchor]":
				b, _ := io.ReadAll(part)
				seenAnchor = string(b)
			case "expires_after[seconds]":
				b, _ := io.ReadAll(part)
				seenSeconds = string(b)
			}
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"id":"file-3","object":"file"}`)),
		}, nil
	})
	files := &FilesAPI{provider: p}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType:       "image/png",
		ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"expiresAfter": 7200}},
	})
	if err != nil {
		t.Fatalf("UploadFile error = %v", err)
	}
	if seenAnchor != "created_at" {
		t.Errorf("expires_after[anchor] = %q, want created_at", seenAnchor)
	}
	if seenSeconds != "7200" {
		t.Errorf("expires_after[seconds] = %q, want 7200", seenSeconds)
	}
}

func TestDeepSeekFilesUploadFileExpiresAfterOutOfRange(t *testing.T) {
	p := New(Config{APIKey: "k"})
	files := &FilesAPI{provider: p}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType:       "image/png",
		ProviderOptions: map[string]interface{}{"deepseek": map[string]interface{}{"expiresAfter": 60}},
	})
	if err == nil {
		t.Fatal("expected error for expiresAfter below 1 hour")
	}
}

func TestDeepSeekProviderFilesReturnsFilesAPI(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if _, ok := p.Files().(*FilesAPI); !ok {
		t.Fatalf("Files() = %T, want *FilesAPI", p.Files())
	}
}
