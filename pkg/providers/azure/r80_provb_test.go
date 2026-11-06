package azure

import (
	"context"
	stdhttp "net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS azure-mai-image-model usage counts are z.number(): a fractional count
// is accepted; Go decoded *int with encoding/json and failed the call. Go
// truncates it (known difference "Fractional counts").
func TestR80provbAzureMaiImageFractionalUsage(t *testing.T) {
	server, _ := newMaiServer(t, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1791229019,"usage":{"num_output_tokens":576.5,"num_input_text_tokens":10,"num_input_image_tokens":0},"data":[{"b64_json":"iVBORw0KGgo="}]}`))
	})
	result, err := maiImageModel(t, server, "MAI-Image-2.6").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.OutputTokens != 576 || result.Usage.InputTokens != 10 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}
