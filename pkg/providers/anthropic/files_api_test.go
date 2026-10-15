package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("anthropic-beta")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "file-abc123",
			"filename":   "test.pdf",
			"mime_type":  "application/pdf",
			"size_bytes": 12345,
			"created_at": "2025-04-14T12:00:00Z",
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}},
		MediaType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if beta != BetaHeaderFilesAPI {
		t.Fatalf("anthropic-beta = %q", beta)
	}
	if res.ProviderReference["anthropic"] != "file-abc123" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
}

// TS appends a Blob typed with the media type, so the multipart part carries
// that Content-Type, and the call's headers reach the request.
func TestFilesAPI_UploadFile_PartContentTypeAndHeaders(t *testing.T) {
	var partType, custom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		custom = r.Header.Get("X-Custom")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		if fhs := r.MultipartForm.File["file"]; len(fhs) == 1 {
			partType = fhs[0].Header.Get("Content-Type")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "f1", "filename": "a.txt", "mime_type": "text/plain", "size_bytes": 1, "created_at": "x"})
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeText, Text: "hello"},
		MediaType: "text/plain",
		Filename:  "a.txt",
		Headers:   map[string]string{"X-Custom": "yes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if partType != "text/plain" {
		t.Fatalf("part Content-Type = %q, want text/plain", partType)
	}
	if custom != "yes" {
		t.Fatalf("X-Custom = %q", custom)
	}
}

func TestFilesAPI_UploadFile_ErrorIsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeText, Text: "x"},
	})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 429 || perr.Message != "slow down" || perr.ErrorCode != "rate_limit_error" || !perr.IsRetryable() {
		t.Fatalf("err = %#v", err)
	}
}
