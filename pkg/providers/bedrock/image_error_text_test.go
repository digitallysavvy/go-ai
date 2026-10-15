package bedrock

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Error texts mirror the TS image model verbatim.
func TestImageErrorTextsMatchTS(t *testing.T) {
	_, err := bedrockImageFileBase64(provider.ImageFile{Type: "url", URL: "https://x/y.png"})
	if err == nil || err.Error() != "URL-based images are not supported for Amazon Bedrock image editing. Please provide the image data directly." {
		t.Fatalf("err = %v", err)
	}
	m := NewImageModel(nil, "amazon.nova-canvas-v1:0")
	_, _, err = m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:          "x",
		Files:           []provider.ImageFile{{Data: []byte{1, 2, 3}}},
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"taskType": "TEXT_IMAGE"}},
	})
	if err == nil || !strings.HasPrefix(err.Error(), "Unsupported task type: TEXT_IMAGE") {
		t.Fatalf("err = %v", err)
	}
}
