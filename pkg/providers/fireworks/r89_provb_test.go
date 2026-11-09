package fireworks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS fireworks-image-model.ts doGenerateAsync: the result's response
// headers are the image download's headers (getFromApi responseHeaders).
func TestR89provbAsyncImageResponseHeaders(t *testing.T) {
	const modelID = "accounts/fireworks/models/flux-kontext-pro"
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workflows/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/workflows/" + modelID:
			_, _ = fmt.Fprint(w, `{"request_id":"r1"}`)
		case "/v1/workflows/" + modelID + "/get_result":
			_, _ = fmt.Fprintf(w, `{"id":"r1","status":"Ready","result":{"sample":%q}}`, srv.URL+"/img")
		}
	})
	mux.HandleFunc("/img", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Download", "yes")
		_, _ = w.Write([]byte{1, 2, 3})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	res, err := NewImageModel(providerForAsyncServer(srv.URL), modelID).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Response == nil || res.Response.Headers["X-Download"] != "yes" || res.Response.ModelID != modelID {
		t.Fatalf("response = %+v, want download headers and model ID", res.Response)
	}
}
