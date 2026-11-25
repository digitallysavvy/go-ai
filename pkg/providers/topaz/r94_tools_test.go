package topaz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS postFormDataToApi / postJsonToApi / getFromApi fail on !response.ok:
// an unfollowed 3xx is the Topaz API error, not a success body.
func TestR94toolsTopazNon2xxIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"process_id":"p","status":"Completed","download_url":"x","requestId":"r"}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	want300 := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
			t.Fatalf("%s: err = %T %v, want the status 300 API error", name, err, err)
		}
	}
	im, _ := p.ImageModel("standard-v2")
	_, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{Type: "file", MediaType: "image/png", Data: []byte{1}}},
	})
	want300("image", err)
	m := &ImageModel{prov: p, modelID: "standard-v2"}
	_, err = m.getStatus(context.Background(), "p", nil)
	want300("image status", err)
	_, _, err = m.download(context.Background(), "p", nil)
	want300("image download", err)

	vm, _ := p.VideoModel("proteus")
	w, h := 64, 64
	_, err = vm.(*VideoModel).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		InputReferences: []provider.VideoModelV3File{{Type: "url", URL: "https://example.com/a.mp4", MediaType: "video/mp4"}},
		ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"output": map[string]interface{}{"width": w, "height": h}}},
	}})
	want300("video start", err)
	_, err = vm.(*VideoModel).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"requestId":"r"}`)})
	want300("video status", err)
}
