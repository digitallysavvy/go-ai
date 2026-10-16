package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-image.ts keeps only `images` for non-gateway provider entries.
func TestGenerateImage_NonGatewayProviderMetadataKeepsOnlyImages(t *testing.T) {
	t.Parallel()
	model := &testutil.MockImageModel{
		MaxImages: 1,
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			return &types.ImageResult{
				Images: [][]byte{pngBytes},
				ProviderMetadata: map[string]interface{}{
					"mock": map[string]interface{}{
						"images": []interface{}{map[string]interface{}{"a": 1}},
						"extra":  "dropped",
					},
				},
			}, nil
		},
	}
	res, err := GenerateImage(context.Background(), GenerateImageOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	entry := res.ProviderMetadata["mock"].(map[string]interface{})
	if _, ok := entry["extra"]; ok {
		t.Fatalf("extra key kept: %v", entry)
	}
	if _, ok := entry["images"]; !ok {
		t.Fatalf("images missing: %v", entry)
	}
}

// TS step.reasoning keeps empty reasoning parts.
func TestReasoningPartsOfKeepsEmptyParts(t *testing.T) {
	t.Parallel()
	got := reasoningPartsOf([]types.ContentPart{types.ReasoningContent{Text: ""}, types.ReasoningContent{Text: "x"}})
	if len(got) != 2 {
		t.Fatalf("got %d parts", len(got))
	}
}
