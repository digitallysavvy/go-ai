package voyage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r90toolsVoyageServer(t *testing.T, reply string) (*Provider, *string) {
	t.Helper()
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL}), &body
}

// TS voyage-embedding-model.ts 97-104 and voyage-reranking-model.ts 79-86:
// postJsonToApi stringifies the body literal in its key order; Go
// marshalled maps with sorted keys.
func TestR90toolsVoyageBodyKeyOrder(t *testing.T) {
	p, body := r90toolsVoyageServer(t, `{"object":"list","data":[{"object":"embedding","embedding":[1],"index":0}],"model":"voyage-3","usage":{"total_tokens":1}}`)
	model := NewEmbeddingModel(p, "voyage-3")
	if _, err := model.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"voyage": map[string]interface{}{"outputDtype": "float", "inputType": "query", "truncation": true}},
	}); err != nil {
		t.Fatal(err)
	}
	if want := `{"input":["a"],"model":"voyage-3","input_type":"query","truncation":true,"output_dtype":"float"}`; *body != want {
		t.Errorf("embed body\n got %s\nwant %s", *body, want)
	}

	p, body = r90toolsVoyageServer(t, `{"data":[{"index":0,"relevance_score":0.5}]}`)
	topN := 1
	if _, err := NewRerankingModel(p, "rerank-2").DoRerank(context.Background(), &provider.RerankOptions{
		Query: "q", Documents: []string{"d"}, TopN: &topN,
		ProviderOptions: map[string]interface{}{"voyage": map[string]interface{}{"truncation": false, "returnDocuments": true}},
	}); err != nil {
		t.Fatal(err)
	}
	if want := `{"query":"q","documents":["d"],"model":"rerank-2","top_k":1,"return_documents":true,"truncation":false}`; *body != want {
		t.Errorf("rerank body\n got %s\nwant %s", *body, want)
	}
}
