package heygen

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR94provgEmptyStatusIsInvalidResponseData: TS reads data.status with
// z.string(), so "" passes the schema and reaches the
// InvalidResponseDataError of an unexpected status. Go rejected it as an
// "Invalid JSON response" ProviderError.
func TestR94provgEmptyStatusIsInvalidResponseData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":""}}`))
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "heygen-video-1")
	_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %T %v, want InvalidResponseDataError", err, err)
	}
}

// r94provgZodURLCases is zod 4.6.7 z.url().startsWith('https://').safeParse
// run in Node 24 (scratch zurl.mjs): input, success, parsed value.
var r94provgZodURLCases = []struct {
	in    string
	ok    bool
	value string
}{
	{"https://x.com/a", true, "https://x.com/a"},
	{" https://x.com/a", true, "https://x.com/a"},
	{"https://x.com/a ", true, "https://x.com/a"},
	{"https://x.com/a\n", true, "https://x.com/a"},
	{"https://exa\nmple.com/a", true, "https://example.com/a"},
	{"https:///foo", true, "https:///foo"},
	{"https://", false, ""},
	{"https://x.com:99999", false, ""},
	{"HTTPS://x.com", false, ""},
	{"https://user:pw@x.com", true, "https://user:pw@x.com"},
	{"https://[::1]/", true, "https://[::1]/"},
	{"https://x com/", false, ""},
	{"https://%zz/", false, ""},
	{"https://a.b/é", true, "https://a.b/é"},
	{"https:x.com", false, ""},
	{"\thttps://x.com", true, "https://x.com"},
	{"https://x.com/\t", true, "https://x.com/"},
	{"https:// x.com", false, ""},
	{"https://x.com ", true, "https://x.com"},
}

// TestR94provgOptionURLIsZodURL: heygenVideoAssetSchema url is
// z.url().startsWith('https://'). zod accepts what the WHATWG parser accepts
// after a JS trim, and parseProviderOptions returns the trimmed value
// without tabs and newlines, which TS then sends. Go used net/url, required
// a host, and kept the raw value.
func TestR94provgOptionURLIsZodURL(t *testing.T) {
	for _, c := range r94provgZodURLCases {
		opts, err := parseVideoModelOptions(map[string]interface{}{"heygen": map[string]interface{}{
			"image": map[string]interface{}{"type": "url", "url": c.in},
		}})
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.in, err, c.ok)
			continue
		}
		if c.ok && opts.Image.URL != c.value {
			t.Errorf("%q: url = %q, want %q", c.in, opts.Image.URL, c.value)
		}
		// assetBody (the standard input path) checks the same schema and
		// sends asset.url as given.
		body, err := assetBody(VideoAsset{Type: AssetTypeURL, URL: c.in}, "image")
		if (err == nil) != c.ok {
			t.Errorf("assetBody %q: err = %v, want ok %v", c.in, err, c.ok)
		} else if c.ok && body["url"] != c.in {
			t.Errorf("assetBody %q: url = %v, want the input", c.in, body["url"])
		}
	}
}

// TestR94provgTypedOptionsNotMutated: the parsed url value must not be
// written into a caller's typed options.
func TestR94provgTypedOptionsNotMutated(t *testing.T) {
	typed := &VideoModelOptions{
		Image:           &VideoAsset{Type: AssetTypeURL, URL: " https://x.com/i"},
		ReferenceImages: []VideoAsset{{Type: AssetTypeURL, URL: "https://x.com/r\n"}},
	}
	opts, err := parseVideoModelOptions(map[string]interface{}{"heygen": typed})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Image.URL != "https://x.com/i" || opts.ReferenceImages[0].URL != "https://x.com/r" {
		t.Fatalf("parsed = %q %q", opts.Image.URL, opts.ReferenceImages[0].URL)
	}
	if typed.Image.URL != " https://x.com/i" || typed.ReferenceImages[0].URL != "https://x.com/r\n" {
		t.Fatalf("caller options mutated: %q %q", typed.Image.URL, typed.ReferenceImages[0].URL)
	}
}

// doStart (postJsonToApi) and doStatus (getFromApi) run
// heygenFailedResponseHandler for any non-2xx: an unfollowed 3xx is the
// HeyGen API error, not a start result or a DownloadError.
func TestR94provgNon2xxIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"data":{"video_id":"v","status":"pending"},"error":{"message":"moved"}}`))
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "heygen-video-1")
	_, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 {
		t.Errorf("start: err = %T %v, want the 300 API error", err, err)
	}
	_, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	var dl *providererrors.DownloadError
	if !errors.As(err, &pe) || pe.StatusCode != 300 || errors.As(err, &dl) {
		t.Errorf("status: err = %T %v, want the 300 API error", err, err)
	}
}
