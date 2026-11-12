package deepinfra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS fileToBlob runs (for every file, then the mask) before
// postFormDataToApi: a failed URL download is the DownloadError itself, not
// a retryable "Cannot connect to API" error, and no edit request is sent.
func TestR94toolsDeepInfraEditDownloadFailsBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []provider.ImageFile
		mask  *provider.ImageFile
	}{
		{"file", []provider.ImageFile{{Type: "url", URL: "https://127.0.0.1:1/x.png"}}, nil},
		{"mask", []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte{1}}}, &provider.ImageFile{Type: "url", URL: "https://127.0.0.1:1/m.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQID"}]}`))
			}))
			defer server.Close()
			model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"}).ImageModel("stabilityai/sd3.5")
			n := 1
			_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
				Prompt: "a cat", N: &n, Files: tc.files, Mask: tc.mask,
			})
			if _, ok := err.(*providererrors.DownloadError); !ok {
				t.Fatalf("err = %T %v, want *DownloadError", err, err)
			}
			if got := hits.Load(); got != 0 {
				t.Fatalf("edit requests = %d, want 0", got)
			}
		})
	}
}
