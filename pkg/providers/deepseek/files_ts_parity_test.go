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

func TestDeepSeekFiles_TooLargeMessageUsesGroupedNumbers(t *testing.T) {
	files := &FilesAPI{provider: New(Config{APIKey: "k"})}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: make([]byte, maxDeepSeekFileSizeBytes+1)},
		MediaType: "image/png",
	})
	if err == nil || !strings.Contains(err.Error(), "(67,108,864 bytes). Received 67,108,865 bytes.") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeepSeekFiles_FilePartCarriesMediaType(t *testing.T) {
	var partType string
	p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FormName() == "file" {
				partType = part.Header.Get("Content-Type")
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"f1","object":"file"}`))}, nil
	})
	files := &FilesAPI{provider: p}
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		MediaType: "image/png",
		Filename:  "a.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if partType != "image/png" {
		t.Fatalf("part content-type = %q, want image/png", partType)
	}
}
