package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR89provaNullAmazonBedrockKeyFallsBack: TS reads
// `parseProviderOptions('amazonBedrock') ?? parseProviderOptions('bedrock')`,
// and parseProviderOptions returns undefined for a null key, so
// `{amazonBedrock: null, bedrock: {...}}` uses the legacy key (TS
// amazon-bedrock-embedding-model.ts / amazon-bedrock-image-model.ts doEmbed /
// doGenerate). Validation already took the same fallback.
func TestR89provaNullAmazonBedrockKeyFallsBack(t *testing.T) {
	providerOptions := map[string]interface{}{
		"amazonBedrock": nil,
		"bedrock":       map[string]interface{}{"dimensions": 512.0, "negativeText": "blurry"},
	}

	if err := validateEmbeddingProviderOptions(map[string]interface{}{
		"amazonBedrock": nil, "bedrock": map[string]interface{}{"dimensions": 512.0},
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	emb := bedrockEmbeddingOptions(providerOptions)
	if emb == nil || emb.TitanOptions == nil || emb.TitanOptions.Dimensions == nil || *emb.TitanOptions.Dimensions != 512 {
		t.Fatalf("embedding options = %#v", emb)
	}

	body, _, err := NewImageModel(nil, "amazon.nova-canvas-v1:0").buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:          "a cat",
		ProviderOptions: map[string]interface{}{"amazonBedrock": nil, "bedrock": map[string]interface{}{"negativeText": "blurry"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := body["textToImageParams"].(map[string]interface{})
	if params["negativeText"] != "blurry" {
		t.Fatalf("textToImageParams = %#v", params)
	}
}
