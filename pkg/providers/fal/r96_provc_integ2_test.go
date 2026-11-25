package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcImageOutOfRangeNumberInImage: falImageSchema strips an
// undeclared key of an image object, and JSON.parse reads a number beyond
// the double range there as Infinity, so the image still downloads.
func TestR96provcImageOutOfRangeNumberInImage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fal-ai/flux/dev":
			_, _ = w.Write([]byte(`{"images":[{"url":"` + srv.URL + `/img.png","extra":1e400}],"seed":1}`))
		case "/img.png":
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := NewImageModel(p, "fal-ai/flux/dev").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil || len(res.Images) != 1 {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
}
