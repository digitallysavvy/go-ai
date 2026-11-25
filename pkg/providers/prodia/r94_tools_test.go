package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const r94toolsImg2Vid = "inference.wan2-2.lightning.img2vid.v0"

// TS ProdiaVideoModel resolves config.headers (loadApiKey) before it
// downloads the input image: a missing key is the LoadAPIKeyError, even
// when the image URL cannot be downloaded.
func TestR94toolsProdiaImg2VidKeyResolvedBeforeImageDownload(t *testing.T) {
	t.Setenv("PRODIA_TOKEN", "")
	vm, _ := New(Config{BaseURL: "http://127.0.0.1:1"}).VideoModel(r94toolsImg2Vid)
	_, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "p", Image: &provider.VideoModelV3File{Type: "url", URL: "https://127.0.0.1:1/in.png"},
	})
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %T %v, want a LoadAPIKeyError", err, err)
	}
}

// The key is resolved once per img2vid call, as in TS, and sent.
func TestR94toolsProdiaImg2VidAPIKeyFuncCalledOnce(t *testing.T) {
	var calls atomic.Int32
	var auth atomic.Value
	jobJSON := `{"id":"job-1","state":{"current":"completed"}}`
	body, ct := buildTestMultipartBody(jobJSON, []byte("vid"), "video/mp4")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	vm, _ := New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) {
		calls.Add(1)
		return "fn-key", nil
	}}).VideoModel(r94toolsImg2Vid)
	res, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "p", Image: &provider.VideoModelV3File{Type: "file", Data: []byte("img"), MediaType: "image/png"},
	})
	if err != nil || len(res.Videos) != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("APIKeyFunc calls = %d, want 1", got)
	}
	if got := auth.Load(); got != "Bearer fn-key" {
		t.Fatalf("Authorization = %v", got)
	}
}
