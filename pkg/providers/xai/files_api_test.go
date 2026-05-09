package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var teamIDSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("team_id") == "team-123" {
			teamIDSeen = true
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "file-xyz789",
			"filename":   "data.csv",
			"bytes":      512,
			"created_at": 1700000000,
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"teamId": "team-123"},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if !teamIDSeen {
		t.Fatal("expected team_id field")
	}
	if res.ProviderReference["xai"] != "file-xyz789" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
}
