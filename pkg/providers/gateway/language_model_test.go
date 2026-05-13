package gateway

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func newTestGatewayLanguageModel(modelID string) *LanguageModel {
	return NewLanguageModel(&Provider{config: Config{}}, modelID)
}

func TestGatewayLanguageModelMetadataAndCapabilities(t *testing.T) {
	model := newTestGatewayLanguageModel("openai/gpt-5")

	if model.SpecificationVersion() != "v3" {
		t.Fatalf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "gateway" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "openai/gpt-5" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}
	if !model.SupportsTools() || !model.SupportsStructuredOutput() || !model.SupportsImageInput() {
		t.Fatal("gateway language model should report all core capabilities as supported")
	}
}

func TestGatewayLanguageModelGetModelConfigHeaders(t *testing.T) {
	model := newTestGatewayLanguageModel("anthropic/claude-3.7")
	headers := model.getModelConfigHeaders(true)
	if headers["ai-language-model-specification-version"] != "3" {
		t.Fatalf("spec version header = %q", headers["ai-language-model-specification-version"])
	}
	if headers["ai-language-model-id"] != "anthropic/claude-3.7" {
		t.Fatalf("model ID header = %q", headers["ai-language-model-id"])
	}
	if headers["ai-language-model-streaming"] != "true" {
		t.Fatalf("streaming header = %q", headers["ai-language-model-streaming"])
	}
}

func TestGatewayLanguageModelConvertContentPart(t *testing.T) {
	model := newTestGatewayLanguageModel("x")

	text, err := model.convertContentPart(types.TextContent{Text: "hello"})
	if err != nil {
		t.Fatalf("text conversion error = %v", err)
	}
	if text["type"] != "text" || text["text"] != "hello" {
		t.Fatalf("text part = %#v", text)
	}

	imageURL, err := model.convertContentPart(types.ImageContent{URL: "https://example.com/image.png"})
	if err != nil {
		t.Fatalf("image URL conversion error = %v", err)
	}
	if imageURL["type"] != "image" || imageURL["image"] != "https://example.com/image.png" {
		t.Fatalf("image URL part = %#v", imageURL)
	}

	imageBytes := []byte("img-bytes")
	imageData, err := model.convertContentPart(types.ImageContent{Image: imageBytes, MimeType: "image/png"})
	if err != nil {
		t.Fatalf("image bytes conversion error = %v", err)
	}
	encodedImage := imageData["image"].(string)
	if !strings.HasPrefix(encodedImage, "data:image/png;base64,") {
		t.Fatalf("image data prefix = %q", encodedImage)
	}
	if !strings.Contains(encodedImage, base64.StdEncoding.EncodeToString(imageBytes)) {
		t.Fatalf("image data missing encoded payload: %q", encodedImage)
	}

	fileBytes := []byte("file-bytes")
	filePart, err := model.convertContentPart(types.FileContent{Data: fileBytes, MimeType: "application/pdf"})
	if err != nil {
		t.Fatalf("file conversion error = %v", err)
	}
	encodedFile := filePart["data"].(string)
	if !strings.HasPrefix(encodedFile, "data:application/pdf;base64,") {
		t.Fatalf("file data prefix = %q", encodedFile)
	}
	if filePart["mimeType"] != "application/pdf" {
		t.Fatalf("file mimeType = %#v", filePart["mimeType"])
	}

	_, err = model.convertContentPart(types.CustomContent{Kind: "xai-citation"})
	if err == nil || !strings.Contains(err.Error(), "unsupported content part type") {
		t.Fatalf("unsupported part error = %v", err)
	}
}

func TestGatewayLanguageModelProviderOptionsMerge(t *testing.T) {
	model := &LanguageModel{
		provider: &Provider{
			config: Config{
				DisallowPromptTraining: true,
				HIPAACompliant:         true,
				QuotaEntityID:          "quota-123",
			},
		},
		modelID: "openai/gpt-5",
	}

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"gateway": map[string]interface{}{
				"only": []interface{}{"openai"},
			},
			"openai": map[string]interface{}{
				"parallelToolCalls": true,
			},
		},
	}

	got := model.providerOptions(opts)
	gatewayOpts, ok := got["gateway"].(map[string]interface{})
	if !ok {
		t.Fatalf("gateway options type = %T", got["gateway"])
	}
	if gatewayOpts["disallowPromptTraining"] != true || gatewayOpts["hipaaCompliant"] != true || gatewayOpts["quotaEntityId"] != "quota-123" {
		t.Fatalf("missing config-derived gateway options: %#v", gatewayOpts)
	}
	if _, ok := gatewayOpts["only"]; !ok {
		t.Fatalf("caller gateway options should be merged: %#v", gatewayOpts)
	}
	if _, ok := got["openai"]; !ok {
		t.Fatalf("non-gateway provider options should be preserved: %#v", got)
	}
}
