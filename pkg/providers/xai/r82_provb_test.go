package xai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS XaiFiles: toFileMetadata keeps present empty strings, the filename is
// `response.filename ?? filename` when truthy, and the looseObject option
// schema rejects a null teamId / expiresAfter / filePath and a non-string
// filePath.
func TestR82provbXAIFilesMetadataAndOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"f","filename":"","purpose":"","status":""}`))
	}))
	defer srv.Close()
	files := &FilesAPI{provider: New(Config{APIKey: "k", BaseURL: srv.URL})}
	res, err := files.UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}, Filename: "a.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["xai"].(map[string]interface{})
	for _, k := range []string{"filename", "purpose", "status"} {
		if v, ok := meta[k]; !ok || v != "" {
			t.Fatalf("metadata %s = %v (present %v), want a present empty string", k, v, ok)
		}
	}
	if res.Filename != "" {
		t.Fatalf("filename = %q, want none", res.Filename)
	}
	for _, o := range []map[string]interface{}{{"teamId": nil}, {"expiresAfter": nil}, {"filePath": nil}, {"filePath": 5}} {
		if _, err := files.UploadFile(context.Background(), types.UploadFileOptions{
			Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}, ProviderOptions: map[string]interface{}{"xai": o},
		}); err == nil {
			t.Fatalf("options %v accepted; the TS schema rejects them", o)
		}
	}
}
