package openai

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var gotPurpose string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !strings.HasPrefix(mt, "multipart/form-data") {
			t.Fatalf("content-type = %s", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "purpose" {
				b, _ := io.ReadAll(part)
				gotPurpose = string(b)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "file-123", "filename": "test.csv"})
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
	if gotPurpose != "assistants" {
		t.Fatalf("purpose = %q", gotPurpose)
	}
	if res.ProviderReference["openai"] != "file-123" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
}
