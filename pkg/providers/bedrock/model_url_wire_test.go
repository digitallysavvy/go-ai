package bedrock

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getUrl: `${baseUrl}/model/${encodeURIComponent(modelId)}/...` for the
// language, image and embedding models alike -- ':' is sent as %3A and any
// path prefix on a custom base URL is kept.
func TestBedrockModelWireURLs(t *testing.T) {
	var mu sync.Mutex
	var uris []string
	p := New(Config{Region: "us-east-1", BaseURL: "https://proxy.test/prefix/", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://proxy.test/prefix",
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			uris = append(uris, req.URL.RequestURI())
			mu.Unlock()
			return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})},
	})

	img, err := p.ImageModel("amazon.nova-canvas-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = img.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	emb, err := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = emb.DoEmbed(context.Background(), "hi", &provider.EmbedModelOptions{})
	lm, err := p.LanguageModel("anthropic.claude-v2:1")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})

	want := []string{
		"/prefix/model/amazon.nova-canvas-v1%3A0/invoke",
		"/prefix/model/amazon.titan-embed-text-v2%3A0/invoke",
		"/prefix/model/anthropic.claude-v2%3A1/converse",
	}
	mu.Lock()
	defer mu.Unlock()
	if len(uris) != len(want) {
		t.Fatalf("uris = %v", uris)
	}
	for i := range want {
		if uris[i] != want[i] {
			t.Errorf("uri[%d] = %q, want %q", i, uris[i], want[i])
		}
	}
}
