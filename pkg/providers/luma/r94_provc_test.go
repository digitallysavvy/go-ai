package luma

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS postJsonToApi / getFromApi check response.ok: an unfollowed 3xx on the
// submit or the status poll fails with the Luma API error.
func TestR94provcNon2xxFails(t *testing.T) {
	for _, failPoll := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && failPoll {
				_, _ = w.Write([]byte(`{"id":"g1","state":"queued"}`))
				return
			}
			w.WriteHeader(300)
			_, _ = w.Write([]byte(`{"id":"g1","state":"completed","assets":{"image":"https://example.com/i.png"}}`))
		}))
		model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "photon-1")
		res, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		srv.Close()
		if pe := (*providererrors.ProviderError)(nil); !errors.As(err, &pe) || pe.StatusCode != 300 {
			t.Fatalf("failPoll=%v: err = %v (result %+v), want the 300 API error", failPoll, err, res)
		}
	}
}

// TS createJsonResponseHandler reads response.text() (TextDecoder drops a
// UTF-8 BOM): BOM-prefixed submit and status bodies validate, so the
// failed generation is reported instead of "Invalid JSON response".
func TestR94provcResponseBodyIsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"id":"g1","state":"queued"}`))
			return
		}
		_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"id":"g1","state":"failed"}`))
	}))
	defer srv.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "photon-1")
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err == nil || err.Error() != "Image generation failed." {
		t.Fatalf("err = %v, want Image generation failed.", err)
	}
}
