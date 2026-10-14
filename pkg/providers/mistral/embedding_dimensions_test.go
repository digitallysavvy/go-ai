package mistral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #22338: the top-level embedding dimensions setting maps to the provider
// field output_dimension; an explicit provider option wins.
func TestEmbeddingTopLevelDimensions(t *testing.T) {
	cases := []struct {
		name     string
		top      *int
		provider interface{}
		want     interface{}
	}{
		{"unset", nil, nil, nil},
		{"top-level only", dimPtr(256), nil, float64(256)},
		{"provider option only", nil, float64(512), float64(512)},
		{"provider option wins", dimPtr(256), float64(512), float64(512)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()
			m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "mistral-embed")
			opts := &provider.EmbedModelOptions{Dimensions: tc.top}
			if tc.provider != nil {
				opts.ProviderOptions = map[string]interface{}{"mistral": map[string]interface{}{"outputDimension": tc.provider}}
			}
			_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, opts)
			if body == nil {
				t.Fatal("no request captured")
			}
			got := body["output_dimension"]
			if got != tc.want {
				t.Fatalf("output_dimension = %v, want %v", got, tc.want)
			}
		})
	}
}

func dimPtr(v int) *int { return &v }
