package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #22338: top-level dimensions maps to the Titan "dimensions", Cohere
// "output_dimension" and Nova "embeddingDimension" fields; provider options win.
func TestBedrockEmbeddingTopLevelDimensions(t *testing.T) {
	d := 256
	type tc struct {
		name, model, optKey string
		top                 *int
		prov                interface{}
		get                 func(map[string]interface{}) interface{}
		want                interface{}
	}
	titan := func(b map[string]interface{}) interface{} { return b["dimensions"] }
	cohereGet := func(b map[string]interface{}) interface{} { return b["output_dimension"] }
	nova := func(b map[string]interface{}) interface{} {
		s, _ := b["singleEmbeddingParams"].(map[string]interface{})
		return s["embeddingDimension"]
	}
	cases := []tc{
		{"titan unset", "amazon.titan-embed-text-v2:0", "dimensions", nil, nil, titan, nil},
		{"titan top", "amazon.titan-embed-text-v2:0", "dimensions", &d, nil, titan, float64(256)},
		{"titan provider wins", "amazon.titan-embed-text-v2:0", "dimensions", &d, float64(512), titan, float64(512)},
		{"cohere unset", "cohere.embed-v4:0", "outputDimension", nil, nil, cohereGet, nil},
		{"cohere top", "cohere.embed-v4:0", "outputDimension", &d, nil, cohereGet, float64(256)},
		{"cohere provider wins", "cohere.embed-v4:0", "outputDimension", &d, float64(1024), cohereGet, float64(1024)},
		{"nova default", "amazon.nova-2-multimodal-embeddings-v1:0", "embeddingDimension", nil, nil, nova, float64(1024)},
		{"nova top", "amazon.nova-2-multimodal-embeddings-v1:0", "embeddingDimension", &d, nil, nova, float64(256)},
		{"nova provider wins", "amazon.nova-2-multimodal-embeddings-v1:0", "embeddingDimension", &d, float64(384), nova, float64(384)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()
			p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b", BaseURL: srv.URL})
			opts := &provider.EmbedModelOptions{Dimensions: c.top}
			if c.prov != nil {
				opts.ProviderOptions = map[string]interface{}{"amazonBedrock": map[string]interface{}{c.optKey: c.prov}}
			}
			_, _ = NewEmbeddingModel(p, c.model).DoEmbed(context.Background(), "hi", opts)
			if body == nil {
				t.Fatal("no request captured")
			}
			if got := c.get(body); got != c.want {
				t.Fatalf("got %v, want %v (body %v)", got, c.want, body)
			}
		})
	}
}
