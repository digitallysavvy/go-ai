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

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcBFLBodiesOutOfRangeUndeclared(t *testing.T) {
	for name, check := range map[string]func([]byte) error{
		"image submit": validateBFLImageSubmitBody,
		"video submit": validateBFLSubmitBody,
	} {
		if err := check([]byte(`{"id":"t1","polling_url":"https://api.bfl.ai/v1/get_result?id=t1","queue":1e999}`)); err != nil {
			t.Errorf("%s: want valid, got %v", name, err)
		}
	}
	if err := validateBFLPollBody([]byte(`{"status":"Pending","progress":1e999}`)); err != nil {
		t.Errorf("image poll: want valid, got %v", err)
	}
	if err := validateBFLVideoPollBody([]byte(`{"status":"Pending","progress":1e999}`)); err != nil {
		t.Errorf("video poll: want valid, got %v", err)
	}
}

// secureJsonParse rejects a __proto__ key before the schema runs: the
// validator fails with the JSONParseError (the caller's "Invalid JSON
// response" cause) instead of passing it to the typed decode.
func TestR98provcBFLBodiesPrototypeKey(t *testing.T) {
	body := []byte(`{"id":"t1","polling_url":"https://api.bfl.ai/v1/get_result?id=t1","__proto__":{"x":1}}`)
	for name, check := range map[string]func([]byte) error{
		"image submit": validateBFLImageSubmitBody,
		"video submit": validateBFLSubmitBody,
		"image poll":   validateBFLPollBody,
		"video poll":   validateBFLVideoPollBody,
	} {
		err := check(body)
		var pe *providererrors.JSONParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: want JSONParseError, got %v", name, err)
		}
	}
}

// TS createJsonErrorResponseHandler / createStatusCodeErrorResponseHandler: a
// failed body that does not match bflErrorSchema (and every failed image
// download) gives response.statusText, the server's reason phrase.
func TestR98provcBFLReasonPhrase(t *testing.T) {
	dl := &providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`[1]`)}
	err := NewVideoModel(New(Config{APIKey: "k"}), "m").handleVideoPollError(context.Background(), dl)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
		t.Errorf("video poll: got %T %v", err, err)
	}

	// Not parallel: swaps the process-global http.DefaultTransport.
	oldDefault := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 418,
			Status:     "418 Custom Phrase",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[1]`)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	oldDownload := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = oldDownload })

	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "https://api.bfl.ai"}), "flux-pro")
	_, err = m.getPollBody(context.Background(), "https://api.bfl.ai/v1/get_result?id=1", nil)
	if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
		t.Errorf("image poll: got %T %v", err, err)
	}
	_, _, err = m.downloadImage(context.Background(), "https://delivery-us1.bfl.ai/img.png", nil)
	if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
		t.Errorf("image download: got %T %v", err, err)
	}
}

// TS getFromApi appends getRuntimeEnvironmentUserAgent() to the poll and
// image download user agent (cr-r98-provg-to-provc).
func TestR98provcBFLRawRequestsCarryRuntimeUserAgent(t *testing.T) {
	var got []string
	oldDefault := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = append(got, r.Header.Get("User-Agent"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"status":"Pending"}`)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldDefault })
	oldDownload := downloadTransport
	downloadTransport = func() http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(func() { downloadTransport = oldDownload })

	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "https://api.bfl.ai"}), "flux-pro")
	ua := map[string]string{"user-agent": "app/1 ai-sdk-black-forest-labs/9"}
	if _, err := m.getPollBody(context.Background(), "https://api.bfl.ai/v1/get_result?id=1", ua); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.downloadImage(context.Background(), "https://delivery-us1.bfl.ai/img.png", ua); err != nil {
		t.Fatal(err)
	}
	want := "app/1 ai-sdk-black-forest-labs/9 " + providerutils.RuntimeEnvironmentUserAgent()
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("user agents = %q, want %q", got, want)
	}
}
