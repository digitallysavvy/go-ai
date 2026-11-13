package deepinfra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS fileToBlob: `file.type === 'url'` always goes through downloadBlob, so
// an empty URL fails the download (DownloadError) instead of uploading the
// file's empty data.
func TestR93toolsDeepInfraEditEmptyURLIsDownloaded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQID"}]}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	n := 1
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n, Files: []provider.ImageFile{{Type: "url", URL: ""}},
	})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) {
		t.Fatalf("err = %T %v, want a DownloadError", err, err)
	}
}
