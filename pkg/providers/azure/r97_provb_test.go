package azure

import (
	"context"
	stdhttp "net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97provbMaiImageFractionalTokenMetadata: TS maiImageResponseSchema
// token counts are z.number(), and providerMetadata.azure.images keeps
// textTokens / imageTokens as received; Go truncated them into *int. The
// ImageUsage int fields still truncate the sums.
func TestR97provbMaiImageFractionalTokenMetadata(t *testing.T) {
	server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"usage":{"num_input_text_tokens":10.5,"num_input_image_tokens":2,"num_output_tokens":3.5},"data":[{"b64_json":"iVBORw0KGgo="}]}`))
	})
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	meta := result.ProviderMetadata["azure"].(map[string]interface{})["images"].([]map[string]interface{})
	want := []map[string]interface{}{{"created": 1.0, "textTokens": 10.5, "imageTokens": 2}}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("metadata = %#v, want %#v", meta, want)
	}
	// TS usage: input 12.5, output 3.5, total 16; Go's ints truncate the sums.
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 || result.Usage.TotalTokens != 16 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

// TestR97provbImageOptionsNullInvalid: TS azureImageModelOptions keys are
// z.enum / z.boolean().optional(), not nullish, so a present null throws
// "invalid azure provider options" (Go skipped it like an absent key).
func TestR97provbImageOptionsNullInvalid(t *testing.T) {
	for _, key := range []string{"api", "autoAspectRatio", "webGrounding"} {
		_, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": map[string]interface{}{key: nil}})
		if err == nil {
			t.Errorf("%s: null accepted, want invalid azure provider options", key)
		}
	}
	if _, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": map[string]interface{}{"other": nil}}); err != nil {
		t.Errorf("an undeclared key is stripped (non-strict object): %v", err)
	}
}
