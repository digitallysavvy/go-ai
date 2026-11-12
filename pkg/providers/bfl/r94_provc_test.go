package bfl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS getFromApi checks response.ok: an unfollowed 3xx on the image poll
// fails with the Black Forest Labs API error.
func TestR94provcImagePollNon2xxFails(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"t1","polling_url":"` + srv.URL + `/poll"}`))
			return
		}
		if r.URL.Path == "/img.png" {
			_, _ = w.Write([]byte("\x89PNG"))
			return
		}
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + srv.URL + `/img.png"}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, PollIntervalMillis: 1})
	res, err := NewImageModel(p, "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if pe := (*providererrors.ProviderError)(nil); !errors.As(err, &pe) || pe.StatusCode != 300 {
		t.Fatalf("err = %v (result %+v), want the 300 API error", err, res)
	}
}

// TS createJsonResponseHandler and bflFailedResponseHandler read the body
// as text (TextDecoder drops a UTF-8 BOM).
func TestR94provcResponseBodyIsText(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/flux-pro":
			_, _ = w.Write([]byte(bom + `{"id":"t1","polling_url":"` + srv.URL + `/poll"}`))
		case r.URL.Path == "/flux-3-video":
			_, _ = w.Write([]byte(bom + `{"id":"t1","polling_url":"` + srv.URL + `/poll"}`))
		case r.URL.Path == "/poll":
			_, _ = w.Write([]byte(`{"status":"Error"}`))
		default:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(bom + `{"detail":"bad thing"}`))
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, PollIntervalMillis: 1})
	_, err := NewImageModel(p, "flux-pro").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err == nil || err.Error() != "Black Forest Labs generation failed." {
		t.Errorf("image: err = %v, want the generation failure (the submit parsed)", err)
	}
	if _, err := NewVideoModel(p, "flux-3-video").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1}}); err != nil {
		t.Errorf("video start: %v", err)
	}
	_, err = NewImageModel(p, "flux-dev").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if pe := (*providererrors.ProviderError)(nil); !errors.As(err, &pe) || pe.Message != "bad thing" {
		t.Errorf("error body: err = %v, want message bad thing", err)
	}
}
