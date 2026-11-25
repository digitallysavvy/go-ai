package voyage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// OVC-01 probe: Go-typed option literals (map[string]string,
// map[string]bool, int64 numbers) reach the request like their JSON form.
func TestR91toolsVoyageTypedOptionLiterals(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/rerank") {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"relevance_score":0.5}],"usage":{"total_tokens":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1],"index":0}],"model":"m","usage":{"total_tokens":1}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	if _, err := NewEmbeddingModel(p, "voyage-3").DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"voyage": map[string]string{"inputType": "query"}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"input_type":"query"`) {
		t.Errorf("embedding body = %s", body)
	}
	if _, err := NewEmbeddingModel(p, "voyage-3").DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"voyage": map[string]int64{"outputDimension": 256}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"output_dimension":256`) {
		t.Errorf("embedding body = %s", body)
	}
	if _, err := NewRerankingModel(p, "rerank-2").DoRerank(context.Background(), &provider.RerankOptions{
		Query: "q", Documents: []string{"a"},
		ProviderOptions: map[string]interface{}{"voyage": map[string]bool{"returnDocuments": true, "truncation": false}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"return_documents":true`) || !strings.Contains(body, `"truncation":false`) {
		t.Errorf("rerank body = %s", body)
	}
}
