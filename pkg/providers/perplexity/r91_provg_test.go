package perplexity

import (
	"context"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg2-05: a missing API key is the LoadAPIKeyError itself (TS loadApiKey),
// not a wrapping ProviderError.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "")
	em, _ := New(Config{}).EmbeddingModel("pplx-embed-v1-0.6b")
	_, err := em.DoEmbed(context.Background(), "hi", nil)
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}

// provg2-11: replayed tool-call arguments keep the input's key order (TS
// JSON.stringify(part.input ?? {}), convert-to-perplexity-input.ts:143).
func TestR91provgToolCallArgumentsKeyOrder(t *testing.T) {
	part := types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: `{"b":1,"a":{"z":1,"y":2}}`, Arguments: map[string]interface{}{"b": 1, "a": map[string]interface{}{"z": 1, "y": 2}}}
	if got := perplexityToolCallArguments(part); got != `{"b":1,"a":{"z":1,"y":2}}` {
		t.Fatalf("arguments = %s", got)
	}
	if got := perplexityToolCallArguments(types.ToolCallContent{Input: `null`}); got != `{}` {
		t.Fatalf("null input = %s", got)
	}
}
