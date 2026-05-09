package voyage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVoyageRerankRequestAndResponse(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.5}]}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewRerankingModel(p, ModelRerank2)
	top := 1
	res, err := model.DoRerank(t.Context(), &provider.RerankOptions{
		Query:     "q",
		Documents: []string{"a", "b"},
		TopN:      &top,
	})
	if err != nil {
		t.Fatalf("DoRerank error: %v", err)
	}
	if body["query"] != "q" || body["model"] != ModelRerank2 {
		t.Fatalf("unexpected body: %#v", body)
	}
	if body["top_k"] != float64(1) {
		t.Fatalf("top_k = %#v", body["top_k"])
	}
	if len(res.Ranking) != 2 || res.Ranking[0].Index != 1 {
		t.Fatalf("unexpected ranking: %#v", res.Ranking)
	}
}
