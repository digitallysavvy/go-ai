package together

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS loadApiKey throws LoadAPIKeyError before the fetch; the rerank handler
// wrapped it in a status-0 ProviderError.
func TestR92provbRerankMissingAPIKey(t *testing.T) {
	t.Setenv("TOGETHER_API_KEY", "")
	t.Setenv("TOGETHER_AI_API_KEY", "")
	m, _ := New(Config{BaseURL: "http://127.0.0.1:1"}).RerankingModel("Salesforce/Llama-Rank-v1")
	_, err := m.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("err = %T %v, want *LoadAPIKeyError", err, err)
	}
}
