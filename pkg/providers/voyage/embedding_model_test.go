package voyage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVoyageEmbeddingRequestAndResponse(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.2,0.3],"index":1},{"embedding":[0.1,0.4],"index":0}],"usage":{"total_tokens":12}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewEmbeddingModel(p, ModelVoyage3)
	res, err := model.DoEmbedMany(t.Context(), []string{"a", "b"}, &provider.EmbedModelOptions{})
	if err != nil {
		t.Fatalf("DoEmbedMany error: %v", err)
	}
	if body["model"] != ModelVoyage3 {
		t.Fatalf("model = %v", body["model"])
	}
	if len(res.Embeddings) != 2 || res.Embeddings[0][0] != 0.1 {
		t.Fatalf("unexpected embeddings ordering: %#v", res.Embeddings)
	}
	if res.Usage.TotalTokens != 12 {
		t.Fatalf("tokens = %d, want 12", res.Usage.TotalTokens)
	}
}

func TestVoyageEmbeddingRejectsTooManyInputs(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://example.invalid"})
	model := NewEmbeddingModel(p, ModelVoyage3)
	inputs := make([]string, model.MaxEmbeddingsPerCall()+1)
	for i := range inputs {
		inputs[i] = "x"
	}

	_, err := model.DoEmbedMany(t.Context(), inputs, nil)
	if err == nil || !strings.Contains(err.Error(), "too many embedding values") {
		t.Fatalf("expected too many values error, got %v", err)
	}
}
