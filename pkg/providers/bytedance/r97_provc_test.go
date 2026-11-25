package bytedance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provcImageFractionalUsage: byteDanceImageResponseSchema declares
// usage.output_tokens / total_tokens as z.number().nullish(), so TS accepts
// 10.5 and returns the images; Go failed the call decoding it into an int.
// The count now truncates like the central decoders do.
func TestR97provcImageFractionalUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}],"usage":{"output_tokens":10.5,"total_tokens":20}}`))
	}))
	defer srv.Close()
	prov, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := newImageModel(prov, string(ModelSeedream50)).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if res.Base64Image != "aGk=" || res.Usage.OutputTokens != 10 || res.Usage.TotalTokens != 20 {
		t.Fatalf("res = %+v", res)
	}
}
