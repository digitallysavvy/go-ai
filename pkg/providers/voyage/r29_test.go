package voyage

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseProviderOptions with voyageRerankingModelOptionsSchema rejects
// non-boolean option values.
func TestR29_RerankValidatesProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	_, err := NewRerankingModel(p, ModelRerank2).DoRerank(t.Context(), &provider.RerankOptions{
		Query: "q", Documents: []string{"a"},
		ProviderOptions: map[string]interface{}{"voyage": map[string]interface{}{"truncation": "yes"}},
	})
	var inv *providererrors.InvalidArgumentError
	if !errors.As(err, &inv) {
		t.Fatalf("want InvalidArgumentError, got %T %v", err, err)
	}
}

// TS throws LoadAPIKeyError unwrapped.
func TestR29_MissingKeyIsUnwrapped(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	p := New(Config{BaseURL: "http://127.0.0.1:1"})
	_, err := NewRerankingModel(p, ModelRerank2).DoRerank(t.Context(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	var pe *providererrors.ProviderError
	if errors.As(err, &pe) || !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("want bare LoadAPIKeyError, got %T %v", err, err)
	}
}
