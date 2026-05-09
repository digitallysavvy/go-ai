package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
