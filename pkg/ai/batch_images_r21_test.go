package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertImageResult: succeeded image results come back as generated
// files with detected media types and per-image provider metadata.
func TestExperimentalGetBatchResults_ImagesAreGeneratedFiles(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	stream := &mockBatchItemStream{items: []provider.BatchV4ItemResult{{
		Type: provider.BatchRequestTypeImage, ID: "img-1", Status: provider.BatchItemSucceeded,
		ImageResult: &types.ImageResult{
			Images:           [][]byte{jpeg, []byte("opaque")},
			ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"images": []interface{}{map[string]interface{}{"a": 1.0}, map[string]interface{}{"b": 2.0}}}},
		},
	}}}
	mock := &mockBatchV4{providerName: "mock.batch", resultsFn: func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
		return stream, nil
	}}
	rs, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: mock, Batch: BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := rs.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Images) != 2 {
		t.Fatalf("Images = %d, want 2", len(item.Images))
	}
	if item.Images[0].MediaType != "image/jpeg" || item.Images[1].MediaType != "image/png" {
		t.Fatalf("media types = %q, %q", item.Images[0].MediaType, item.Images[1].MediaType)
	}
	if item.Images[1].ProviderMetadata == nil {
		t.Fatalf("per-image provider metadata missing: %+v", item.Images[1])
	}
}
