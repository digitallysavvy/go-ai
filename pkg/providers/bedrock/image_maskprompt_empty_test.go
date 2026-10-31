package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `maskPrompt != null` treats an empty string as present: with files the
// task becomes INPAINTING and maskPrompt "" is forwarded.
func TestBedrockImageEmptyMaskPromptStillInpaints(t *testing.T) {
	m := NewImageModel(New(Config{Region: "us-east-1"}), "amazon.nova-canvas-v1:0")
	body, _, err := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:          "x",
		Files:           []provider.ImageFile{{Type: "file", Data: []byte("img")}},
		ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"maskPrompt": ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["taskType"] != "INPAINTING" {
		t.Fatalf("taskType = %v", body["taskType"])
	}
	params := body["inPaintingParams"].(map[string]interface{})
	if v, ok := params["maskPrompt"]; !ok || v != "" {
		t.Fatalf("params = %v", params)
	}
}
