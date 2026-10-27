package google

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS validates every batch result line with the GenerateContent
// responseSchema; a body that fails it is an invalid_response item even when
// a lax decode would accept it (a grounding chunk without its required uri).
func TestR46_BatchResultLineFailsResponseSchema(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	bad := json.RawMessage(`{"candidates":[{"content":{"parts":[{"text":"x"}]},"groundingMetadata":{"groundingChunks":[{"web":{"title":"t"}}]}}]}`)
	item := b.convertGoogleBatchResultLine("k1", bad, nil)
	if item.Status != provider.BatchItemFailed || item.Error == nil || item.Error.Code != "invalid_response" {
		t.Fatalf("item = %+v", item)
	}
	ok := json.RawMessage(`{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
	if got := b.convertGoogleBatchResultLine("k2", ok, nil); got.Status != provider.BatchItemSucceeded {
		t.Fatalf("valid item = %+v", got)
	}
}
