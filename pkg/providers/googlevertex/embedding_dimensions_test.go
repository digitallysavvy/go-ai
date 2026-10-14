package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #22338: top-level dimensions maps to outputDimensionality; the provider
// option wins. Covers both the :predict and :embedContent endpoints.
func TestVertexEmbeddingTopLevelDimensions(t *testing.T) {
	two := 256
	for _, model := range []string{"text-embedding-005", "gemini-embedding-2"} {
		for _, tc := range []struct {
			name string
			top  *int
			prov interface{}
			want interface{}
		}{
			{"unset", nil, nil, nil},
			{"top-level", &two, nil, float64(256)},
			{"provider wins", &two, float64(512), float64(512)},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				var body map[string]interface{}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewDecoder(r.Body).Decode(&body)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("{}"))
				}))
				defer srv.Close()
				p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: srv.URL})
				if err != nil {
					t.Fatal(err)
				}
				opts := &provider.EmbedModelOptions{Dimensions: tc.top}
				if tc.prov != nil {
					opts.ProviderOptions = map[string]interface{}{"vertex": map[string]interface{}{"outputDimensionality": tc.prov}}
				}
				_, _ = NewEmbeddingModel(p, model).DoEmbedMany(context.Background(), []string{"a"}, opts)
				var params map[string]interface{}
				if model == "text-embedding-005" {
					params, _ = body["parameters"].(map[string]interface{})
				} else {
					params, _ = body["embedContentConfig"].(map[string]interface{})
				}
				if params["outputDimensionality"] != tc.want {
					t.Fatalf("outputDimensionality = %v, want %v (body %v)", params["outputDimensionality"], tc.want, body)
				}
			})
		}
	}
}
