package fireworks

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

// TS parseProviderOptions on fireworks chat (openai-compatible), embedding and
// image options.
func TestProviderOptionsValidation(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	ctx := context.Background()

	chat, _ := p.LanguageModel("m")
	_, err := chat.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"fireworks": map[string]interface{}{"reasoningEffort": 1}}})
	wantInvalidArgument(t, "chat", err)
	_, err = chat.DoStream(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openaiCompatible": map[string]interface{}{"user": 1}}})
	wantInvalidArgument(t, "chat stream", err)

	emb, _ := p.EmbeddingModel("m")
	_, err = emb.DoEmbedMany(ctx, []string{"a"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"fireworks": map[string]interface{}{"user": 1}}})
	wantInvalidArgument(t, "embedding", err)

	for _, model := range []string{"accounts/fireworks/models/flux-1-dev-fp8", "accounts/fireworks/models/flux-kontext-pro"} {
		img, _ := p.ImageModel(model)
		for _, o := range []map[string]interface{}{
			{"guidance_scale": "x"}, {"num_inference_steps": 1.5}, {"output_format": "gif"}, {"webhook_url": 1},
			{"prompt_upsampling": "y"}, {"safety_tolerance": 7}, {"cfg_scale": true}, {"steps": 2.5},
		} {
			_, err = img.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "cat", ProviderOptions: map[string]interface{}{"fireworks": o}})
			wantInvalidArgument(t, model+" image", err)
		}
	}
	if err := validateImageOptions(map[string]interface{}{"fireworks": map[string]interface{}{"safety_tolerance": 6, "output_format": "png", "extra": []int{1}}}); err != nil {
		t.Errorf("valid image options rejected: %v", err)
	}
}
