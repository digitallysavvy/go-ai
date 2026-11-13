package replicate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS cancelReplicatePrediction posts the string body ” with postToApi,
// which sets no Content-Type: fetch sends text/plain;charset=UTF-8. The
// status poll (getFromApi) carries the same user agent as the create
// request: no download() ai-sdk tag.
func TestR99provcCancelContentTypeAndPollUserAgent(t *testing.T) {
	srv, rec := r88provcServer(t, "p99", nil, nil)
	if err := r88provcGenerate(context.Background(), srv.URL, 1); err == nil {
		t.Fatal("want timeout error")
	}
	cancels := r88provcWaitCancels(t, rec, 1)
	if len(cancels) != 1 || cancels[0].headers.Get("Content-Type") != "text/plain;charset=UTF-8" {
		t.Fatalf("cancels = %+v", cancels)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var postUA, getUA string
	for _, q := range rec.requests {
		switch {
		case q.method == http.MethodGet:
			getUA = q.headers.Get("User-Agent")
		case q.path == "/models/black-forest-labs/flux-schnell/predictions":
			postUA = q.headers.Get("User-Agent")
		}
	}
	if postUA == "" || getUA != postUA {
		t.Fatalf("poll User-Agent %q, create %q", getUA, postUA)
	}
}

// createBinaryResponseHandler: a 204 output download has a null body.
func TestR99provcImageDownloadNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "black-forest-labs/flux-schnell")
	_, _, err := m.downloadImage(context.Background(), srv.URL+"/o.png")
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 204 {
		t.Fatalf("err = %#v", err)
	}
}
