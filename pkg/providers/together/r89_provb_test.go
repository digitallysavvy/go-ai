package together

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS togetherai-reranking-model.ts sends rank_fields: rerankingOptions?.rankFields,
// so an explicit empty list is sent as [].
func TestR89provbRerankEmptyRankFieldsSent(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).RerankingModel("Salesforce/Llama-Rank-v1")
	if _, err := m.DoRerank(context.Background(), &provider.RerankOptions{Documents: []string{"a"}, Query: "q",
		ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"rankFields": []interface{}{}}}}); err != nil {
		t.Fatal(err)
	}
	if rf, ok := body["rank_fields"].([]interface{}); !ok || len(rf) != 0 {
		t.Fatalf("rank_fields = %#v, want []", body["rank_fields"])
	}
}
