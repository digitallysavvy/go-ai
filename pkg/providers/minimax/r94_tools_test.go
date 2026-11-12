package minimax

import (
	"context"
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
