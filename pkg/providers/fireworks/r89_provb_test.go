package fireworks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TS createJsonErrorResponseHandler (fireworksErrorSchema): the APICallError
// keeps the response headers, body and data, for a matching body and for
// the status-text fallback alike, so retry-after drives the retry delay.
func TestR89provbAPIErrorKeepsResponse(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"slow down","code":"rate_limit"}}`: "slow down",
		`{"error":"slow down"}`:                                 "slow down",
		`<html>busy</html>`:                                     "Too Many Requests",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After-Ms", "10")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(body))
		}))
		lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("accounts/fireworks/models/llama-v3p1-8b-instruct")
		_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != want || pe.ResponseHeaders["Retry-After-Ms"] != "10" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}
