package together

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func wantInvalidArgument(t *testing.T, what string, err error) {
	t.Helper()
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) {
		t.Errorf("%s: error = %v, want InvalidArgumentError", what, err)
	}
}

// TS parseProviderOptions on togetherai chat (openai-compatible), embedding,
// image and reranking options: a mistyped value fails before any request.
func TestProviderOptionsValidation(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	ctx := context.Background()

	chat, _ := p.LanguageModel("m")
	for _, key := range []string{"togetherai", "openaiCompatible", "openai-compatible"} {
		_, err := chat.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{key: map[string]interface{}{"user": 1}}})
		wantInvalidArgument(t, "chat "+key, err)
	}
	_, err := chat.DoStream(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"strictJsonSchema": "x"}}})
	wantInvalidArgument(t, "chat stream", err)

	emb, _ := p.EmbeddingModel("m")
	_, err = emb.DoEmbedMany(ctx, []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"dimensions": "x"}}})
	wantInvalidArgument(t, "embedding", err)

	img, _ := p.ImageModel("black-forest-labs/FLUX.1-schnell")
	for _, o := range []map[string]interface{}{{"steps": "x"}, {"guidance": true}, {"negative_prompt": 1}, {"disable_safety_checker": "no"}} {
		_, err = img.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "cat", ProviderOptions: map[string]interface{}{"togetherai": o}})
		wantInvalidArgument(t, "image", err)
	}

	rr, _ := p.RerankingModel("m")
	_, err = rr.DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"},
		ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"rankFields": "title"}}})
	wantInvalidArgument(t, "rerank", err)

	if err := validateImageOptions(map[string]interface{}{"togetherai": map[string]interface{}{"steps": nil, "extra": 1}}); err != nil {
		t.Errorf("nullish/loose image options rejected: %v", err)
	}
}
