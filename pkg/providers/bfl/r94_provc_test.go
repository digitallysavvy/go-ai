package bfl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
