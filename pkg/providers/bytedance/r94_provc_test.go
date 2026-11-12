package bytedance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS postJsonToApi checks response.ok: an unfollowed 3xx fails with the
// ByteDance API error instead of parsing the body as a success.
func TestR94provcNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"id":"t1","data":[{"b64_json":"aGk="}]}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	vm, _ := p.VideoModel(string(ModelSeedance10Pro))
	if _, err := vm.(provider.VideoModelStarter).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1}}); err == nil {
		t.Error("video start: want the 300 API error")
	}
	im, _ := p.ImageModel("seedream-4-0-250828")
	if res, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err == nil {
		t.Errorf("image: result = %+v, want the 300 API error", res)
	}
}
