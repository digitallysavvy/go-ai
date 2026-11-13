package bfl

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS sends {'user-agent': combinedHeaders['user-agent']} to a delivery host,
// so the combined provider/call user agent (the caller's own agent plus the
// SDK suffix) travels, not the bare SDK suffix.
func TestR46_DeliveryDownloadSendsCombinedUserAgent(t *testing.T) {
	// Not parallel: swaps the process-global http.DefaultTransport.
	var got http.Header
	oldDefault := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Clone()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader("image")),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	oldDownload := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = oldDownload })

	m := NewImageModel(New(Config{APIKey: "secret", BaseURL: "https://api.bfl.ai"}), "flux-pro")
	_, _, err := m.downloadImage(context.Background(), "https://delivery-us1.bfl.ai/img.png", map[string]string{
		"user-agent": "my-app/1 ai-sdk/black-forest-labs/9",
	})
	if err != nil {
		t.Fatal(err)
	}
	// TS getFromApi then appends the runtime tag (r98).
	if got.Get("User-Agent") != "my-app/1 ai-sdk/black-forest-labs/9 "+providerutils.RuntimeEnvironmentUserAgent() {
		t.Fatalf("user agent = %q", got.Get("User-Agent"))
	}
	if got.Get("X-Key") != "" {
		t.Fatal("credentials must not reach a delivery host")
	}
}
