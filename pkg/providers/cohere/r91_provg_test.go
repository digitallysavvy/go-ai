package cohere

import (
	"context"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provg2-05: a missing API key is the LoadAPIKeyError itself (TS loadApiKey),
// not a wrapping ProviderError.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("COHERE_API_KEY", "")
	em, _ := New(Config{}).EmbeddingModel("embed-v4.0")
	_, err := em.DoEmbed(context.Background(), "hi", nil)
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}
