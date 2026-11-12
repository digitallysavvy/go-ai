package google

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS mapGoogleBatchStatus strips one BATCH_STATE_ or JOB_STATE_ prefix
// (/^(?:BATCH|JOB)_STATE_/), never both.
func TestR93provgBatchStatusSinglePrefix(t *testing.T) {
	for raw, want := range map[string]provider.BatchStatusValue{
		"BATCH_STATE_SUCCEEDED":           provider.BatchStatusCompleted,
		"JOB_STATE_FAILED":                provider.BatchStatusFailed,
		"BATCH_STATE_JOB_STATE_SUCCEEDED": provider.BatchStatusPending,
	} {
		if got := mapGoogleBatchStatus(raw, true, true, false); got != want {
			t.Fatalf("%s: got %s, want %s", raw, got, want)
		}
	}
}

// TS convertGoogleImageBatchResult takes every inline image file part, an
// empty payload included, so the item is an image result rather than a
// failed text item with an unsupported "file" block.
func TestR93provgBatchEmptyInlineImageIsImageResult(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	resp := json.RawMessage(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":""}}]},"finishReason":"STOP"}]}`)
	item := b.convertGoogleBatchResultLine("k", resp, nil)
	if item.Status != provider.BatchItemSucceeded || item.Type != provider.BatchRequestTypeImage || item.ImageResult == nil || len(item.ImageResult.Base64Images) != 1 || item.ImageResult.Base64Images[0] != "" {
		t.Fatalf("item = %+v", item)
	}
}
