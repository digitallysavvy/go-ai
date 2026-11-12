package minimax

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS Promise.all over the downloads: an image without a URL rejects before
// any download settles, so it wins over a failed download of an earlier
// image.
func TestR94toolsMiniMaxImageMissingURLWinsOverDownloadFailure(t *testing.T) {
	srv, _, _ := r91toolsImageServer(t, func() (int, string) {
		return 200, `{"data":[{"url":"IMAGE_URL.missing"},{"size":"1x1"}]}`
	}, nil)
	m, _ := New(Config{APIKey: "k", ImageBaseURL: srv.URL + "/v1"}).Image(ModelH3Image)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: r91toolsN(2)})
	if err == nil || err.Error() != "MiniMax returned an image without a URL." {
		t.Fatalf("err = %v", err)
	}
}

// TS postJsonToApi fails on !response.ok: an unfollowed 3xx from the video
// create call is the MiniMax API error, not a success body.
func TestR94toolsMiniMaxVideoCreateNon2xxIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"task_id":"t"}`))
	}))
	defer srv.Close()
	vm, _ := New(Config{APIKey: "k", VideoBaseURL: srv.URL}).VideoModel(ModelH3)
	_, err := vm.(*VideoModel).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	var mmErr *Error
	if !errors.As(err, &mmErr) || mmErr.StatusCode != http.StatusMultipleChoices {
		t.Fatalf("err = %T %v, want the status 300 API error", err, err)
	}
}
