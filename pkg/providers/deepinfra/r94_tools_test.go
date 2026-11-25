package deepinfra

import (
	"context"
	"errors"
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

// TS resolves config.headers (loadApiKey) before fileToBlob: a missing key
// is the LoadAPIKeyError even when an edit file cannot be downloaded, and
// an apiKey function runs once per edit call.
func TestR94toolsDeepInfraEditKeyResolvedBeforeDownload(t *testing.T) {
	t.Setenv("DEEPINFRA_API_KEY", "")
	model, _ := New(Config{BaseURL: "http://127.0.0.1:1/openai"}).ImageModel("stabilityai/sd3.5")
	n := 1
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n, Files: []provider.ImageFile{{Type: "url", URL: "https://127.0.0.1:1/x.png"}},
	})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %T %v, want a LoadAPIKeyError", err, err)
	}
}

func TestR94toolsDeepInfraEditAPIKeyFuncCalledOnce(t *testing.T) {
	var calls atomic.Int32
	var auth atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"AQID"}]}`))
	}))
	defer server.Close()
	model, _ := New(Config{BaseURL: server.URL + "/openai", APIKeyFunc: func(context.Context) (string, error) {
		calls.Add(1)
		return "fn-key", nil
	}}).ImageModel("stabilityai/sd3.5")
	n := 1
	res, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat", N: &n, Files: []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte{1}}},
	})
	if err != nil || len(res.Images) != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("APIKeyFunc calls = %d, want 1", got)
	}
	if got := auth.Load(); got != "Bearer fn-key" {
		t.Fatalf("Authorization = %v", got)
	}
}

// TS postJsonToApi / postFormDataToApi fail on !response.ok: an unfollowed
// 3xx is the DeepInfra API error, not a success body.
func TestR94toolsDeepInfraNon2xxIsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"images":["AQID"],"data":[{"b64_json":"AQID"}]}`))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"}).ImageModel("stabilityai/sd3.5")
	n := 1
	for name, files := range map[string][]provider.ImageFile{
		"generate": nil,
		"edit":     {{Type: "file", MediaType: "image/png", Data: []byte{1}}},
	} {
		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat", N: &n, Files: files})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
			t.Fatalf("%s: err = %T %v, want the status 300 API error", name, err, err)
		}
	}
}
