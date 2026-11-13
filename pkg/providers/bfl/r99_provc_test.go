package bfl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func r99provcSwapTransport(t *testing.T, rt roundTripFunc) {
	oldDefault := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	oldDownload := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = oldDownload })
}

// TS getFromApi: the video status poll carries the runtime tag after the
// provider headers' agent (trusted host) or alone (other host), not
// download()'s ai-sdk tag.
func TestR99provcBFLVideoPollUserAgent(t *testing.T) {
	var got []string
	r99provcSwapTransport(t, func(r *http.Request) (*http.Response, error) {
		got = append(got, r.Header.Get("User-Agent"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"status":"Pending"}`)), Request: r}, nil
	})
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: "https://api.bfl.ai"}), "flux-3-video")
	headers := map[string]string{"user-agent": "app/1 ai-sdk-black-forest-labs/9"}
	for _, u := range []string{"https://api.bfl.ai/v1/get_result?id=1", "https://polls.example.com/r?id=1"} {
		if _, _, err := m.fetchStatusJSON(context.Background(), u, headers); err != nil {
			t.Fatal(err)
		}
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if len(got) != 2 || got[0] != "app/1 ai-sdk-black-forest-labs/9 "+rt || got[1] != rt {
		t.Fatalf("user agents = %q", got)
	}
}

// TS createBinaryResponseHandler: a 204 image download has a null body.
func TestR99provcBFLImageDownloadNoContent(t *testing.T) {
	r99provcSwapTransport(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Header: http.Header{"X-A": []string{"b"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "https://api.bfl.ai"}), "flux-pro")
	_, _, err := m.downloadImage(context.Background(), "https://delivery-us1.bfl.ai/img.png", nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 204 || pe.ResponseHeaders["x-a"] != "b" {
		t.Fatalf("err = %#v", err)
	}
}
